package platform

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	ClusterD                      = "d"
	ClusterDCloud                 = "dcloud"
	defaultSubscriptionDCloud     = "019a575c-9a53-71ab-8028-2b0383d7a02f"
	defaultAPIBaseURL             = "https://management.d.pjlab.org.cn"
	defaultKubernetesBaseURL      = "https://compute.d.pjlab.org.cn"
	defaultIAMBaseURL             = "https://iam.d.pjlab.org.cn"
	defaultCloudAPIBaseURL        = "https://management-cloud.d.pjlab.org.cn"
	defaultCloudKubernetesBaseURL = "https://compute-cloud.d.pjlab.org.cn"
	defaultCloudIAMBaseURL        = "https://iam-cloud.d.pjlab.org.cn"
	defaultResourceGroup          = "default"
	defaultRegion                 = "cn-pj-01"
	defaultPageLimit              = 100
	defaultConfigPath             = "/root/.rayctl/platform.json"
)

type VirtualClusterClient struct {
	accessKey         string
	secretKey         string
	baseURL           string
	kubernetesBaseURL string
	iamBaseURL        string
	subscription      string
	resourceGroup     string
	region            string
	httpClient        *http.Client
}

type VirtualCluster struct {
	UID         string `json:"uid"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

type StorageVolumeResource struct {
	ID                       string `json:"id"`
	RID                      string `json:"rid"`
	Name                     string `json:"name"`
	DisplayName              string `json:"display_name"`
	Zone                     string `json:"zone"`
	ResourceGroupName        string `json:"resource_group_name"`
	ResourceGroupDisplayName string `json:"resource_group_display_name"`
}

type ECSVirtualMachine struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	UID        string `json:"uid"`
	DisplayName string `json:"display_name"`
	CreatorID  string `json:"creator_id"`
	State      string `json:"state"`
	Properties struct {
		Hostname          string `json:"hostname"`
		MachineType       string `json:"machine_type"`
		VirtualMachineType string `json:"virtual_machine_type"`
		ImageID           string `json:"image_id"`
		Metadata          struct {
			Items []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"items"`
		} `json:"metadata"`
		NetworkInterfaces []struct {
			Properties struct {
				IPv4Addr string `json:"ip_v4_addr"`
				VPCInfo  struct {
					UID         string `json:"uid"`
					Name        string `json:"name"`
					DisplayName string `json:"display_name"`
				} `json:"vpc_info"`
			} `json:"properties"`
		} `json:"network_interfaces"`
	} `json:"properties"`
}

type AISpace struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	UID         string `json:"uid"`
	DisplayName string `json:"display_name"`
	CreatorID   string `json:"creator_id"`
	State       string `json:"state"`
	Properties  struct {
		Type      string `json:"type"`
		ImagePath string `json:"image_path"`
		HostIP    string `json:"host_ip"`
		VirtualMachineProperties struct {
			MachineType       string `json:"machine_type"`
			NetworkInterfaces []struct {
				Properties struct {
					IPv4Addr string `json:"ip_v4_addr"`
					VPCInfo  struct {
						UID         string `json:"uid"`
						Name        string `json:"name"`
						DisplayName string `json:"display_name"`
					} `json:"vpc_info"`
				} `json:"properties"`
			} `json:"network_interfaces"`
		} `json:"virtual_machine_properties"`
	} `json:"properties"`
}

type IAMUser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
}

type virtualClusterListResponse struct {
	VirtualClusters []VirtualCluster `json:"virtual_clusters"`
	Items           []VirtualCluster `json:"items"`
	NextPageToken   string           `json:"next_page_token"`
}

type ecsVirtualMachineListResponse struct {
	VirtualMachines []ECSVirtualMachine `json:"virtual_machines"`
}

type aiSpaceListResponse struct {
	AISpaces []AISpace `json:"ai_spaces"`
}

type iamUserListResponse struct {
	Users []IAMUser `json:"users"`
}

