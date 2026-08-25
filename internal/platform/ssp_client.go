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

type SSPCluster struct {
	ID            string `json:"id"`
	UID           string `json:"uid"`
	Name          string `json:"name"`
	DisplayName   string `json:"display_name"`
	Description   string `json:"description"`
	State         string `json:"state"`
	Region        string `json:"region"`
	CreateTime    string `json:"create_time"`
	UpdateTime    string `json:"update_time"`
	ProfileName   string `json:"-"`
	Subscription  string `json:"-"`
	ResourceGroup string `json:"-"`
	Properties    struct {
		Type   string `json:"type"`
		Source struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
			UID  string `json:"uid"`
		} `json:"source"`
		VPCUID      string `json:"vpc_uid"`
		InfraType   string `json:"infra_type"`
		QueueStatus struct {
			Num int `json:"num"`
		} `json:"queue_status"`
		NodeStatus struct {
			Total     int `json:"total"`
			Ready     int `json:"ready"`
			Unhealthy int `json:"unhealthy"`
			Idle      int `json:"idle"`
		} `json:"node_status"`
	} `json:"properties"`
}

type SSPResourceSummary struct {
	ResourceType          string `json:"resource_type"`
	Unit                  string `json:"unit"`
	Total                 string `json:"total"`
	Allocated             string `json:"allocated"`
	Unallocated           string `json:"unallocated"`
	SpotQueueAllocated    string `json:"spot_queue_allocated"`
	ElasticQueueAllocated string `json:"elastic_queue_allocated"`
}

type sspClusterListResponse struct {
	Clusters      []SSPCluster `json:"clusters"`
	TotalSize     int          `json:"total_size"`
	NextPageToken string       `json:"next_page_token"`
}

type sspClusterSummaryResponse struct {
	SummaryData []SSPResourceSummary `json:"summary_data"`
}

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

type SSPTrainingJobWorker struct {
	Name      string `json:"name"`
	UID       string `json:"uid"`
	Namespace string `json:"namespace"`
	Phase     string `json:"phase"`
	ACN       struct {
		Name     string `json:"name"`
		UID      string `json:"uid"`
		HostName string `json:"host_name"`
		HostIP   string `json:"host_ip"`
	} `json:"acn"`
	Containers []struct {
		Name string `json:"name"`
	} `json:"containers"`
	Resource struct {
		CPUCount              any    `json:"cpu_count"`
		MemoryGiB             any    `json:"memory_gib"`
		AccelerateDeviceCount any    `json:"accelerate_device_count"`
		AccelerateDeviceModel string `json:"accelerate_device_model"`
		MachineType           string `json:"machine_type"`
	} `json:"resource"`
}

type sspTrainingJobWorkerListResponse struct {
	Workers   []SSPTrainingJobWorker `json:"workers"`
	TotalSize int                    `json:"total_size"`
}

type sspTrainingJobListResponse struct {
	TrainingJobs  []SSPTrainingJob `json:"training_jobs"`
	TotalSize     int              `json:"total_size"`
	NextPageToken string           `json:"next_page_token"`
}

type SSPWorkspace struct {
	Name          string
	UID           string
	Subscription  string
	ResourceGroup string
	Region        string
	ProfileName   string
	State         string
	ClusterName   string
	ClusterUID    string
	CreateTime    string
	UpdateTime    string
}

type SSPQueue struct {
	ID               string `json:"id"`
	UID              string `json:"uid"`
	Name             string `json:"name"`
	DisplayName      string `json:"display_name"`
	State            string `json:"state"`
	Type             string `json:"type"`
	QueueType        string `json:"queue_type"`
	WorkspaceName    string `json:"workspace_name"`
	SubscriptionName string `json:"subscription_name"`
	ResourceGroup    string `json:"resource_group_name"`
	Region           string `json:"region"`
	CreateTime       string `json:"create_time"`
	UpdateTime       string `json:"update_time"`
	ProfileName      string `json:"-"`
	Properties       struct {
		Type        string `json:"type"`
		QueueType   string `json:"queue_type"`
		ClusterName string `json:"cluster_name"`
		ClusterUID  string `json:"cluster_uid"`
		NodeStatus  struct {
			Total     int `json:"total"`
			Ready     int `json:"ready"`
			Unhealthy int `json:"unhealthy"`
			Idle      int `json:"idle"`
		} `json:"node_status"`
		Cluster struct {
			Name string `json:"name"`
			UID  string `json:"uid"`
		} `json:"cluster"`
		Workspace struct {
			Name string `json:"name"`
			UID  string `json:"uid"`
		} `json:"workspace"`
		AdvancedSettings struct {
			ProvideSpotResourceEnabled *bool  `json:"provide_spot_resource_enabled"`
			DequeueStrategy            string `json:"dequeue_strategy"`
		} `json:"advanced_settings"`
	} `json:"properties"`
	Spec struct {
		Type        string `json:"type"`
		QueueType   string `json:"queue_type"`
		ClusterName string `json:"cluster_name"`
		ClusterUID  string `json:"cluster_uid"`
	} `json:"spec"`
}

