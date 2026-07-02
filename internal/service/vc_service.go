package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"rayctl/internal/platform"
)

type VCService struct {
	vcClient *platform.VirtualClusterClient
}

type VCListResult struct {
	Items []VCListItem
}

type VCListItem struct {
	Name   string
	UID    string
	Tenant string
	Region string
	State  string
}

type VCDetailResult struct {
	Name   string
	UID    string
	Tenant string
	Region string
	State  string
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

	list, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	normalized := strings.ToLower(identifier)
	exact := make([]VCListItem, 0)
	fuzzy := make([]VCListItem, 0)
	for _, item := range list.Items {
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
		return vcDetailFromListItem(exact[0]), nil
	case len(exact) > 1:
		return nil, fmt.Errorf("vc %q matched multiple virtual clusters: %s", identifier, joinVCCandidates(exact))
	case len(fuzzy) == 1:
		return vcDetailFromListItem(fuzzy[0]), nil
	case len(fuzzy) > 1:
		return nil, fmt.Errorf("vc %q matched multiple virtual clusters: %s", identifier, joinVCCandidates(fuzzy))
	default:
		return nil, fmt.Errorf("vc %q not found", identifier)
	}
}

func vcListItemFromPlatform(cluster platform.VirtualCluster) VCListItem {
	return VCListItem{
		Name:   firstNonEmpty(strings.TrimSpace(cluster.Name), strings.TrimSpace(cluster.DisplayName), "vc-"+strings.TrimSpace(cluster.UID)),
		UID:    strings.TrimSpace(cluster.UID),
		Tenant: firstNonEmpty(strings.TrimSpace(cluster.ProfileName), "-"),
		Region: firstNonEmpty(strings.TrimSpace(cluster.Region), "-"),
		State:  firstNonEmpty(strings.TrimSpace(cluster.State), "-"),
	}
}

func vcDetailFromListItem(item VCListItem) *VCDetailResult {
	return &VCDetailResult{
		Name:   item.Name,
		UID:    item.UID,
		Tenant: item.Tenant,
		Region: item.Region,
		State:  item.State,
	}
}

func joinVCCandidates(items []VCListItem) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, fmt.Sprintf("%s(%s)", item.Name, item.Tenant))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