type storageVolumePageResponse struct {
	Resources     []StorageVolumeResource `json:"resources"`
	NextPageToken string                  `json:"next_page_token"`
}

type config struct {
	AccessKey         string `json:"access_key"`
	SecretKey         string `json:"secret_key"`
	Subscription      string `json:"subscription_id"`
	Cluster           string `json:"cluster"`
	BaseURL           string `json:"base_url"`
	KubernetesBaseURL string `json:"kubernetes_base_url"`
	IAMBaseURL        string `json:"iam_base_url"`
	ResourceGroup     string `json:"resource_group"`
	Region            string `json:"region"`
}

type ConfigSnapshot struct {
	AccessKey         string `json:"access_key"`
	SecretKey         string `json:"secret_key"`
	Subscription      string `json:"subscription_id"`
	Cluster           string `json:"cluster"`
	BaseURL           string `json:"base_url"`
	KubernetesBaseURL string `json:"kubernetes_base_url"`
	IAMBaseURL        string `json:"iam_base_url"`
	ResourceGroup     string `json:"resource_group"`
	Region            string `json:"region"`
}

func NewVirtualClusterClientFromEnv() (*VirtualClusterClient, bool) {
	if client, ok := newVirtualClusterClientFromFile(defaultConfigPath); ok {
		return client, true
	}

	accessKey := strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_ACCESS_KEY"))
	secretKey := strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_SECRET_KEY"))
	cluster := normalizeClusterName(os.Getenv("RAYCTL_PLATFORM_CLUSTER"))
	subscription := strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_SUBSCRIPTION_ID"))
	if subscription == "" {
		subscription = defaultSubscriptionForCluster(cluster)
	}
	if accessKey == "" || secretKey == "" || subscription == "" {
		return nil, false
	}

	baseURL, kubernetesBaseURL := defaultBaseURLsForCluster(cluster)
	iamBaseURL := defaultIAMBaseURLForCluster(cluster)
	if override := strings.TrimRight(strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_BASE_URL")), "/"); override != "" {
		baseURL = override
	}
	if override := strings.TrimRight(strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_KUBERNETES_BASE_URL")), "/"); override != "" {
		kubernetesBaseURL = override
	}
	if override := strings.TrimRight(strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_IAM_BASE_URL")), "/"); override != "" {
		iamBaseURL = override
	}

	resourceGroup := strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_RESOURCE_GROUP"))
	if resourceGroup == "" {
		resourceGroup = defaultResourceGroup
	}

	region := strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_REGION"))
	if region == "" {
		region = defaultRegion
	}

	return &VirtualClusterClient{
		accessKey:         accessKey,
		secretKey:         secretKey,
		baseURL:           baseURL,
		kubernetesBaseURL: kubernetesBaseURL,
		iamBaseURL:        iamBaseURL,
		subscription:      subscription,
		resourceGroup:     resourceGroup,
		region:            region,
		httpClient:        &http.Client{Timeout: 10 * time.Second},
	}, true
}

