package platform

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ListSSPClustersForProfile keeps VC workload discovery in the profile that
// resolved the VC. It uses the same SSP cluster list endpoint as ListSSPClusters.
func (c *VirtualClusterClient) ListSSPClustersForProfile(ctx context.Context, profileName, region string) ([]SSPCluster, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	const pageSize = 100
	result := make([]SSPCluster, 0)
	seen := make(map[string]struct{})
	for skip := 0; ; {
		endpoint, _ := url.Parse(profile.BaseURL)
		endpoint.Path = "/compute/ssp/v1/subscriptions/-/resourceGroups/-/regions/-/clusters"
		query := endpoint.Query()
		query.Set("page_size", strconv.Itoa(pageSize))
		query.Set("skip", strconv.Itoa(skip))
		endpoint.RawQuery = query.Encode()
		var payload sspClusterListResponse
		if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &payload); err != nil {
			return nil, err
		}
		for _, cluster := range payload.Clusters {
			normalizeSSPCluster(&cluster, profile)
			cluster.Subscription = firstNonEmpty(cluster.Subscription, profile.Subscription)
			cluster.ResourceGroup = firstNonEmpty(cluster.ResourceGroup, profile.ResourceGroup, defaultResourceGroup)
			cluster.Region = firstNonEmpty(cluster.Region, profile.Region)
			if strings.TrimSpace(region) != "" && !strings.EqualFold(strings.TrimSpace(cluster.Region), strings.TrimSpace(region)) {
				continue
			}
			key := strings.ToLower(firstNonEmpty(cluster.UID, cluster.Name))
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, cluster)
		}
		count := len(payload.Clusters)
		if count == 0 || count < pageSize || (payload.TotalSize > 0 && skip+count >= payload.TotalSize) {
			break
		}
		skip += count
	}
	return result, nil
}
