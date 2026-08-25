package platform

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type SSPAIRQueueRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
	UID  string `json:"uid"`
}

type SSPAIROwnership struct {
	CreatorID   string `json:"creator_id"`
	CreatorName string `json:"creator_name"`
	OwnerID     string `json:"owner_id"`
	TenantID    string `json:"tenant_id"`
}

type SSPAIRDNATRule struct {
	NATGatewayName string `json:"nat_gateway_name"`
	Name           string `json:"dnat_rule_name"`
	EIPName        string `json:"eip_name"`
	ExternalIP     string `json:"external_ip"`
	ExternalPort   string `json:"external_port"`
	InternalPort   any    `json:"internal_port"`
	Protocol       string `json:"protocol"`
}

type SSPAIRResource struct {
	MachineTypes              []string `json:"machine_types"`
	CPUCount                  any      `json:"cpu_count"`
	MemoryGiB                 any      `json:"memory_gib"`
	AccelerateDeviceCount     any      `json:"accelerate_device_count"`
	AccelerateDeviceModel     string   `json:"accelerate_device_model"`
	AccelerateDeviceMemoryGiB any      `json:"accelerate_device_memory_gib"`
	RDMAName                  string   `json:"rdma_name"`
}

type SSPAIRContainer struct {
	Name     string         `json:"name"`
	Image    SSPAIRImage    `json:"image"`
	Resource SSPAIRResource `json:"resource"`
}

type SSPAIRImage struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type SSPAIRPodTemplate struct {
	Containers []SSPAIRContainer `json:"containers"`
	ShmSizeGiB any               `json:"shm_size_gib"`
}

type SSPAIRVolumeMount struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	MountPath  string `json:"mount_path"`
	Endpoint   string `json:"endpoint"`
	Zone       string `json:"zone"`
	Region     string `json:"region"`
	AccessMode string `json:"access_mode"`
}

type SSPAIRJob struct {
	SubscriptionName  string          `json:"subscription_name"`
	ResourceGroupName string          `json:"resource_group_name"`
	Region            string          `json:"region"`
	WorkspaceName     string          `json:"workspace_name"`
	Name              string          `json:"name"`
	UID               string          `json:"uid"`
	ID                string          `json:"id"`
	Ownership         SSPAIROwnership `json:"ownership"`
	ProfileName       string          `json:"-"`
	Spec              struct {
		Queue     SSPAIRQueueRef   `json:"queue"`
		Priority  string           `json:"priority"`
		DNATRules []SSPAIRDNATRule `json:"dnat_rules"`
		LWS       struct {
			Replicas             int `json:"replicas"`
			LeaderWorkerTemplate struct {
				Leader       SSPAIRPodTemplate   `json:"leader"`
				Worker       SSPAIRPodTemplate   `json:"worker"`
				Size         int                 `json:"size"`
				VolumeMounts []SSPAIRVolumeMount `json:"volume_mounts"`
			} `json:"leader_worker_template"`
		} `json:"lws"`
	} `json:"spec"`
	Status struct {
		State                  string `json:"state"`
		ReadyReplicas          int    `json:"ready_replicas"`
		Replicas               int    `json:"replicas"`
		UpdatedReplicas        int    `json:"updated_replicas"`
		CreateTime             string `json:"create_time"`
		UpdateTime             string `json:"update_time"`
		LeaderServiceClusterIP string `json:"leader_service_cluster_ip"`
	} `json:"status"`
	TotalInstances int `json:"total_instances"`
}

type SSPAIRWorker struct {
	Name            string `json:"name"`
	UID             string `json:"uid"`
	Phase           string `json:"phase"`
	HostIP          string `json:"host_ip"`
	IP              string `json:"ip"`
	StartTime       string `json:"start_time"`
	RestartCount    int    `json:"restart_count"`
	LastStartedTime string `json:"last_started_time"`
	CreateTime      string `json:"create_time"`
}