func newVirtualClusterClientFromFile(configPath string) (*VirtualClusterClient, bool) {
	content, err := os.ReadFile(filepath.Clean(configPath))
	if err != nil {
		return nil, false
	}

	var cfg config
	if err := json.Unmarshal(content, &cfg); err != nil {
		return nil, false
	}

	accessKey := strings.TrimSpace(cfg.AccessKey)
	secretKey := strings.TrimSpace(cfg.SecretKey)
	cluster := normalizeClusterName(cfg.Cluster)
	subscription := strings.TrimSpace(cfg.Subscription)
	if subscription == "" {
		subscription = defaultSubscriptionForCluster(cluster)
	}
	if accessKey == "" || secretKey == "" || subscription == "" {
		return nil, false
	}

	baseURL, kubernetesBaseURL := defaultBaseURLsForCluster(cluster)
	iamBaseURL := defaultIAMBaseURLForCluster(cluster)
	if override := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"); override != "" {
		baseURL = override
	}
	if override := strings.TrimRight(strings.TrimSpace(cfg.KubernetesBaseURL), "/"); override != "" {
		kubernetesBaseURL = override
	}
	if override := strings.TrimRight(strings.TrimSpace(cfg.IAMBaseURL), "/"); override != "" {
		iamBaseURL = override
	}

	resourceGroup := strings.TrimSpace(cfg.ResourceGroup)
	if resourceGroup == "" {
		resourceGroup = defaultResourceGroup
	}

	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = defaultRegion
	}

	return &VirtualClusterClient{
		accessKey:         accessKey,
		secretKey:         secretKey,
		baseURL:           baseURL,
		kubernetesBaseURL: kubernetesBaseURL,
		iamBaseURL:        iamBaseURL,
		subscription:      subscription,
		resourceGroup:     resourceGroup,
		region:            region,
		httpClient:        &http.Client{Timeout: 10 * time.Second},
	}, true
}

func DefaultConfigPath() string {
	return defaultConfigPath
}

func LoadConfigSnapshot(configPath string) (*ConfigSnapshot, error) {
	content, err := os.ReadFile(filepath.Clean(configPath))
	if err != nil {
		return nil, err
	}

	var cfg ConfigSnapshot
	if err := json.Unmarshal(content, &cfg); err != nil {
		return nil, err
	}
	cfg.Cluster = normalizeClusterName(cfg.Cluster)
	if strings.TrimSpace(cfg.Subscription) == "" {
		cfg.Subscription = defaultSubscriptionForCluster(cfg.Cluster)
	}
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.KubernetesBaseURL) == "" {
		baseURL, kubernetesBaseURL := defaultBaseURLsForCluster(cfg.Cluster)
		if strings.TrimSpace(cfg.BaseURL) == "" {
			cfg.BaseURL = baseURL
		}
		if strings.TrimSpace(cfg.KubernetesBaseURL) == "" {
			cfg.KubernetesBaseURL = kubernetesBaseURL
		}
	}
	if strings.TrimSpace(cfg.IAMBaseURL) == "" {
		cfg.IAMBaseURL = defaultIAMBaseURLForCluster(cfg.Cluster)
	}
	return &cfg, nil
}

func SaveConfigSnapshot(configPath string, cfg *ConfigSnapshot) error {
	if cfg == nil {
		return fmt.Errorf("platform config is required")
	}
	cfg.Cluster = normalizeClusterName(cfg.Cluster)
	if strings.TrimSpace(cfg.Subscription) == "" {
		cfg.Subscription = defaultSubscriptionForCluster(cfg.Cluster)
	}
	baseURL, kubernetesBaseURL := defaultBaseURLsForCluster(cfg.Cluster)
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = baseURL
	}
	if strings.TrimSpace(cfg.KubernetesBaseURL) == "" {
		cfg.KubernetesBaseURL = kubernetesBaseURL
	}
	if strings.TrimSpace(cfg.IAMBaseURL) == "" {
		cfg.IAMBaseURL = defaultIAMBaseURLForCluster(cfg.Cluster)
	}

	content, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Clean(configPath), append(content, '\n'), 0o600)
}

func normalizeClusterName(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", ClusterD:
		return ClusterD
	case ClusterDCloud, "cloud":
		return ClusterDCloud
	default:
		return ClusterD
	}
}

func defaultBaseURLsForCluster(cluster string) (string, string) {
	switch normalizeClusterName(cluster) {
	case ClusterDCloud:
		return defaultCloudAPIBaseURL, defaultCloudKubernetesBaseURL
	default:
		return defaultAPIBaseURL, defaultKubernetesBaseURL
	}
}

func defaultIAMBaseURLForCluster(cluster string) string {
	switch normalizeClusterName(cluster) {
	case ClusterDCloud:
		return defaultCloudIAMBaseURL
	default:
		return defaultIAMBaseURL
	}
}