type SSPQueueNode struct {
	ID          string `json:"id"`
	UID         string `json:"uid"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	HostIP      string `json:"host_ip"`
	MachineType string `json:"machine_type"`
	State       string `json:"state"`
	Zone        string `json:"zone"`
	SummaryData []struct {
		ResourceType string `json:"resource_type"`
		Allocated    string `json:"allocated"`
		Total        string `json:"total"`
		Unallocated  string `json:"unallocated"`
		Unit         string `json:"unit"`
	} `json:"summary_data"`
}

type sspQueueNodeListResponse struct {
	Nodes         []SSPQueueNode `json:"nodes"`
	TotalSize     int            `json:"total_size"`
	NextPageToken string         `json:"next_page_token"`
}

type SSPQueueWorkloadQuery struct {
	Type     string
	State    string
	Priority string
}

type SSPQueueWorkload struct {
	Name        string `json:"name"`
	UID         string `json:"uid"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Type        string `json:"type"`
	State       string `json:"state"`
	Priority    string `json:"priority"`
	Workspace   struct {
		Name string `json:"name"`
		UID  string `json:"uid"`
	} `json:"workspace"`
	Tasks []struct {
		Name     string `json:"name"`
		Replicas int    `json:"replicas"`
		Resource struct {
			MachineTypes          []string `json:"machine_types"`
			CPU                   any      `json:"cpu"`
			Memory                any      `json:"memory"`
			AccelerateDeviceCount any      `json:"accelerate_device_count"`
		} `json:"resource"`
	} `json:"tasks"`
	Ownership struct {
		CreatorID   string `json:"creator_id"`
		CreatorName string `json:"creator_name"`
	} `json:"ownership"`
	CreateTime string `json:"create_time"`
}

type sspQueueWorkloadListResponse struct {
	Workloads     []SSPQueueWorkload `json:"workloads"`
	TotalSize     int                `json:"total_size"`
	NextPageToken string             `json:"next_page_token"`
}

type SSPQueueResourceDetails struct {
	Name           string
	UID            string
	State          string
	Type           string
	WorkspaceName  string
	WorkspaceUID   string
	ClusterName    string
	ClusterUID     string
	VClusterName   string
	Subscription   string
	ResourceGroup  string
	Region         string
	ProfileName    string
	NodeNames      []string
	NodeCount      int
	NodeCountKnown bool
	SpotLending    *bool
	DequeuePolicy  string
	CreateTime     string
	UpdateTime     string
}

type sspQueueListResponse struct {
	Queues        []SSPQueue `json:"queues"`
	TotalSize     int        `json:"total_size"`
	NextPageToken string     `json:"next_page_token"`
}

func (c *VirtualClusterClient) ListSSPClusters(ctx context.Context, region string) ([]SSPCluster, error) {
	profiles := c.sspProfilesForRegion(region)
	if len(profiles) == 0 {
		return nil, fmt.Errorf("no platform profile configured for region %q", region)
	}

	result := make([]SSPCluster, 0)
	seen := make(map[string]struct{})
	var lastErr error
	success := false
	for _, profile := range profiles {
		const pageSize = 100
		for skip := 0; ; {
			endpoint, _ := url.Parse(profile.BaseURL)
			endpoint.Path = "/compute/ssp/v1/subscriptions/-/resourceGroups/-/regions/-/clusters"
			query := endpoint.Query()
			query.Set("page_size", strconv.Itoa(pageSize))
			query.Set("skip", strconv.Itoa(skip))
			endpoint.RawQuery = query.Encode()

			var payload sspClusterListResponse
			if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &payload); err != nil {
				lastErr = err
				break
			}
			success = true
			for index := range payload.Clusters {
				cluster := payload.Clusters[index]
				normalizeSSPCluster(&cluster, profile)
				if strings.TrimSpace(region) != "" && !strings.EqualFold(strings.TrimSpace(cluster.Region), strings.TrimSpace(region)) {
					continue
				}
				key := strings.ToLower(profile.Name + "|" + firstNonEmpty(cluster.UID, cluster.Name))
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				result = append(result, cluster)
			}
			count := len(payload.Clusters)
			if count == 0 || (payload.TotalSize > 0 && skip+count >= payload.TotalSize) || count < pageSize {
				break
			}
			skip += count
		}
	}
	if !success {
		return nil, lastErr
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
		}
		return result[i].ProfileName < result[j].ProfileName
	})
	return result, nil
}

func (c *VirtualClusterClient) GetSSPCluster(ctx context.Context, cluster SSPCluster) (*SSPCluster, error) {
	profile, endpoint, err := c.sspClusterURL(cluster)
	if err != nil {
		return nil, err
	}
	var result SSPCluster
	if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &result); err != nil {
		return nil, err
	}
	normalizeSSPCluster(&result, profile)
	return &result, nil
}

func (c *VirtualClusterClient) GetSSPClusterSummary(ctx context.Context, cluster SSPCluster) ([]SSPResourceSummary, error) {
	profile, endpoint, err := c.sspClusterURL(cluster)
	if err != nil {
		return nil, err
	}
	endpoint.Path += "/summary"
	var payload sspClusterSummaryResponse
	if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &payload); err != nil {
		return nil, err
	}
	return payload.SummaryData, nil
}

