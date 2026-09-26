package platform

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type AIComputeNode struct {
	ID           string `json:"id"`
	UID          string `json:"uid"`
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	ResourceType string `json:"resource_type"`
	TenantID     string `json:"tenant_id"`
	Zone         string `json:"zone"`
	State        string `json:"state"`
	Properties   struct {
		MachineType        string `json:"machine_type"`
		Model              string `json:"model"`
		AcceleratorModel   string `json:"accelerator_model"`
		AcceleratorType    string `json:"accelerator_type"`
		VirtualClusterName string `json:"virtual_cluster_name"`
		NodePoolName       string `json:"node_pool_name"`
		HostIP             string `json:"host_ip"`
		HostName           string `json:"host_name"`
	} `json:"properties"`
	ProfileName string `json:"-"`
}

type VirtualClusterNode struct {
	AIComputeNode
	Kind string `json:"-"`
}

type virtualClusterNodeListResponse struct {
	AIComputeNodes []AIComputeNode `json:"ai_compute_nodes"`
	BareMetalNodes []AIComputeNode `json:"bare_metal_nodes"`
	TotalSize      int             `json:"total_size"`
	NextPageToken  string          `json:"next_page_token"`
}

type VirtualClusterResourceAmount struct {
	CPU          string `json:"cpu"`
	Memory       string `json:"memory"`
	Device       string `json:"device"`
	LocalStorage string `json:"local_storage"`
}

type VirtualClusterNodeResourceUsage struct {
	UID         string `json:"uid"`
	MachineName string `json:"machine_name"`
	Usage       struct {
		Total     VirtualClusterResourceAmount `json:"total"`
		Allocated VirtualClusterResourceAmount `json:"allocated"`
		Available VirtualClusterResourceAmount `json:"available"`
		Releasing VirtualClusterResourceAmount `json:"releasing"`
	} `json:"usage"`
}

type virtualClusterNodeResourceUsageResponse struct {
	NodeUsages []VirtualClusterNodeResourceUsage `json:"node_usages"`
}

type virtualClusterNodeRemoveResponse struct {
	AIComputeNodes []AIComputeNode `json:"ai_compute_nodes"`
}

type aiComputeNodeListResponse struct {
	AIComputeNodes []AIComputeNode `json:"ai_compute_nodes"`
}

func (c *VirtualClusterClient) listAIComputeNodesWithProfile(ctx context.Context, profile clientProfile) ([]AIComputeNode, error) {
	return c.listAIComputeNodesWithProfileFilter(ctx, profile, "")
}

func (c *VirtualClusterClient) listAIComputeNodesWithProfileFilter(ctx context.Context, profile clientProfile, filter string) ([]AIComputeNode, error) {
	skip := 0
	result := make([]AIComputeNode, 0)
	for {
		u, _ := url.Parse(profile.BaseURL)
		u.Path = "/compute/ecp/v1/subscriptions/-/resourceGroups/-/zones/-/aiComputeNodes"
		query := u.Query()
		if strings.TrimSpace(filter) != "" {
			query.Set("filter", filter)
		}
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("skip", fmt.Sprintf("%d", skip))
		u.RawQuery = query.Encode()

		var payload aiComputeNodeListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return nil, err
		}
		if len(payload.AIComputeNodes) == 0 {
			break
		}
		for i := range payload.AIComputeNodes {
			payload.AIComputeNodes[i].ProfileName = profile.Name
		}
		result = append(result, payload.AIComputeNodes...)
		if len(payload.AIComputeNodes) < defaultPageLimit {
			break
		}
		skip += len(payload.AIComputeNodes)
	}
	return result, nil
}

func (c *VirtualClusterClient) ListAIComputeNodes(ctx context.Context) ([]AIComputeNode, error) {
	result := make([]AIComputeNode, 0)
	seen := make(map[string]struct{})
	var lastErr error
	success := false
	queries := queryProfiles(ctx, c.orderedProfiles(), c.listAIComputeNodesWithProfile)
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
			key := firstNonEmpty(strings.TrimSpace(item.UID), strings.TrimSpace(item.ID), strings.TrimSpace(item.Properties.HostName), strings.TrimSpace(item.Properties.HostIP))
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
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no platform profile available")
}

// FindAIComputeNodes performs an exact lookup without loading the complete ECP
// node inventory. The current environment is searched first so the returned
// host belongs to the Kubernetes cluster selected by the caller.
func (c *VirtualClusterClient) FindAIComputeNodes(ctx context.Context, identifier string) ([]AIComputeNode, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, nil
	}

	escaped := escapeSSPFilterValue(identifier)
	filter := fmt.Sprintf(`name="%s" OR display_name="%s"`, escaped, escaped)
	if looksLikeClusterUUID(identifier) {
		filter = fmt.Sprintf(`uid="%s"`, escaped)
	}

	var lastErr error
	succeeded := false
	for _, profile := range c.currentEnvironmentProfiles() {
		items, err := c.listAIComputeNodesWithProfileFilter(ctx, profile, filter)
		if err != nil {
			lastErr = err
			continue
		}
		succeeded = true
		if len(items) > 0 {
			return items, nil
		}
	}
	if succeeded {
		return nil, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no platform profile available")
}

