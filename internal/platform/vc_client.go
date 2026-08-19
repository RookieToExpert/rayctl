package platform

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

func (c *VirtualClusterClient) ResolveDisplayNames(ctx context.Context, uids []string) (map[string]string, error) {
	names, _, err := c.ResolveDisplayNamesWithProfiles(ctx, uids)
	return names, err
}

func (c *VirtualClusterClient) ResolveDisplayNamesWithProfiles(ctx context.Context, uids []string) (map[string]string, map[string]string, error) {
	uniqueUIDs := make(map[string]struct{}, len(uids))
	for _, uid := range uids {
		uid = strings.TrimSpace(uid)
		if uid != "" {
			uniqueUIDs[uid] = struct{}{}
		}
	}
	if len(uniqueUIDs) == 0 {
		return map[string]string{}, map[string]string{}, nil
	}

	clusters, err := c.listCurrentEnvironmentVirtualClusters(ctx)
	if err != nil {
		return nil, nil, err
	}

	names := make(map[string]string, len(uniqueUIDs))
	profiles := make(map[string]string, len(uniqueUIDs))
	for _, cluster := range clusters {
		if _, ok := uniqueUIDs[cluster.UID]; !ok {
			continue
		}
		names[cluster.UID] = firstNonEmpty(cluster.Name, cluster.DisplayName, cluster.UID)
		if profile := strings.TrimSpace(cluster.ProfileName); profile != "" {
			profiles[cluster.UID] = profile
		}
	}
	return names, profiles, nil
}

func (c *VirtualClusterClient) ListVirtualClusters(ctx context.Context) ([]VirtualCluster, error) {
	return c.listVirtualClusters(ctx)
}

// ListVirtualClustersForEnvironment lists VCs across every configured profile
// by default. An explicit environment narrows the lookup to the matching
// tenant profile so callers can disambiguate duplicate VC names safely.
func (c *VirtualClusterClient) ListVirtualClustersForEnvironment(ctx context.Context, environment string) ([]VirtualCluster, error) {
	environment = strings.TrimSpace(environment)
	if environment == "" {
		return c.listVirtualClustersForProfiles(ctx, c.orderedProfiles())
	}

	profileName, err := c.ResolveProfileName(environment)
	if err != nil {
		return nil, err
	}
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.listVirtualClustersForProfiles(ctx, []clientProfile{profile})
}

// FindExactVirtualCluster resolves a VC name in the current profile through
// the single-resource API. Callers can explicitly fall back to profile lists
// when they intentionally support fuzzy or cross-profile identifiers.
func (c *VirtualClusterClient) FindExactVirtualCluster(ctx context.Context, identifier string) (*VirtualCluster, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("virtual cluster identifier is required")
	}

	if uid := strings.TrimPrefix(identifier, "vc-"); looksLikeClusterUUID(uid) {
		return &VirtualCluster{UID: uid, Name: "vc-" + uid}, nil
	}
	if looksLikeClusterUUID(identifier) {
		return &VirtualCluster{UID: identifier, Name: "vc-" + identifier}, nil
	}

	if profile, ok := c.currentClientProfile(); ok && strings.TrimSpace(profile.Subscription) != "" {
		cluster, err := c.getVirtualClusterWithProfile(ctx, profile, identifier)
		if err == nil {
			cluster.ProfileName = profile.Name
			return cluster, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	resource, err := c.FindResourceByName(ctx, identifier, "virtualClusters")
	if err != nil {
		return nil, err
	}
	return &VirtualCluster{
		UID:         strings.TrimSpace(resource.ID),
		ID:          strings.TrimSpace(resource.RID),
		Name:        firstNonEmpty(strings.TrimSpace(resource.Name), identifier),
		DisplayName: strings.TrimSpace(resource.DisplayName),
		TenantID:    ridSegment(resource.RID, "subscriptions"),
		Region:      firstNonEmpty(strings.TrimSpace(resource.Region), ridSegment(resource.RID, "regions")),
		State:       strings.TrimSpace(resource.State),
		ProfileName: strings.TrimSpace(resource.ProfileName),
	}, nil
}

func (c *VirtualClusterClient) getVirtualClusterWithProfile(ctx context.Context, profile clientProfile, name string) (*VirtualCluster, error) {
	u, _ := url.Parse(profile.BaseURL)
	u.Path = fmt.Sprintf(
		"/compute/ecp/v1/subscriptions/%s/resourceGroups/%s/regions/%s/virtualClusters/%s",
		profile.Subscription,
		profile.ResourceGroup,
		profile.Region,
		strings.TrimSpace(name),
	)

	var cluster VirtualCluster
	if err := c.getJSONWithProfile(ctx, profile, u.String(), &cluster); err != nil {
		return nil, err
	}
	cluster.UID = firstNonEmpty(strings.TrimSpace(cluster.UID), strings.TrimSpace(cluster.ID))
	if cluster.UID == "" {
		return nil, fmt.Errorf("virtual cluster %q detail did not contain uid", name)
	}
	cluster.Name = firstNonEmpty(strings.TrimSpace(cluster.Name), strings.TrimSpace(name))
	cluster.TenantID = firstNonEmpty(strings.TrimSpace(cluster.TenantID), strings.TrimSpace(profile.Subscription))
	cluster.Region = firstNonEmpty(strings.TrimSpace(cluster.Region), strings.TrimSpace(profile.Region))
	return &cluster, nil
}

func (c *VirtualClusterClient) ListCurrentProfileVirtualClusters(ctx context.Context) ([]VirtualCluster, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}
	items, err := c.listVirtualClustersForProfile(ctx, profile)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].ProfileName = profile.Name
	}
	return items, nil
}

