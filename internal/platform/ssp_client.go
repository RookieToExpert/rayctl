package platform

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// SSPTrainingJob is the platform representation returned by the AIT service.
type SSPTrainingJob struct {
	ID                string `json:"id"`
	UID               string `json:"uid"`
	Name              string `json:"name"`
	DisplayName       string `json:"display_name"`
	Namespace         string `json:"namespace"`
	WorkspaceName     string `json:"workspace_name"`
	Region            string `json:"region"`
	ResourceGroupName string `json:"resource_group_name"`
	SubscriptionName  string `json:"subscription_name"`
	ProfileName       string `json:"-"`
	Ownership         struct {
		CreatorID   string `json:"creator_id"`
		CreatorName string `json:"creator_name"`
		OwnerID     string `json:"owner_id"`
		TenantID    string `json:"tenant_id"`
	} `json:"ownership"`
	Status SSPTrainingJobStatus `json:"status"`
	Spec   SSPTrainingJobSpec   `json:"spec"`
}

type SSPTrainingJobStatus struct {
	State      string           `json:"state"`
	CreateTime string           `json:"create_time"`
	StartTime  string           `json:"start_time"`
	EndTime    string           `json:"end_time"`
	UpdateTime string           `json:"update_time"`
	Conditions []map[string]any `json:"conditions"`
}

type SSPTrainingJobSpec struct {
	Framework    string                      `json:"framework"`
	Priority     string                      `json:"priority"`
	VolumeMounts []SSPTrainingJobVolumeMount `json:"volume_mounts"`
	Queue        struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
		UID  string `json:"uid"`
	} `json:"queue"`
	VCJob struct {
		Tasks []SSPTrainingJobTask `json:"tasks"`
	} `json:"vc_job"`
}

type SSPTrainingJobVolumeMount struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	MountPath  string `json:"mount_path"`
	Endpoint   string `json:"endpoint"`
	Zone       string `json:"zone"`
	Region     string `json:"region"`
	AccessMode string `json:"access_mode"`
}

type SSPTrainingJobTask struct {
	Name         string `json:"name"`
	Role         string `json:"role"`
	Replicas     int    `json:"replicas"`
	Image        string `json:"image"`
	ImageType    string `json:"image_type"`
	Command      any    `json:"command"`
	Args         any    `json:"args"`
	ResourceSpec struct {
		MachineTypes              []string `json:"machine_types"`
		CPUCount                  any      `json:"cpu_count"`
		MemoryGiB                 any      `json:"memory_gib"`
		AccelerateDeviceCount     any      `json:"accelerate_device_count"`
		AccelerateDeviceModel     string   `json:"accelerate_device_model"`
		AccelerateDeviceMemoryGiB any      `json:"accelerate_device_memory_gib"`
		RDMAName                  string   `json:"rdma_name"`
		SharedMemorySizeGiB       any      `json:"shm_size_gib"`
	} `json:"resource_spec"`
}

type sspTrainingJobListResponse struct {
	TrainingJobs  []SSPTrainingJob `json:"training_jobs"`
	TotalSize     int              `json:"total_size"`
	NextPageToken string           `json:"next_page_token"`
}

type SSPWorkspace struct {
	Name          string
	Subscription  string
	ResourceGroup string
	Region        string
	ProfileName   string
}