func (c *VirtualClusterClient) ListSSPClusterQueues(ctx context.Context, cluster SSPCluster) ([]SSPQueue, error) {
	profile, endpoint, err := c.sspClusterURL(cluster)
	if err != nil {
		return nil, err
	}
	endpoint.Path += "/queues"
	const pageSize = 100
	result := make([]SSPQueue, 0)
	for skip := 0; ; {
		query := endpoint.Query()
		query.Set("page_size", strconv.Itoa(pageSize))
		query.Set("skip", strconv.Itoa(skip))
		query.Set("order_by", "create_time desc")
		endpoint.RawQuery = query.Encode()
		var payload sspQueueListResponse
		if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &payload); err != nil {
			return nil, err
		}
		for index := range payload.Queues {
			queue := payload.Queues[index]
			queue.ProfileName = profile.Name
			queue.SubscriptionName = firstNonEmpty(queue.SubscriptionName, cluster.Subscription)
			queue.ResourceGroup = firstNonEmpty(queue.ResourceGroup, cluster.ResourceGroup)
			queue.Region = firstNonEmpty(queue.Region, cluster.Region)
			queue.Type = firstNonEmpty(queue.Type, queue.QueueType, queue.Properties.Type, queue.Properties.QueueType)
			queue.WorkspaceName = firstNonEmpty(queue.WorkspaceName, queue.Properties.Workspace.Name)
			result = append(result, queue)
		}
		count := len(payload.Queues)
		if count == 0 || (payload.TotalSize > 0 && len(result) >= payload.TotalSize) || count < pageSize {
			break
		}
		skip += count
	}
	return result, nil
}

func (c *VirtualClusterClient) sspClusterURL(cluster SSPCluster) (clientProfile, *url.URL, error) {
	profile, ok := c.clientProfileByName(cluster.ProfileName)
	if !ok {
		return clientProfile{}, nil, fmt.Errorf("platform profile %q not found", cluster.ProfileName)
	}
	subscription := firstNonEmpty(cluster.Subscription, profile.Subscription)
	resourceGroup := firstNonEmpty(cluster.ResourceGroup, profile.ResourceGroup, defaultResourceGroup)
	region := firstNonEmpty(cluster.Region, profile.Region)
	if subscription == "" || region == "" || strings.TrimSpace(cluster.Name) == "" {
		return clientProfile{}, nil, fmt.Errorf("cluster subscription, region and name are required")
	}
	endpoint, _ := url.Parse(profile.BaseURL)
	endpoint.Path = fmt.Sprintf(
		"/compute/ssp/v1/subscriptions/%s/resourceGroups/%s/regions/%s/clusters/%s",
		url.PathEscape(subscription), url.PathEscape(resourceGroup), url.PathEscape(region), url.PathEscape(cluster.Name),
	)
	return profile, endpoint, nil
}

func normalizeSSPCluster(cluster *SSPCluster, profile clientProfile) {
	cluster.ProfileName = profile.Name
	parts := strings.Split(strings.Trim(strings.TrimSpace(cluster.ID), "/"), "/")
	for index := 0; index+1 < len(parts); index += 2 {
		switch parts[index] {
		case "subscriptions":
			cluster.Subscription = parts[index+1]
		case "resourceGroups":
			cluster.ResourceGroup = parts[index+1]
		case "regions":
			cluster.Region = parts[index+1]
		case "clusters":
			cluster.Name = firstNonEmpty(cluster.Name, parts[index+1])
		}
	}
}