func (c *VirtualClusterClient) listVirtualClusters(ctx context.Context) ([]VirtualCluster, error) {
	return c.listVirtualClustersForProfiles(ctx, c.currentEnvironmentProfiles())
}

func (c *VirtualClusterClient) listCurrentEnvironmentVirtualClusters(ctx context.Context) ([]VirtualCluster, error) {
	return c.listVirtualClustersForProfiles(ctx, c.currentEnvironmentProfiles())
}

func (c *VirtualClusterClient) listVirtualClustersForProfiles(ctx context.Context, profiles []clientProfile) ([]VirtualCluster, error) {
	type profileResult struct {
		profile clientProfile
		items   []VirtualCluster
		err     error
	}
	results := make([]profileResult, len(profiles))
	var wg sync.WaitGroup
	for index, profile := range profiles {
		wg.Add(1)
		go func() {
			defer wg.Done()
			items, err := c.listVirtualClustersForProfile(ctx, profile)
			results[index] = profileResult{profile: profile, items: items, err: err}
		}()
	}
	wg.Wait()

	result := make([]VirtualCluster, 0)
	seen := make(map[string]struct{})
	var lastErr error
	success := false
	for _, profileResult := range results {
		if profileResult.err != nil {
			lastErr = profileResult.err
			continue
		}
		success = true
		for _, item := range profileResult.items {
			item.ProfileName = profileResult.profile.Name
			key := firstNonEmpty(strings.TrimSpace(item.UID), strings.TrimSpace(item.Name), strings.TrimSpace(item.DisplayName))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, item)
		}
	}
	if success {
		return result, nil
	}
	return nil, lastErr
}

func ridSegment(rid string, key string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(rid), "/"), "/")
	for index := 0; index+1 < len(parts); index++ {
		if strings.EqualFold(parts[index], key) {
			return strings.TrimSpace(parts[index+1])
		}
	}
	return ""
}

func (c *VirtualClusterClient) listVirtualClustersForProfile(ctx context.Context, profile clientProfile) ([]VirtualCluster, error) {
	skip := 0
	clusters := make([]VirtualCluster, 0)
	for {
		reqURL := c.virtualClusterListURL(profile, skip)
		var payload virtualClusterListResponse
		if err := c.getJSONWithProfile(ctx, profile, reqURL, &payload); err != nil {
			return nil, fmt.Errorf("list virtual clusters: %w", err)
		}

		items := payload.VirtualClusters
		if len(items) == 0 {
			items = payload.Items
		}
		if len(items) == 0 {
			break
		}
		clusters = append(clusters, items...)
		if len(items) < defaultPageLimit {
			break
		}
		skip += len(items)
	}
	return clusters, nil
}

func (c *VirtualClusterClient) virtualClusterListURL(profile clientProfile, skip int) string {
	u, _ := url.Parse(profile.BaseURL)
	u.Path = "/compute/ecp/v1/subscriptions/-/resourceGroups/-/regions/-/virtualClusters"
	query := u.Query()
	query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
	query.Set("skip", fmt.Sprintf("%d", skip))
	u.RawQuery = query.Encode()
	return u.String()
}