type SSPAIRGateway struct {
	SubscriptionName  string          `json:"subscription_name"`
	ResourceGroupName string          `json:"resource_group_name"`
	Region            string          `json:"region"`
	WorkspaceName     string          `json:"workspace_name"`
	Name              string          `json:"name"`
	UID               string          `json:"uid"`
	ID                string          `json:"id"`
	DisplayName       string          `json:"display_name"`
	Ownership         SSPAIROwnership `json:"ownership"`
	ProfileName       string          `json:"-"`
	Spec              struct {
		Queue     SSPAIRQueueRef   `json:"queue"`
		Priority  string           `json:"priority"`
		DNATRules []SSPAIRDNATRule `json:"dnat_rules"`
		Replicas  int              `json:"replicas"`
		Resource  SSPAIRResource   `json:"resource"`
	} `json:"spec"`
	Status struct {
		State      string `json:"state"`
		CreateTime string `json:"create_time"`
		UpdateTime string `json:"update_time"`
	} `json:"status"`
}

type sspAIRJobListResponse struct {
	AIRs      []SSPAIRJob `json:"airs"`
	TotalSize int         `json:"total_size"`
}

type sspAIRWorkerListResponse struct {
	Workers   []SSPAIRWorker `json:"workers"`
	TotalSize int            `json:"total_size"`
}

type sspAIRGatewayListResponse struct {
	Gateways  []SSPAIRGateway `json:"infer_gateways"`
	TotalSize int             `json:"total_size"`
}

func (c *VirtualClusterClient) ListSSPAIRJobs(ctx context.Context, workspace SSPWorkspace, name string) ([]SSPAIRJob, error) {
	profile, endpoint, err := c.sspAIRWorkspaceURL(workspace, "airs")
	if err != nil {
		return nil, err
	}
	items := make([]SSPAIRJob, 0)
	for skip := 0; ; skip += 100 {
		query := endpoint.Query()
		query.Set("page_size", "100")
		query.Set("skip", strconv.Itoa(skip))
		query.Set("order_by", "create_time desc")
		if strings.TrimSpace(name) != "" {
			query.Set("filter", fmt.Sprintf(`name="%s"`, escapeSSPFilterValue(name)))
		}
		endpoint.RawQuery = query.Encode()
		var payload sspAIRJobListResponse
		if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &payload); err != nil {
			return nil, err
		}
		for index := range payload.AIRs {
			normalizeSSPAIRJob(&payload.AIRs[index], workspace, profile)
		}
		items = append(items, payload.AIRs...)
		if strings.TrimSpace(name) != "" || len(payload.AIRs) == 0 || len(items) >= payload.TotalSize || len(payload.AIRs) < 100 {
			return items, nil
		}
	}
}

func (c *VirtualClusterClient) GetSSPAIRJob(ctx context.Context, job SSPAIRJob) (*SSPAIRJob, error) {
	workspace := SSPWorkspace{Name: job.WorkspaceName, Subscription: job.SubscriptionName, ResourceGroup: job.ResourceGroupName, Region: job.Region, ProfileName: job.ProfileName}
	profile, endpoint, err := c.sspAIRWorkspaceURL(workspace, "airs", job.Name)
	if err != nil {
		return nil, err
	}
	var result SSPAIRJob
	if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &result); err != nil {
		return nil, err
	}
	normalizeSSPAIRJob(&result, workspace, profile)
	return &result, nil
}

func (c *VirtualClusterClient) ListSSPAIRWorkers(ctx context.Context, job SSPAIRJob, limit int) ([]SSPAIRWorker, int, error) {
	if limit <= 0 {
		limit = 20
	}
	workspace := SSPWorkspace{Name: job.WorkspaceName, Subscription: job.SubscriptionName, ResourceGroup: job.ResourceGroupName, Region: job.Region, ProfileName: job.ProfileName}
	profile, endpoint, err := c.sspAIRWorkspaceURL(workspace, "airs", job.Name, "workers")
	if err != nil {
		return nil, 0, err
	}
	workers := make([]SSPAIRWorker, 0, limit)
	total := 0
	for skip := 0; len(workers) < limit; {
		pageSize := limit - len(workers)
		if pageSize > 100 {
			pageSize = 100
		}
		query := endpoint.Query()
		query.Set("page_size", strconv.Itoa(pageSize))
		query.Set("skip", strconv.Itoa(skip))
		query.Set("order_by", "group_index asc,worker_index asc")
		endpoint.RawQuery = query.Encode()
		var payload sspAIRWorkerListResponse
		if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &payload); err != nil {
			return nil, 0, err
		}
		if payload.TotalSize > total {
			total = payload.TotalSize
		}
		workers = append(workers, payload.Workers...)
		skip += len(payload.Workers)
		if len(payload.Workers) == 0 || (total > 0 && skip >= total) || len(payload.Workers) < pageSize {
			break
		}
	}
	if len(workers) > limit {
		workers = workers[:limit]
	}
	return workers, total, nil
}

