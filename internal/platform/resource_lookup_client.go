package platform

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type StorageVolumeResource struct {
	ID                       string                    `json:"id"`
	RID                      string                    `json:"rid"`
	Name                     string                    `json:"name"`
	DisplayName              string                    `json:"display_name"`
	Type                     string                    `json:"type"`
	ResourceType             string                    `json:"resource_type"`
	State                    string                    `json:"state"`
	Zone                     string                    `json:"zone"`
	Region                   string                    `json:"region"`
	Properties               string                    `json:"properties"`
	CreatorID                string                    `json:"creator_id"`
	OwnerID                  string                    `json:"owner_id"`
	CreateTime               string                    `json:"create_time"`
	UpdateTime               string                    `json:"update_time"`
	ResourceGroupName        string                    `json:"resource_group_name"`
	ResourceGroupDisplayName string                    `json:"resource_group_display_name"`
	RelatedResources         []StorageResourceRelation `json:"related_resources"`
	ProfileName              string                    `json:"-"`
}

type StorageResourceRelation struct {
	RelationType string                `json:"relation_type"`
	RelationRole string                `json:"relation_role"`
	Resource     StorageVolumeResource `json:"resource"`
}

type storageVolumePageResponse struct {
	Resources     []StorageVolumeResource `json:"resources"`
	NextPageToken string                  `json:"next_page_token"`
	TotalSize     int                     `json:"total_size"`
}

func (c *VirtualClusterClient) listStorageVolumeResourcesWithProfile(ctx context.Context, profile clientProfile, zone string) ([]StorageVolumeResource, error) {
	pageToken := "1"
	cursor := newPageCursor(pageToken)
	resources := make([]StorageVolumeResource, 0)
	for {
		u, _ := url.Parse(profile.BaseURL)
		u.Path = "/rmh/v1/resources:page"
		query := u.Query()
		query.Set("filter", fmt.Sprintf(`resource_type="storage.afs.v1.volume" OR resource_type="storage.afs.v2.volume"  AND zone="*%s*"`, zone))
		query.Set("page_size", "200")
		query.Set("page_token", pageToken)
		u.RawQuery = query.Encode()

		var payload storageVolumePageResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return nil, err
		}
		resources = append(resources, payload.Resources...)
		if !cursor.advance(payload.NextPageToken) {
			break
		}
		pageToken = payload.NextPageToken
	}
	return resources, nil
}