// ListSSPWorkspaces returns SSP workspaces from RMH without relying on
// workspace namespaces being visible through the current kubeconfig.
func (c *VirtualClusterClient) ListSSPWorkspaces(ctx context.Context, region string) ([]SSPWorkspace, error) {
	region = strings.TrimSpace(region)
	profiles := c.sspProfilesForRegion(region)
	if len(profiles) == 0 {
		return nil, fmt.Errorf("no platform profile configured for region %q", region)
	}

	workspaces := make(map[string]SSPWorkspace)
	var lastErr error
	success := false
	for _, profile := range profiles {
		profileRegion := firstNonEmpty(region, strings.TrimSpace(profile.Region))
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
		UID:         strings.TrimSpace(resource.ID),
		Region:      strings.TrimSpace(resource.Region),
		ProfileName: strings.TrimSpace(profileName),
		State:       strings.TrimSpace(resource.State),
		CreateTime:  strings.TrimSpace(resource.CreateTime),
		UpdateTime:  strings.TrimSpace(resource.UpdateTime),
	}
	var properties map[string]any
	if json.Unmarshal([]byte(resource.Properties), &properties) == nil {
		workspace.ClusterName = firstNestedString(properties, "cluster_name", "virtual_cluster_name", "vcluster_name")
		workspace.ClusterUID = firstNestedString(properties, "cluster_uid", "virtual_cluster_uid", "vcluster_uid")
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

func (c *VirtualClusterClient) ListSSPQueues(ctx context.Context, workspace SSPWorkspace) ([]SSPQueue, error) {
	profile, ok := c.clientProfileByName(workspace.ProfileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", workspace.ProfileName)
	}
	subscription := firstNonEmpty(strings.TrimSpace(workspace.Subscription), strings.TrimSpace(profile.Subscription))
	region := firstNonEmpty(strings.TrimSpace(workspace.Region), strings.TrimSpace(profile.Region))
	resourceGroup := firstNonEmpty(strings.TrimSpace(workspace.ResourceGroup), strings.TrimSpace(profile.ResourceGroup), defaultResourceGroup)
	if subscription == "" || region == "" || strings.TrimSpace(workspace.Name) == "" {
		return nil, fmt.Errorf("workspace subscription, region and name are required for queue lookup")
	}

	const pageSize = 100
	result := make([]SSPQueue, 0)
	for skip := 0; ; {
		endpoint, _ := url.Parse(profile.BaseURL)
		endpoint.Path = fmt.Sprintf(
			"/compute/ssp/v1/subscriptions/%s/resourceGroups/%s/regions/%s/workspaces/%s/queues",
			url.PathEscape(subscription),
			url.PathEscape(resourceGroup),
			url.PathEscape(region),
			url.PathEscape(workspace.Name),
		)
		query := endpoint.Query()
		query.Set("page_size", strconv.Itoa(pageSize))
		query.Set("skip", strconv.Itoa(skip))
		query.Set("order_by", "create_time desc")
		endpoint.RawQuery = query.Encode()

		var payload sspQueueListResponse
		if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &payload); err != nil {
			return nil, err
		}
		for index := range payload.Queues {
			queue := &payload.Queues[index]
			queue.ProfileName = profile.Name
			queue.WorkspaceName = firstNonEmpty(strings.TrimSpace(queue.WorkspaceName), workspace.Name)
			queue.SubscriptionName = firstNonEmpty(strings.TrimSpace(queue.SubscriptionName), subscription)
			queue.ResourceGroup = firstNonEmpty(strings.TrimSpace(queue.ResourceGroup), resourceGroup)
			queue.Region = firstNonEmpty(strings.TrimSpace(queue.Region), region)
			queue.UID = firstNonEmpty(strings.TrimSpace(queue.UID), sspResourceIdentifier(queue.ID))
			queue.Type = firstNonEmpty(strings.TrimSpace(queue.Type), strings.TrimSpace(queue.QueueType), strings.TrimSpace(queue.Properties.Type), strings.TrimSpace(queue.Properties.QueueType), strings.TrimSpace(queue.Spec.Type), strings.TrimSpace(queue.Spec.QueueType))
			result = append(result, *queue)
		}
		count := len(payload.Queues)
		if count == 0 || (payload.TotalSize > 0 && len(result) >= payload.TotalSize) || count < pageSize {
			break
		}
		skip += count
	}
	return result, nil
}

func (c *VirtualClusterClient) GetSSPQueue(ctx context.Context, profileName string, subscription string, resourceGroup string, region string, cluster string, queueName string) (*SSPQueue, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	subscription = firstNonEmpty(strings.TrimSpace(subscription), strings.TrimSpace(profile.Subscription))
	resourceGroup = firstNonEmpty(strings.TrimSpace(resourceGroup), strings.TrimSpace(profile.ResourceGroup), defaultResourceGroup)
	region = firstNonEmpty(strings.TrimSpace(region), strings.TrimSpace(profile.Region))
	cluster = strings.TrimSpace(cluster)
	queueName = strings.TrimSpace(queueName)
	if subscription == "" || region == "" || cluster == "" || queueName == "" {
		return nil, fmt.Errorf("subscription, region, cluster and queue are required for queue detail lookup")
	}

	endpoint, _ := url.Parse(profile.BaseURL)
	endpoint.Path = fmt.Sprintf(
		"/compute/ssp/v1/subscriptions/%s/resourceGroups/%s/regions/%s/clusters/%s/queues/%s",
		url.PathEscape(subscription), url.PathEscape(resourceGroup), url.PathEscape(region), url.PathEscape(cluster), url.PathEscape(queueName),
	)
	var queue SSPQueue
	if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &queue); err != nil {
		return nil, err
	}
	queue.ProfileName = profile.Name
	queue.SubscriptionName = firstNonEmpty(strings.TrimSpace(queue.SubscriptionName), subscription)
	queue.ResourceGroup = firstNonEmpty(strings.TrimSpace(queue.ResourceGroup), resourceGroup)
	queue.Region = firstNonEmpty(strings.TrimSpace(queue.Region), region)
	queue.UID = firstNonEmpty(strings.TrimSpace(queue.UID), sspResourceIdentifier(queue.ID))
	queue.Type = firstNonEmpty(strings.TrimSpace(queue.Type), strings.TrimSpace(queue.QueueType), strings.TrimSpace(queue.Properties.Type), strings.TrimSpace(queue.Properties.QueueType), strings.TrimSpace(queue.Spec.Type), strings.TrimSpace(queue.Spec.QueueType))
	queue.WorkspaceName = firstNonEmpty(strings.TrimSpace(queue.WorkspaceName), strings.TrimSpace(queue.Properties.Workspace.Name))
	return &queue, nil
}