func (c *VirtualClusterClient) ListAIComputeNodesForProfile(ctx context.Context, profileName string) ([]AIComputeNode, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.listAIComputeNodesWithProfile(ctx, profile)
}

func (c *VirtualClusterClient) ListAssignableAIComputeNodesForProfile(ctx context.Context, profileName string) ([]AIComputeNode, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.listAIComputeNodesWithProfileFilter(ctx, profile, `(state="RUNNING" OR state="ERROR")`)
}

func (c *VirtualClusterClient) ListVirtualClusterNodes(
	ctx context.Context,
	profileName string,
	subscriptionID string,
	region string,
	virtualClusterName string,
) ([]VirtualClusterNode, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	subscriptionID = firstNonEmpty(strings.TrimSpace(subscriptionID), strings.TrimSpace(profile.Subscription))
	region = firstNonEmpty(strings.TrimSpace(region), strings.TrimSpace(profile.Region))
	virtualClusterName = strings.TrimSpace(virtualClusterName)
	if subscriptionID == "" {
		return nil, fmt.Errorf("subscription id is required for virtual cluster node lookup")
	}
	if virtualClusterName == "" {
		return nil, fmt.Errorf("virtual cluster name is required")
	}

	const pageSize = 100
	result := make([]VirtualClusterNode, 0)
	for skip := 0; ; {
		u, _ := url.Parse(profile.BaseURL)
		u.Path = fmt.Sprintf(
			"/compute/ecp/v1/subscriptions/%s/resourceGroups/%s/regions/%s/virtualClusters/%s/nodePools/-/nodes",
			subscriptionID,
			profile.ResourceGroup,
			region,
			virtualClusterName,
		)
		query := u.Query()
		query.Set("page_size", strconv.Itoa(pageSize))
		query.Set("filter", "")
		query.Set("skip", strconv.Itoa(skip))
		u.RawQuery = query.Encode()

		var payload virtualClusterNodeListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return nil, err
		}
		for _, node := range payload.AIComputeNodes {
			node.ProfileName = profile.Name
			result = append(result, VirtualClusterNode{AIComputeNode: node, Kind: "ACN"})
		}
		for _, node := range payload.BareMetalNodes {
			node.ProfileName = profile.Name
			result = append(result, VirtualClusterNode{AIComputeNode: node, Kind: "BMS"})
		}

		pageCount := len(payload.AIComputeNodes) + len(payload.BareMetalNodes)
		if pageCount == 0 || (payload.TotalSize > 0 && len(result) >= payload.TotalSize) {
			break
		}
		if pageCount < pageSize && strings.TrimSpace(payload.NextPageToken) == "" {
			break
		}
		skip += pageCount
	}
	return result, nil
}

func (c *VirtualClusterClient) BatchGetVirtualClusterNodeResourceUsages(
	ctx context.Context,
	profileName string,
	subscriptionID string,
	region string,
	virtualClusterName string,
	virtualClusterUID string,
	nodeUIDs []string,
) ([]VirtualClusterNodeResourceUsage, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	subscriptionID = firstNonEmpty(strings.TrimSpace(subscriptionID), strings.TrimSpace(profile.Subscription))
	region = firstNonEmpty(strings.TrimSpace(region), strings.TrimSpace(profile.Region))
	if subscriptionID == "" {
		return nil, fmt.Errorf("subscription id is required for virtual cluster resource usage")
	}

	u, _ := url.Parse(profile.BaseURL)
	u.Path = fmt.Sprintf(
		"/compute/ecp/v1/subscriptions/%s/resourceGroups/%s/regions/%s/virtualClusters/%s/nodePools/-/nodes/-/resourceUsages:batchGet",
		subscriptionID,
		profile.ResourceGroup,
		region,
		strings.TrimSpace(virtualClusterName),
	)
	query := u.Query()
	for _, nodeUID := range nodeUIDs {
		if nodeUID = strings.TrimSpace(nodeUID); nodeUID != "" {
			query.Add("node_uids", nodeUID)
		}
	}
	if len(query["node_uids"]) == 0 {
		return nil, nil
	}
	query.Set("request_id", strings.TrimPrefix(strings.TrimSpace(virtualClusterUID), "vc-"))
	u.RawQuery = query.Encode()

	var result virtualClusterNodeResourceUsageResponse
	if err := c.getJSONWithProfile(ctx, profile, u.String(), &result); err != nil {
		return nil, err
	}
	return result.NodeUsages, nil
}