func (c *VirtualClusterClient) ListStorageVolumeResources(ctx context.Context, zone string) ([]StorageVolumeResource, error) {
	resources := make([]StorageVolumeResource, 0)
	seen := make(map[string]struct{})
	var lastErr error
	success := false
	queries := queryProfiles(ctx, c.orderedProfiles(), func(ctx context.Context, profile clientProfile) ([]StorageVolumeResource, error) {
		return c.listStorageVolumeResourcesWithProfile(ctx, profile, zone)
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, query := range queries {
		if query.err != nil {
			lastErr = query.err
			continue
		}
		success = true
		for _, item := range query.value {
			item.ProfileName = query.profile.Name
			key := firstNonEmpty(strings.TrimSpace(item.ID), strings.TrimSpace(item.RID), strings.TrimSpace(item.Name))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			resources = append(resources, item)
		}
	}
	if success {
		return resources, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) FindStorageVolumeResourceByUID(ctx context.Context, uid string) (*StorageVolumeResource, error) {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return nil, fmt.Errorf("storage volume uid is required")
	}

	for _, candidate := range uidSearchCandidates(uid) {
		resource, err := c.findStorageVolumeResourceByFragment(ctx, candidate)
		if err == nil && resource != nil {
			return resource, nil
		}
	}
	return nil, fmt.Errorf("storage volume resource with uid %q not found", uid)
}

func (c *VirtualClusterClient) FindStorageVolumeResource(ctx context.Context, identifier string) (*StorageVolumeResource, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("storage volume identifier is required")
	}

	if strings.HasPrefix(strings.ToLower(identifier), "afs-") {
		// AFS names are canonical resource identifiers. Do not retry the same
		// platform search through UID candidates and then by name again.
		return c.findStorageVolumeResourceByFieldFragment(ctx, "name", identifier)
	}
	if resource, err := c.FindStorageVolumeResourceByUID(ctx, identifier); err == nil && resource != nil {
		return resource, nil
	}
	return c.findStorageVolumeResourceByFieldFragment(ctx, "name", identifier)
}

func (c *VirtualClusterClient) FindResourceByName(ctx context.Context, name string, resourceKinds ...string) (*StorageVolumeResource, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("resource name is required")
	}

	kindSet := make(map[string]struct{}, len(resourceKinds))
	for _, kind := range resourceKinds {
		kind = strings.Trim(strings.TrimSpace(kind), "/")
		if kind != "" {
			kindSet[kind] = struct{}{}
		}
	}

	type profileResult struct {
		resource *StorageVolumeResource
		err      error
	}
	queryCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	profiles := c.orderedProfiles()
	resultCh := make(chan profileResult, len(profiles))
	for _, profile := range profiles {
		profile := profile
		go func() {
			profileCtx, profileCancel := context.WithTimeout(queryCtx, 2*time.Second)
			defer profileCancel()
			resource, err := c.findResourceByNameWithProfile(profileCtx, profile, name, kindSet)
			resultCh <- profileResult{resource: resource, err: err}
		}()
	}

	var lastErr error
	for range profiles {
		result := <-resultCh
		if result.resource != nil {
			cancel()
			return result.resource, nil
		}
		if result.err != nil {
			lastErr = result.err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	if len(resourceKinds) > 0 {
		return nil, fmt.Errorf("resource %q with kind %q not found", name, strings.Join(resourceKinds, ","))
	}
	return nil, fmt.Errorf("resource %q not found", name)
}

func (c *VirtualClusterClient) findResourceByNameWithProfile(ctx context.Context, profile clientProfile, name string, kindSet map[string]struct{}) (*StorageVolumeResource, error) {
	pageToken := "1"
	cursor := newPageCursor(pageToken)
	for {
		u, _ := url.Parse(profile.BaseURL)
		u.Path = "/rmh/v1/resources:page"
		query := u.Query()
		query.Set("filter", fmt.Sprintf(`name="%s"`, name))
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("page_token", pageToken)
		u.RawQuery = query.Encode()

		var payload storageVolumePageResponse
		if err := c.postJSONWithProfile(ctx, profile, u.String(), map[string]any{}, &payload); err != nil {
			return nil, err
		}

		for i := range payload.Resources {
			resource := &payload.Resources[i]
			resource.ProfileName = profile.Name
			if !strings.EqualFold(strings.TrimSpace(resource.Name), name) {
				continue
			}
			if len(kindSet) > 0 && !resourceRIDHasAnyKind(resource.RID, kindSet) {
				continue
			}
			return resource, nil
		}

		nextPageToken := strings.TrimSpace(payload.NextPageToken)
		if len(payload.Resources) == 0 || !cursor.advance(nextPageToken) {
			break
		}
		pageToken = nextPageToken
	}
	return nil, nil
}

func (c *VirtualClusterClient) FindResourceByUID(ctx context.Context, uid string, resourceKinds ...string) (*StorageVolumeResource, error) {
	return c.findResourceByUIDWithProfiles(ctx, c.orderedProfiles(), uid, resourceKinds...)
}

func (c *VirtualClusterClient) FindResourceByUIDForProfile(ctx context.Context, profileName string, uid string, resourceKinds ...string) (*StorageVolumeResource, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.findResourceByUIDWithProfiles(ctx, []clientProfile{profile}, uid, resourceKinds...)
}

func (c *VirtualClusterClient) findResourceByUIDWithProfiles(ctx context.Context, profiles []clientProfile, uid string, resourceKinds ...string) (*StorageVolumeResource, error) {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return nil, fmt.Errorf("resource uid is required")
	}

	kindSet := make(map[string]struct{}, len(resourceKinds))
	for _, kind := range resourceKinds {
		kind = strings.Trim(strings.TrimSpace(kind), "/")
		if kind != "" {
			kindSet[kind] = struct{}{}
		}
	}

	var lastErr error
	for _, profile := range profiles {
		pageToken := "1"
		cursor := newPageCursor(pageToken)
		for {
			u, _ := url.Parse(profile.BaseURL)
			u.Path = "/rmh/v1/resources:page"
			query := u.Query()
			query.Set("filter", fmt.Sprintf(`uid="*%s*"`, uid))
			query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
			query.Set("page_token", pageToken)
			u.RawQuery = query.Encode()

			var payload storageVolumePageResponse
			if err := c.postJSONWithProfile(ctx, profile, u.String(), map[string]any{}, &payload); err != nil {
				lastErr = err
				break
			}
			for i := range payload.Resources {
				resource := &payload.Resources[i]
				resource.ProfileName = profile.Name
				if !strings.EqualFold(strings.TrimSpace(resource.ID), uid) && !strings.Contains(strings.TrimSpace(resource.ID), uid) {
					continue
				}
				if len(kindSet) > 0 && !resourceRIDHasAnyKind(resource.RID, kindSet) {
					continue
				}
				return resource, nil
			}

			nextPageToken := strings.TrimSpace(payload.NextPageToken)
			if len(payload.Resources) == 0 || !cursor.advance(nextPageToken) {
				break
			}
			pageToken = nextPageToken
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	if len(resourceKinds) > 0 {
		return nil, fmt.Errorf("resource uid %q with kind %q not found", uid, strings.Join(resourceKinds, ","))
	}
	return nil, fmt.Errorf("resource uid %q not found", uid)
}

func resourceRIDHasAnyKind(rid string, kinds map[string]struct{}) bool {
	ridParts := strings.Split(strings.Trim(rid, "/"), "/")
	for _, part := range ridParts {
		if _, ok := kinds[part]; ok {
			return true
		}
	}
	return false
}

func (c *VirtualClusterClient) findStorageVolumeResourceByFragment(ctx context.Context, fragment string) (*StorageVolumeResource, error) {
	return c.findStorageVolumeResourceByFieldFragment(ctx, "uid", fragment)
}

func (c *VirtualClusterClient) findStorageVolumeResourceByFieldFragment(ctx context.Context, field string, fragment string) (*StorageVolumeResource, error) {
	fragment = strings.TrimSpace(fragment)
	if fragment == "" {
		return nil, fmt.Errorf("storage volume fragment is required")
	}

	type profileResult struct {
		resource *StorageVolumeResource
		err      error
	}
	queryCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	profiles := c.orderedProfiles()
	resultCh := make(chan profileResult, len(profiles))
	for _, profile := range profiles {
		profile := profile
		go func() {
			profileCtx, profileCancel := context.WithTimeout(queryCtx, 2*time.Second)
			defer profileCancel()
			resource, err := c.findStorageVolumeResourceByFieldFragmentWithProfile(profileCtx, profile, field, fragment)
			resultCh <- profileResult{resource: resource, err: err}
		}()
	}

	var lastErr error
	for range profiles {
		result := <-resultCh
		if result.resource != nil {
			cancel()
			return result.resource, nil
		}
		if result.err != nil {
			lastErr = result.err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("storage volume resource with fragment %q not found", fragment)
}

func (c *VirtualClusterClient) findStorageVolumeResourceByFieldFragmentWithProfile(ctx context.Context, profile clientProfile, field string, fragment string) (*StorageVolumeResource, error) {
	pageToken := "1"
	cursor := newPageCursor(pageToken)
	for {
		u, _ := url.Parse(profile.BaseURL)
		u.Path = "/rmh/v1/resources:page"
		query := u.Query()

		filter := fmt.Sprintf(`(resource_type="storage.afs.v1.volume" OR resource_type="storage.afs.v2.volume") AND %s="*%s*"`, field, fragment)
		query.Set("filter", filter)
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("page_token", pageToken)
		u.RawQuery = query.Encode()

		var payload storageVolumePageResponse
		if err := c.postJSONWithProfile(ctx, profile, u.String(), map[string]any{}, &payload); err != nil {
			return nil, err
		}

		for i := range payload.Resources {
			resource := &payload.Resources[i]
			resource.ProfileName = profile.Name
			if field == "name" {
				if strings.EqualFold(strings.TrimSpace(resource.Name), fragment) || strings.Contains(strings.TrimSpace(resource.Name), fragment) {
					return resource, nil
				}
				continue
			}
			if strings.EqualFold(strings.TrimSpace(resource.ID), fragment) || strings.Contains(strings.TrimSpace(resource.ID), fragment) {
				return resource, nil
			}
		}

		nextPageToken := strings.TrimSpace(payload.NextPageToken)
		if len(payload.Resources) == 0 || !cursor.advance(nextPageToken) {
			break
		}
		pageToken = nextPageToken
	}
	return nil, nil
}

func uidSearchCandidates(uid string) []string {
	parts := strings.Split(strings.TrimSpace(uid), "-")
	candidates := make([]string, 0, 4)
	seen := make(map[string]struct{})
	appendCandidate := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		candidates = append(candidates, value)
	}

	appendCandidate(uid)
	if len(parts) >= 3 {
		appendCandidate(strings.Join(parts[2:], "-"))
	}
	if len(parts) >= 4 {
		appendCandidate(strings.Join(parts[3:], "-"))
	}
	if len(parts) >= 2 {
		appendCandidate(strings.Join(parts[len(parts)-2:], "-"))
	}
	return candidates
}