func (c *VirtualClusterClient) ListSSPQueueWorkloads(ctx context.Context, profileName string, subscription string, resourceGroup string, region string, cluster string, queueName string, query SSPQueueWorkloadQuery) ([]SSPQueueWorkload, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	subscription = firstNonEmpty(strings.TrimSpace(subscription), strings.TrimSpace(profile.Subscription))
	resourceGroup = firstNonEmpty(strings.TrimSpace(resourceGroup), strings.TrimSpace(profile.ResourceGroup), defaultResourceGroup)
	region = firstNonEmpty(strings.TrimSpace(region), strings.TrimSpace(profile.Region))
	cluster = strings.TrimSpace(cluster)
	queueName = strings.TrimSpace(queueName)
	if subscription == "" || region == "" || cluster == "" || queueName == "" {
		return nil, fmt.Errorf("subscription, region, cluster and queue are required for queue workload lookup")
	}

	filters := make([]string, 0, 3)
	for _, item := range []struct {
		key   string
		value string
	}{{"type", query.Type}, {"state", query.State}, {"priority", query.Priority}} {
		if value := strings.TrimSpace(item.value); value != "" {
			filters = append(filters, fmt.Sprintf(`%s="%s"`, item.key, escapeSSPFilterValue(value)))
		}
	}

	const pageSize = 100
	result := make([]SSPQueueWorkload, 0)
	for skip := 0; ; {
		endpoint, _ := url.Parse(profile.BaseURL)
		endpoint.Path = fmt.Sprintf(
			"/compute/ssp/v1/subscriptions/%s/resourceGroups/%s/regions/%s/clusters/%s/queues/%s/workloads",
			url.PathEscape(subscription), url.PathEscape(resourceGroup), url.PathEscape(region), url.PathEscape(cluster), url.PathEscape(queueName),
		)
		values := endpoint.Query()
		values.Set("page_size", strconv.Itoa(pageSize))
		values.Set("skip", strconv.Itoa(skip))
		values.Set("order_by", "create_time desc")
		if len(filters) > 0 {
			values.Set("filter", strings.Join(filters, " AND "))
		}
		endpoint.RawQuery = values.Encode()

		var payload sspQueueWorkloadListResponse
		if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &payload); err != nil {
			return nil, err
		}
		result = append(result, payload.Workloads...)
		count := len(payload.Workloads)
		if count == 0 || (payload.TotalSize > 0 && len(result) >= payload.TotalSize) || count < pageSize {
			break
		}
		skip += count
	}
	return result, nil
}

func (c *VirtualClusterClient) ListSSPQueueNodes(ctx context.Context, profileName string, subscription string, resourceGroup string, region string, cluster string, queue string) ([]SSPQueueNode, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	subscription = firstNonEmpty(strings.TrimSpace(subscription), strings.TrimSpace(profile.Subscription))
	resourceGroup = firstNonEmpty(strings.TrimSpace(resourceGroup), strings.TrimSpace(profile.ResourceGroup), defaultResourceGroup)
	region = firstNonEmpty(strings.TrimSpace(region), strings.TrimSpace(profile.Region))
	cluster = strings.TrimSpace(cluster)
	queue = strings.TrimSpace(queue)
	if subscription == "" || region == "" || cluster == "" || queue == "" {
		return nil, fmt.Errorf("subscription, region, cluster and queue are required for queue node lookup")
	}

	const pageSize = 100
	result := make([]SSPQueueNode, 0)
	seen := make(map[string]struct{})
	type pageResult struct {
		skip    int
		payload sspQueueNodeListResponse
		err     error
	}
	fetchPage := func(skip int) pageResult {
		endpoint, _ := url.Parse(profile.BaseURL)
		endpoint.Path = fmt.Sprintf(
			"/compute/ssp/v1/subscriptions/%s/resourceGroups/%s/regions/%s/clusters/%s/queues/%s/nodes",
			url.PathEscape(subscription),
			url.PathEscape(resourceGroup),
			url.PathEscape(region),
			url.PathEscape(cluster),
			url.PathEscape(queue),
		)
		query := endpoint.Query()
		query.Set("page_size", strconv.Itoa(pageSize))
		query.Set("skip", strconv.Itoa(skip))
		endpoint.RawQuery = query.Encode()

		var payload sspQueueNodeListResponse
		if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &payload); err != nil {
			return pageResult{skip: skip, err: err}
		}
		return pageResult{skip: skip, payload: payload}
	}
	appendPage := func(payload sspQueueNodeListResponse) int {
		added := 0
		for _, node := range payload.Nodes {
			key := firstNonEmpty(strings.TrimSpace(node.UID), strings.TrimSpace(node.Name), strings.TrimSpace(node.HostIP))
			if key == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, node)
			added++
		}
		return added
	}

	initial := make(chan pageResult, 2)
	for _, skip := range []int{0, pageSize} {
		go func(offset int) { initial <- fetchPage(offset) }(skip)
	}
	pages := make(map[int]pageResult, 2)
	for range 2 {
		page := <-initial
		pages[page.skip] = page
	}
	first := pages[0]
	if first.err != nil {
		return nil, first.err
	}
	appendPage(first.payload)
	totalSize := first.payload.TotalSize
	needSecond := totalSize > len(first.payload.Nodes) || (totalSize == 0 && len(first.payload.Nodes) >= pageSize)
	if !needSecond {
		return result, nil
	}
	second := pages[pageSize]
	if second.err != nil {
		return nil, second.err
	}
	appendPage(second.payload)
	if totalSize > 0 && len(result) >= totalSize {
		return result, nil
	}

	for skip := pageSize * 2; ; skip += pageSize {
		page := fetchPage(skip)
		if page.err != nil {
			return nil, page.err
		}
		added := appendPage(page.payload)
		if len(page.payload.Nodes) == 0 || added == 0 || (totalSize > 0 && len(result) >= totalSize) || (totalSize == 0 && len(page.payload.Nodes) < pageSize) {
			break
		}
	}
	return result, nil
}

