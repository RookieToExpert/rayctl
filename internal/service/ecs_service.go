package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"rayctl/internal/platform"
)

var (
	kubevirtVMGVR = schema.GroupVersionResource{
		Group:    "kubevirt.io",
		Version:  "v1",
		Resource: "virtualmachines",
	}
	kubevirtVMIGVR = schema.GroupVersionResource{
		Group:    "kubevirt.io",
		Version:  "v1",
		Resource: "virtualmachineinstances",
	}
)

type ECSService struct {
	dynamicClient dynamic.Interface
	vcClient      ECSPlatform
}

type ECSCheckResult struct {
	Items []ECSCheckItem
}

type ECSCheckQueryResult struct {
	Identifier string
	Result     *ECSCheckResult
	Err        error
}

type ECSCheckItem struct {
	ResourceType string
	Name         string
	UID          string
	VMName       string
	Namespace    string
	Node         string
	Creator      string
	InternalIP   string
	State        string
	MachineType  string
	Type         string
	ImageName    string
	VPC          string
}

type nodeOwnershipHint struct {
	Tenant      string
	ClusterName string
}

type vmResourceContext struct {
	VMName       string
	Namespace    string
	Node         string
	InternalIP   string
	RawText      string
	ResourceUID  string
	ResourceName string
}

type ecsCheckSnapshot struct {
	service     *ECSService
	vmis        []unstructured.Unstructured
	vmiContexts []vmResourceContext
	vmiOnce     sync.Once
	vmiErr      error

	vmOnce     sync.Once
	vmContexts []vmResourceContext
	vmErr      error

	fallbackOnce sync.Once
	fallbackECS  []platform.ECSVirtualMachine
	fallbackAIS  []platform.AISpace
	fallbackErr  error
}

func NewECSService(dynamicClient dynamic.Interface, vcClient *platform.VirtualClusterClient) *ECSService {
	if vcClient == nil {
		return NewECSServiceWithPlatform(dynamicClient, nil)
	}
	return NewECSServiceWithPlatform(dynamicClient, vcClient)
}

func NewECSServiceWithPlatform(dynamicClient dynamic.Interface, vcClient ECSPlatform) *ECSService {
	return &ECSService{
		dynamicClient: dynamicClient,
		vcClient:      vcClient,
	}
}

func (s *ECSService) Check(ctx context.Context, keyword string) (*ECSCheckResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("ecs/ais identifier is required")
	}
	snapshot, err := s.newECSCheckSnapshot()
	if err != nil {
		return nil, err
	}
	return s.checkWithSnapshot(ctx, keyword, snapshot)
}

func (s *ECSService) CheckMany(ctx context.Context, keywords []string, maxParallel int) []ECSCheckQueryResult {
	snapshot, snapshotErr := s.newECSCheckSnapshot()
	return boundedMap(ctx, keywords, maxParallel, func(queryCtx context.Context, keyword string) ECSCheckQueryResult {
		result := ECSCheckQueryResult{Identifier: keyword}
		if snapshotErr != nil {
			result.Err = snapshotErr
			return result
		}
		result.Result, result.Err = s.checkWithSnapshot(queryCtx, keyword, snapshot)
		return result
	})
}

func (s *ECSService) newECSCheckSnapshot() (*ecsCheckSnapshot, error) {
	if s.vcClient == nil {
		return nil, fmt.Errorf("ecs/ais 查询需要平台配置")
	}
	return &ecsCheckSnapshot{service: s}, nil
}

