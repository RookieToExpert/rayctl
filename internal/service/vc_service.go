package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"rayctl/internal/platform"
)

type VCService struct {
	vcClient           *platform.VirtualClusterClient
	clientset          kubernetes.Interface
	allocatableOnce    sync.Once
	allocatableByNode  map[string]corev1.ResourceList
	allocatableListErr error
}

type VCListResult struct {
	Items []VCListItem
}

type VCListItem struct {
	Name         string
	UID          string
	Subscription string
	Tenant       string
	Region       string
	State        string
}

type VCDetailResult struct {
	Name         string
	UID          string
	Subscription string
	Tenant       string
	Region       string
	State        string
}

type VCNodeListResult struct {
	ClusterName string
	ClusterUID  string
	ProfileName string
	Items       []VCNodeListItem
}

type VCNodeListItem struct {
	Kind        string
	UID         string
	Name        string
	HostName    string
	HostIP      string
	State       string
	Zone        string
	MachineType string
	Model       string
	NodePool    string
}

type VCNodeRemoveResult struct {
	ClusterName string
	ClusterUID  string
	ProfileName string
	Nodes       []VCNodeListItem
	RequestURL  string
	Payload     string
	Result      string

	subscription string
	region       string
	acnUIDs      []string
}

type VCResourceUsageResult struct {
	ClusterName string
	ClusterUID  string
	ProfileName string
	Items       []VCNodeResourceUsageItem
}

type VCNodeResourceUsageItem struct {
	UID      string
	HostName string
	HostIP   string
	State    string
	Usage    platform.VirtualClusterNodeResourceUsage
}

func (r *VCResourceUsageResult) FilterFreeNodes() {
	if r == nil {
		return
	}
	filtered := r.Items[:0]
	for _, item := range r.Items {
		usage := item.Usage.Usage
		if hasAcceleratorResource(usage.Total.Device, usage.Allocated.Device, usage.Available.Device) {
			if resourceAmountAvailable(usage.Available.Device, usage.Total.Device, usage.Allocated.Device) {
				filtered = append(filtered, item)
			}
			continue
		}
		if resourceAmountAvailable(usage.Available.CPU, usage.Total.CPU, usage.Allocated.CPU) &&
			resourceAmountAvailable(usage.Available.Memory, usage.Total.Memory, usage.Allocated.Memory) {
			filtered = append(filtered, item)
		}
	}
	r.Items = filtered
}

func hasAcceleratorResource(values ...string) bool {
	for _, value := range values {
		if positive, valid := positiveResourceQuantity(value); valid && positive {
			return true
		}
	}
	return false
}

func resourceAmountAvailable(available, total, allocated string) bool {
	if positive, valid := positiveResourceQuantity(available); valid {
		return positive
	}
	totalQuantity, totalValid := parsePlatformResourceQuantity(total)
	allocatedQuantity, allocatedValid := parsePlatformResourceQuantity(allocated)
	return totalValid && allocatedValid && totalQuantity.Cmp(allocatedQuantity) > 0
}

func positiveResourceQuantity(value string) (bool, bool) {
	quantity, valid := parsePlatformResourceQuantity(value)
	return valid && quantity.Sign() > 0, valid
}

func parsePlatformResourceQuantity(value string) (resource.Quantity, bool) {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" {
		return resource.Quantity{}, false
	}
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		if strings.HasSuffix(value, suffix) {
			value = strings.TrimSuffix(value, suffix) + strings.TrimSuffix(suffix, "B")
			break
		}
	}
	quantity, err := resource.ParseQuantity(value)
	return quantity, err == nil
}

func NewVCService(vcClient *platform.VirtualClusterClient) *VCService {
	return &VCService{vcClient: vcClient}
}

func NewVCServiceWithKubeClient(vcClient *platform.VirtualClusterClient, clientset kubernetes.Interface) *VCService {
	return &VCService{vcClient: vcClient, clientset: clientset}
}