func (c *VirtualClusterClient) GetSSPQueueResource(ctx context.Context, profileName string, identifier string) (*SSPQueueResourceDetails, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("queue identifier is required")
	}

	endpoint, _ := url.Parse(profile.BaseURL)
	endpoint.Path = "/rmh/v1/resources:page"
	query := endpoint.Query()
	query.Set("filter", fmt.Sprintf(`resource_type="compute.ssp.v1.queue" AND name="*%s*"`, escapeSSPFilterValue(identifier)))
	query.Set("page_size", "100")
	query.Set("page_token", "1")
	endpoint.RawQuery = query.Encode()

	var payload storageVolumePageResponse
	if err := c.postJSONWithProfile(ctx, profile, endpoint.String(), map[string]any{}, &payload); err != nil {
		return nil, err
	}
	type candidate struct {
		queue   StorageVolumeResource
		cluster StorageVolumeResource
	}
	candidates := make([]candidate, 0)
	for _, resource := range payload.Resources {
		if strings.EqualFold(firstNonEmpty(resource.Type, resource.ResourceType), "compute.ssp.v1.queue") {
			candidates = append(candidates, candidate{queue: resource})
		}
		for _, relation := range resource.RelatedResources {
			related := relation.Resource
			if strings.EqualFold(firstNonEmpty(related.Type, related.ResourceType), "compute.ssp.v1.queue") {
				candidates = append(candidates, candidate{queue: related, cluster: resource})
			}
		}
	}
	identifierLower := strings.ToLower(strings.TrimPrefix(identifier, "ssp-"))
	exact := make([]candidate, 0, 1)
	fuzzy := make([]candidate, 0, 1)
	for _, item := range candidates {
		name := strings.ToLower(strings.TrimSpace(item.queue.Name))
		uid := strings.ToLower(strings.TrimSpace(item.queue.ID))
		if identifierLower == name || identifierLower == uid {
			exact = append(exact, item)
		} else if strings.Contains(name, identifierLower) || strings.Contains(uid, identifierLower) {
			fuzzy = append(fuzzy, item)
		}
	}
	selected := exact
	if len(selected) == 0 {
		selected = fuzzy
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("SSP queue resource %q not found", identifier)
	}
	if len(selected) > 1 {
		return nil, fmt.Errorf("SSP queue resource %q matched multiple queues", identifier)
	}
	return sspQueueResourceDetails(selected[0].queue, selected[0].cluster, profile.Name), nil
}

func (c *VirtualClusterClient) FindSSPQueueResource(ctx context.Context, region string, identifier string) (*SSPQueueResourceDetails, error) {
	profiles := c.sspProfilesForRegion(region)
	if len(profiles) == 0 {
		return nil, fmt.Errorf("no platform profile configured for region %q", region)
	}
	var lastErr error
	for _, profile := range profiles {
		details, err := c.GetSSPQueueResource(ctx, profile.Name, identifier)
		if err != nil {
			lastErr = err
			continue
		}
		if strings.TrimSpace(region) != "" && details.Region != "" && !strings.EqualFold(strings.TrimSpace(details.Region), strings.TrimSpace(region)) {
			continue
		}
		return details, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("SSP queue resource %q not found", identifier)
}

func (c *VirtualClusterClient) ListSSPQueueResources(ctx context.Context, profileName string, region string) ([]SSPQueueResourceDetails, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	const pageSize = 100
	result := make([]SSPQueueResourceDetails, 0)
	seen := make(map[string]struct{})
	pageToken := "1"
	for {
		endpoint, _ := url.Parse(profile.BaseURL)
		endpoint.Path = "/rmh/v1/resources:page"
		query := endpoint.Query()
		query.Set("filter", `resource_type="compute.ssp.v1.queue"`)
		query.Set("page_size", strconv.Itoa(pageSize))
		query.Set("page_token", pageToken)
		endpoint.RawQuery = query.Encode()

		var payload storageVolumePageResponse
		if err := c.postJSONWithProfile(ctx, profile, endpoint.String(), map[string]any{}, &payload); err != nil {
			return nil, err
		}
		for _, resource := range payload.Resources {
			if strings.EqualFold(firstNonEmpty(resource.Type, resource.ResourceType), "compute.ssp.v1.queue") {
				appendSSPQueueResource(&result, seen, resource, StorageVolumeResource{}, profile.Name, region)
			}
			for _, relation := range resource.RelatedResources {
				related := relation.Resource
				if strings.EqualFold(firstNonEmpty(related.Type, related.ResourceType), "compute.ssp.v1.queue") {
					appendSSPQueueResource(&result, seen, related, resource, profile.Name, region)
				}
			}
		}
		next := strings.TrimSpace(payload.NextPageToken)
		if next == "" || next == pageToken || len(payload.Resources) < pageSize {
			break
		}
		pageToken = next
	}
	return result, nil
}

func appendSSPQueueResource(result *[]SSPQueueResourceDetails, seen map[string]struct{}, queue StorageVolumeResource, cluster StorageVolumeResource, profileName string, region string) {
	details := sspQueueResourceDetails(queue, cluster, profileName)
	if requested := strings.TrimSpace(region); requested != "" && !strings.EqualFold(details.Region, requested) {
		return
	}
	key := firstNonEmpty(strings.TrimSpace(details.UID), strings.TrimSpace(details.Name))
	if key == "" {
		return
	}
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}
	*result = append(*result, *details)
}

