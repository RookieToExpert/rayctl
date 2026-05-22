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
	"sort"
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
	currentProfile    string
	profiles          map[string]clientProfile
	httpClient        *http.Client
}

type clientProfile struct {
	Name              string
	AccessKey         string
	SecretKey         string
	BaseURL           string
	KubernetesBaseURL string
	IAMBaseURL        string
	Subscription      string
	ResourceGroup     string
	Region            string
}

type VirtualCluster struct {
	UID         string `json:"uid"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	ProfileName string `json:"-"`
}

type StorageVolumeResource struct {
	ID                       string `json:"id"`
	RID                      string `json:"rid"`
	Name                     string `json:"name"`
	DisplayName              string `json:"display_name"`
	Zone                     string `json:"zone"`
	ResourceGroupName        string `json:"resource_group_name"`
	ResourceGroupDisplayName string `json:"resource_group_display_name"`
	ProfileName              string `json:"-"`
}

type ECSVirtualMachine struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	UID         string `json:"uid"`
	DisplayName string `json:"display_name"`
	CreatorID   string `json:"creator_id"`
	State       string `json:"state"`
	ProfileName string `json:"-"`
	Properties struct {
		Hostname           string `json:"hostname"`
		MachineType        string `json:"machine_type"`
		VirtualMachineType string `json:"virtual_machine_type"`
		ImageID            string `json:"image_id"`
		Metadata           struct {
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
	ProfileName string `json:"-"`
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

type AIComputeNode struct {
	ID          string `json:"id"`
	UID         string `json:"uid"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	TenantID    string `json:"tenant_id"`
	State       string `json:"state"`
	Properties  struct {
		MachineType        string `json:"machine_type"`
		VirtualClusterName string `json:"virtual_cluster_name"`
		HostIP             string `json:"host_ip"`
		HostName           string `json:"host_name"`
	} `json:"properties"`
	ProfileName string `json:"-"`
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

type aiComputeNodeListResponse struct {
	AIComputeNodes []AIComputeNode `json:"ai_compute_nodes"`
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

type multiProfileConfig struct {
	CurrentProfile string            `json:"current_profile"`
	Profiles       map[string]config `json:"profiles"`
}

type ConfigProfile struct {
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
	CurrentProfile    string                   `json:"current_profile,omitempty"`
	Profiles          map[string]ConfigProfile `json:"profiles,omitempty"`
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
		currentProfile:    "env",
		profiles: map[string]clientProfile{
			"env": {
				Name:              "env",
				AccessKey:         accessKey,
				SecretKey:         secretKey,
				BaseURL:           baseURL,
				KubernetesBaseURL: kubernetesBaseURL,
				IAMBaseURL:        iamBaseURL,
				Subscription:      subscription,
				ResourceGroup:     resourceGroup,
				Region:            region,
			},
		},
		httpClient:        &http.Client{Timeout: 10 * time.Second},
	}, true
}

func newVirtualClusterClientFromFile(configPath string) (*VirtualClusterClient, bool) {
	content, err := os.ReadFile(filepath.Clean(configPath))
	if err != nil {
		return nil, false
	}

	var multiCfg multiProfileConfig
	if err := json.Unmarshal(content, &multiCfg); err == nil && len(multiCfg.Profiles) > 0 {
		currentProfile := normalizeCurrentProfileName(multiCfg.CurrentProfile, multiCfg.Profiles)
		profiles := make(map[string]clientProfile, len(multiCfg.Profiles))
		for name, raw := range multiCfg.Profiles {
			profile, ok := makeClientProfile(name, raw)
			if !ok {
				continue
			}
			profiles[name] = profile
		}
		current, ok := profiles[currentProfile]
		if !ok {
			for name, profile := range profiles {
				currentProfile = name
				current = profile
				ok = true
				break
			}
		}
		if !ok {
			return nil, false
		}
		return &VirtualClusterClient{
			accessKey:         current.AccessKey,
			secretKey:         current.SecretKey,
			baseURL:           current.BaseURL,
			kubernetesBaseURL: current.KubernetesBaseURL,
			iamBaseURL:        current.IAMBaseURL,
			subscription:      current.Subscription,
			resourceGroup:     current.ResourceGroup,
			region:            current.Region,
			currentProfile:    currentProfile,
			profiles:          profiles,
			httpClient:        &http.Client{Timeout: 10 * time.Second},
		}, true
	}

	var cfg config
	if err := json.Unmarshal(content, &cfg); err != nil {
		return nil, false
	}
	profile, ok := makeClientProfile("default", cfg)
	if !ok {
		return nil, false
	}

	return &VirtualClusterClient{
		accessKey:         profile.AccessKey,
		secretKey:         profile.SecretKey,
		baseURL:           profile.BaseURL,
		kubernetesBaseURL: profile.KubernetesBaseURL,
		iamBaseURL:        profile.IAMBaseURL,
		subscription:      profile.Subscription,
		resourceGroup:     profile.ResourceGroup,
		region:            profile.Region,
		currentProfile:    profile.Name,
		profiles:          map[string]clientProfile{profile.Name: profile},
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

	var multiCfg multiProfileConfig
	if err := json.Unmarshal(content, &multiCfg); err == nil && len(multiCfg.Profiles) > 0 {
		currentProfile := normalizeCurrentProfileName(multiCfg.CurrentProfile, multiCfg.Profiles)
		snapshot := &ConfigSnapshot{
			CurrentProfile: currentProfile,
			Profiles:       make(map[string]ConfigProfile, len(multiCfg.Profiles)),
		}
		for name, raw := range multiCfg.Profiles {
			profile := ConfigProfile{
				AccessKey:         raw.AccessKey,
				SecretKey:         raw.SecretKey,
				Subscription:      raw.Subscription,
				Cluster:           normalizeClusterName(raw.Cluster),
				BaseURL:           strings.TrimRight(strings.TrimSpace(raw.BaseURL), "/"),
				KubernetesBaseURL: strings.TrimRight(strings.TrimSpace(raw.KubernetesBaseURL), "/"),
				IAMBaseURL:        strings.TrimRight(strings.TrimSpace(raw.IAMBaseURL), "/"),
				ResourceGroup:     strings.TrimSpace(raw.ResourceGroup),
				Region:            strings.TrimSpace(raw.Region),
			}
			applyConfigProfileDefaults(&profile)
			snapshot.Profiles[name] = profile
		}
		if current, ok := snapshot.Profiles[currentProfile]; ok {
			snapshot.AccessKey = current.AccessKey
			snapshot.SecretKey = current.SecretKey
			snapshot.Subscription = current.Subscription
			snapshot.Cluster = current.Cluster
			snapshot.BaseURL = current.BaseURL
			snapshot.KubernetesBaseURL = current.KubernetesBaseURL
			snapshot.IAMBaseURL = current.IAMBaseURL
			snapshot.ResourceGroup = current.ResourceGroup
			snapshot.Region = current.Region
		}
		return snapshot, nil
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
	if len(cfg.Profiles) > 0 {
		currentProfile := normalizeCurrentProfileName(cfg.CurrentProfile, configProfileMapToConfig(cfg.Profiles))
		if currentProfile == "" {
			currentProfile = cfg.CurrentProfile
		}
		profiles := cloneConfigProfiles(cfg.Profiles)
		current := profiles[currentProfile]
		if strings.TrimSpace(cfg.AccessKey) != "" {
			current.AccessKey = cfg.AccessKey
		}
		if strings.TrimSpace(cfg.SecretKey) != "" {
			current.SecretKey = cfg.SecretKey
		}
		if strings.TrimSpace(cfg.Subscription) != "" || strings.TrimSpace(cfg.Cluster) != "" {
			current.Subscription = cfg.Subscription
		}
		if strings.TrimSpace(cfg.Cluster) != "" {
			current.Cluster = cfg.Cluster
		}
		if strings.TrimSpace(cfg.BaseURL) != "" || strings.TrimSpace(cfg.Cluster) != "" {
			current.BaseURL = cfg.BaseURL
		}
		if strings.TrimSpace(cfg.KubernetesBaseURL) != "" || strings.TrimSpace(cfg.Cluster) != "" {
			current.KubernetesBaseURL = cfg.KubernetesBaseURL
		}
		if strings.TrimSpace(cfg.IAMBaseURL) != "" || strings.TrimSpace(cfg.Cluster) != "" {
			current.IAMBaseURL = cfg.IAMBaseURL
		}
		if strings.TrimSpace(cfg.ResourceGroup) != "" {
			current.ResourceGroup = cfg.ResourceGroup
		}
		if strings.TrimSpace(cfg.Region) != "" {
			current.Region = cfg.Region
		}
		applyConfigProfileDefaults(&current)
		profiles[currentProfile] = current

		output := multiProfileConfig{
			CurrentProfile: currentProfile,
			Profiles:       make(map[string]config, len(profiles)),
		}
		for name, profile := range profiles {
			output.Profiles[name] = config{
				AccessKey:         profile.AccessKey,
				SecretKey:         profile.SecretKey,
				Subscription:      profile.Subscription,
				Cluster:           profile.Cluster,
				BaseURL:           profile.BaseURL,
				KubernetesBaseURL: profile.KubernetesBaseURL,
				IAMBaseURL:        profile.IAMBaseURL,
				ResourceGroup:     profile.ResourceGroup,
				Region:            profile.Region,
			}
		}

		content, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		dir := filepath.Dir(configPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Clean(configPath), append(content, '\n'), 0o600)
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

func makeClientProfile(name string, cfg config) (clientProfile, bool) {
	cluster := normalizeClusterName(cfg.Cluster)
	accessKey := strings.TrimSpace(cfg.AccessKey)
	secretKey := strings.TrimSpace(cfg.SecretKey)
	subscription := strings.TrimSpace(cfg.Subscription)
	if subscription == "" {
		subscription = defaultSubscriptionForCluster(cluster)
	}
	if accessKey == "" || secretKey == "" || subscription == "" {
		return clientProfile{}, false
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
	return clientProfile{
		Name:              strings.TrimSpace(name),
		AccessKey:         accessKey,
		SecretKey:         secretKey,
		BaseURL:           baseURL,
		KubernetesBaseURL: kubernetesBaseURL,
		IAMBaseURL:        iamBaseURL,
		Subscription:      subscription,
		ResourceGroup:     resourceGroup,
		Region:            region,
	}, true
}

func normalizeCurrentProfileName(current string, profiles map[string]config) string {
	current = strings.TrimSpace(current)
	if current != "" {
		if _, ok := profiles[current]; ok {
			return current
		}
	}
	if _, ok := profiles["ailabdev"]; ok {
		return "ailabdev"
	}
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func configProfileMapToConfig(values map[string]ConfigProfile) map[string]config {
	result := make(map[string]config, len(values))
	for name, value := range values {
		result[name] = config{
			AccessKey:         value.AccessKey,
			SecretKey:         value.SecretKey,
			Subscription:      value.Subscription,
			Cluster:           value.Cluster,
			BaseURL:           value.BaseURL,
			KubernetesBaseURL: value.KubernetesBaseURL,
			IAMBaseURL:        value.IAMBaseURL,
			ResourceGroup:     value.ResourceGroup,
			Region:            value.Region,
		}
	}
	return result
}

func applyConfigProfileDefaults(profile *ConfigProfile) {
	if profile == nil {
		return
	}
	profile.Cluster = normalizeClusterName(profile.Cluster)
	if strings.TrimSpace(profile.Subscription) == "" {
		profile.Subscription = defaultSubscriptionForCluster(profile.Cluster)
	}
	baseURL, kubernetesBaseURL := defaultBaseURLsForCluster(profile.Cluster)
	if strings.TrimSpace(profile.BaseURL) == "" {
		profile.BaseURL = baseURL
	}
	if strings.TrimSpace(profile.KubernetesBaseURL) == "" {
		profile.KubernetesBaseURL = kubernetesBaseURL
	}
	if strings.TrimSpace(profile.IAMBaseURL) == "" {
		profile.IAMBaseURL = defaultIAMBaseURLForCluster(profile.Cluster)
	}
	if strings.TrimSpace(profile.ResourceGroup) == "" {
		profile.ResourceGroup = defaultResourceGroup
	}
	if strings.TrimSpace(profile.Region) == "" {
		profile.Region = defaultRegion
	}
}

func cloneConfigProfiles(values map[string]ConfigProfile) map[string]ConfigProfile {
	result := make(map[string]ConfigProfile, len(values))
	for name, value := range values {
		result[name] = value
	}
	return result
}

func (c *VirtualClusterClient) orderedProfiles() []clientProfile {
	if len(c.profiles) == 0 {
		return []clientProfile{{
			Name:              firstNonEmpty(c.currentProfile, "default"),
			AccessKey:         c.accessKey,
			SecretKey:         c.secretKey,
			BaseURL:           c.baseURL,
			KubernetesBaseURL: c.kubernetesBaseURL,
			IAMBaseURL:        c.iamBaseURL,
			Subscription:      c.subscription,
			ResourceGroup:     c.resourceGroup,
			Region:            c.region,
		}}
	}
	profiles := make([]clientProfile, 0, len(c.profiles))
	if current, ok := c.profiles[c.currentProfile]; ok {
		profiles = append(profiles, current)
	}
	names := make([]string, 0, len(c.profiles))
	for name := range c.profiles {
		if name == c.currentProfile {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		profiles = append(profiles, c.profiles[name])
	}
	return profiles
}

func (c *VirtualClusterClient) listECSVirtualMachinesWithProfile(ctx context.Context, profile clientProfile) ([]ECSVirtualMachine, error) {
	skip := 0
	result := make([]ECSVirtualMachine, 0)
	for {
		u, _ := url.Parse(profile.BaseURL)
		u.Path = "/compute/ecs/v2/subscriptions/-/resourceGroups/-/zones/-/virtualMachines"
		query := u.Query()
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("skip", fmt.Sprintf("%d", skip))
		query.Set("page_token", "")
		query.Set("order_by", "created_at desc")
		u.RawQuery = query.Encode()

		var payload ecsVirtualMachineListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
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

func (c *VirtualClusterClient) listAISpacesWithProfile(ctx context.Context, profile clientProfile) ([]AISpace, error) {
	skip := 0
	result := make([]AISpace, 0)
	for {
		u, _ := url.Parse(profile.BaseURL)
		u.Path = "/compute/ais/v1/subscriptions/-/resourceGroups/-/zones/-/aiSpaces"
		query := u.Query()
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("skip", fmt.Sprintf("%d", skip))
		query.Set("page_token", "")
		query.Set("order_by", "created_at desc")
		query.Set("filter", "")
		u.RawQuery = query.Encode()

		var payload aiSpaceListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
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

func (c *VirtualClusterClient) resolveUsernamesWithProfile(ctx context.Context, profile clientProfile, ids []string) (map[string]string, error) {
	result := make(map[string]string, len(ids))
	for start := 0; start < len(ids); start += defaultPageLimit {
		end := start + defaultPageLimit
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		filters := make([]string, 0, len(chunk))
		for _, id := range chunk {
			filters = append(filters, fmt.Sprintf(`id="%s"`, id))
		}

		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = "/iam/idp/v1/getUsers"
		query := u.Query()
		query.Set("includeAdmin", "true")
		query.Set("page_token", "1")
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("order_by", "create_time desc")
		query.Set("filter", strings.Join(filters, " OR "))
		u.RawQuery = query.Encode()

		var payload iamUserListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return nil, err
		}
		for _, user := range payload.Users {
			result[user.ID] = firstNonEmpty(user.Username, user.Name, user.ID)
		}
	}
	return result, nil
}

func (c *VirtualClusterClient) listStorageVolumeResourcesWithProfile(ctx context.Context, profile clientProfile, zone string) ([]StorageVolumeResource, error) {
	pageToken := "1"
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
		if strings.TrimSpace(payload.NextPageToken) == "" {
			break
		}
		pageToken = payload.NextPageToken
	}
	return resources, nil
}

func (c *VirtualClusterClient) listAIComputeNodesWithProfile(ctx context.Context, profile clientProfile) ([]AIComputeNode, error) {
	skip := 0
	result := make([]AIComputeNode, 0)
	for {
		u, _ := url.Parse(profile.BaseURL)
		u.Path = "/compute/ecp/v1/subscriptions/-/resourceGroups/-/zones/-/aiComputeNodes"
		query := u.Query()
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

func (c *VirtualClusterClient) ResolveDisplayNames(ctx context.Context, uids []string) (map[string]string, error) {
	names, _, err := c.ResolveDisplayNamesWithProfiles(ctx, uids)
	return names, err
}

func (c *VirtualClusterClient) ResolveDisplayNamesWithProfiles(ctx context.Context, uids []string) (map[string]string, map[string]string, error) {
	uniqueUIDs := make(map[string]struct{}, len(uids))
	for _, uid := range uids {
		uid = strings.TrimSpace(uid)
		if uid == "" {
			continue
		}
		uniqueUIDs[uid] = struct{}{}
	}
	if len(uniqueUIDs) == 0 {
		return map[string]string{}, map[string]string{}, nil
	}

	clusters, err := c.listVirtualClusters(ctx)
	if err != nil {
		return nil, nil, err
	}

	result := make(map[string]string, len(uniqueUIDs))
	profiles := make(map[string]string, len(uniqueUIDs))
	for _, cluster := range clusters {
		if _, ok := uniqueUIDs[cluster.UID]; !ok {
			continue
		}
		result[cluster.UID] = firstNonEmpty(cluster.Name, cluster.DisplayName, cluster.UID)
		if strings.TrimSpace(cluster.ProfileName) != "" {
			profiles[cluster.UID] = strings.TrimSpace(cluster.ProfileName)
		}
	}

	return result, profiles, nil
}

func (c *VirtualClusterClient) ListVirtualClusters(ctx context.Context) ([]VirtualCluster, error) {
	return c.listVirtualClusters(ctx)
}

func (c *VirtualClusterClient) ListECSVirtualMachines(ctx context.Context) ([]ECSVirtualMachine, error) {
	result := make([]ECSVirtualMachine, 0)
	seen := make(map[string]struct{})
	var lastErr error
	success := false
	for _, profile := range c.orderedProfiles() {
		items, err := c.listECSVirtualMachinesWithProfile(ctx, profile)
		if err != nil {
			lastErr = err
			continue
		}
		success = true
		for _, item := range items {
			item.ProfileName = profile.Name
			key := firstNonEmpty(strings.TrimSpace(item.UID), strings.TrimSpace(item.ID), strings.TrimSpace(item.Name))
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

func (c *VirtualClusterClient) ListAISpaces(ctx context.Context) ([]AISpace, error) {
	result := make([]AISpace, 0)
	seen := make(map[string]struct{})
	var lastErr error
	success := false
	for _, profile := range c.orderedProfiles() {
		items, err := c.listAISpacesWithProfile(ctx, profile)
		if err != nil {
			lastErr = err
			continue
		}
		success = true
		for _, item := range items {
			key := firstNonEmpty(strings.TrimSpace(item.UID), strings.TrimSpace(item.ID), strings.TrimSpace(item.Name))
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
	var lastErr error
	success := false
	remaining := append([]string(nil), unique...)
	for _, profile := range c.orderedProfiles() {
		if len(remaining) == 0 {
			break
		}
		resolved, err := c.resolveUsernamesWithProfile(ctx, profile, remaining)
		if err != nil {
			lastErr = err
			continue
		}
		success = true
		nextRemaining := make([]string, 0, len(remaining))
		for _, id := range remaining {
			if value, ok := resolved[id]; ok && strings.TrimSpace(value) != "" {
				result[id] = value
				continue
			}
			nextRemaining = append(nextRemaining, id)
		}
		remaining = nextRemaining
	}
	if len(result) > 0 || success {
		return result, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return map[string]string{}, nil
}

func (c *VirtualClusterClient) ListAIComputeNodes(ctx context.Context) ([]AIComputeNode, error) {
	result := make([]AIComputeNode, 0)
	seen := make(map[string]struct{})
	var lastErr error
	success := false
	for _, profile := range c.orderedProfiles() {
		items, err := c.listAIComputeNodesWithProfile(ctx, profile)
		if err != nil {
			lastErr = err
			continue
		}
		success = true
		for _, item := range items {
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

func (c *VirtualClusterClient) GetVolcanoJob(ctx context.Context, vclusterName string, namespace string, jobName string) (*unstructured.Unstructured, error) {
	var lastErr error
	for _, profile := range c.orderedProfiles() {
		reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, fmt.Sprintf("/apis/batch.volcano.sh/v1alpha1/namespaces/%s/jobs/%s", namespace, jobName), nil)
		var obj unstructured.Unstructured
		if err := c.getJSONWithProfile(ctx, profile, reqURL, &obj); err != nil {
			lastErr = err
			continue
		}
		return &obj, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) GetPodGroup(ctx context.Context, vclusterName string, namespace string, podGroupName string) (*unstructured.Unstructured, error) {
	var lastErr error
	for _, profile := range c.orderedProfiles() {
		reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, fmt.Sprintf("/apis/scheduling.volcano.sh/v1beta1/namespaces/%s/podgroups/%s", namespace, podGroupName), nil)
		var obj unstructured.Unstructured
		if err := c.getJSONWithProfile(ctx, profile, reqURL, &obj); err != nil {
			lastErr = err
			continue
		}
		return &obj, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) GetSecret(ctx context.Context, vclusterName string, namespace string, secretName string) (*corev1.Secret, error) {
	var lastErr error
	for _, profile := range c.orderedProfiles() {
		reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, fmt.Sprintf("/api/v1/namespaces/%s/secrets/%s", namespace, secretName), nil)
		var secret corev1.Secret
		if err := c.getJSONWithProfile(ctx, profile, reqURL, &secret); err != nil {
			lastErr = err
			continue
		}
		return &secret, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) GetPersistentVolumeClaim(ctx context.Context, vclusterName string, namespace string, claimName string) (*corev1.PersistentVolumeClaim, error) {
	var lastErr error
	for _, profile := range c.orderedProfiles() {
		reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, fmt.Sprintf("/api/v1/namespaces/%s/persistentvolumeclaims/%s", namespace, claimName), nil)
		var pvc corev1.PersistentVolumeClaim
		if err := c.getJSONWithProfile(ctx, profile, reqURL, &pvc); err != nil {
			lastErr = err
			continue
		}
		return &pvc, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) GetPersistentVolume(ctx context.Context, vclusterName string, pvName string) (*corev1.PersistentVolume, error) {
	var lastErr error
	for _, profile := range c.orderedProfiles() {
		reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, fmt.Sprintf("/api/v1/persistentvolumes/%s", pvName), nil)
		var pv corev1.PersistentVolume
		if err := c.getJSONWithProfile(ctx, profile, reqURL, &pv); err != nil {
			lastErr = err
			continue
		}
		return &pv, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) ListStorageVolumeResources(ctx context.Context, zone string) ([]StorageVolumeResource, error) {
	resources := make([]StorageVolumeResource, 0)
	seen := make(map[string]struct{})
	var lastErr error
	success := false
	for _, profile := range c.orderedProfiles() {
		items, err := c.listStorageVolumeResourcesWithProfile(ctx, profile, zone)
		if err != nil {
			lastErr = err
			continue
		}
		success = true
		for _, item := range items {
			item.ProfileName = profile.Name
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

	var lastErr error
	for _, profile := range c.orderedProfiles() {
		u, _ := url.Parse(profile.BaseURL)
		u.Path = "/rmh/v1/resources:page"
		query := u.Query()

		filter := fmt.Sprintf(`resource_type="storage.afs.v1.volume" OR resource_type="storage.afs.v2.volume"  AND %s="*%s*"`, field, fragment)
		query.Set("filter", filter)
		query.Set("page_size", "10")
		query.Set("page_token", "1")
		u.RawQuery = query.Encode()

		var payload storageVolumePageResponse
		if err := c.postJSONWithProfile(ctx, profile, u.String(), map[string]any{}, &payload); err != nil {
			lastErr = err
			continue
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
		if len(payload.Resources) > 0 {
			return &payload.Resources[0], nil
		}
	}
	if lastErr != nil {
		return nil, lastErr
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

	var lastErr error
	for _, profile := range c.orderedProfiles() {
		reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, "/api/v1/pods", query)
		var podList corev1.PodList
		if err := c.getJSONWithProfile(ctx, profile, reqURL, &podList); err != nil {
			lastErr = err
			continue
		}
		return podList.Items, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) ListEvents(ctx context.Context, vclusterName string, namespace string, name string, kind string) ([]corev1.Event, error) {
	query := url.Values{}
	fieldSelector := []string{fmt.Sprintf("involvedObject.name=%s", name)}
	if strings.TrimSpace(kind) != "" {
		fieldSelector = append(fieldSelector, fmt.Sprintf("involvedObject.kind=%s", kind))
	}
	query.Set("fieldSelector", strings.Join(fieldSelector, ","))

	var lastErr error
	for _, profile := range c.orderedProfiles() {
		reqURL := c.kubernetesClusterURLForProfile(profile, vclusterName, fmt.Sprintf("/api/v1/namespaces/%s/events", namespace), query)
		var eventList corev1.EventList
		if err := c.getJSONWithProfile(ctx, profile, reqURL, &eventList); err != nil {
			lastErr = err
			continue
		}
		return eventList.Items, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) GetPodLogs(ctx context.Context, vclusterName string, namespace string, podName string, tailLines int64) ([]string, error) {
	query := url.Values{}
	query.Set("tailLines", fmt.Sprintf("%d", tailLines))

	var (
		body string
		err  error
	)
	for _, profile := range c.orderedProfiles() {
		reqURL := c.kubernetesClusterURLForProfile(profile, vclusterName, fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/log", namespace, podName), query)
		body, err = c.getTextWithProfile(ctx, profile, reqURL)
		if err == nil {
			break
		}
	}
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
	result := make([]VirtualCluster, 0)
	seen := make(map[string]struct{})
	var lastErr error
	success := false
	for _, profile := range c.orderedProfiles() {
		items, err := c.listVirtualClustersForProfile(ctx, profile)
		if err != nil {
			lastErr = err
			continue
		}
		success = true
		for _, item := range items {
			item.ProfileName = profile.Name
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

func (c *VirtualClusterClient) listVirtualClustersForProfile(ctx context.Context, profile clientProfile) ([]VirtualCluster, error) {
	skip := 0
	clusters := make([]VirtualCluster, 0)

	for {
		reqURL := c.listURLForProfile(profile, skip)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build virtual cluster request: %w", err)
		}

		headers, err := c.authHeadersForProfile(profile, reqURL, http.MethodGet)
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

func (c *VirtualClusterClient) listURLForProfile(profile clientProfile, skip int) string {
	u, _ := url.Parse(profile.BaseURL)
	u.Path = "/compute/ecp/v1/subscriptions/-/resourceGroups/-/regions/-/virtualClusters"

	query := u.Query()
	query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
	query.Set("skip", fmt.Sprintf("%d", skip))
	u.RawQuery = query.Encode()
	return u.String()
}

func (c *VirtualClusterClient) authHeadersForProfile(profile clientProfile, reqURL string, method string) (map[string]string, error) {
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

	mac := hmac.New(sha256.New, []byte(profile.SecretKey))
	_, _ = mac.Write([]byte(signContent))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return map[string]string{
		"Date":          dateHeader,
		"Host":          parsedURL.Host,
		"Accept":        "application/json",
		"Authorization": fmt.Sprintf(`hmac accesskey="%s", algorithm="hmac-sha256", headers="date host @request-target", signature="%s"`, profile.AccessKey, signature),
	}, nil
}

func (c *VirtualClusterClient) getJSONWithProfile(ctx context.Context, profile clientProfile, reqURL string, out any) error {
	body, err := c.doRequestWithProfile(ctx, profile, http.MethodGet, reqURL, "", nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	return nil
}

func (c *VirtualClusterClient) postJSONWithProfile(ctx context.Context, profile clientProfile, reqURL string, payload any, out any) error {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request for %s: %w", reqURL, err)
	}

	body, err := c.doRequestWithProfile(ctx, profile, http.MethodPost, reqURL, "application/json", bodyBytes)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	return nil
}

func (c *VirtualClusterClient) getTextWithProfile(ctx context.Context, profile clientProfile, reqURL string) (string, error) {
	body, err := c.doRequestWithProfile(ctx, profile, http.MethodGet, reqURL, "", nil)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *VirtualClusterClient) doRequestWithProfile(ctx context.Context, profile clientProfile, method string, reqURL string, contentType string, requestBody []byte) ([]byte, error) {
	var bodyReader io.Reader
	if len(requestBody) > 0 {
		bodyReader = bytes.NewReader(requestBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	headers, err := c.authHeadersForProfile(profile, reqURL, method)
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

func (c *VirtualClusterClient) kubernetesResourceURLForProfile(profile clientProfile, vclusterName string, path string, query url.Values) string {
	u, _ := url.Parse(profile.KubernetesBaseURL)
	u.Path = fmt.Sprintf("/ecp/v1/kubernetes/virtualClusters/%s%s", vclusterName, path)
	if query != nil {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

func (c *VirtualClusterClient) kubernetesClusterURLForProfile(profile clientProfile, vclusterName string, path string, query url.Values) string {
	u, _ := url.Parse(profile.KubernetesBaseURL)
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