func defaultSubscriptionForCluster(cluster string) string {
	switch normalizeClusterName(cluster) {
	case ClusterDCloud:
		return defaultSubscriptionDCloud
	default:
		return ""
	}
}

func (c *VirtualClusterClient) ResolveDisplayNames(ctx context.Context, uids []string) (map[string]string, error) {
	uniqueUIDs := make(map[string]struct{}, len(uids))
	for _, uid := range uids {
		uid = strings.TrimSpace(uid)
		if uid == "" {
			continue
		}
		uniqueUIDs[uid] = struct{}{}
	}
	if len(uniqueUIDs) == 0 {
		return map[string]string{}, nil
	}

	clusters, err := c.listVirtualClusters(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string, len(uniqueUIDs))
	for _, cluster := range clusters {
		if _, ok := uniqueUIDs[cluster.UID]; !ok {
			continue
		}
		result[cluster.UID] = firstNonEmpty(cluster.Name, cluster.DisplayName, cluster.UID)
	}

	return result, nil
}

func (c *VirtualClusterClient) ListVirtualClusters(ctx context.Context) ([]VirtualCluster, error) {
	return c.listVirtualClusters(ctx)
}

func (c *VirtualClusterClient) ListECSVirtualMachines(ctx context.Context) ([]ECSVirtualMachine, error) {
	skip := 0
	result := make([]ECSVirtualMachine, 0)
	for {
		u, _ := url.Parse(c.baseURL)
		u.Path = "/compute/ecs/v2/subscriptions/-/resourceGroups/-/zones/-/virtualMachines"
		query := u.Query()
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("skip", fmt.Sprintf("%d", skip))
		query.Set("page_token", "")
		query.Set("order_by", "created_at desc")
		u.RawQuery = query.Encode()

		var payload ecsVirtualMachineListResponse
		if err := c.getJSON(ctx, u.String(), &payload); err != nil {
			return nil, err
		}
		if len(payload.VirtualMachines) == 0 {
			break
		}
		result = append(result, payload.VirtualMachines...)
		if len(payload.VirtualMachines) < defaultPageLimit {
			break
		}
		skip += len(payload.VirtualMachines)
	}
	return result, nil
}

func (c *VirtualClusterClient) ListAISpaces(ctx context.Context) ([]AISpace, error) {
	skip := 0
	result := make([]AISpace, 0)
	for {
		u, _ := url.Parse(c.baseURL)
		u.Path = "/compute/ais/v1/subscriptions/-/resourceGroups/-/zones/-/aiSpaces"
		query := u.Query()
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("skip", fmt.Sprintf("%d", skip))
		query.Set("page_token", "")
		query.Set("order_by", "created_at desc")
		query.Set("filter", "")
		u.RawQuery = query.Encode()

		var payload aiSpaceListResponse
		if err := c.getJSON(ctx, u.String(), &payload); err != nil {
			return nil, err
		}
		if len(payload.AISpaces) == 0 {
			break
		}
		result = append(result, payload.AISpaces...)
		if len(payload.AISpaces) < defaultPageLimit {
			break
		}
		skip += len(payload.AISpaces)
	}
	return result, nil
}

func (c *VirtualClusterClient) ResolveUsernames(ctx context.Context, ids []string) (map[string]string, error) {
	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return map[string]string{}, nil
	}

	result := make(map[string]string, len(unique))
	for start := 0; start < len(unique); start += defaultPageLimit {
		end := start + defaultPageLimit
		if end > len(unique) {
			end = len(unique)
		}
		chunk := unique[start:end]

		filters := make([]string, 0, len(chunk))
		for _, id := range chunk {
			filters = append(filters, fmt.Sprintf(`id="%s"`, id))
		}

		u, _ := url.Parse(c.iamBaseURL)
		u.Path = "/iam/idp/v1/getUsers"
		query := u.Query()
		query.Set("includeAdmin", "true")
		query.Set("page_token", "1")
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("order_by", "create_time desc")
		query.Set("filter", strings.Join(filters, " OR "))
		u.RawQuery = query.Encode()

		var payload iamUserListResponse
		if err := c.getJSON(ctx, u.String(), &payload); err != nil {
			return nil, err
		}
		for _, user := range payload.Users {
			result[user.ID] = firstNonEmpty(user.Username, user.Name, user.ID)
		}
	}

	return result, nil
}