func (s *ECSService) checkWithSnapshot(ctx context.Context, keyword string, snapshot *ecsCheckSnapshot) (*ECSCheckResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("ecs/ais identifier is required")
	}
	ecsCall := asyncCall(ctx, func(ctx context.Context) ([]platform.ECSVirtualMachine, error) {
		return s.vcClient.FindCurrentProfileECSVirtualMachines(ctx, keyword)
	})
	aisCall := asyncCall(ctx, func(ctx context.Context) ([]platform.AISpace, error) {
		return s.vcClient.FindCurrentProfileAISpaces(ctx, keyword)
	})
	contexts, err := snapshot.baseVMIContexts(ctx)
	if err != nil {
		return nil, err
	}
	ecsResult := <-ecsCall
	aisResult := <-aisCall

	matchedECS := filterECSResources(ecsResult.Value, keyword)
	matchedAIS := filterAISpaces(aisResult.Value, keyword)
	if len(matchedECS) == 0 && len(matchedAIS) == 0 {
		fallbackECS, fallbackAIS, err := snapshot.fallbackResources(ctx)
		if err != nil {
			return nil, err
		}
		matchedECS = filterECSResources(fallbackECS, keyword)
		matchedAIS = filterAISpaces(fallbackAIS, keyword)
	}

	if len(matchedECS) == 0 && len(matchedAIS) == 0 {
		return nil, fmt.Errorf("ecs/ais %q not found", keyword)
	}

	if !resourcesHaveVMContext(contexts, matchedECS, matchedAIS, keyword) {
		var err error
		contexts, err = snapshot.allVMContexts(ctx)
		if err != nil {
			return nil, err
		}
	}

	creatorIDs := make([]string, 0, len(matchedECS)+len(matchedAIS))
	for _, item := range matchedECS {
		creatorIDs = append(creatorIDs, item.CreatorID)
	}
	for _, item := range matchedAIS {
		creatorIDs = append(creatorIDs, item.CreatorID)
	}
	usernames, err := s.vcClient.ResolveUsernames(ctx, creatorIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve creator usernames: %w", err)
	}

	items := make([]ECSCheckItem, 0, len(matchedECS)+len(matchedAIS))
	for _, resource := range matchedECS {
		matches := matchVMContextsForECS(contexts, resource, keyword)
		if len(matches) == 0 {
			matches = []vmResourceContext{{}}
		}
		for _, match := range matches {
			internalIP := firstNonEmpty(match.InternalIP, ecsPlatformIP(resource), "-")
			items = append(items, ECSCheckItem{
				ResourceType: "ECS",
				Name:         firstNonEmpty(resource.Name, resource.DisplayName, "-"),
				UID:          firstNonEmpty(resource.UID, "-"),
				VMName:       dashIfEmptyECS(match.VMName),
				Namespace:    dashIfEmptyECS(match.Namespace),
				Node:         dashIfEmptyECS(match.Node),
				Creator:      dashIfEmptyECS(firstNonEmpty(usernames[resource.CreatorID], resource.CreatorID)),
				InternalIP:   dashIfEmptyECS(internalIP),
				State:        dashIfEmptyECS(strings.TrimSpace(resource.State)),
				MachineType:  dashIfEmptyECS(strings.TrimSpace(resource.Properties.MachineType)),
				Type:         dashIfEmptyECS(strings.TrimSpace(resource.Properties.VirtualMachineType)),
				ImageName:    dashIfEmptyECS(ecsImageName(resource)),
				VPC:          dashIfEmptyECS(ecsVPC(resource)),
			})
		}
	}

	for _, resource := range matchedAIS {
		matches := matchVMContextsForAIS(contexts, resource)
		if len(matches) == 0 {
			matches = []vmResourceContext{{}}
		}
		for _, match := range matches {
			internalIP := firstNonEmpty(match.InternalIP, strings.TrimSpace(resource.Properties.HostIP), "-")
			items = append(items, ECSCheckItem{
				ResourceType: "AIS",
				Name:         firstNonEmpty(resource.DisplayName, resource.Name, "-"),
				UID:          firstNonEmpty(resource.UID, "-"),
				VMName:       dashIfEmptyECS(match.VMName),
				Namespace:    dashIfEmptyECS(match.Namespace),
				Node:         dashIfEmptyECS(match.Node),
				Creator:      dashIfEmptyECS(firstNonEmpty(usernames[resource.CreatorID], resource.CreatorID)),
				InternalIP:   dashIfEmptyECS(internalIP),
				State:        dashIfEmptyECS(strings.TrimSpace(resource.State)),
				MachineType:  dashIfEmptyECS(strings.TrimSpace(resource.Properties.VirtualMachineProperties.MachineType)),
				Type:         dashIfEmptyECS(strings.TrimSpace(resource.Properties.Type)),
				ImageName:    dashIfEmptyECS(strings.TrimSpace(resource.Properties.ImagePath)),
				VPC:          dashIfEmptyECS(aisVPC(resource)),
			})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].ResourceType == items[j].ResourceType {
			if items[i].Name == items[j].Name {
				return items[i].VMName < items[j].VMName
			}
			return items[i].Name < items[j].Name
		}
		return items[i].ResourceType < items[j].ResourceType
	})

	return &ECSCheckResult{Items: items}, nil
}

