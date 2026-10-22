package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"rayctl/internal/concurrent"
)

// ListLookupVirtualClusters preserves failed profiles so absence checks cannot
// mistake a partially readable environment for a complete empty inventory.
func (c *VirtualClusterClient) ListLookupVirtualClusters(ctx context.Context) ([]VirtualCluster, error) {
	type result struct {
		profile clientProfile
		items   []VirtualCluster
		err     error
	}
	results := concurrent.Map(ctx, c.currentEnvironmentProfiles(), 6, func(ctx context.Context, profile clientProfile) result {
		items, err := c.listVirtualClustersForProfile(ctx, profile)
		return result{profile: profile, items: items, err: err}
	})
	items, errs := []VirtualCluster{}, []error{}
	seen := map[string]bool{}
	for _, result := range results {
		if result.err != nil {
			errs = append(errs, fmt.Errorf("profile %q: %w", result.profile.Name, result.err))
			continue
		}
		for _, item := range result.items {
			item.ProfileName = result.profile.Name
			key := result.profile.Name + "/" + firstNonEmpty(strings.TrimSpace(item.UID), strings.TrimSpace(item.Name), strings.TrimSpace(item.DisplayName))
			if !seen[key] {
				seen[key] = true
				items = append(items, item)
			}
		}
	}
	return items, errors.Join(errs...)
}