func (c *VirtualClusterClient) ListSSPAIRGateways(ctx context.Context, workspace SSPWorkspace, name string) ([]SSPAIRGateway, error) {
	profile, endpoint, err := c.sspAIRWorkspaceURL(workspace, "inferGateways")
	if err != nil {
		return nil, err
	}
	items := make([]SSPAIRGateway, 0)
	for skip := 0; ; skip += 100 {
		query := endpoint.Query()
		query.Set("page_size", "100")
		query.Set("skip", strconv.Itoa(skip))
		query.Set("order_by", "create_time desc")
		if strings.TrimSpace(name) != "" {
			query.Set("filter", fmt.Sprintf(`name="%s"`, escapeSSPFilterValue(name)))
		}
		endpoint.RawQuery = query.Encode()
		var payload sspAIRGatewayListResponse
		if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &payload); err != nil {
			return nil, err
		}
		for index := range payload.Gateways {
			normalizeSSPAIRGateway(&payload.Gateways[index], workspace, profile)
		}
		items = append(items, payload.Gateways...)
		if strings.TrimSpace(name) != "" || len(payload.Gateways) == 0 || len(items) >= payload.TotalSize || len(payload.Gateways) < 100 {
			return items, nil
		}
	}
}

func (c *VirtualClusterClient) sspAIRWorkspaceURL(workspace SSPWorkspace, suffix ...string) (clientProfile, *url.URL, error) {
	profile, ok := c.clientProfileByName(workspace.ProfileName)
	if !ok {
		return clientProfile{}, nil, fmt.Errorf("platform profile %q not found", workspace.ProfileName)
	}
	subscription := firstNonEmpty(strings.TrimSpace(workspace.Subscription), strings.TrimSpace(profile.Subscription))
	resourceGroup := firstNonEmpty(strings.TrimSpace(workspace.ResourceGroup), strings.TrimSpace(profile.ResourceGroup), defaultResourceGroup)
	region := firstNonEmpty(strings.TrimSpace(workspace.Region), strings.TrimSpace(profile.Region))
	if subscription == "" || region == "" || strings.TrimSpace(workspace.Name) == "" {
		return clientProfile{}, nil, fmt.Errorf("AIR workspace subscription, region and name are required")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(profile.KubernetesBaseURL), "/")
	if baseURL == "" {
		return clientProfile{}, nil, fmt.Errorf("profile %q has no kubernetes_base_url", profile.Name)
	}
	endpoint, _ := url.Parse(baseURL)
	parts := []string{"air", "data", "v1", "subscriptions", subscription, "resourceGroups", resourceGroup, "regions", region, "workspaces", workspace.Name}
	parts = append(parts, suffix...)
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	endpoint.Path = "/" + strings.Join(parts, "/")
	return profile, endpoint, nil
}

func normalizeSSPAIRJob(job *SSPAIRJob, workspace SSPWorkspace, profile clientProfile) {
	job.ProfileName = profile.Name
	job.WorkspaceName = firstNonEmpty(job.WorkspaceName, workspace.Name)
	job.SubscriptionName = firstNonEmpty(job.SubscriptionName, workspace.Subscription, profile.Subscription)
	job.ResourceGroupName = firstNonEmpty(job.ResourceGroupName, workspace.ResourceGroup, profile.ResourceGroup, defaultResourceGroup)
	job.Region = firstNonEmpty(job.Region, workspace.Region, profile.Region)
}

func normalizeSSPAIRGateway(gateway *SSPAIRGateway, workspace SSPWorkspace, profile clientProfile) {
	gateway.ProfileName = profile.Name
	gateway.WorkspaceName = firstNonEmpty(gateway.WorkspaceName, workspace.Name)
	gateway.SubscriptionName = firstNonEmpty(gateway.SubscriptionName, workspace.Subscription, profile.Subscription)
	gateway.ResourceGroupName = firstNonEmpty(gateway.ResourceGroupName, workspace.ResourceGroup, profile.ResourceGroup, defaultResourceGroup)
	gateway.Region = firstNonEmpty(gateway.Region, workspace.Region, profile.Region)
}