func (s *VCService) List(ctx context.Context) (*VCListResult, error) {
	if s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required")
	}

	clusters, err := s.vcClient.ListVirtualClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list virtual clusters: %w", err)
	}

	items := make([]VCListItem, 0, len(clusters))
	for _, cluster := range clusters {
		items = append(items, vcListItemFromPlatform(cluster))
	}
	sort.Slice(items, func(i, j int) bool {
		if strings.EqualFold(items[i].Tenant, items[j].Tenant) {
			return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
		}
		return strings.ToLower(items[i].Tenant) < strings.ToLower(items[j].Tenant)
	})

	return &VCListResult{Items: items}, nil
}

func (s *VCService) Get(ctx context.Context, identifier string) (*VCDetailResult, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("vc identifier is required")
	}
	if !looksLikeVCUID(identifier) {
		exactCtx, cancelExact := context.WithTimeout(ctx, 2*time.Second)
		cluster, exactErr := s.vcClient.FindExactVirtualCluster(exactCtx, identifier)
		cancelExact()
		if exactErr == nil {
			item := vcListItemFromPlatform(*cluster)
			if item.UID != "" && item.Subscription != "" {
				return vcDetailFromListItem(item), nil
			}
		}
	}
	if clusters, currentErr := s.vcClient.ListCurrentProfileVirtualClusters(ctx); currentErr == nil {
		items := make([]VCListItem, 0, len(clusters))
		for _, cluster := range clusters {
			items = append(items, vcListItemFromPlatform(cluster))
		}
		if result, matched, err := matchVCIdentifier(identifier, items); matched {
			return result, err
		}
	}

	list, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	if result, matched, err := matchVCIdentifier(identifier, list.Items); matched {
		return result, err
	}
	return nil, fmt.Errorf("vc %q not found", identifier)
}

func matchVCIdentifier(identifier string, items []VCListItem) (*VCDetailResult, bool, error) {
	normalized := strings.ToLower(identifier)
	exact := make([]VCListItem, 0)
	fuzzy := make([]VCListItem, 0)
	for _, item := range items {
		fields := []string{
			item.Name,
			item.UID,
			"vc-" + strings.TrimPrefix(item.UID, "vc-"),
		}
		matchedFuzzy := false
		for _, field := range fields {
			field = strings.ToLower(strings.TrimSpace(field))
			if field == "" {
				continue
			}
			if field == normalized {
				exact = append(exact, item)
				matchedFuzzy = false
				break
			}
			if strings.Contains(field, normalized) {
				matchedFuzzy = true
			}
		}
		if matchedFuzzy {
			fuzzy = append(fuzzy, item)
		}
	}

	switch {
	case len(exact) == 1:
		return vcDetailFromListItem(exact[0]), true, nil
	case len(exact) > 1:
		return nil, true, fmt.Errorf("vc %q matched multiple virtual clusters: %s", identifier, joinVCCandidates(exact))
	case len(fuzzy) == 1:
		return vcDetailFromListItem(fuzzy[0]), true, nil
	case len(fuzzy) > 1:
		return nil, true, fmt.Errorf("vc %q matched multiple virtual clusters: %s", identifier, joinVCCandidates(fuzzy))
	default:
		return nil, false, nil
	}
}

func looksLikeVCUID(value string) bool {
	value = strings.TrimPrefix(strings.TrimSpace(value), "vc-")
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

func (s *VCService) ListNodes(ctx context.Context, clusterIdentifier string) (*VCNodeListResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required")
	}
	cluster, err := s.Get(ctx, clusterIdentifier)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cluster.Subscription) == "" {
		return nil, fmt.Errorf("cannot determine subscription id for vc %q", cluster.Name)
	}
	if strings.TrimSpace(cluster.Tenant) == "" || cluster.Tenant == "-" {
		return nil, fmt.Errorf("cannot determine platform profile for vc %q", cluster.Name)
	}

	nodes, err := s.vcClient.ListVirtualClusterNodes(
		ctx,
		cluster.Tenant,
		cluster.Subscription,
		cluster.Region,
		cluster.Name,
	)
	if err != nil {
		return nil, fmt.Errorf("list nodes in vc %s: %w", cluster.Name, err)
	}
	items := make([]VCNodeListItem, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, vcNodeListItemFromPlatform(node))
	}
	s.enrichVCNodeModels(ctx, items)
	sort.Slice(items, func(i, j int) bool {
		left := firstNonEmpty(items[i].HostIP, items[i].HostName, items[i].Name, items[i].UID)
		right := firstNonEmpty(items[j].HostIP, items[j].HostName, items[j].Name, items[j].UID)
		return strings.ToLower(left) < strings.ToLower(right)
	})
	return &VCNodeListResult{
		ClusterName: cluster.Name,
		ClusterUID:  cluster.UID,
		ProfileName: cluster.Tenant,
		Items:       items,
	}, nil
}

