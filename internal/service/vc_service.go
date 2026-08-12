package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"rayctl/internal/platform"
)

type VCService struct {
	vcClient *platform.VirtualClusterClient
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

func NewVCService(vcClient *platform.VirtualClusterClient) *VCService {
	return &VCService{vcClient: vcClient}
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
		NodePool:    strings.TrimSpace(node.Properties.NodePoolName),
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