func (s *ecsCheckSnapshot) baseVMIContexts(ctx context.Context) ([]vmResourceContext, error) {
	s.vmiOnce.Do(func() {
		vmiList, err := s.service.dynamicClient.Resource(kubevirtVMIGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		if err != nil {
			s.vmiErr = fmt.Errorf("ecs/ais 查询依赖 HC kubeconfig，请切换到 HC kubeconfig 后再执行: %w", err)
			return
		}
		s.vmis = vmiList.Items
		s.vmiContexts = buildVMIResourceContexts(vmiList.Items)
	})
	return s.vmiContexts, s.vmiErr
}

func (s *ecsCheckSnapshot) allVMContexts(ctx context.Context) ([]vmResourceContext, error) {
	s.vmOnce.Do(func() {
		vmList, err := s.service.dynamicClient.Resource(kubevirtVMGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		if err != nil {
			s.vmErr = fmt.Errorf("ecs/ais 查询依赖 HC kubeconfig，请切换到 HC kubeconfig 后再执行: %w", err)
			return
		}
		s.vmContexts = buildVMResourceContexts(vmList.Items, s.vmis)
	})
	return s.vmContexts, s.vmErr
}

func (s *ecsCheckSnapshot) fallbackResources(ctx context.Context) ([]platform.ECSVirtualMachine, []platform.AISpace, error) {
	s.fallbackOnce.Do(func() {
		ecsCall := asyncCall(ctx, s.service.vcClient.ListECSVirtualMachines)
		aisCall := asyncCall(ctx, s.service.vcClient.ListAISpaces)
		ecsResult := <-ecsCall
		aisResult := <-aisCall
		if ecsResult.Err != nil {
			s.fallbackErr = fmt.Errorf("list ecs resources: %w", ecsResult.Err)
			return
		}
		if aisResult.Err != nil {
			s.fallbackErr = fmt.Errorf("list ais resources: %w", aisResult.Err)
			return
		}
		s.fallbackECS = ecsResult.Value
		s.fallbackAIS = aisResult.Value
	})
	return s.fallbackECS, s.fallbackAIS, s.fallbackErr
}

func (s *ECSService) ResolveSingle(ctx context.Context, keyword string) (*ECSCheckItem, error) {
	result, err := s.Check(ctx, keyword)
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Items) == 0 {
		return nil, fmt.Errorf("ecs/ais %q not found", keyword)
	}
	if len(result.Items) > 1 {
		candidates := make([]string, 0, len(result.Items))
		for _, item := range result.Items {
			candidates = append(candidates, fmt.Sprintf("%s:%s (%s/%s)", item.ResourceType, item.Name, item.Namespace, item.VMName))
		}
		sort.Strings(candidates)
		return nil, fmt.Errorf("multiple ecs/ais resources matched %q: %s", keyword, strings.Join(candidates, ", "))
	}
	return &result.Items[0], nil
}

func (s *ECSService) ResolveNodeOwnership(ctx context.Context) (map[string]nodeOwnershipHint, error) {
	if s.vcClient == nil {
		return nil, fmt.Errorf("ecs/ais 查询需要平台配置")
	}

	vmList, err := s.dynamicClient.Resource(kubevirtVMGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list virtualmachines: %w", err)
	}
	vmiList, err := s.dynamicClient.Resource(kubevirtVMIGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list virtualmachineinstances: %w", err)
	}

	contexts := buildVMResourceContexts(vmList.Items, vmiList.Items)
	ecsResources, err := s.vcClient.ListECSVirtualMachines(ctx)
	if err != nil {
		return nil, err
	}
	aisResources, err := s.vcClient.ListAISpaces(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]nodeOwnershipHint)
	setHint := func(nodeName string, hint nodeOwnershipHint) {
		nodeName = strings.TrimSpace(nodeName)
		if nodeName == "" {
			return
		}
		existing, ok := result[nodeName]
		if !ok {
			result[nodeName] = hint
			return
		}
		if strings.TrimSpace(existing.Tenant) == "" && strings.TrimSpace(hint.Tenant) != "" {
			existing.Tenant = hint.Tenant
		}
		if (strings.TrimSpace(existing.ClusterName) == "" || strings.TrimSpace(existing.ClusterName) == "-") && strings.TrimSpace(hint.ClusterName) != "" {
			existing.ClusterName = hint.ClusterName
		}
		result[nodeName] = existing
	}

	for _, resource := range ecsResources {
		matches := matchVMContextsForECS(contexts, resource, firstNonEmpty(strings.TrimSpace(resource.UID), strings.TrimSpace(resource.Name), strings.TrimSpace(resource.DisplayName)))
		hint := nodeOwnershipHint{
			Tenant:      strings.TrimSpace(resource.ProfileName),
			ClusterName: firstNonEmpty(strings.TrimSpace(resource.DisplayName), strings.TrimSpace(resource.Name), "ecs"),
		}
		for _, match := range matches {
			setHint(match.Node, hint)
		}
	}

	for _, resource := range aisResources {
		matches := matchVMContextsForAIS(contexts, resource)
		hint := nodeOwnershipHint{
			Tenant:      strings.TrimSpace(resource.ProfileName),
			ClusterName: firstNonEmpty(strings.TrimSpace(resource.DisplayName), strings.TrimSpace(resource.Name), "ais"),
		}
		for _, match := range matches {
			setHint(match.Node, hint)
		}
	}

	return result, nil
}