func (s *VCService) enrichVCNodeModels(ctx context.Context, items []VCNodeListItem) {
	if s == nil || s.clientset == nil || len(items) == 0 {
		return
	}
	nodes, err := s.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{ResourceVersion: "0"})
	if err != nil {
		return
	}
	modelByHost := make(map[string]string, len(nodes.Items))
	for _, node := range nodes.Items {
		model := nodeAcceleratorModel(node.Labels)
		if model != "" {
			modelByHost[strings.ToLower(strings.TrimSpace(node.Name))] = model
		}
	}
	for index := range items {
		if strings.TrimSpace(items[index].Model) != "" {
			continue
		}
		items[index].Model = modelByHost[strings.ToLower(strings.TrimSpace(items[index].HostName))]
	}
}

func (s *VCService) GetResourceUsage(ctx context.Context, clusterIdentifier string) (*VCResourceUsageResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required")
	}
	cluster, err := s.Get(ctx, clusterIdentifier)
	if err != nil {
		return nil, err
	}
	return s.getResourceUsage(ctx, cluster)
}

func (s *VCService) GetResourceUsageForProfile(ctx context.Context, profileName, subscription, region, clusterName string) (*VCResourceUsageResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required")
	}
	platformCluster, err := s.vcClient.FindExactVirtualClusterForProfile(ctx, profileName, subscription, region, clusterName)
	if err != nil {
		return nil, err
	}
	cluster := vcDetailFromListItem(vcListItemFromPlatform(*platformCluster))
	cluster.Subscription = firstNonEmpty(strings.TrimSpace(subscription), cluster.Subscription)
	cluster.Region = firstNonEmpty(strings.TrimSpace(region), cluster.Region)
	cluster.Tenant = firstNonEmpty(strings.TrimSpace(profileName), cluster.Tenant)
	return s.getResourceUsage(ctx, cluster)
}

func (s *VCService) getResourceUsage(ctx context.Context, cluster *VCDetailResult) (*VCResourceUsageResult, error) {
	nodes, err := s.vcClient.ListVirtualClusterNodes(
		ctx,
		cluster.Tenant,
		cluster.Subscription,
		cluster.Region,
		cluster.Name,
	)
	if err != nil {
		return nil, fmt.Errorf("list nodes in vc %s: %w", cluster.Name, err)
	}

	const usageBatchSize = 100
	uidBatches := make([][]string, 0, (len(nodes)+usageBatchSize-1)/usageBatchSize)
	for start := 0; start < len(nodes); start += usageBatchSize {
		end := min(start+usageBatchSize, len(nodes))
		batch := make([]string, 0, end-start)
		for _, node := range nodes[start:end] {
			if uid := strings.TrimSpace(node.UID); uid != "" {
				batch = append(batch, uid)
			}
		}
		if len(batch) > 0 {
			uidBatches = append(uidBatches, batch)
		}
	}
	type usageBatchResult struct {
		items []platform.VirtualClusterNodeResourceUsage
		err   error
	}
	usageBatches := boundedMap(ctx, uidBatches, 4, func(queryCtx context.Context, nodeUIDs []string) usageBatchResult {
		items, queryErr := s.vcClient.BatchGetVirtualClusterNodeResourceUsages(
			queryCtx,
			cluster.Tenant,
			cluster.Subscription,
			cluster.Region,
			cluster.Name,
			cluster.UID,
			nodeUIDs,
		)
		return usageBatchResult{items: items, err: queryErr}
	})
	usageByUID := make(map[string]platform.VirtualClusterNodeResourceUsage, len(nodes))
	for _, batch := range usageBatches {
		if batch.err != nil {
			return nil, fmt.Errorf("get node resource usage for vc %s: %w", cluster.Name, batch.err)
		}
		for _, usage := range batch.items {
			usageByUID[strings.TrimSpace(usage.UID)] = usage
		}
	}

	items := make([]VCNodeResourceUsageItem, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, VCNodeResourceUsageItem{
			UID:      strings.TrimSpace(node.UID),
			HostName: strings.TrimSpace(node.Properties.HostName),
			HostIP:   strings.TrimSpace(node.Properties.HostIP),
			State:    strings.TrimSpace(node.State),
			Usage:    usageByUID[strings.TrimSpace(node.UID)],
		})
	}
	if kubernetesNodes, nodeErr := s.vcClient.ListKubernetesNodesForProfile(ctx, cluster.Tenant, cluster.Name); nodeErr == nil {
		applyKubernetesNodeAllocatable(items, kubernetesNodes)
	} else {
		s.applyLocalNodeAllocatable(ctx, items)
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(firstNonEmpty(items[i].HostIP, items[i].HostName, items[i].UID)) <
			strings.ToLower(firstNonEmpty(items[j].HostIP, items[j].HostName, items[j].UID))
	})
	return &VCResourceUsageResult{
		ClusterName: cluster.Name,
		ClusterUID:  cluster.UID,
		ProfileName: cluster.Tenant,
		Items:       items,
	}, nil
}