func (c *VirtualClusterClient) GetVolcanoJob(ctx context.Context, vclusterName string, namespace string, jobName string) (*unstructured.Unstructured, error) {
	reqURL := c.kubernetesResourceURL(vclusterName, fmt.Sprintf("/apis/batch.volcano.sh/v1alpha1/namespaces/%s/jobs/%s", namespace, jobName), nil)
	var obj unstructured.Unstructured
	if err := c.getJSON(ctx, reqURL, &obj); err != nil {
		return nil, err
	}
	return &obj, nil
}

func (c *VirtualClusterClient) GetPodGroup(ctx context.Context, vclusterName string, namespace string, podGroupName string) (*unstructured.Unstructured, error) {
	reqURL := c.kubernetesResourceURL(vclusterName, fmt.Sprintf("/apis/scheduling.volcano.sh/v1beta1/namespaces/%s/podgroups/%s", namespace, podGroupName), nil)
	var obj unstructured.Unstructured
	if err := c.getJSON(ctx, reqURL, &obj); err != nil {
		return nil, err
	}
	return &obj, nil
}

func (c *VirtualClusterClient) GetSecret(ctx context.Context, vclusterName string, namespace string, secretName string) (*corev1.Secret, error) {
	reqURL := c.kubernetesResourceURL(vclusterName, fmt.Sprintf("/api/v1/namespaces/%s/secrets/%s", namespace, secretName), nil)
	var secret corev1.Secret
	if err := c.getJSON(ctx, reqURL, &secret); err != nil {
		return nil, err
	}
	return &secret, nil
}

func (c *VirtualClusterClient) GetPersistentVolumeClaim(ctx context.Context, vclusterName string, namespace string, claimName string) (*corev1.PersistentVolumeClaim, error) {
	reqURL := c.kubernetesResourceURL(vclusterName, fmt.Sprintf("/api/v1/namespaces/%s/persistentvolumeclaims/%s", namespace, claimName), nil)
	var pvc corev1.PersistentVolumeClaim
	if err := c.getJSON(ctx, reqURL, &pvc); err != nil {
		return nil, err
	}
	return &pvc, nil
}

func (c *VirtualClusterClient) GetPersistentVolume(ctx context.Context, vclusterName string, pvName string) (*corev1.PersistentVolume, error) {
	reqURL := c.kubernetesResourceURL(vclusterName, fmt.Sprintf("/api/v1/persistentvolumes/%s", pvName), nil)
	var pv corev1.PersistentVolume
	if err := c.getJSON(ctx, reqURL, &pv); err != nil {
		return nil, err
	}
	return &pv, nil
}

func (c *VirtualClusterClient) ListStorageVolumeResources(ctx context.Context, zone string) ([]StorageVolumeResource, error) {
	pageToken := "1"
	resources := make([]StorageVolumeResource, 0)

	for {
		u, _ := url.Parse(c.baseURL)
		u.Path = "/rmh/v1/resources:page"
		query := u.Query()
		query.Set("filter", fmt.Sprintf(`resource_type="storage.afs.v1.volume" OR resource_type="storage.afs.v2.volume"  AND zone="*%s*"`, zone))
		query.Set("page_size", "200")
		query.Set("page_token", pageToken)
		u.RawQuery = query.Encode()

		var payload storageVolumePageResponse
		if err := c.getJSON(ctx, u.String(), &payload); err != nil {
			return nil, err
		}

		resources = append(resources, payload.Resources...)
		if strings.TrimSpace(payload.NextPageToken) == "" {
			break
		}
		pageToken = payload.NextPageToken
	}

	return resources, nil
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

	if resource, err := c.FindStorageVolumeResourceByUID(ctx, identifier); err == nil && resource != nil {
		return resource, nil
	}
	return c.findStorageVolumeResourceByFieldFragment(ctx, "name", identifier)
}