func (c *VirtualClusterClient) RemoveAIComputeNodesFromVirtualCluster(
	ctx context.Context,
	profileName string,
	subscriptionID string,
	region string,
	virtualClusterName string,
	acnUIDs []string,
) ([]AIComputeNode, error) {
	reqURL, payload, profile, err := c.buildAIComputeNodeRemoveRequest(profileName, subscriptionID, region, virtualClusterName, acnUIDs)
	if err != nil {
		return nil, err
	}

	var response virtualClusterNodeRemoveResponse
	if err := c.postJSONWithProfile(ctx, profile, reqURL, payload, &response); err != nil {
		return nil, err
	}
	for i := range response.AIComputeNodes {
		response.AIComputeNodes[i].ProfileName = profile.Name
	}
	return response.AIComputeNodes, nil
}

func (c *VirtualClusterClient) AddAIComputeNodesToVirtualCluster(
	ctx context.Context,
	profileName string,
	subscriptionID string,
	region string,
	virtualClusterName string,
	acnUIDs []string,
) ([]AIComputeNode, error) {
	reqURL, payload, profile, err := c.buildAIComputeNodeMutationRequest(profileName, subscriptionID, region, virtualClusterName, acnUIDs, "add")
	if err != nil {
		return nil, err
	}

	var response virtualClusterNodeRemoveResponse
	if err := c.postJSONWithProfile(ctx, profile, reqURL, payload, &response); err != nil {
		return nil, err
	}
	for i := range response.AIComputeNodes {
		response.AIComputeNodes[i].ProfileName = profile.Name
	}
	return response.AIComputeNodes, nil
}

func (c *VirtualClusterClient) BuildAIComputeNodeAddRequest(
	profileName string,
	subscriptionID string,
	region string,
	virtualClusterName string,
	acnUIDs []string,
) (string, map[string]any, error) {
	reqURL, payload, _, err := c.buildAIComputeNodeMutationRequest(profileName, subscriptionID, region, virtualClusterName, acnUIDs, "add")
	return reqURL, payload, err
}

func (c *VirtualClusterClient) BuildAIComputeNodeRemoveRequest(
	profileName string,
	subscriptionID string,
	region string,
	virtualClusterName string,
	acnUIDs []string,
) (string, map[string]any, error) {
	reqURL, payload, _, err := c.buildAIComputeNodeRemoveRequest(profileName, subscriptionID, region, virtualClusterName, acnUIDs)
	return reqURL, payload, err
}

func (c *VirtualClusterClient) buildAIComputeNodeRemoveRequest(
	profileName string,
	subscriptionID string,
	region string,
	virtualClusterName string,
	acnUIDs []string,
) (string, map[string]any, clientProfile, error) {
	return c.buildAIComputeNodeMutationRequest(profileName, subscriptionID, region, virtualClusterName, acnUIDs, "remove")
}

func (c *VirtualClusterClient) buildAIComputeNodeMutationRequest(
	profileName string,
	subscriptionID string,
	region string,
	virtualClusterName string,
	acnUIDs []string,
	action string,
) (string, map[string]any, clientProfile, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return "", nil, clientProfile{}, fmt.Errorf("platform profile %q not found", profileName)
	}
	subscriptionID = firstNonEmpty(strings.TrimSpace(subscriptionID), strings.TrimSpace(profile.Subscription))
	region = firstNonEmpty(strings.TrimSpace(region), strings.TrimSpace(profile.Region))
	virtualClusterName = strings.TrimSpace(virtualClusterName)
	if subscriptionID == "" {
		return "", nil, clientProfile{}, fmt.Errorf("subscription id is required for virtual cluster node %s", action)
	}
	if virtualClusterName == "" {
		return "", nil, clientProfile{}, fmt.Errorf("virtual cluster name is required")
	}
	if len(acnUIDs) == 0 {
		return "", nil, clientProfile{}, fmt.Errorf("at least one acn uid is required")
	}
	if action != "add" && action != "remove" {
		return "", nil, clientProfile{}, fmt.Errorf("unsupported virtual cluster node action %q", action)
	}

	u, _ := url.Parse(profile.BaseURL)
	u.Path = fmt.Sprintf(
		"/compute/ecp/v1/subscriptions/%s/resourceGroups/%s/regions/%s/virtualClusters/%s/AIComputeNodes:%s",
		subscriptionID,
		profile.ResourceGroup,
		region,
		virtualClusterName,
		action,
	)
	payload := map[string]any{
		"subscription_name":    subscriptionID,
		"resource_group_name":  profile.ResourceGroup,
		"region":               region,
		"virtual_cluster_name": virtualClusterName,
		"acn_uids":             acnUIDs,
	}
	return u.String(), payload, profile, nil
}
