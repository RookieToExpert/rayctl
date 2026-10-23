package platform

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

type workspaceInventoryProfileError struct {
	profile, region string
	cause           error
}

func (e *workspaceInventoryProfileError) Unwrap() error { return e.cause }
func (e *workspaceInventoryProfileError) Error() string {
	scope := e.region + "/" + e.profile
	var status *HTTPStatusError
	if errors.As(e.cause, &status) {
		return fmt.Sprintf("%s workspace 清单查询失败（HTTP %d）", scope, status.StatusCode)
	}
	return fmt.Sprintf("%s workspace 清单查询失败：%v", scope, e.cause)
}

// ListSSPWorkspacesWithStatus retains readable scopes alongside any failed
// profiles/pages. A non-nil error means this inventory is incomplete.
func (c *VirtualClusterClient) ListSSPWorkspacesWithStatus(ctx context.Context, region string) ([]SSPWorkspace, error) {
	region = strings.TrimSpace(region)
	profiles := c.inventoryProfilesForRegion(region)
	if len(profiles) == 0 {
		return nil, fmt.Errorf("no platform profile configured for region %q", region)
	}
	workspaces := map[string]SSPWorkspace{}
	var failures []error
	success := false
	for _, profile := range profiles {
		profileRegion := firstNonEmpty(region, strings.TrimSpace(profile.Region))
		pageToken := "1"
		seenTokens := map[string]bool{}
		count := 0
		fail := func(err error) {
			failures = append(failures, &workspaceInventoryProfileError{profile: profile.Name, region: profileRegion, cause: err})
		}
		for {
			seenTokens[pageToken] = true
			endpoint, _ := url.Parse(profile.BaseURL)
			endpoint.Path = "/rmh/v1/resources:page"
			query := endpoint.Query()
			filter := `resource_type="compute.ssp.v1.workspace"`
			if profileRegion != "" {
				filter += fmt.Sprintf(` AND region="*%s*"`, escapeSSPFilterValue(profileRegion))
			}
			query.Set("filter", filter)
			query.Set("page_size", "200")
			query.Set("page_token", pageToken)
			endpoint.RawQuery = query.Encode()
			var payload storageVolumePageResponse
			if err := c.postJSONWithProfile(ctx, profile, endpoint.String(), map[string]any{}, &payload); err != nil {
				fail(err)
				break
			}
			success = true
			count += len(payload.Resources)
			for _, resource := range payload.Resources {
				name := strings.TrimSpace(resource.Name)
				kind := firstNonEmpty(strings.TrimSpace(resource.Type), strings.TrimSpace(resource.ResourceType))
				if name == "" || !strings.EqualFold(kind, "compute.ssp.v1.workspace") {
					continue
				}
				workspace := sspWorkspaceFromResource(resource, profile.Name)
				workspaces[workspace.Subscription+"|"+name] = workspace
			}
			if payload.TotalSize > 0 && count >= payload.TotalSize {
				break
			}
			next := strings.TrimSpace(payload.NextPageToken)
			if next == "" {
				if payload.TotalSize > count {
					fail(fmt.Errorf("分页未完成：已读取 %d/%d 条，缺少下一页 token", count, payload.TotalSize))
				}
				break
			}
			if seenTokens[next] {
				fail(fmt.Errorf("分页未完成：下一页 token 重复，已读取 %d 条", count))
				break
			}
			pageToken = next
		}
	}
	if !success {
		return nil, errors.Join(failures...)
	}
	result := make([]SSPWorkspace, 0, len(workspaces))
	for _, workspace := range workspaces {
		result = append(result, workspace)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].Subscription < result[j].Subscription
	})
	return result, errors.Join(failures...)
}