func (c *VirtualClusterClient) findStorageVolumeResourceByFragment(ctx context.Context, fragment string) (*StorageVolumeResource, error) {
	return c.findStorageVolumeResourceByFieldFragment(ctx, "uid", fragment)
}

func (c *VirtualClusterClient) findStorageVolumeResourceByFieldFragment(ctx context.Context, field string, fragment string) (*StorageVolumeResource, error) {
	fragment = strings.TrimSpace(fragment)
	if fragment == "" {
		return nil, fmt.Errorf("storage volume fragment is required")
	}

	u, _ := url.Parse(c.baseURL)
	u.Path = "/rmh/v1/resources:page"
	query := u.Query()

	filter := fmt.Sprintf(`resource_type="storage.afs.v1.volume" OR resource_type="storage.afs.v2.volume"  AND %s="*%s*"`, field, fragment)
	query.Set("filter", filter)
	query.Set("page_size", "10")
	query.Set("page_token", "1")
	u.RawQuery = query.Encode()

	var payload storageVolumePageResponse
	if err := c.postJSON(ctx, u.String(), map[string]any{}, &payload); err != nil {
		return nil, err
	}

	for i := range payload.Resources {
		resource := &payload.Resources[i]
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
	if len(payload.Resources) > 0 {
		return &payload.Resources[0], nil
	}
	return nil, fmt.Errorf("storage volume resource with fragment %q not found", fragment)
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

func (c *VirtualClusterClient) ListJobPods(ctx context.Context, vclusterName string, namespace string, jobName string) ([]corev1.Pod, error) {
	query := url.Values{}
	query.Set("labelSelector", fmt.Sprintf("volcano.sh/job-name=%s", jobName))
	query.Set("filter", fmt.Sprintf("namespace=%q", namespace))
	query.Set("order", "name asc")

	reqURL := c.kubernetesResourceURL(vclusterName, "/api/v1/pods", query)
	var podList corev1.PodList
	if err := c.getJSON(ctx, reqURL, &podList); err != nil {
		return nil, err
	}
	return podList.Items, nil
}

func (c *VirtualClusterClient) ListEvents(ctx context.Context, vclusterName string, namespace string, name string, kind string) ([]corev1.Event, error) {
	query := url.Values{}
	fieldSelector := []string{fmt.Sprintf("involvedObject.name=%s", name)}
	if strings.TrimSpace(kind) != "" {
		fieldSelector = append(fieldSelector, fmt.Sprintf("involvedObject.kind=%s", kind))
	}
	query.Set("fieldSelector", strings.Join(fieldSelector, ","))

	reqURL := c.kubernetesClusterURL(vclusterName, fmt.Sprintf("/api/v1/namespaces/%s/events", namespace), query)
	var eventList corev1.EventList
	if err := c.getJSON(ctx, reqURL, &eventList); err != nil {
		return nil, err
	}
	return eventList.Items, nil
}

func (c *VirtualClusterClient) GetPodLogs(ctx context.Context, vclusterName string, namespace string, podName string, tailLines int64) ([]string, error) {
	query := url.Values{}
	query.Set("tailLines", fmt.Sprintf("%d", tailLines))

	reqURL := c.kubernetesClusterURL(vclusterName, fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/log", namespace, podName), query)
	body, err := c.getText(ctx, reqURL)
	if err != nil {
		return nil, err
	}

	lines := make([]string, 0)
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, "\n")
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return []string{"<empty log output>"}, nil
	}
	return lines, nil
}

func (c *VirtualClusterClient) listVirtualClusters(ctx context.Context) ([]VirtualCluster, error) {
	skip := 0
	clusters := make([]VirtualCluster, 0)

	for {
		reqURL := c.listURL(skip)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build virtual cluster request: %w", err)
		}

		headers, err := c.authHeaders(reqURL, http.MethodGet)
		if err != nil {
			return nil, err
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request virtual clusters: %w", err)
		}

		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read virtual cluster response: %w", readErr)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("virtual cluster api returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var payload virtualClusterListResponse
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("decode virtual cluster response: %w", err)
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

func (c *VirtualClusterClient) listURL(skip int) string {
	u, _ := url.Parse(c.baseURL)
	u.Path = "/compute/ecp/v1/subscriptions/-/resourceGroups/-/regions/-/virtualClusters"

	query := u.Query()
	query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
	query.Set("skip", fmt.Sprintf("%d", skip))
	u.RawQuery = query.Encode()
	return u.String()
}

func (c *VirtualClusterClient) authHeaders(reqURL string, method string) (map[string]string, error) {
	parsedURL, err := url.Parse(reqURL)
	if err != nil {
		return nil, fmt.Errorf("parse auth url: %w", err)
	}

	dateHeader := time.Now().UTC().Format(http.TimeFormat)
	requestTarget := parsedURL.Path
	if parsedURL.RawQuery != "" {
		requestTarget += "?" + parsedURL.RawQuery
	}

	signContent := fmt.Sprintf("date: %s\nhost: %s\n@request-target: %s %s", dateHeader, parsedURL.Host, strings.ToLower(method), requestTarget)

	mac := hmac.New(sha256.New, []byte(c.secretKey))
	_, _ = mac.Write([]byte(signContent))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return map[string]string{
		"Date":          dateHeader,
		"Host":          parsedURL.Host,
		"Accept":        "application/json",
		"Authorization": fmt.Sprintf(`hmac accesskey="%s", algorithm="hmac-sha256", headers="date host @request-target", signature="%s"`, c.accessKey, signature),
	}, nil
}

func (c *VirtualClusterClient) getJSON(ctx context.Context, reqURL string, out any) error {
	body, err := c.doRequest(ctx, http.MethodGet, reqURL, "", nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	return nil
}

func (c *VirtualClusterClient) postJSON(ctx context.Context, reqURL string, payload any, out any) error {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request for %s: %w", reqURL, err)
	}

	body, err := c.doRequest(ctx, http.MethodPost, reqURL, "application/json", bodyBytes)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	return nil
}

func (c *VirtualClusterClient) getText(ctx context.Context, reqURL string) (string, error) {
	body, err := c.doRequest(ctx, http.MethodGet, reqURL, "", nil)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *VirtualClusterClient) doRequest(ctx context.Context, method string, reqURL string, contentType string, requestBody []byte) ([]byte, error) {
	var bodyReader io.Reader
	if len(requestBody) > 0 {
		bodyReader = bytes.NewReader(requestBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	headers, err := c.authHeaders(reqURL, method)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if strings.TrimSpace(contentType) != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", reqURL, err)
	}

	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read response from %s: %w", reqURL, readErr)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request %s returned %d: %s", reqURL, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (c *VirtualClusterClient) kubernetesResourceURL(vclusterName string, path string, query url.Values) string {
	u, _ := url.Parse(c.kubernetesBaseURL)
	u.Path = fmt.Sprintf("/ecp/v1/kubernetes/virtualClusters/%s%s", vclusterName, path)
	if query != nil {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

func (c *VirtualClusterClient) kubernetesClusterURL(vclusterName string, path string, query url.Values) string {
	u, _ := url.Parse(c.kubernetesBaseURL)
	u.Path = fmt.Sprintf("/ecp/v1/clusters/%s%s", vclusterName, path)
	if query != nil {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
