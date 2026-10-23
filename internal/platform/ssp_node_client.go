package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// ListSSPClustersForNodeUsage returns a complete cluster inventory for the
// selected environment and region. Node classification must not use a partial list
// when another selected profile or a later page could own the same VC.
func (c *VirtualClusterClient) ListSSPClustersForNodeUsage(ctx context.Context, region string) ([]SSPCluster, error) {
	profiles := c.inventoryProfilesForRegion(region)
	if len(profiles) == 0 {
		return nil, fmt.Errorf("no platform profile configured for region %q", region)
	}

	result := make([]SSPCluster, 0)
	seen := make(map[string]struct{})
	for _, profile := range profiles {
		clusters, err := c.listSSPClustersForNodeUsageProfile(ctx, profile, region)
		if err != nil {
			return nil, fmt.Errorf("list SSP clusters for node usage in profile %q: %w", profile.Name, err)
		}
		for _, cluster := range clusters {
			key := strings.ToLower(profile.Name + "|" + firstNonEmpty(cluster.UID, cluster.Name))
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, cluster)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
		}
		return result[i].ProfileName < result[j].ProfileName
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// This inventory needs stronger completeness checks than ordinary catalog
// discovery, which can still use successfully read profiles after a failure.
func (c *VirtualClusterClient) listSSPClustersForNodeUsageProfile(ctx context.Context, profile clientProfile, region string) ([]SSPCluster, error) {
	endpoint, err := url.Parse(profile.BaseURL)
	if err != nil || endpoint.Host == "" || endpoint.Scheme == "" {
		return nil, fmt.Errorf("invalid platform endpoint")
	}
	endpoint.Path = "/compute/ssp/v1/subscriptions/-/resourceGroups/-/regions/-/clusters"
	const pageSize = 100
	result := make([]SSPCluster, 0)
	seenTokens := make(map[string]bool)
	seenPages := make(map[string]bool)
	var total *int
	skip, token := 0, ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		query := endpoint.Query()
		query.Set("page_size", strconv.Itoa(pageSize))
		query.Set("skip", strconv.Itoa(skip))
		if token != "" {
			query.Set("page_token", token)
		} else {
			query.Del("page_token")
		}
		endpoint.RawQuery = query.Encode()
		var payload struct {
			Clusters      []SSPCluster `json:"clusters"`
			TotalSize     *int         `json:"total_size"`
			NextPageToken string       `json:"next_page_token"`
		}
		if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &payload); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if payload.Clusters == nil {
			return nil, fmt.Errorf("SSP cluster inventory response has no non-null clusters array")
		}
		if payload.TotalSize != nil {
			if *payload.TotalSize < 0 || (total != nil && *total != *payload.TotalSize) {
				return nil, fmt.Errorf("SSP cluster inventory total_size is inconsistent")
			}
			total = payload.TotalSize
		}
		count := skip + len(payload.Clusters)
		if total != nil && count > *total {
			return nil, fmt.Errorf("SSP cluster inventory exceeds total_size")
		}
		more := payload.NextPageToken != "" || (total != nil && count < *total)
		if len(payload.Clusters) == 0 {
			if more {
				return nil, fmt.Errorf("SSP cluster inventory is incomplete: empty page before pagination finished")
			}
			return result, nil
		}
		page, _ := json.Marshal(payload.Clusters)
		if seenPages[string(page)] {
			return nil, fmt.Errorf("SSP cluster inventory is incomplete: repeated page")
		}
		seenPages[string(page)] = true
		for _, cluster := range payload.Clusters {
			normalizeSSPCluster(&cluster, profile)
			if strings.TrimSpace(cluster.UID) == "" && strings.TrimSpace(cluster.Name) == "" {
				return nil, fmt.Errorf("SSP cluster inventory contains a cluster with no UID or name")
			}
			cluster.Subscription = firstNonEmpty(cluster.Subscription, profile.Subscription)
			cluster.ResourceGroup = firstNonEmpty(cluster.ResourceGroup, profile.ResourceGroup, defaultResourceGroup)
			cluster.Region = firstNonEmpty(cluster.Region, profile.Region)
			if strings.TrimSpace(region) != "" && !strings.EqualFold(strings.TrimSpace(cluster.Region), strings.TrimSpace(region)) {
				continue
			}
			result = append(result, cluster)
		}
		if !more && (total != nil || len(payload.Clusters) < pageSize) {
			return result, nil
		}
		if payload.NextPageToken != "" {
			if seenTokens[payload.NextPageToken] {
				return nil, fmt.Errorf("SSP cluster inventory is incomplete: repeated page token")
			}
			seenTokens[payload.NextPageToken] = true
		}
		skip, token = count, payload.NextPageToken
	}
}