func (s *VCService) applyLocalNodeAllocatable(ctx context.Context, items []VCNodeResourceUsageItem) {
	if s == nil || s.clientset == nil || len(items) == 0 {
		return
	}
	s.allocatableOnce.Do(func() {
		nodes, err := s.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{ResourceVersion: "0"})
		if err != nil {
			s.allocatableListErr = err
			return
		}
		s.allocatableByNode = make(map[string]corev1.ResourceList, len(nodes.Items)*2)
		for index := range nodes.Items {
			node := &nodes.Items[index]
			resources := node.Status.Allocatable
			if name := normalizeVCNodeLookupKey(node.Name); name != "" {
				s.allocatableByNode[name] = resources
			}
			for _, address := range node.Status.Addresses {
				if address.Type != corev1.NodeInternalIP {
					continue
				}
				if ip := normalizeVCNodeLookupKey(address.Address); ip != "" {
					s.allocatableByNode[ip] = resources
				}
			}
		}
	})
	if s.allocatableListErr != nil {
		return
	}
	applyAllocatableByNode(items, s.allocatableByNode)
}

func applyKubernetesNodeAllocatable(items []VCNodeResourceUsageItem, nodes []corev1.Node) {
	allocatableByNode := make(map[string]corev1.ResourceList, len(nodes)*2)
	for index := range nodes {
		node := &nodes[index]
		if name := normalizeVCNodeLookupKey(node.Name); name != "" {
			allocatableByNode[name] = node.Status.Allocatable
		}
		for _, address := range node.Status.Addresses {
			if address.Type == corev1.NodeInternalIP {
				allocatableByNode[normalizeVCNodeLookupKey(address.Address)] = node.Status.Allocatable
			}
		}
	}
	applyAllocatableByNode(items, allocatableByNode)
}

func applyAllocatableByNode(items []VCNodeResourceUsageItem, allocatableByNode map[string]corev1.ResourceList) {
	for index := range items {
		resources, ok := allocatableByNode[normalizeVCNodeLookupKey(items[index].HostName)]
		if !ok {
			resources, ok = allocatableByNode[normalizeVCNodeLookupKey(items[index].HostIP)]
		}
		if ok {
			applyAllocatableToVCNodeUsage(&items[index].Usage, resources)
		}
	}
}

