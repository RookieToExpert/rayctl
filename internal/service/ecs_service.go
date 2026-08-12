package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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
	if s.vcClient == nil {
		return nil, fmt.Errorf("ecs/ais 查询需要平台配置")
	}

	vmiCall := asyncCall(ctx, func(ctx context.Context) (*unstructured.UnstructuredList, error) {
		return s.dynamicClient.Resource(kubevirtVMIGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	})
	ecsCall := asyncCall(ctx, func(ctx context.Context) ([]platform.ECSVirtualMachine, error) {
		return s.vcClient.FindCurrentProfileECSVirtualMachines(ctx, keyword)
	})
	aisCall := asyncCall(ctx, func(ctx context.Context) ([]platform.AISpace, error) {
		return s.vcClient.FindCurrentProfileAISpaces(ctx, keyword)
	})
	vmiResult := <-vmiCall
	ecsResult := <-ecsCall
	aisResult := <-aisCall

	if vmiResult.Err != nil {
		return nil, fmt.Errorf("ecs/ais 查询依赖 HC kubeconfig，请切换到 HC kubeconfig 后再执行: %w", vmiResult.Err)
	}

	matchedECS := filterECSResources(ecsResult.Value, keyword)
	matchedAIS := filterAISpaces(aisResult.Value, keyword)
	contexts := buildVMIResourceContexts(vmiResult.Value.Items)
	if len(matchedECS) == 0 && len(matchedAIS) == 0 {
		ecsFallbackCall := asyncCall(ctx, s.vcClient.ListECSVirtualMachines)
		aisFallbackCall := asyncCall(ctx, s.vcClient.ListAISpaces)
		ecsFallback := <-ecsFallbackCall
		aisFallback := <-aisFallbackCall
		if ecsFallback.Err != nil {
			return nil, fmt.Errorf("list ecs resources: %w", ecsFallback.Err)
		}
		if aisFallback.Err != nil {
			return nil, fmt.Errorf("list ais resources: %w", aisFallback.Err)
		}
		matchedECS = filterECSResources(ecsFallback.Value, keyword)
		matchedAIS = filterAISpaces(aisFallback.Value, keyword)
	}

	if len(matchedECS) == 0 && len(matchedAIS) == 0 {
		return nil, fmt.Errorf("ecs/ais %q not found", keyword)
	}

	if !resourcesHaveVMContext(contexts, matchedECS, matchedAIS, keyword) {
		vmList, err := s.dynamicClient.Resource(kubevirtVMGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("ecs/ais 查询依赖 HC kubeconfig，请切换到 HC kubeconfig 后再执行: %w", err)
		}
		contexts = buildVMResourceContexts(vmList.Items, vmiResult.Value.Items)
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
		if value == keyword || strings.Contains(value, keyword) {
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
	candidates := []string{
		strings.ToLower(strings.TrimSpace(resource.Name)),
		strings.ToLower(strings.TrimSpace(resource.UID)),
		strings.ToLower(strings.TrimSpace(resource.DisplayName)),
	}
	result := make([]vmResourceContext, 0)
	for _, item := range contexts {
		for _, candidate := range candidates {
			if candidate == "" {
				continue
			}
			if strings.Contains(item.RawText, candidate) {
				result = append(result, item)
				break
			}
		}
	}
	return deduplicateVMContexts(result)
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