func buildVMResourceContexts(vms []unstructured.Unstructured, vmis []unstructured.Unstructured) []vmResourceContext {
	vmiByKey := make(map[string]unstructured.Unstructured, len(vmis))
	for _, vmi := range vmis {
		key := vmi.GetNamespace() + "/" + vmi.GetName()
		vmiByKey[key] = vmi
	}

	result := make([]vmResourceContext, 0, len(vms))
	for _, vm := range vms {
		key := vm.GetNamespace() + "/" + vm.GetName()
		vmi := vmiByKey[key]
		rawBytes, _ := json.Marshal(vm.Object)
		rawText := strings.ToLower(string(rawBytes))
		if vmi.Object != nil {
			vmiRawBytes, _ := json.Marshal(vmi.Object)
			rawText += "\n" + strings.ToLower(string(vmiRawBytes))
		}

		result = append(result, vmResourceContext{
			VMName:       vm.GetName(),
			Namespace:    vm.GetNamespace(),
			Node:         firstNonEmpty(getNestedString(vmi.Object, "status", "nodeName"), getNestedString(vmi.Object, "metadata", "labels", "kubevirt.io/nodeName")),
			InternalIP:   getVMIInterfaceIP(vmi.Object),
			RawText:      rawText,
			ResourceUID:  firstNonEmpty(getNestedString(vm.Object, "metadata", "annotations", "resource.compute.sensecore.cn/uid"), getNestedString(vmi.Object, "metadata", "annotations", "resource.compute.sensecore.cn/uid"), getNestedString(vm.Object, "metadata", "labels", "resourcemanager_id"), getNestedString(vmi.Object, "metadata", "labels", "resourcemanager_id")),
			ResourceName: firstNonEmpty(getNestedString(vm.Object, "metadata", "annotations", "resource.compute.sensecore.cn/name"), getNestedString(vmi.Object, "metadata", "annotations", "resource.compute.sensecore.cn/name")),
		})
	}
	return result
}

