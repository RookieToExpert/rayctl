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

type NetworkResourceService struct {
	client *platform.VirtualClusterClient
}

type VPCListResult struct{ Items []VPCListItem }
type SubnetListResult struct{ Items []SubnetListItem }
type NATGatewayListResult struct{ Items []NATGatewayListItem }
type AFSListResult struct{ Items []AFSListItem }

type VPCListItem struct {
	Name, UID, State, CIDR, RDMA, Default, NATGateways, Region, CreatedAt, UpdatedAt string
	SubnetCount                                                                      int
}

type SubnetListItem struct {
	Name, UID, State, VPC, CIDR, Gateway, AvailableIPs, Scope, Provider, NetworkType, Zone, CreatedAt, UpdatedAt string
}

type NATGatewayListItem struct {
	Name, UID, State, VPC, Subnet, GatewayType, EIPs, Zone, CreatedAt, UpdatedAt string
	EIPCount, SNATCount, DNATCount                                               int
}

type AFSListItem struct {
	Name, UID, RID, State, Capacity, StorageClass, Zone, Region, CreatedAt, UpdatedAt string
}

func NewNetworkResourceService(client *platform.VirtualClusterClient) *NetworkResourceService {
	return &NetworkResourceService{client: client}
}

func (s *NetworkResourceService) ListVPCs(ctx context.Context) (*VPCListResult, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	resources, err := s.client.ListVPCResources(ctx)
	if err != nil {
		return nil, fmt.Errorf("list vpcs: %w", err)
	}
	items := make([]VPCListItem, 0, len(resources))
	for _, resource := range resources {
		natNames := make([]string, 0, len(resource.Properties.NATGatewayInfos))
		for _, nat := range resource.Properties.NATGatewayInfos {
			natNames = append(natNames, firstNonEmpty(strings.TrimSpace(nat.Name), strings.TrimSpace(nat.DisplayName), strings.TrimSpace(nat.UID)))
		}
		items = append(items, VPCListItem{
			Name:        resourceDisplayName(resource.Name, resource.DisplayName, resource.UID),
			UID:         strings.TrimSpace(resource.UID),
			State:       resource.State,
			CIDR:        resource.Properties.CIDR,
			RDMA:        boolText(resource.Properties.EnableRDMANetwork),
			Default:     boolText(resource.Properties.IsDefault),
			SubnetCount: resource.Properties.SubnetCount,
			NATGateways: strings.Join(nonEmptyStrings(natNames), ", "),
			Region:      resource.Region,
			CreatedAt:   formatResourceTime(resource.CreateTime),
			UpdatedAt:   formatResourceTime(resource.UpdateTime),
		})
	}
	sortByResourceName(items, func(item VPCListItem) string { return item.Name })
	return &VPCListResult{Items: items}, nil
}

func (s *NetworkResourceService) GetVPC(ctx context.Context, identifier string) (*VPCListItem, error) {
	result, err := s.ListVPCs(ctx)
	if err != nil {
		return nil, err
	}
	index, err := findResourceIndex(identifier, "vpc", len(result.Items), func(i int) []string {
		return []string{result.Items[i].Name, result.Items[i].UID}
	})
	if err != nil {
		return nil, err
	}
	return &result.Items[index], nil
}

func (s *NetworkResourceService) ListSubnets(ctx context.Context) (*SubnetListResult, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	resources, err := s.client.ListSubnetResources(ctx)
	if err != nil {
		return nil, fmt.Errorf("list subnets: %w", err)
	}
	items := make([]SubnetListItem, 0, len(resources))
	for _, resource := range resources {
		items = append(items, SubnetListItem{
			Name:         resourceDisplayName(resource.Name, resource.DisplayName, resource.UID),
			UID:          strings.TrimSpace(resource.UID),
			State:        resource.State,
			VPC:          resourceDisplayName(resource.Properties.VPCInfo.Name, resource.Properties.VPCInfo.DisplayName, resource.Properties.VPCInfo.UID),
			CIDR:         resource.Properties.CIDR,
			Gateway:      resource.Properties.GatewayIP,
			AvailableIPs: resource.Properties.V4AvailableIPs,
			Scope:        resource.Properties.Scope,
			Provider:     resource.Properties.Provider,
			NetworkType:  resource.Properties.NetworkType,
			Zone:         resource.Zone,
			CreatedAt:    formatResourceTime(resource.CreateTime),
			UpdatedAt:    formatResourceTime(resource.UpdateTime),
		})
	}
	sortByResourceName(items, func(item SubnetListItem) string { return item.Name })
	return &SubnetListResult{Items: items}, nil
}

