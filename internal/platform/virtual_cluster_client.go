package platform

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	ClusterD                      = "d"
	ClusterDCloud                 = "dcloud"
	defaultSubscriptionDCloud     = "019a575c-9a53-71ab-8028-2b0383d7a02f"
	defaultAPIBaseURL             = "https://management.d.pjlab.org.cn"
	defaultKubernetesBaseURL      = "https://compute.d.pjlab.org.cn"
	defaultIAMBaseURL             = "https://iam.d.pjlab.org.cn"
	defaultMonitorBaseURL         = "https://monitor.d.pjlab.org.cn"
	defaultCloudAPIBaseURL        = "https://management-cloud.d.pjlab.org.cn"
	defaultCloudKubernetesBaseURL = "https://compute-cloud.d.pjlab.org.cn"
	defaultCloudIAMBaseURL        = "https://iam-cloud.d.pjlab.org.cn"
	defaultCloudMonitorBaseURL    = "https://monitor-cloud.d.pjlab.org.cn"
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
	CMSAccessKey      string
	CMSSecretKey      string
	BaseURL           string
	KubernetesBaseURL string
	IAMBaseURL        string
	MonitorBaseURL    string
	Subscription      string
	ResourceGroup     string
	Region            string
}

type VirtualCluster struct {
	UID         string `json:"uid"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Region      string `json:"region"`
	State       string `json:"state"`
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
	Properties  struct {
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
		Type                     string `json:"type"`
		ImagePath                string `json:"image_path"`
		HostIP                   string `json:"host_ip"`
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
	ID            string `json:"id"`
	Name          string `json:"name"`
	Username      string `json:"username"`
	TenantCode    string `json:"tenant_code"`
	Status        string `json:"status"`
	Source        string `json:"source"`
	ConsoleState  string `json:"console_state"`
	OpenAPIState  string `json:"open_api_state"`
	MFAState      string `json:"mfa_state"`
	CreateTime    string `json:"create_time"`
	LastLoginTime string `json:"last_login_time"`
}

type IAMGroup struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	DisplayName    string `json:"display_name"`
	PosixGroupName string `json:"posix_group_name"`
	TenantCode     string `json:"tenant_code"`
	Status         string `json:"status"`
	CreateTime     string `json:"create_time"`
}

type IAMBindingPolicy struct {
	ID             string        `json:"id"`
	Scope          string        `json:"scope"`
	MemberType     string        `json:"member_type"`
	MemberName     string        `json:"member_name"`
	MemberIdentify string        `json:"member_identify"`
	MemberValue    string        `json:"member_value"`
	Level          string        `json:"level"`
	Service        string        `json:"service"`
	CreateTime     string        `json:"create_time"`
	UpdateTime     string        `json:"update_time"`
	RoleInfos      []IAMRoleInfo `json:"role_infos"`
}

type IAMRoleInfo struct {
	DisplayName      string `json:"display_name"`
	RoleName         string `json:"role_name"`
	AvailableService string `json:"available_service"`
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
	Users         []IAMUser `json:"users"`
	NextPageToken string    `json:"next_page_token"`
	TotalSize     int       `json:"total_size"`
}

type iamGroupListResponse struct {
	Groups        []IAMGroup `json:"groups"`
	NextPageToken string     `json:"next_page_token"`
	TotalSize     int        `json:"total_size"`
}

type iamBindingPolicyListResponse struct {
	Policies      []IAMBindingPolicy `json:"policies"`
	NextPageToken string             `json:"next_page_token"`
	TotalSize     int                `json:"total_size"`
}

type aiComputeNodeListResponse struct {
	AIComputeNodes []AIComputeNode `json:"ai_compute_nodes"`
}

type storageVolumePageResponse struct {
	Resources     []StorageVolumeResource `json:"resources"`
	NextPageToken string                  `json:"next_page_token"`
}

type telemetryResourceListResponse struct {
	Resources []telemetryResource `json:"resources"`
}

type telemetryResource struct {
	RID        string          `json:"rid"`
	Properties json.RawMessage `json:"properties"`
}

type queryTokenResponse struct {
	Token string `json:"token"`
}

type cmsLogQueryResponse struct {
	Logs []cmsLogItem `json:"logs"`
}

type cmsLogItem struct {
	ObservedTS string         `json:"observed_ts"`
	Level      string         `json:"level"`
	Msg        string         `json:"msg"`
	Resource   map[string]any `json:"resource"`
}

type config struct {
	AccessKey         string `json:"access_key"`
	SecretKey         string `json:"secret_key"`
	CMSAccessKey      string `json:"cms_access_key"`
	CMSSecretKey      string `json:"cms_secret_key"`
	Subscription      string `json:"subscription_id"`
	Cluster           string `json:"cluster"`
	BaseURL           string `json:"base_url"`
	KubernetesBaseURL string `json:"kubernetes_base_url"`
	IAMBaseURL        string `json:"iam_base_url"`
	MonitorBaseURL    string `json:"monitor_base_url"`
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
	CMSAccessKey      string `json:"cms_access_key"`
	CMSSecretKey      string `json:"cms_secret_key"`
	Subscription      string `json:"subscription_id"`
	Cluster           string `json:"cluster"`
	BaseURL           string `json:"base_url"`
	KubernetesBaseURL string `json:"kubernetes_base_url"`
	IAMBaseURL        string `json:"iam_base_url"`
	MonitorBaseURL    string `json:"monitor_base_url"`
	ResourceGroup     string `json:"resource_group"`
	Region            string `json:"region"`
}

type ConfigSnapshot struct {
	CurrentProfile    string                   `json:"current_profile,omitempty"`
	Profiles          map[string]ConfigProfile `json:"profiles,omitempty"`
	AccessKey         string                   `json:"access_key"`
	SecretKey         string                   `json:"secret_key"`
	CMSAccessKey      string                   `json:"cms_access_key"`
	CMSSecretKey      string                   `json:"cms_secret_key"`
	Subscription      string                   `json:"subscription_id"`
	Cluster           string                   `json:"cluster"`
	BaseURL           string                   `json:"base_url"`
	KubernetesBaseURL string                   `json:"kubernetes_base_url"`
	IAMBaseURL        string                   `json:"iam_base_url"`
	MonitorBaseURL    string                   `json:"monitor_base_url"`
	ResourceGroup     string                   `json:"resource_group"`
	Region            string                   `json:"region"`
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
	monitorBaseURL := defaultMonitorBaseURLForCluster(cluster)
	if override := strings.TrimRight(strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_BASE_URL")), "/"); override != "" {
		baseURL = override
	}
	if override := strings.TrimRight(strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_KUBERNETES_BASE_URL")), "/"); override != "" {
		kubernetesBaseURL = override
	}
	if override := strings.TrimRight(strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_IAM_BASE_URL")), "/"); override != "" {
		iamBaseURL = override
	}
	if override := strings.TrimRight(strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_MONITOR_BASE_URL")), "/"); override != "" {
		monitorBaseURL = override
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
				CMSAccessKey:      strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_CMS_ACCESS_KEY")),
				CMSSecretKey:      strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_CMS_SECRET_KEY")),
				BaseURL:           baseURL,
				KubernetesBaseURL: kubernetesBaseURL,
				IAMBaseURL:        iamBaseURL,
				MonitorBaseURL:    monitorBaseURL,
				Subscription:      subscription,
				ResourceGroup:     resourceGroup,
				Region:            region,
			},
		},
		httpClient: &http.Client{Timeout: 10 * time.Second},
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
				CMSAccessKey:      raw.CMSAccessKey,
				CMSSecretKey:      raw.CMSSecretKey,
				Subscription:      raw.Subscription,
				Cluster:           normalizeClusterName(raw.Cluster),
				BaseURL:           strings.TrimRight(strings.TrimSpace(raw.BaseURL), "/"),
				KubernetesBaseURL: strings.TrimRight(strings.TrimSpace(raw.KubernetesBaseURL), "/"),
				IAMBaseURL:        strings.TrimRight(strings.TrimSpace(raw.IAMBaseURL), "/"),
				MonitorBaseURL:    strings.TrimRight(strings.TrimSpace(raw.MonitorBaseURL), "/"),
				ResourceGroup:     strings.TrimSpace(raw.ResourceGroup),
				Region:            strings.TrimSpace(raw.Region),
			}
			applyConfigProfileDefaults(&profile)
			snapshot.Profiles[name] = profile
		}
		if current, ok := snapshot.Profiles[currentProfile]; ok {
			snapshot.AccessKey = current.AccessKey
			snapshot.SecretKey = current.SecretKey
			snapshot.CMSAccessKey = current.CMSAccessKey
			snapshot.CMSSecretKey = current.CMSSecretKey
			snapshot.Subscription = current.Subscription
			snapshot.Cluster = current.Cluster
			snapshot.BaseURL = current.BaseURL
			snapshot.KubernetesBaseURL = current.KubernetesBaseURL
			snapshot.IAMBaseURL = current.IAMBaseURL
			snapshot.MonitorBaseURL = current.MonitorBaseURL
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
	if strings.TrimSpace(cfg.MonitorBaseURL) == "" {
		cfg.MonitorBaseURL = defaultMonitorBaseURLForCluster(cfg.Cluster)
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
		if strings.TrimSpace(cfg.CMSAccessKey) != "" {
			current.CMSAccessKey = cfg.CMSAccessKey
		}
		if strings.TrimSpace(cfg.CMSSecretKey) != "" {
			current.CMSSecretKey = cfg.CMSSecretKey
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
		if strings.TrimSpace(cfg.MonitorBaseURL) != "" || strings.TrimSpace(cfg.Cluster) != "" {
			current.MonitorBaseURL = cfg.MonitorBaseURL
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
				CMSAccessKey:      profile.CMSAccessKey,
				CMSSecretKey:      profile.CMSSecretKey,
				Subscription:      profile.Subscription,
				Cluster:           profile.Cluster,
				BaseURL:           profile.BaseURL,
				KubernetesBaseURL: profile.KubernetesBaseURL,
				IAMBaseURL:        profile.IAMBaseURL,
				MonitorBaseURL:    profile.MonitorBaseURL,
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
	if strings.TrimSpace(cfg.MonitorBaseURL) == "" {
		cfg.MonitorBaseURL = defaultMonitorBaseURLForCluster(cfg.Cluster)
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

func defaultMonitorBaseURLForCluster(cluster string) string {
	switch normalizeClusterName(cluster) {
	case ClusterDCloud:
		return defaultCloudMonitorBaseURL
	default:
		return defaultMonitorBaseURL
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
	if accessKey == "" || secretKey == "" {
		return clientProfile{}, false
	}
	baseURL, kubernetesBaseURL := defaultBaseURLsForCluster(cluster)
	iamBaseURL := defaultIAMBaseURLForCluster(cluster)
	monitorBaseURL := defaultMonitorBaseURLForCluster(cluster)
	if override := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"); override != "" {
		baseURL = override
	}
	if override := strings.TrimRight(strings.TrimSpace(cfg.KubernetesBaseURL), "/"); override != "" {
		kubernetesBaseURL = override
	}
	if override := strings.TrimRight(strings.TrimSpace(cfg.IAMBaseURL), "/"); override != "" {
		iamBaseURL = override
	}
	if override := strings.TrimRight(strings.TrimSpace(cfg.MonitorBaseURL), "/"); override != "" {
		monitorBaseURL = override
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
		CMSAccessKey:      strings.TrimSpace(cfg.CMSAccessKey),
		CMSSecretKey:      strings.TrimSpace(cfg.CMSSecretKey),
		BaseURL:           baseURL,
		KubernetesBaseURL: kubernetesBaseURL,
		IAMBaseURL:        iamBaseURL,
		MonitorBaseURL:    monitorBaseURL,
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
			CMSAccessKey:      value.CMSAccessKey,
			CMSSecretKey:      value.CMSSecretKey,
			Subscription:      value.Subscription,
			Cluster:           value.Cluster,
			BaseURL:           value.BaseURL,
			KubernetesBaseURL: value.KubernetesBaseURL,
			IAMBaseURL:        value.IAMBaseURL,
			MonitorBaseURL:    value.MonitorBaseURL,
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
	if strings.TrimSpace(profile.MonitorBaseURL) == "" {
		profile.MonitorBaseURL = defaultMonitorBaseURLForCluster(profile.Cluster)
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

func (c *VirtualClusterClient) currentClientProfile() (clientProfile, bool) {
	if len(c.profiles) == 0 {
		return clientProfile{
			Name:              firstNonEmpty(c.currentProfile, "default"),
			AccessKey:         c.accessKey,
			SecretKey:         c.secretKey,
			BaseURL:           c.baseURL,
			KubernetesBaseURL: c.kubernetesBaseURL,
			IAMBaseURL:        c.iamBaseURL,
			Subscription:      c.subscription,
			ResourceGroup:     c.resourceGroup,
			Region:            c.region,
		}, true
	}
	profile, ok := c.profiles[c.currentProfile]
	return profile, ok
}

func (c *VirtualClusterClient) orderedCMSProfiles() []clientProfile {
	profiles := c.orderedProfiles()
	result := make([]clientProfile, 0, len(profiles))
	for _, profile := range profiles {
		if strings.TrimSpace(profile.CMSAccessKey) == "" || strings.TrimSpace(profile.CMSSecretKey) == "" {
			continue
		}
		result = append(result, profile)
	}
	return result
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

func (c *VirtualClusterClient) listUsersForProfile(ctx context.Context, profile clientProfile) ([]IAMUser, error) {
	pageToken := "1"
	users := make([]IAMUser, 0)
	for {
		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = "/iam/idp/v1/users"
		query := u.Query()
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("page_token", pageToken)
		query.Set("filter", `(status="valid")`)
		u.RawQuery = query.Encode()

		var payload iamUserListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return nil, err
		}
		if len(payload.Users) == 0 {
			break
		}
		users = append(users, payload.Users...)
		if strings.TrimSpace(payload.NextPageToken) == "" || payload.NextPageToken == pageToken {
			break
		}
		pageToken = payload.NextPageToken
	}
	return users, nil
}

func (c *VirtualClusterClient) FindUsers(ctx context.Context, identifier string) ([]IAMUser, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}

	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("user identifier is required")
	}

	users, err := c.listUsersForProfile(ctx, profile)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}

	exact := make([]IAMUser, 0)
	prefix := make([]IAMUser, 0)
	contains := make([]IAMUser, 0)
	lowerID := strings.ToLower(identifier)
	for _, user := range users {
		username := strings.TrimSpace(user.Username)
		id := strings.TrimSpace(user.ID)
		name := strings.TrimSpace(user.Name)
		switch {
		case username == identifier || id == identifier:
			exact = append(exact, user)
		case strings.HasPrefix(strings.ToLower(username), lowerID) || strings.HasPrefix(strings.ToLower(id), lowerID) || strings.HasPrefix(strings.ToLower(name), lowerID):
			prefix = append(prefix, user)
		case strings.Contains(strings.ToLower(username), lowerID) || strings.Contains(strings.ToLower(id), lowerID) || strings.Contains(strings.ToLower(name), lowerID):
			contains = append(contains, user)
		}
	}

	result := exact
	if len(result) == 0 {
		result = prefix
	}
	if len(result) == 0 {
		result = contains
	}
	sort.Slice(result, func(i, j int) bool {
		if strings.TrimSpace(result[i].Username) != strings.TrimSpace(result[j].Username) {
			return strings.TrimSpace(result[i].Username) < strings.TrimSpace(result[j].Username)
		}
		return strings.TrimSpace(result[i].ID) < strings.TrimSpace(result[j].ID)
	})
	if len(result) == 0 {
		return nil, fmt.Errorf("user %q not found in current tenant", identifier)
	}
	return result, nil
}

func (c *VirtualClusterClient) ListUserGroups(ctx context.Context, userID string) ([]IAMGroup, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("user id is required")
	}

	pageToken := "1"
	result := make([]IAMGroup, 0)
	for {
		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = fmt.Sprintf("/iam/idp/v1/users/%s:getGroups", url.PathEscape(userID))
		query := u.Query()
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("page_token", pageToken)
		query.Set("order_by", "create_time desc")
		u.RawQuery = query.Encode()

		var payload iamGroupListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return nil, err
		}
		if len(payload.Groups) == 0 {
			break
		}
		result = append(result, payload.Groups...)
		nextToken := strings.TrimSpace(payload.NextPageToken)
		if nextToken == "" || nextToken == pageToken {
			break
		}
		pageToken = nextToken
	}
	return result, nil
}

func (c *VirtualClusterClient) listGroupsForProfile(ctx context.Context, profile clientProfile) ([]IAMGroup, error) {
	pageToken := "1"
	result := make([]IAMGroup, 0)
	for {
		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = "/iam/idp/v1/groups"
		query := u.Query()
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("page_token", pageToken)
		query.Set("order_by", "create_time desc")
		u.RawQuery = query.Encode()

		var payload iamGroupListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return nil, err
		}
		if len(payload.Groups) == 0 {
			break
		}
		result = append(result, payload.Groups...)
		nextToken := strings.TrimSpace(payload.NextPageToken)
		if nextToken == "" || nextToken == pageToken {
			break
		}
		pageToken = nextToken
	}
	return result, nil
}

func (c *VirtualClusterClient) FindGroups(ctx context.Context, identifier string) ([]IAMGroup, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}

	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("group identifier is required")
	}

	groups, err := c.listGroupsForProfile(ctx, profile)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}

	exact := make([]IAMGroup, 0)
	prefix := make([]IAMGroup, 0)
	contains := make([]IAMGroup, 0)
	normalized := strings.ToLower(identifier)
	for _, group := range groups {
		fields := []string{
			strings.TrimSpace(group.ID),
			strings.TrimSpace(group.Name),
			strings.TrimSpace(group.DisplayName),
			strings.TrimSpace(group.PosixGroupName),
		}
		matchedExact := false
		matchedPrefix := false
		matchedContains := false
		for _, field := range fields {
			if field == "" {
				continue
			}
			lower := strings.ToLower(field)
			switch {
			case lower == normalized:
				matchedExact = true
			case strings.HasPrefix(lower, normalized):
				matchedPrefix = true
			case strings.Contains(lower, normalized):
				matchedContains = true
			}
		}
		switch {
		case matchedExact:
			exact = append(exact, group)
		case matchedPrefix:
			prefix = append(prefix, group)
		case matchedContains:
			contains = append(contains, group)
		}
	}

	result := exact
	if len(result) == 0 {
		result = prefix
	}
	if len(result) == 0 {
		result = contains
	}
	sort.Slice(result, func(i, j int) bool {
		left := firstNonEmpty(result[i].DisplayName, result[i].Name, result[i].PosixGroupName, result[i].ID)
		right := firstNonEmpty(result[j].DisplayName, result[j].Name, result[j].PosixGroupName, result[j].ID)
		if left != right {
			return left < right
		}
		return strings.TrimSpace(result[i].ID) < strings.TrimSpace(result[j].ID)
	})
	if len(result) == 0 {
		return nil, fmt.Errorf("group %q not found in current tenant", identifier)
	}
	return result, nil
}

func (c *VirtualClusterClient) ListIAMBindingPolicies(ctx context.Context) ([]IAMBindingPolicy, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}

	pageToken := "1"
	result := make([]IAMBindingPolicy, 0)
	for {
		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = "/iam/authz/v1/bindingPolicies"
		query := u.Query()
		query.Set("page_token", pageToken)
		query.Set("pageSize", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("order_by", "create_time desc")
		u.RawQuery = query.Encode()

		var payload iamBindingPolicyListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return nil, err
		}
		if len(payload.Policies) == 0 {
			break
		}
		result = append(result, payload.Policies...)

		nextToken := strings.TrimSpace(payload.NextPageToken)
		if nextToken != "" && nextToken != pageToken {
			pageToken = nextToken
			continue
		}
		if len(payload.Policies) < defaultPageLimit {
			break
		}
		current, err := strconv.Atoi(pageToken)
		if err != nil {
			break
		}
		pageToken = strconv.Itoa(current + 1)
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

func (c *VirtualClusterClient) ListCurrentProfileVirtualClusters(ctx context.Context) ([]VirtualCluster, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}
	items, err := c.listVirtualClustersForProfile(ctx, profile)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].ProfileName = profile.Name
	}
	return items, nil
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

func (c *VirtualClusterClient) ListVolcanoJobs(ctx context.Context, vclusterName string, namespace string) ([]unstructured.Unstructured, error) {
	var lastErr error
	for _, profile := range c.orderedProfiles() {
		reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, fmt.Sprintf("/apis/batch.volcano.sh/v1alpha1/namespaces/%s/jobs", namespace), nil)
		var list unstructured.UnstructuredList
		if err := c.getJSONWithProfile(ctx, profile, reqURL, &list); err != nil {
			lastErr = err
			continue
		}
		return list.Items, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) ListPods(ctx context.Context, vclusterName string, namespace string) ([]corev1.Pod, error) {
	query := url.Values{}
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

func (c *VirtualClusterClient) ListClusterRoleBindings(ctx context.Context, vclusterName string, labelSelector string) ([]rbacv1.ClusterRoleBinding, error) {
	return c.listClusterRoleBindingsWithProfiles(ctx, c.orderedProfiles(), vclusterName, labelSelector, "")
}

func (c *VirtualClusterClient) ListClusterRoleBindingsForProfile(ctx context.Context, profileName string, vclusterName string, labelSelector string) ([]rbacv1.ClusterRoleBinding, error) {
	return c.ListClusterRoleBindingsForProfileToken(ctx, profileName, vclusterName, labelSelector, "")
}

func (c *VirtualClusterClient) ListClusterRoleBindingsForProfileToken(ctx context.Context, profileName string, vclusterName string, labelSelector string, bearerToken string) ([]rbacv1.ClusterRoleBinding, error) {
	profileName = strings.TrimSpace(profileName)
	if profileName == "" {
		return c.listClusterRoleBindingsWithProfiles(ctx, c.orderedProfiles(), vclusterName, labelSelector, bearerToken)
	}
	profile, ok := c.profiles[profileName]
	if !ok {
		return c.listClusterRoleBindingsWithProfiles(ctx, c.orderedProfiles(), vclusterName, labelSelector, bearerToken)
	}
	return c.listClusterRoleBindingsWithProfiles(ctx, []clientProfile{profile}, vclusterName, labelSelector, bearerToken)
}

func (c *VirtualClusterClient) listClusterRoleBindingsWithProfiles(ctx context.Context, profiles []clientProfile, vclusterName string, labelSelector string, bearerToken string) ([]rbacv1.ClusterRoleBinding, error) {
	query := url.Values{}
	if strings.TrimSpace(labelSelector) != "" {
		query.Set("labelSelector", strings.TrimSpace(labelSelector))
	}

	var lastErr error
	for _, profile := range profiles {
		reqURL := c.kubernetesClusterURLForProfile(profile, vclusterName, "/apis/rbac.authorization.k8s.io/v1/clusterrolebindings", query)
		var list rbacv1.ClusterRoleBindingList
		var err error
		if strings.TrimSpace(bearerToken) != "" {
			err = c.getJSONWithBearerProfile(ctx, profile, reqURL, bearerToken, &list)
		} else {
			err = c.getJSONWithProfile(ctx, profile, reqURL, &list)
		}
		if err != nil {
			lastErr = err
			continue
		}
		return list.Items, nil
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
		pageToken := "1"
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
				lastErr = err
				break
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
			if nextPageToken == "" || nextPageToken == pageToken || len(payload.Resources) == 0 {
				break
			}
			pageToken = nextPageToken
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
	for _, profile := range c.orderedCMSProfiles() {
		clusterRef, err := c.cmsClusterRefForProfile(ctx, profile, vclusterName)
		if err != nil {
			lastErr = err
			continue
		}
		queryToken, err := c.cmsQueryTokenForProfile(ctx, profile, clusterRef)
		if err != nil {
			lastErr = err
			continue
		}
		reqURL := c.kubernetesClusterURLForProfile(profile, clusterRef, fmt.Sprintf("/api/v1/namespaces/%s/events", namespace), query)
		var eventList corev1.EventList
		if err := c.getJSONWithCMSProfileToken(ctx, profile, reqURL, queryToken, &eventList); err != nil {
			lastErr = err
			continue
		}
		return eventList.Items, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) GetPodLogs(ctx context.Context, vclusterName string, namespace string, podName string, tailLines int64) ([]string, error) {
	var lastErr error
	for _, profile := range c.orderedCMSProfiles() {
		clusterRef, err := c.cmsClusterRefForProfile(ctx, profile, vclusterName)
		if err != nil {
			lastErr = err
			continue
		}
		lines, err := c.queryPodLogsWithCMSProfile(ctx, profile, clusterRef, namespace, podName, tailLines)
		if err != nil {
			lastErr = err
			continue
		}
		return lines, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no cms profile available for pod logs")
}

func (c *VirtualClusterClient) queryPodLogsWithCMSProfile(ctx context.Context, profile clientProfile, vclusterName string, namespace string, podName string, tailLines int64) ([]string, error) {
	queryToken, err := c.cmsQueryTokenForProfile(ctx, profile, vclusterName)
	if err != nil {
		return nil, err
	}

	reqURL := c.cmsLogQueryURL(profile, namespace, podName, tailLines)
	var payload cmsLogQueryResponse
	body, err := c.doRequestWithCMSProfile(ctx, profile, http.MethodGet, reqURL, "", nil, queryToken)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode cms log response: %w", err)
	}

	lines := make([]string, 0, len(payload.Logs))
	for _, item := range payload.Logs {
		pod := emptyDash(fmt.Sprintf("%v", item.Resource["k8s.pod.name"]))
		container := emptyDash(fmt.Sprintf("%v", item.Resource["k8s.container.name"]))
		level := emptyDash(item.Level)
		msg := emptyDash(item.Msg)
		ts := emptyDash(item.ObservedTS)
		lines = append(lines, fmt.Sprintf("[%s] [%s] [%s/%s] %s", ts, level, pod, container, msg))
	}
	if len(lines) == 0 {
		return []string{"<empty log output>"}, nil
	}
	return lines, nil
}

func (c *VirtualClusterClient) cmsQueryTokenForProfile(ctx context.Context, profile clientProfile, vclusterName string) (string, error) {
	telemetryRID, err := c.getPrivateTelemetryStationRID(ctx, profile)
	if err != nil {
		return "", err
	}
	return c.createCMSQueryToken(ctx, profile, telemetryRID, virtualClusterUID(vclusterName))
}

func (c *VirtualClusterClient) cmsClusterRefForProfile(ctx context.Context, profile clientProfile, vclusterName string) (string, error) {
	ref := strings.TrimSpace(vclusterName)
	if ref == "" {
		return "", fmt.Errorf("virtual cluster identifier is required")
	}

	items, err := c.listVirtualClustersForProfile(ctx, profile)
	if err == nil {
		for _, item := range items {
			uid := strings.TrimSpace(item.UID)
			if uid == "" {
				continue
			}
			if ref == uid || ref == "vc-"+uid || ref == strings.TrimSpace(item.Name) || ref == strings.TrimSpace(item.DisplayName) {
				return "vc-" + uid, nil
			}
		}
	}

	if strings.HasPrefix(ref, "vc-") {
		return ref, err
	}
	if looksLikeClusterUUID(ref) {
		return "vc-" + ref, err
	}
	return ref, err
}

func looksLikeClusterUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, ch := range value {
		switch i {
		case 8, 13, 18, 23:
			if ch != '-' {
				return false
			}
		default:
			if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') && (ch < 'A' || ch > 'F') {
				return false
			}
		}
	}
	return true
}

func (c *VirtualClusterClient) getPrivateTelemetryStationRID(ctx context.Context, profile clientProfile) (string, error) {
	u, _ := url.Parse(profile.BaseURL)
	u.Path = "/rmh/v1/resources"
	query := u.Query()
	query.Set("filter", `resource_type="monitor.ts.v1.telemetryStation"`)
	u.RawQuery = query.Encode()

	var payload telemetryResourceListResponse
	if err := c.getJSONWithCMSProfile(ctx, profile, u.String(), &payload); err != nil {
		return "", err
	}
	for _, resource := range payload.Resources {
		if strings.TrimSpace(resource.RID) == "" {
			continue
		}
		props := parseTelemetryProperties(resource.Properties)
		if strings.EqualFold(strings.TrimSpace(fmt.Sprintf("%v", props["station_type"])), "PRIVATE") {
			return strings.TrimSpace(resource.RID), nil
		}
	}
	return "", fmt.Errorf("private telemetryStation rid not found")
}

func parseTelemetryProperties(raw json.RawMessage) map[string]any {
	props := map[string]any{}
	if len(raw) == 0 {
		return props
	}
	if err := json.Unmarshal(raw, &props); err == nil {
		return props
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return props
	}
	_ = json.Unmarshal([]byte(encoded), &props)
	return props
}

func (c *VirtualClusterClient) createCMSQueryToken(ctx context.Context, profile clientProfile, telemetryRID string, vcUUID string) (string, error) {
	if strings.TrimSpace(telemetryRID) == "" {
		return "", fmt.Errorf("telemetry station rid is required")
	}
	if strings.TrimSpace(vcUUID) == "" {
		return "", fmt.Errorf("virtual cluster uuid is required")
	}

	u, _ := url.Parse(profile.MonitorBaseURL)
	u.Path = fmt.Sprintf("/monitor/ts/data/v1%s/query/token", telemetryRID)
	body := map[string]any{
		"expire_time":   time.Now().UTC().Add(30 * 24 * time.Hour).Format("2006-01-02T15:04:05Z"),
		"scope":         "RESOURCE",
		"resource_type": "compute.ecp.v1.virtualCluster",
		"resource_ids":  []string{vcUUID},
	}

	var payload queryTokenResponse
	if err := c.postJSONWithCMSProfile(ctx, profile, u.String(), body, &payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.Token) == "" {
		return "", fmt.Errorf("cms query token is empty")
	}
	return strings.TrimSpace(payload.Token), nil
}

func (c *VirtualClusterClient) postJSONWithCMSProfile(ctx context.Context, profile clientProfile, reqURL string, payload any, out any) error {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request for %s: %w", reqURL, err)
	}
	body, err := c.doRequestWithCMSProfile(ctx, profile, http.MethodPost, reqURL, "application/json", bodyBytes)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	return nil
}

func (c *VirtualClusterClient) cmsLogQueryURL(profile clientProfile, namespace string, podName string, tailLines int64) string {
	u, _ := url.Parse(profile.MonitorBaseURL)
	u.Path = "/query/v1/logs"
	query := u.Query()
	query.Set("model_name", "logs.compute.ecp.v1.virtualCluster.logs")
	for _, dimension := range []string{"resource_type", "resource_id", "id", "ts", "observed_ts", "workload_type", "workload_name", "namespace", "pod", "container", "node", "level", "msg"} {
		query.Add("dimensions", dimension)
	}
	query.Set("filter", fmt.Sprintf(`namespace="%s" AND pod="%s"`, strings.TrimSpace(namespace), strings.TrimSpace(podName)))
	query.Set("time_dimension.dimension", "observed_ts")
	query.Set("start", time.Now().UTC().Add(-7*24*time.Hour).Format(time.RFC3339))
	query.Set("end", time.Now().UTC().Add(5*time.Minute).Format(time.RFC3339))
	if tailLines <= 0 {
		tailLines = 10
	}
	query.Set("page_size", fmt.Sprintf("%d", tailLines))
	query.Set("skip", "0")
	query.Set("order_by", "observed_ts desc")
	u.RawQuery = query.Encode()
	return u.String()
}

func virtualClusterUID(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "vc-") {
		return strings.TrimSpace(strings.TrimPrefix(value, "vc-"))
	}
	return value
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
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
	return authHeadersForCredentials(profile.AccessKey, profile.SecretKey, reqURL, method)
}

func (c *VirtualClusterClient) authHeadersForCMSProfile(profile clientProfile, reqURL string, method string) (map[string]string, error) {
	return authHeadersForCredentials(profile.CMSAccessKey, profile.CMSSecretKey, reqURL, method)
}

func authHeadersForCredentials(accessKey string, secretKey string, reqURL string, method string) (map[string]string, error) {
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

	mac := hmac.New(sha256.New, []byte(secretKey))
	_, _ = mac.Write([]byte(signContent))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return map[string]string{
		"Date":          dateHeader,
		"Host":          parsedURL.Host,
		"Accept":        "application/json",
		"Authorization": fmt.Sprintf(`hmac accesskey="%s", algorithm="hmac-sha256", headers="date host @request-target", signature="%s"`, accessKey, signature),
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

func (c *VirtualClusterClient) getJSONWithCMSProfile(ctx context.Context, profile clientProfile, reqURL string, out any) error {
	body, err := c.doRequestWithCMSProfile(ctx, profile, http.MethodGet, reqURL, "", nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	return nil
}

func (c *VirtualClusterClient) getJSONWithCMSProfileToken(ctx context.Context, profile clientProfile, reqURL string, bearerToken string, out any) error {
	body, err := c.doRequestWithCMSProfile(ctx, profile, http.MethodGet, reqURL, "", nil, bearerToken)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	return nil
}

func (c *VirtualClusterClient) getJSONWithBearerProfile(ctx context.Context, profile clientProfile, reqURL string, bearerToken string, out any) error {
	body, err := c.doSignedRequest(ctx, profile, http.MethodGet, reqURL, "", nil, false, bearerToken)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	return nil
}

func (c *VirtualClusterClient) getTextWithCMSProfile(ctx context.Context, profile clientProfile, reqURL string, bearerToken string) (string, error) {
	body, err := c.doRequestWithCMSProfile(ctx, profile, http.MethodGet, reqURL, "", nil, bearerToken)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *VirtualClusterClient) doRequestWithProfile(ctx context.Context, profile clientProfile, method string, reqURL string, contentType string, requestBody []byte) ([]byte, error) {
	return c.doSignedRequest(ctx, profile, method, reqURL, contentType, requestBody, false, "")
}

func (c *VirtualClusterClient) doRequestWithCMSProfile(ctx context.Context, profile clientProfile, method string, reqURL string, contentType string, requestBody []byte, bearerToken ...string) ([]byte, error) {
	token := ""
	if len(bearerToken) > 0 {
		token = bearerToken[0]
	}
	return c.doSignedRequest(ctx, profile, method, reqURL, contentType, requestBody, true, token)
}

func (c *VirtualClusterClient) doSignedRequest(ctx context.Context, profile clientProfile, method string, reqURL string, contentType string, requestBody []byte, useCMS bool, bearerToken string) ([]byte, error) {
	var bodyReader io.Reader
	if len(requestBody) > 0 {
		bodyReader = bytes.NewReader(requestBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	headers := map[string]string{}
	if strings.TrimSpace(bearerToken) != "" {
		headers["Accept"] = "application/json"
		headers["Authorization"] = "Bearer " + strings.TrimSpace(bearerToken)
	} else {
		var err error
		if useCMS {
			headers, err = c.authHeadersForCMSProfile(profile, reqURL, method)
		} else {
			headers, err = c.authHeadersForProfile(profile, reqURL, method)
		}
		if err != nil {
			return nil, err
		}
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