func buildVMIResourceContexts(vmis []unstructured.Unstructured) []vmResourceContext {
	result := make([]vmResourceContext, 0, len(vmis))
	for _, vmi := range vmis {
		rawBytes, _ := json.Marshal(vmi.Object)
		result = append(result, vmResourceContext{
			VMName:       vmi.GetName(),
			Namespace:    vmi.GetNamespace(),
			Node:         firstNonEmpty(getNestedString(vmi.Object, "status", "nodeName"), getNestedString(vmi.Object, "metadata", "labels", "kubevirt.io/nodeName")),
			InternalIP:   getVMIInterfaceIP(vmi.Object),
			RawText:      strings.ToLower(string(rawBytes)),
			ResourceUID:  firstNonEmpty(getNestedString(vmi.Object, "metadata", "annotations", "resource.compute.sensecore.cn/uid"), getNestedString(vmi.Object, "metadata", "labels", "resourcemanager_id")),
			ResourceName: getNestedString(vmi.Object, "metadata", "annotations", "resource.compute.sensecore.cn/name"),
		})
	}
	return result
}

func resourcesHaveVMContext(contexts []vmResourceContext, ecsResources []platform.ECSVirtualMachine, aisResources []platform.AISpace, keyword string) bool {
	for _, resource := range ecsResources {
		if len(matchVMContextsForECS(contexts, resource, keyword)) == 0 {
			return false
		}
	}
	for _, resource := range aisResources {
		if len(matchVMContextsForAIS(contexts, resource)) == 0 {
			return false
		}
	}
	return true
}

func getVMIInterfaceIP(obj map[string]any) string {
	raw, found, err := unstructured.NestedSlice(obj, "status", "interfaces")
	if err != nil || !found {
		return ""
	}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ip := strings.TrimSpace(fmt.Sprintf("%v", entry["ipAddress"]))
		if ip != "" {
			return ip
		}
	}
	return ""
}

func filterECSResources(items []platform.ECSVirtualMachine, keyword string) []platform.ECSVirtualMachine {
	lowerKeyword := strings.ToLower(strings.TrimSpace(keyword))
	result := make([]platform.ECSVirtualMachine, 0)
	for _, item := range items {
		if resourceMatches(lowerKeyword, item.Name, item.UID, item.DisplayName, item.ID, item.Properties.Hostname) {
			result = append(result, item)
		}
	}
	return result
}

func filterAISpaces(items []platform.AISpace, keyword string) []platform.AISpace {
	lowerKeyword := strings.ToLower(strings.TrimSpace(keyword))
	result := make([]platform.AISpace, 0)
	for _, item := range items {
		if resourceMatches(lowerKeyword, item.Name, item.UID, item.DisplayName, item.ID) {
			result = append(result, item)
		}
	}
	return result
}

func resourceMatches(keyword string, fields ...string) bool {
	if keyword == "" {
		return false
	}
	for _, field := range fields {
		value := strings.ToLower(strings.TrimSpace(field))
		if value == "" {
			continue
		}
		if value == keyword || (!looksLikeECSUUID(keyword) && strings.Contains(value, keyword)) {
			return true
		}
	}
	return false
}

func matchVMContextsForECS(contexts []vmResourceContext, resource platform.ECSVirtualMachine, keyword string) []vmResourceContext {
	lowerKeyword := strings.ToLower(strings.TrimSpace(keyword))
	result := make([]vmResourceContext, 0)
	for _, item := range contexts {
		switch {
		case item.ResourceUID != "" && item.ResourceUID == strings.TrimSpace(resource.UID):
			result = append(result, item)
		case item.ResourceName != "" && item.ResourceName == strings.TrimSpace(resource.Name):
			result = append(result, item)
		case lowerKeyword != "" && strings.Contains(item.RawText, lowerKeyword):
			result = append(result, item)
		}
	}
	return deduplicateVMContexts(result)
}