func (s *NetworkResourceService) GetSubnet(ctx context.Context, identifier string) (*SubnetListItem, error) {
	result, err := s.ListSubnets(ctx)
	if err != nil {
		return nil, err
	}
	index, err := findResourceIndex(identifier, "subnet", len(result.Items), func(i int) []string {
		return []string{result.Items[i].Name, result.Items[i].UID}
	})
	if err != nil {
		return nil, err
	}
	return &result.Items[index], nil
}

func (s *NetworkResourceService) ListNATGateways(ctx context.Context) (*NATGatewayListResult, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	resources, err := s.client.ListNATGatewayResources(ctx)
	if err != nil {
		return nil, fmt.Errorf("list nat gateways: %w", err)
	}
	items := make([]NATGatewayListItem, 0, len(resources))
	for _, resource := range resources {
		eips := make([]string, 0, len(resource.Properties.EIPs))
		for _, eip := range resource.Properties.EIPs {
			label := firstNonEmpty(strings.TrimSpace(eip.Name), strings.TrimSpace(eip.UID))
			if strings.TrimSpace(eip.IP) != "" {
				label = fmt.Sprintf("%s(%s)", label, strings.TrimSpace(eip.IP))
			}
			eips = append(eips, label)
		}
		items = append(items, NATGatewayListItem{
			Name:        resourceDisplayName(resource.Name, resource.DisplayName, resource.UID),
			UID:         strings.TrimSpace(resource.UID),
			State:       resource.State,
			VPC:         resourceDisplayName(resource.Properties.VPCInfo.Name, resource.Properties.VPCInfo.DisplayName, resource.Properties.VPCInfo.UID),
			Subnet:      resourceDisplayName(resource.Properties.SubnetInfo.Name, resource.Properties.SubnetInfo.DisplayName, resource.Properties.SubnetInfo.UID),
			GatewayType: resource.Properties.GatewayType,
			EIPs:        strings.Join(nonEmptyStrings(eips), ", "),
			EIPCount:    len(resource.Properties.EIPs),
			SNATCount:   len(resource.Properties.SNATRules),
			DNATCount:   len(resource.Properties.DNATRules),
			Zone:        resource.Zone,
			CreatedAt:   formatResourceTime(resource.CreateTime),
			UpdatedAt:   formatResourceTime(resource.UpdateTime),
		})
	}
	sortByResourceName(items, func(item NATGatewayListItem) string { return item.Name })
	return &NATGatewayListResult{Items: items}, nil
}

func (s *NetworkResourceService) GetNATGateway(ctx context.Context, identifier string) (*NATGatewayListItem, error) {
	result, err := s.ListNATGateways(ctx)
	if err != nil {
		return nil, err
	}
	index, err := findResourceIndex(identifier, "nat gateway", len(result.Items), func(i int) []string {
		return []string{result.Items[i].Name, result.Items[i].UID}
	})
	if err != nil {
		return nil, err
	}
	return &result.Items[index], nil
}