func applyAllocatableToVCNodeUsage(usage *platform.VirtualClusterNodeResourceUsage, resources corev1.ResourceList) {
	if usage == nil {
		return
	}
	if cpu, ok := resources[corev1.ResourceCPU]; ok {
		usage.Usage.Total.CPU = formatAllocatableCPU(cpu.MilliValue())
	}
	if memory, ok := resources[corev1.ResourceMemory]; ok {
		usage.Usage.Total.Memory = formatAllocatableMemory(memory.Value())
	}
	if accelerator, ok := allocatableAccelerator(resources); ok {
		usage.Usage.Total.Device = strconv.FormatInt(accelerator, 10)
		allocated, err := strconv.ParseFloat(strings.TrimSpace(usage.Usage.Allocated.Device), 64)
		if err == nil {
			available := float64(accelerator) - allocated
			if available < 0 {
				available = 0
			}
			usage.Usage.Available.Device = strconv.FormatFloat(available, 'f', -1, 64)
		}
	}
}

func allocatableAccelerator(resources corev1.ResourceList) (int64, bool) {
	for _, resourceName := range gpuResourceNames() {
		if accelerator, ok := resources[corev1.ResourceName(resourceName)]; ok {
			return accelerator.Value(), true
		}
	}
	return 0, false
}

func formatAllocatableCPU(milliCPU int64) string {
	return strconv.FormatFloat(float64(milliCPU)/1000, 'f', -1, 64)
}

func formatAllocatableMemory(bytes int64) string {
	const gibibyte = int64(1024 * 1024 * 1024)
	gibibytes := strconv.FormatFloat(float64(bytes)/float64(gibibyte), 'f', 3, 64)
	gibibytes = strings.TrimRight(strings.TrimRight(gibibytes, "0"), ".")
	return gibibytes + "GiB"
}

func normalizeVCNodeLookupKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func (s *VCService) PrepareNodeRemove(ctx context.Context, clusterIdentifier string, nodeIdentifiers []string) (*VCNodeRemoveResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required")
	}
	cluster, err := s.Get(ctx, clusterIdentifier)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cluster.Subscription) == "" {
		return nil, fmt.Errorf("cannot determine subscription id for vc %q", cluster.Name)
	}
	if strings.TrimSpace(cluster.Tenant) == "" || cluster.Tenant == "-" {
		return nil, fmt.Errorf("cannot determine platform profile for vc %q", cluster.Name)
	}

	nodes, err := s.vcClient.ListVirtualClusterNodes(ctx, cluster.Tenant, cluster.Subscription, cluster.Region, cluster.Name)
	if err != nil {
		return nil, fmt.Errorf("list nodes in vc %s: %w", cluster.Name, err)
	}
	available := make([]VCNodeListItem, 0, len(nodes))
	for _, node := range nodes {
		available = append(available, vcNodeListItemFromPlatform(node))
	}
	selected, err := resolveVCNodesForRemoval(nodeIdentifiers, available)
	if err != nil {
		return nil, err
	}

	acnUIDs := make([]string, 0, len(selected))
	for _, node := range selected {
		acnUIDs = append(acnUIDs, node.UID)
	}
	requestURL, payload, err := s.vcClient.BuildAIComputeNodeRemoveRequest(
		cluster.Tenant,
		cluster.Subscription,
		cluster.Region,
		cluster.Name,
		acnUIDs,
	)
	if err != nil {
		return nil, err
	}
	payloadJSON, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal node removal payload: %w", err)
	}
	return &VCNodeRemoveResult{
		ClusterName:  cluster.Name,
		ClusterUID:   cluster.UID,
		ProfileName:  cluster.Tenant,
		Nodes:        selected,
		RequestURL:   requestURL,
		Payload:      string(payloadJSON),
		Result:       "pending confirmation",
		subscription: cluster.Subscription,
		region:       cluster.Region,
		acnUIDs:      acnUIDs,
	}, nil
}

func (s *VCService) ApplyNodeRemove(ctx context.Context, result *VCNodeRemoveResult) error {
	if s == nil || s.vcClient == nil {
		return fmt.Errorf("platform client is required")
	}
	if result == nil || len(result.acnUIDs) == 0 {
		return fmt.Errorf("prepared vc node removal is required")
	}
	_, err := s.vcClient.RemoveAIComputeNodesFromVirtualCluster(
		ctx,
		result.ProfileName,
		result.subscription,
		result.region,
		result.ClusterName,
		result.acnUIDs,
	)
	if err != nil {
		return fmt.Errorf("remove nodes from vc %s: %w", result.ClusterName, err)
	}
	result.Result = "removed"
	return nil
}

