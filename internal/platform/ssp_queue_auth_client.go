package platform

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// FindSSPQueueAuthResource retains the owning profile and authoritative RID
// instead of inferring an IAM scope from a queue/workspace naming convention.
func (c *VirtualClusterClient) FindSSPQueueAuthResource(ctx context.Context, identifier string) (*StorageVolumeResource, error) {
	identifier = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(identifier), "ssp-"))
	if identifier == "" {
		return nil, fmt.Errorf("queue name or UID is required")
	}
	profiles := c.currentEnvironmentProfiles()
	if len(profiles) == 0 {
		return nil, fmt.Errorf("no platform profile available for queue auth lookup")
	}
	resources, firstErr := c.queueAuthResourcesForProfile(ctx, profiles[0], identifier)
	if firstErr == nil {
		if exact := matchQueueAuthResources(resources, identifier, true); len(exact) > 0 {
			return selectQueueAuthResource(exact, identifier)
		}
	}
	errs := make([]error, 0)
	if firstErr != nil {
		errs = append(errs, fmt.Errorf("profile %q: %w", profiles[0].Name, firstErr))
	}
	for _, result := range queryProfiles(ctx, profiles[1:], func(ctx context.Context, profile clientProfile) ([]StorageVolumeResource, error) {
		return c.queueAuthResourcesForProfile(ctx, profile, identifier)
	}) {
		if result.err != nil {
			errs = append(errs, fmt.Errorf("profile %q: %w", result.profile.Name, result.err))
		}
		resources = append(resources, result.value...)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	matches := matchQueueAuthResources(resources, identifier, true)
	if len(matches) == 0 {
		matches = matchQueueAuthResources(resources, identifier, false)
	}
	return selectQueueAuthResource(matches, identifier)
}

func (c *VirtualClusterClient) queueAuthResourcesForProfile(ctx context.Context, profile clientProfile, identifier string) ([]StorageVolumeResource, error) {
	filter := `resource_type="compute.ssp.v1.queue"`
	// RMH's name filter cannot resolve custom queue names from their UID.
	// Read only paginated queue metadata and match full UIDs locally.
	if !looksLikeClusterUUID(identifier) {
		filter += fmt.Sprintf(` AND name="*%s*"`, escapeSSPFilterValue(identifier))
	}
	endpoint, err := url.Parse(profile.BaseURL)
	if err != nil {
		return nil, err
	}
	endpoint.Path = "/rmh/v1/resources:page"
	resources := make([]StorageVolumeResource, 0)
	appendQueue := func(resource StorageVolumeResource) {
		if !strings.EqualFold(firstNonEmpty(resource.Type, resource.ResourceType), "compute.ssp.v1.queue") {
			return
		}
		region := firstNonEmpty(resource.Region, ridSegment(resource.RID, "regions"))
		if profile.Region != "" && region != "" && !strings.EqualFold(profile.Region, region) {
			return
		}
		resource.ProfileName = profile.Name
		resources = append(resources, resource)
	}
	pageToken := "1"
	cursor := newPageCursor(pageToken)
	read := 0
	for {
		query := endpoint.Query()
		query.Set("filter", filter)
		query.Set("page_size", "100")
		query.Set("page_token", pageToken)
		endpoint.RawQuery = query.Encode()
		var payload storageVolumePageResponse
		if err := c.postJSONWithProfile(ctx, profile, endpoint.String(), map[string]any{}, &payload); err != nil {
			return nil, err
		}
		for _, resource := range payload.Resources {
			appendQueue(resource)
			for _, relation := range resource.RelatedResources {
				appendQueue(relation.Resource)
			}
		}
		read += len(payload.Resources)
		next := strings.TrimSpace(payload.NextPageToken)
		if next == "" || len(payload.Resources) == 0 || (payload.TotalSize > 0 && read >= payload.TotalSize) {
			return resources, nil
		}
		if !cursor.advance(next) {
			return nil, fmt.Errorf("incomplete queue lookup: invalid pagination token %q", next)
		}
		pageToken = next
	}
}

func matchQueueAuthResources(resources []StorageVolumeResource, identifier string, exact bool) []StorageVolumeResource {
	identifier = strings.ToLower(identifier)
	matches := make([]StorageVolumeResource, 0)
	seen := make(map[string]bool)
	for _, resource := range resources {
		name, uid := strings.ToLower(strings.TrimSpace(resource.Name)), strings.ToLower(strings.TrimSpace(resource.ID))
		matched := name == identifier || uid == identifier
		if looksLikeClusterUUID(identifier) {
			matched = uid == identifier
		} else if !exact {
			matched = strings.Contains(name, identifier)
		}
		identity := firstNonEmpty(strings.TrimSpace(resource.RID), resource.ProfileName+"|"+uid+"|"+name)
		if !matched || seen[identity] {
			continue
		}
		seen[identity] = true
		matches = append(matches, resource)
	}
	return matches
}

func selectQueueAuthResource(matches []StorageVolumeResource, identifier string) (*StorageVolumeResource, error) {
	if len(matches) == 0 {
		return nil, fmt.Errorf("SSP queue %q not found in the current environment", identifier)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("SSP queue %q matched multiple queues; use a full name or UID, or select the owning profile", identifier)
	}
	resource := matches[0]
	if strings.TrimSpace(resource.RID) == "" {
		return nil, fmt.Errorf("SSP queue %q has no verified resource scope", resource.Name)
	}
	return &resource, nil
}