func (s *NetworkResourceService) ListAFS(ctx context.Context) (*AFSListResult, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	resources, err := s.client.ListCurrentStorageVolumeResources(ctx)
	if err != nil {
		return nil, fmt.Errorf("list afs resources: %w", err)
	}
	items := make([]AFSListItem, 0, len(resources))
	for _, resource := range resources {
		capacity, storageClass := parseAFSProperties(resource.Properties)
		items = append(items, AFSListItem{
			Name:         resourceDisplayName(resource.Name, resource.DisplayName, resource.ID),
			UID:          strings.TrimSpace(resource.ID),
			RID:          strings.TrimSpace(resource.RID),
			State:        resource.State,
			Capacity:     capacity,
			StorageClass: storageClass,
			Zone:         resource.Zone,
			Region:       resource.Region,
			CreatedAt:    formatResourceTime(resource.CreateTime),
			UpdatedAt:    formatResourceTime(resource.UpdateTime),
		})
	}
	sortByResourceName(items, func(item AFSListItem) string { return item.Name })
	return &AFSListResult{Items: items}, nil
}

func (s *NetworkResourceService) GetAFS(ctx context.Context, identifier string) (*AFSListItem, error) {
	result, err := s.ListAFS(ctx)
	if err != nil {
		return nil, err
	}
	index, err := findResourceIndex(identifier, "afs", len(result.Items), func(i int) []string {
		return []string{result.Items[i].Name, result.Items[i].UID, result.Items[i].RID}
	})
	if err != nil {
		return nil, err
	}
	return &result.Items[index], nil
}

func (s *NetworkResourceService) validate() error {
	if s.client == nil {
		return fmt.Errorf("platform client is required")
	}
	return nil
}

func findResourceIndex(identifier string, kind string, count int, fields func(int) []string) (int, error) {
	identifier = strings.ToLower(strings.TrimSpace(identifier))
	if identifier == "" {
		return -1, fmt.Errorf("%s identifier is required", kind)
	}
	exact := make([]int, 0, 1)
	fuzzy := make([]int, 0, 1)
	for i := 0; i < count; i++ {
		fuzzyMatch := false
		for _, field := range fields(i) {
			field = strings.ToLower(strings.TrimSpace(field))
			if field == "" {
				continue
			}
			if field == identifier {
				exact = append(exact, i)
				fuzzyMatch = false
				break
			}
			if strings.Contains(field, identifier) {
				fuzzyMatch = true
			}
		}
		if fuzzyMatch {
			fuzzy = append(fuzzy, i)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return -1, fmt.Errorf("%s %q matched multiple resources", kind, identifier)
	}
	if len(fuzzy) == 1 {
		return fuzzy[0], nil
	}
	if len(fuzzy) > 1 {
		return -1, fmt.Errorf("%s %q matched multiple resources", kind, identifier)
	}
	return -1, fmt.Errorf("%s %q not found", kind, identifier)
}

func parseAFSProperties(raw string) (string, string) {
	var properties struct {
		Resources struct {
			BillingItems struct {
				Capacity json.Number `json:"capacity"`
				Unit     string      `json:"capacity_unit"`
			} `json:"billing_items"`
		} `json:"resources"`
		StorageClass string `json:"storage_class"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&properties); err != nil {
		return "", ""
	}
	capacity := strings.TrimSpace(properties.Resources.BillingItems.Capacity.String())
	if capacity != "" {
		capacity = strings.TrimSuffix(capacity, ".0") + strings.TrimSpace(properties.Resources.BillingItems.Unit)
	}
	return capacity, strings.TrimSpace(properties.StorageClass)
}

func formatResourceTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return parsed.In(time.FixedZone("UTC+8", 8*60*60)).Format("2006-01-02 15:04:05")
}

func resourceDisplayName(name string, displayName string, uid string) string {
	return firstNonEmpty(strings.TrimSpace(name), strings.TrimSpace(displayName), strings.TrimSpace(uid))
}

func boolText(value bool) string {
	if value {
		return "Y"
	}
	return "N"
}

func nonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, strings.TrimSpace(value))
		}
	}
	return result
}

func sortByResourceName[T any](items []T, name func(T) string) {
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(name(items[i])) < strings.ToLower(name(items[j]))
	})
}