func sspQueueResourceDetails(queue StorageVolumeResource, cluster StorageVolumeResource, profileName string) *SSPQueueResourceDetails {
	details := &SSPQueueResourceDetails{
		Name:          strings.TrimSpace(queue.Name),
		UID:           strings.TrimSpace(queue.ID),
		State:         strings.TrimSpace(queue.State),
		ProfileName:   strings.TrimSpace(profileName),
		ClusterName:   strings.TrimSpace(cluster.Name),
		ClusterUID:    strings.TrimSpace(cluster.ID),
		Region:        strings.TrimSpace(queue.Region),
		ResourceGroup: strings.TrimSpace(queue.ResourceGroupName),
		CreateTime:    strings.TrimSpace(queue.CreateTime),
		UpdateTime:    strings.TrimSpace(queue.UpdateTime),
	}
	parts := strings.Split(strings.Trim(strings.TrimSpace(queue.RID), "/"), "/")
	for index := 0; index+1 < len(parts); index += 2 {
		switch parts[index] {
		case "subscriptions":
			details.Subscription = parts[index+1]
		case "resourceGroups":
			details.ResourceGroup = parts[index+1]
		case "regions":
			details.Region = parts[index+1]
		case "clusters":
			details.ClusterName = parts[index+1]
		}
	}
	var queueProperties struct {
		Type  string `json:"type"`
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
		NodeStatus struct {
			Total int `json:"total"`
		} `json:"node_status"`
		Cluster struct {
			Name string `json:"name"`
			UID  string `json:"uid"`
		} `json:"cluster"`
		Workspace struct {
			Name string `json:"name"`
			UID  string `json:"uid"`
		} `json:"workspace"`
		AdvancedSettings struct {
			ProvideSpotResourceEnabled *bool  `json:"provide_spot_resource_enabled"`
			DequeueStrategy            string `json:"dequeue_strategy"`
		} `json:"advanced_settings"`
	}
	if json.Unmarshal([]byte(queue.Properties), &queueProperties) == nil {
		details.Type = strings.TrimSpace(queueProperties.Type)
		details.ClusterName = firstNonEmpty(details.ClusterName, strings.TrimSpace(queueProperties.Cluster.Name))
		details.ClusterUID = firstNonEmpty(details.ClusterUID, strings.TrimSpace(queueProperties.Cluster.UID))
		details.WorkspaceName = strings.TrimSpace(queueProperties.Workspace.Name)
		details.WorkspaceUID = strings.TrimSpace(queueProperties.Workspace.UID)
		details.NodeCount = queueProperties.NodeStatus.Total
		details.NodeCountKnown = queueProperties.NodeStatus.Total > 0
		details.SpotLending = queueProperties.AdvancedSettings.ProvideSpotResourceEnabled
		details.DequeuePolicy = strings.TrimSpace(queueProperties.AdvancedSettings.DequeueStrategy)
		for _, node := range queueProperties.Nodes {
			if name := strings.TrimSpace(node.Name); name != "" {
				details.NodeNames = append(details.NodeNames, name)
			}
		}
		if !details.NodeCountKnown {
			details.NodeCount = len(details.NodeNames)
		}
	}
	var clusterProperties struct {
		Source struct {
			Name string `json:"name"`
		} `json:"source"`
	}
	if json.Unmarshal([]byte(cluster.Properties), &clusterProperties) == nil {
		details.VClusterName = strings.TrimSpace(clusterProperties.Source.Name)
	}
	return details
}

func sspResourceIdentifier(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" {
		return ""
	}
	parts := strings.Split(value, "/")
	return parts[len(parts)-1]
}