func matchVMContextsForAIS(contexts []vmResourceContext, resource platform.AISpace) []vmResourceContext {
	resourceUID := strings.ToLower(strings.TrimSpace(resource.UID))
	nameCandidates := aisContextNameCandidates(resource)
	result := make([]vmResourceContext, 0)
	for _, item := range contexts {
		itemUID := strings.ToLower(strings.TrimSpace(item.ResourceUID))
		if resourceUID != "" && itemUID == resourceUID {
			result = append(result, item)
			continue
		}
		itemName := strings.ToLower(strings.TrimSpace(item.ResourceName))
		if _, ok := nameCandidates[itemName]; ok && itemName != "" {
			result = append(result, item)
			continue
		}

		// Older VMIs may not expose the structured annotations. Retain a
		// conservative fallback that only accepts complete JSON string values.
		rawText := strings.ToLower(item.RawText)
		if resourceUID != "" && strings.Contains(rawText, `"`+resourceUID+`"`) {
			result = append(result, item)
			continue
		}
		for candidate := range nameCandidates {
			if candidate != "" && strings.Contains(rawText, `"`+candidate+`"`) {
				result = append(result, item)
				break
			}
		}
	}
	return deduplicateVMContexts(result)
}

func aisContextNameCandidates(resource platform.AISpace) map[string]struct{} {
	result := make(map[string]struct{})
	for _, raw := range []string{resource.Name, resource.DisplayName} {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		result[name] = struct{}{}
		result["ais-"+name] = struct{}{}
	}
	return result
}

func looksLikeECSUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, char := range value {
		switch index {
		case 8, 13, 18, 23:
			if char != '-' {
				return false
			}
		default:
			if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
				return false
			}
		}
	}
	return true
}

func deduplicateVMContexts(items []vmResourceContext) []vmResourceContext {
	if len(items) <= 1 {
		return items
	}
	result := make([]vmResourceContext, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		key := item.Namespace + "/" + item.VMName
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result
}

func ecsPlatformIP(resource platform.ECSVirtualMachine) string {
	for _, nic := range resource.Properties.NetworkInterfaces {
		if ip := strings.TrimSpace(nic.Properties.IPv4Addr); ip != "" {
			return ip
		}
	}
	return ""
}

func ecsImageName(resource platform.ECSVirtualMachine) string {
	for _, item := range resource.Properties.Metadata.Items {
		if strings.EqualFold(strings.TrimSpace(item.Key), "os-name") && strings.TrimSpace(item.Value) != "" {
			return strings.TrimSpace(item.Value)
		}
	}
	return firstNonEmpty(strings.TrimSpace(resource.Properties.ImageID), strings.TrimSpace(resource.Properties.Hostname), "-")
}

func ecsVPC(resource platform.ECSVirtualMachine) string {
	for _, nic := range resource.Properties.NetworkInterfaces {
		value := firstNonEmpty(
			strings.TrimSpace(nic.Properties.VPCInfo.DisplayName),
			strings.TrimSpace(nic.Properties.VPCInfo.Name),
			strings.TrimSpace(nic.Properties.VPCInfo.UID),
		)
		if value != "" {
			return value
		}
	}
	return "-"
}

func aisVPC(resource platform.AISpace) string {
	for _, nic := range resource.Properties.VirtualMachineProperties.NetworkInterfaces {
		value := firstNonEmpty(
			strings.TrimSpace(nic.Properties.VPCInfo.DisplayName),
			strings.TrimSpace(nic.Properties.VPCInfo.Name),
			strings.TrimSpace(nic.Properties.VPCInfo.UID),
		)
		if value != "" {
			return value
		}
	}
	return "-"
}

func dashIfEmptyECS(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