// ListSSPWorkspaces returns SSP workspaces from RMH without relying on
// workspace namespaces being visible through the current kubeconfig.
func (c *VirtualClusterClient) ListSSPWorkspaces(ctx context.Context, region string) ([]SSPWorkspace, error) {
	region = strings.TrimSpace(region)
	if region == "" {
		return nil, fmt.Errorf("region is required")
	}

	profiles := c.profilesForRegion(region)
	if len(profiles) == 0 {
		return nil, fmt.Errorf("no platform profile configured for region %q", region)
	}

	workspaces := make(map[string]SSPWorkspace)
	var lastErr error
	success := false
	for _, profile := range profiles {
		pageToken := "1"
		seenTokens := make(map[string]struct{})
		profileResultCount := 0
		for {
			if _, ok := seenTokens[pageToken]; ok {
				break
			}
			seenTokens[pageToken] = struct{}{}

			endpoint, _ := url.Parse(profile.BaseURL)
			endpoint.Path = "/rmh/v1/resources:page"
			query := endpoint.Query()
			query.Set("filter", fmt.Sprintf(`resource_type="compute.ssp.v1.workspace" AND region="*%s*"`, escapeSSPFilterValue(region)))
			query.Set("page_size", "200")
			query.Set("page_token", pageToken)
			endpoint.RawQuery = query.Encode()

			var payload storageVolumePageResponse
			if err := c.postJSONWithProfile(ctx, profile, endpoint.String(), map[string]any{}, &payload); err != nil {
				lastErr = err
				break
			}
			success = true
			profileResultCount += len(payload.Resources)
			for _, resource := range payload.Resources {
				name := strings.TrimSpace(resource.Name)
				resourceType := firstNonEmpty(strings.TrimSpace(resource.Type), strings.TrimSpace(resource.ResourceType))
				if name != "" && strings.EqualFold(resourceType, "compute.ssp.v1.workspace") {
					workspace := sspWorkspaceFromResource(resource, profile.Name)
					workspaces[firstNonEmpty(workspace.Subscription+"|"+name, name)] = workspace
				}
			}

			nextPageToken := strings.TrimSpace(payload.NextPageToken)
			if nextPageToken == "" || nextPageToken == pageToken || len(payload.Resources) < 200 ||
				(payload.TotalSize > 0 && profileResultCount >= payload.TotalSize) {
				break
			}
			pageToken = nextPageToken
		}
	}
	if !success {
		return nil, lastErr
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
	return result, nil
}

func sspWorkspaceFromResource(resource StorageVolumeResource, profileName string) SSPWorkspace {
	workspace := SSPWorkspace{
		Name:        strings.TrimSpace(resource.Name),
		Region:      strings.TrimSpace(resource.Region),
		ProfileName: strings.TrimSpace(profileName),
	}
	parts := strings.Split(strings.Trim(strings.TrimSpace(resource.RID), "/"), "/")
	for i := 0; i+1 < len(parts); i += 2 {
		switch parts[i] {
		case "subscriptions":
			workspace.Subscription = parts[i+1]
		case "resourceGroups":
			workspace.ResourceGroup = parts[i+1]
		case "regions":
			workspace.Region = parts[i+1]
		case "workspaces":
			workspace.Name = parts[i+1]
		}
	}
	return workspace
}

// ConfiguredSubscriptionForRegion returns the first configured subscription
// for the requested region. Callers can fall back to Kubernetes labels when it
// is intentionally omitted from platform.json.
func (c *VirtualClusterClient) ConfiguredSubscriptionForRegion(region string) string {
	for _, profile := range c.profilesForRegion(region) {
		if value := strings.TrimSpace(profile.Subscription); value != "" {
			return value
		}
	}
	return ""
}

func (c *VirtualClusterClient) FindSSPTrainingJobs(ctx context.Context, subscription string, region string, workspace string, identifier string) ([]SSPTrainingJob, error) {
	subscription = strings.TrimSpace(subscription)
	region = strings.TrimSpace(region)
	workspace = strings.TrimSpace(workspace)
	identifier = strings.TrimSpace(identifier)
	if subscription == "" {
		return nil, fmt.Errorf("subscription id is required")
	}
	if region == "" {
		return nil, fmt.Errorf("region is required")
	}
	if workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}
	if identifier == "" {
		return nil, fmt.Errorf("training job name or uid is required")
	}

	profiles := c.profilesForRegion(region)
	if len(profiles) == 0 {
		return nil, fmt.Errorf("no platform profile configured for region %q", region)
	}

	filterField := "name"
	if looksLikeClusterUUID(identifier) {
		filterField = "uid"
	}
	filter := fmt.Sprintf(`%s="%s"`, filterField, escapeSSPFilterValue(identifier))
	var lastErr error
	for _, profile := range profiles {
		endpoint, err := sspTrainingJobsURL(profile, subscription, region, workspace)
		if err != nil {
			lastErr = err
			continue
		}
		query := endpoint.Query()
		query.Set("page_size", "100")
		query.Set("skip", "0")
		query.Set("order_by", "created_at desc")
		query.Set("filter", filter)
		endpoint.RawQuery = query.Encode()

		var payload sspTrainingJobListResponse
		if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &payload); err != nil {
			lastErr = err
			continue
		}

		matches := make([]SSPTrainingJob, 0, len(payload.TrainingJobs))
		for _, item := range payload.TrainingJobs {
			if !strings.EqualFold(strings.TrimSpace(item.Name), identifier) &&
				!strings.EqualFold(strings.TrimSpace(item.UID), identifier) {
				continue
			}
			item.ProfileName = profile.Name
			matches = append(matches, item)
		}
		if len(matches) > 0 {
			return matches, nil
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, nil
}

func (c *VirtualClusterClient) profilesForRegion(region string) []clientProfile {
	region = strings.TrimSpace(region)
	profiles := c.orderedProfiles()
	if region == "" {
		return profiles
	}
	result := make([]clientProfile, 0, len(profiles))
	for _, profile := range profiles {
		if strings.EqualFold(strings.TrimSpace(profile.Region), region) {
			result = append(result, profile)
		}
	}
	return result
}

func sspTrainingJobsURL(profile clientProfile, subscription string, region string, workspace string) (*url.URL, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(profile.KubernetesBaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("profile %q has no kubernetes_base_url", profile.Name)
	}
	resourceGroup := strings.TrimSpace(profile.ResourceGroup)
	if resourceGroup == "" {
		resourceGroup = defaultResourceGroup
	}
	return url.Parse(fmt.Sprintf(
		"%s/ait/data/v1/subscriptions/%s/resourceGroups/%s/regions/%s/workspaces/%s/trainingJobs",
		baseURL,
		url.PathEscape(subscription),
		url.PathEscape(resourceGroup),
		url.PathEscape(region),
		url.PathEscape(workspace),
	))
}

func escapeSSPFilterValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}