func firstNestedString(value any, keys ...string) string {
	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keySet[strings.ToLower(key)] = struct{}{}
	}
	var visit func(any) string
	visit = func(current any) string {
		switch typed := current.(type) {
		case map[string]any:
			for key, nested := range typed {
				if _, ok := keySet[strings.ToLower(key)]; ok {
					if text, ok := nested.(string); ok && strings.TrimSpace(text) != "" {
						return strings.TrimSpace(text)
					}
				}
			}
			for _, nested := range typed {
				if result := visit(nested); result != "" {
					return result
				}
			}
		case []any:
			for _, nested := range typed {
				if result := visit(nested); result != "" {
					return result
				}
			}
		}
		return ""
	}
	return visit(value)
}

// ConfiguredSubscriptionForRegion returns the first configured subscription
// for the requested region. Callers can fall back to Kubernetes labels when it
// is intentionally omitted from platform.json.
func (c *VirtualClusterClient) ConfiguredSubscriptionForRegion(region string) string {
	for _, profile := range c.sspProfilesForRegion(region) {
		if value := strings.TrimSpace(profile.Subscription); value != "" {
			return value
		}
	}
	return ""
}

func (c *VirtualClusterClient) FindSSPTrainingJobs(ctx context.Context, subscription string, region string, workspace string, identifier string) ([]SSPTrainingJob, error) {
	return c.findSSPTrainingJobs(ctx, "", subscription, region, workspace, identifier)
}

func (c *VirtualClusterClient) FindSSPTrainingJobsForProfile(ctx context.Context, profileName string, subscription string, region string, workspace string, identifier string) ([]SSPTrainingJob, error) {
	return c.findSSPTrainingJobs(ctx, profileName, subscription, region, workspace, identifier)
}

func (c *VirtualClusterClient) ListSSPTrainingJobWorkers(ctx context.Context, job SSPTrainingJob, limit int) ([]SSPTrainingJobWorker, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	profile, ok := c.clientProfileByName(job.ProfileName)
	if !ok {
		return nil, 0, fmt.Errorf("platform profile %q not found", job.ProfileName)
	}
	subscription := firstNonEmpty(strings.TrimSpace(job.SubscriptionName), strings.TrimSpace(profile.Subscription))
	region := firstNonEmpty(strings.TrimSpace(job.Region), strings.TrimSpace(profile.Region))
	endpoint, err := sspTrainingJobsURL(profile, subscription, region, job.WorkspaceName)
	if err != nil {
		return nil, 0, err
	}
	endpoint.Path += "/" + url.PathEscape(strings.TrimSpace(job.Name)) + "/workers"
	query := endpoint.Query()
	query.Set("page_size", strconv.Itoa(limit))
	query.Set("skip", "0")
	endpoint.RawQuery = query.Encode()
	var payload sspTrainingJobWorkerListResponse
	if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &payload); err != nil {
		return nil, 0, err
	}
	return payload.Workers, payload.TotalSize, nil
}

func (c *VirtualClusterClient) findSSPTrainingJobs(ctx context.Context, profileName string, subscription string, region string, workspace string, identifier string) ([]SSPTrainingJob, error) {
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

	profiles := c.sspProfilesForRegion(region)
	if strings.TrimSpace(profileName) != "" {
		profile, ok := c.clientProfileByName(profileName)
		if !ok {
			return nil, fmt.Errorf("platform profile %q not found", profileName)
		}
		profiles = []clientProfile{profile}
	}
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

// sspProfilesForRegion keeps automatic SSP discovery within the current
// tenant family while allowing its D and PT profiles to be queried together.
func (c *VirtualClusterClient) sspProfilesForRegion(region string) []clientProfile {
	region = strings.TrimSpace(region)
	current, ok := c.currentClientProfile()
	candidates := make([]clientProfile, 0)
	for _, profile := range c.orderedProfiles() {
		if region != "" && !strings.EqualFold(strings.TrimSpace(profile.Region), region) {
			continue
		}
		candidates = append(candidates, profile)
	}
	if !ok {
		return candidates
	}
	result := make([]clientProfile, 0, len(candidates))
	for _, profile := range candidates {
		if sameSSPTenantProfile(current, profile) {
			result = append(result, profile)
		}
	}
	if len(result) == 0 && region != "" {
		return candidates
	}
	return result
}

func sameSSPTenantProfile(current clientProfile, candidate clientProfile) bool {
	if profileTenantFamily(current.Name) == profileTenantFamily(candidate.Name) {
		return true
	}
	if current.AccessKey != "" && strings.EqualFold(strings.TrimSpace(current.AccessKey), strings.TrimSpace(candidate.AccessKey)) {
		return true
	}
	if current.Subscription != "" && strings.EqualFold(strings.TrimSpace(current.Subscription), strings.TrimSpace(candidate.Subscription)) {
		return true
	}
	return false
}

// ConfiguredSSPRegions returns current-tenant SSP regions in profile order.
func (c *VirtualClusterClient) ConfiguredSSPRegions() []string {
	result := make([]string, 0)
	seen := make(map[string]struct{})
	for _, profile := range c.sspProfilesForRegion("") {
		region := strings.TrimSpace(profile.Region)
		key := strings.ToLower(region)
		if region == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, region)
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