func vcListItemFromPlatform(cluster platform.VirtualCluster) VCListItem {
	return VCListItem{
		Name:         firstNonEmpty(strings.TrimSpace(cluster.Name), strings.TrimSpace(cluster.DisplayName), "vc-"+strings.TrimSpace(cluster.UID)),
		UID:          strings.TrimSpace(cluster.UID),
		Subscription: firstNonEmpty(strings.TrimSpace(cluster.TenantID), subscriptionIDFromRID(cluster.ID)),
		Tenant:       firstNonEmpty(strings.TrimSpace(cluster.ProfileName), "-"),
		Region:       firstNonEmpty(strings.TrimSpace(cluster.Region), "-"),
		State:        firstNonEmpty(strings.TrimSpace(cluster.State), "-"),
	}
}

func vcDetailFromListItem(item VCListItem) *VCDetailResult {
	return &VCDetailResult{
		Name:         item.Name,
		UID:          item.UID,
		Subscription: item.Subscription,
		Tenant:       item.Tenant,
		Region:       item.Region,
		State:        item.State,
	}
}

func vcNodeListItemFromPlatform(node platform.VirtualClusterNode) VCNodeListItem {
	return VCNodeListItem{
		Kind:        firstNonEmpty(strings.TrimSpace(node.Kind), "ACN"),
		UID:         strings.TrimSpace(node.UID),
		Name:        firstNonEmpty(strings.TrimSpace(node.Name), strings.TrimSpace(node.DisplayName), strings.TrimSpace(node.UID)),
		HostName:    strings.TrimSpace(node.Properties.HostName),
		HostIP:      strings.TrimSpace(node.Properties.HostIP),
		State:       strings.TrimSpace(node.State),
		Zone:        strings.TrimSpace(node.Zone),
		MachineType: strings.TrimSpace(node.Properties.MachineType),
		Model: firstNonEmpty(
			strings.TrimSpace(node.Properties.Model),
			strings.TrimSpace(node.Properties.AcceleratorModel),
			strings.TrimSpace(node.Properties.AcceleratorType),
		),
		NodePool: strings.TrimSpace(node.Properties.NodePoolName),
	}
}

func resolveVCNodesForRemoval(identifiers []string, nodes []VCNodeListItem) ([]VCNodeListItem, error) {
	if len(identifiers) == 0 {
		return nil, fmt.Errorf("at least one node name, ip, or uid is required")
	}
	selected := make([]VCNodeListItem, 0, len(identifiers))
	seenUIDs := make(map[string]struct{}, len(identifiers))
	for _, identifier := range identifiers {
		identifier = strings.TrimSpace(identifier)
		if identifier == "" {
			continue
		}
		matches := make([]VCNodeListItem, 0, 1)
		for _, node := range nodes {
			matched := false
			for _, candidate := range []string{node.UID, node.Name, node.HostName, node.HostIP} {
				if strings.EqualFold(strings.TrimSpace(candidate), identifier) {
					matched = true
					break
				}
			}
			if matched {
				matches = append(matches, node)
			}
		}
		switch len(matches) {
		case 0:
			return nil, fmt.Errorf("node %q is not present in the target vc", identifier)
		case 1:
			if matches[0].UID == "" {
				return nil, fmt.Errorf("node %q has no acn uid and cannot be removed", identifier)
			}
			if _, exists := seenUIDs[matches[0].UID]; !exists {
				selected = append(selected, matches[0])
				seenUIDs[matches[0].UID] = struct{}{}
			}
		default:
			return nil, fmt.Errorf("node %q matched multiple nodes in the target vc", identifier)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("at least one node name, ip, or uid is required")
	}
	return selected, nil
}

func subscriptionIDFromRID(rid string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(rid), "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if strings.EqualFold(parts[i], "subscriptions") {
			return strings.TrimSpace(parts[i+1])
		}
	}
	return ""
}

func joinVCCandidates(items []VCListItem) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, fmt.Sprintf("%s(%s)", item.Name, item.Tenant))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
