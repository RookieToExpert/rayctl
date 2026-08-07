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

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	defaultConfigFileName         = "platform.json"
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
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	TenantID    string `json:"tenant_id"`
	Region      string `json:"region"`
	State       string `json:"state"`
	ProfileName string `json:"-"`
}

type StorageVolumeResource struct {
	ID                       string `json:"id"`
	RID                      string `json:"rid"`
	Name                     string `json:"name"`
	DisplayName              string `json:"display_name"`
	Type                     string `json:"type"`
	ResourceType             string `json:"resource_type"`
	State                    string `json:"state"`
	Zone                     string `json:"zone"`
	Region                   string `json:"region"`
	Properties               string `json:"properties"`
	CreatorID                string `json:"creator_id"`
	OwnerID                  string `json:"owner_id"`
	CreateTime               string `json:"create_time"`
	UpdateTime               string `json:"update_time"`
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

type IAMUserGroupSearchItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Type        string `json:"type"`
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
	ID               string `json:"id"`
	DisplayName      string `json:"display_name"`
	Description      string `json:"description"`
	RoleName         string `json:"role_name"`
	AvailableService string `json:"available_service"`
}

type IAMSetPolicyRequest struct {
	Scope          string                `json:"scope,omitempty"`
	MemberType     string                `json:"member_type,omitempty"`
	MemberValue    string                `json:"member_value,omitempty"`
	RoleIDs        []string              `json:"role_ids,omitempty"`
	ExcludeMembers []string              `json:"exclude_members"`
	Condition      IAMPolicyCondition    `json:"condition"`
	MemberValues   []string              `json:"member_values,omitempty"`
	Policies       []IAMSetPolicyPayload `json:"policies,omitempty"`
}

type IAMSetPolicyPayload struct {
	Scope          string             `json:"scope"`
	RoleIDs        []string           `json:"role_ids"`
	ExcludeMembers []string           `json:"exclude_members"`
	Condition      IAMPolicyCondition `json:"condition"`
}

type IAMPolicyCondition struct {
	DateNotEquals         []string `json:"date_not_equals"`
	DateLessThan          []string `json:"date_less_than"`
	DateLessThanEquals    []string `json:"date_less_than_equals"`
	DateGreaterThan       []string `json:"date_greater_than"`
	DateGreaterThanEquals []string `json:"date_greater_than_equals"`
	StringEquals          []string `json:"string_equals"`
}

type IAMSetPolicyResponse struct {
	ID       string             `json:"id"`
	PolicyID string             `json:"policy_id"`
	Policy   IAMBindingPolicy   `json:"policy"`
	Policies []IAMBindingPolicy `json:"policies"`
}

type IAMBatchCreatePoliciesRequest struct {
	Policies []IAMBatchCreatePolicy `json:"policies"`
}

type IAMBatchCreatePolicy struct {
	Scope          string                 `json:"scope"`
	RoleIDs        []string               `json:"role_ids"`
	Members        []IAMBatchCreateMember `json:"members"`
	ExcludeMembers []IAMBatchCreateMember `json:"exclude_members"`
	Level          string                 `json:"level"`
	Condition      *IAMPolicyCondition    `json:"condition,omitempty"`
}

type IAMBatchCreateMember struct {
	MemberType  string `json:"member_type"`
	MemberValue string `json:"member_value"`
}

type IAMBatchCreatePoliciesResponse struct {
	PolicyItems []IAMBatchCreatePolicyItem `json:"policy_item"`
}

type IAMBatchCreatePolicyItem struct {
	Member             IAMBatchCreateMember `json:"member"`
	CreatePolicyStatus string               `json:"create_policy_status"`
	RoleID             string               `json:"role_id"`
	Scope              string               `json:"scope"`
	PolicyID           string               `json:"policy_id"`
	ID                 string               `json:"id"`
}

type IAMMemberRelationPoliciesRequest struct {
	MemberType  string `json:"member_type,omitempty"`
	MemberValue string `json:"member_value,omitempty"`
	MemberID    string `json:"member_id,omitempty"`
	PageSize    int    `json:"page_size,omitempty"`
	PageToken   string `json:"page_token,omitempty"`
}

type IAMMemberRelationPoliciesResponse struct {
	Policies      []IAMBindingPolicy        `json:"policies"`
	PolicyItems   []IAMMemberRelationPolicy `json:"policy_item"`
	Items         []IAMMemberRelationPolicy `json:"items"`
	NextPageToken string                    `json:"next_page_token"`
}

type IAMMemberRelationPolicy struct {
	ID             string        `json:"id"`
	PolicyID       string        `json:"policy_id"`
	Scope          string        `json:"scope"`
	MemberType     string        `json:"member_type"`
	MemberValue    string        `json:"member_value"`
	MemberName     string        `json:"member_name"`
	MemberIdentify string        `json:"member_identify"`
	RoleID         string        `json:"role_id"`
	RoleInfos      []IAMRoleInfo `json:"role_infos"`
}

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

type virtualClusterNodeRemoveResponse struct {
	AIComputeNodes []AIComputeNode `json:"ai_compute_nodes"`
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

type iamUserGroupSearchResponse struct {
	Items      []IAMUserGroupSearchItem `json:"items"`
	Users      []IAMUserGroupSearchItem `json:"users"`
	Groups     []IAMUserGroupSearchItem `json:"groups"`
	UserGroups []IAMUserGroupSearchItem `json:"user_groups"`
}

type iamBindingPolicyListResponse struct {
	Policies      []IAMBindingPolicy `json:"policies"`
	NextPageToken string             `json:"next_page_token"`
	TotalSize     int                `json:"total_size"`
}

type iamRoleListResponse struct {
	Roles         []IAMRoleInfo `json:"roles"`
	RoleInfos     []IAMRoleInfo `json:"role_infos"`
	Items         []IAMRoleInfo `json:"items"`
	NextPageToken string        `json:"next_page_token"`
	TotalSize     int           `json:"total_size"`
}

type iamResourceScope struct {
	ID                  string `json:"id"`
	Scope               string `json:"scope"`
	ResourceScope       string `json:"resource_scope"`
	RID                 string `json:"rid"`
	Name                string `json:"name"`
	ResourceName        string `json:"resource_name"`
	DisplayName         string `json:"display_name"`
	ResourceDisplayName string `json:"resource_display_name"`
	ResourceType        string `json:"resource_type"`
	Type                string `json:"type"`
}

type iamResourceScopeListResponse struct {
	Scopes         []iamResourceScope `json:"scopes"`
	ResourceScopes []iamResourceScope `json:"resource_scopes"`
	ScopeInfos     []iamResourceScope `json:"scope_infos"`
	Resources      []iamResourceScope `json:"resources"`
	Items          []iamResourceScope `json:"items"`
	NextPageToken  string             `json:"next_page_token"`
}

type aiComputeNodeListResponse struct {
	AIComputeNodes []AIComputeNode `json:"ai_compute_nodes"`
}

type storageVolumePageResponse struct {
	Resources     []StorageVolumeResource `json:"resources"`
	NextPageToken string                  `json:"next_page_token"`
	TotalSize     int                     `json:"total_size"`
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
	Logs  []cmsLogItem `json:"logs"`
	Items []cmsLogItem `json:"items"`
	Data  []cmsLogItem `json:"data"`
}

type cmsLogItem struct {
	Timestamp    string         `json:"ts"`
	ObservedTS   string         `json:"observed_ts"`
	Level        string         `json:"level"`
	Msg          string         `json:"msg"`
	WorkloadType string         `json:"workload_type"`
	WorkloadName string         `json:"workload_name"`
	Namespace    string         `json:"namespace"`
	Pod          string         `json:"pod"`
	Container    string         `json:"container"`
	Node         string         `json:"node"`
	Resource     map[string]any `json:"resource"`
}

type ECPWorkloadLogQuery struct {
	Start        time.Time
	End          time.Time
	WorkloadType string
	WorkloadName string
	Pods         []string
	Level        string
	Keyword      string
	Namespace    string
	Container    string
	Limit        int
}

type ECPWorkloadLogResult struct {
	VCluster    string
	ProfileName string
	Items       []ECPWorkloadLogItem
}

type ECPWorkloadLogItem struct {
	Timestamp    string
	ObservedTS   string
	Level        string
	WorkloadName string
	Pod          string
	Container    string
	Message      string
}

type CloudAuditQuery struct {
	Start         time.Time
	End           time.Time
	ServiceType   string
	ResourceType  string
	ResourceName  string
	OperationType string
	UserNames     []string
	Limit         int
}

type CloudAuditResult struct {
	ProfileName string
	TotalSize   int
	Items       []CloudAuditEvent
}

type CloudAuditEvent struct {
	Time          string
	ServiceType   string
	ResourceType  string
	ResourceName  string
	OperationType string
	UserName      string
	UserID        string
	Code          string
	Detail        string
}

type cloudAuditResponse struct {
	AuditEvents   []map[string]any `json:"audit_events"`
	NextPageToken string           `json:"next_page_token"`
	TotalSize     int              `json:"total_size"`
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
	if client, ok := newVirtualClusterClientFromFile(DefaultConfigPath()); ok {
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
	if override := strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_CONFIG")); override != "" {
		return filepath.Clean(override)
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".rayctl", defaultConfigFileName)
	}
	return filepath.Join(home, ".rayctl", defaultConfigFileName)
}

func (c *VirtualClusterClient) CurrentProfileName() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.currentProfile)
}

func (c *VirtualClusterClient) CurrentRegion() string {
	if c == nil {
		return ""
	}
	profile, ok := c.currentClientProfile()
	if !ok {
		return strings.TrimSpace(c.region)
	}
	return strings.TrimSpace(profile.Region)
}

func (c *VirtualClusterClient) CurrentSigninURL() string {
	if c == nil {
		return "https://signin.d.pjlab.org.cn/oauth2/token"
	}
	profile, ok := c.currentClientProfile()
	if !ok {
		return "https://signin.d.pjlab.org.cn/oauth2/token"
	}
	return signinURLFromIAMBaseURL(profile.IAMBaseURL)
}

func (c *VirtualClusterClient) CurrentIAMBaseURL() string {
	if c == nil {
		return defaultIAMBaseURL
	}
	profile, ok := c.currentClientProfile()
	if !ok {
		return defaultIAMBaseURL
	}
	return strings.TrimRight(strings.TrimSpace(profile.IAMBaseURL), "/")
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

func signinURLFromIAMBaseURL(iamBaseURL string) string {
	iamBaseURL = strings.TrimRight(strings.TrimSpace(iamBaseURL), "/")
	if iamBaseURL == "" {
		return "https://signin.d.pjlab.org.cn/oauth2/token"
	}
	parsed, err := url.Parse(iamBaseURL)
	if err != nil || parsed.Host == "" {
		return "https://signin.d.pjlab.org.cn/oauth2/token"
	}
	host := parsed.Host
	switch {
	case strings.HasPrefix(host, "iam."):
		host = "signin." + strings.TrimPrefix(host, "iam.")
	case strings.HasPrefix(host, "iam-"):
		host = "signin-" + strings.TrimPrefix(host, "iam-")
	default:
		host = "signin." + host
	}
	parsed.Host = host
	parsed.Path = "/oauth2/token"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	return parsed.String()
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

func (c *VirtualClusterClient) clientProfileByName(profileName string) (clientProfile, bool) {
	profileName = strings.TrimSpace(profileName)
	if profileName == "" || profileName == c.currentProfile {
		return c.currentClientProfile()
	}
	profile, ok := c.profiles[profileName]
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

func (c *VirtualClusterClient) resolveUserIDsWithProfile(ctx context.Context, profile clientProfile, usernames []string) (map[string]string, error) {
	result := make(map[string]string, len(usernames))
	for start := 0; start < len(usernames); start += defaultPageLimit {
		end := start + defaultPageLimit
		if end > len(usernames) {
			end = len(usernames)
		}
		chunk := usernames[start:end]
		filters := make([]string, 0, len(chunk))
		for _, username := range chunk {
			filters = append(filters, fmt.Sprintf(`username="%s"`, escapeIAMFilterValue(username)))
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
			username := strings.TrimSpace(user.Username)
			id := strings.TrimSpace(user.ID)
			if username != "" && id != "" {
				result[strings.ToLower(username)] = id
			}
		}
	}
	return result, nil
}

func escapeIAMFilterValue(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
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

func (c *VirtualClusterClient) findExactUsersForProfile(ctx context.Context, profile clientProfile, identifier string) ([]IAMUser, error) {
	u, _ := url.Parse(profile.IAMBaseURL)
	u.Path = "/iam/idp/v1/getUsers"
	query := u.Query()
	query.Set("includeAdmin", "true")
	query.Set("page_token", "1")
	query.Set("page_size", "10")
	query.Set("order_by", "create_time desc")
	escaped := escapeIAMFilterValue(identifier)
	query.Set("filter", fmt.Sprintf(`username="%s" OR id="%s"`, escaped, escaped))
	u.RawQuery = query.Encode()

	var payload iamUserListResponse
	if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
		return nil, err
	}

	result := make([]IAMUser, 0, len(payload.Users))
	for _, user := range payload.Users {
		if strings.EqualFold(strings.TrimSpace(user.Username), identifier) || strings.EqualFold(strings.TrimSpace(user.ID), identifier) {
			result = append(result, user)
		}
	}
	return result, nil
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

	// Most callers provide a complete username or user ID. Resolve that with one
	// filtered request before falling back to the paginated fuzzy search.
	exact, exactErr := c.findExactUsersForProfile(ctx, profile, identifier)
	if len(exact) > 0 {
		sort.Slice(exact, func(i, j int) bool {
			return strings.TrimSpace(exact[i].Username) < strings.TrimSpace(exact[j].Username)
		})
		return exact, nil
	}

	users, err := c.listUsersForProfile(ctx, profile)
	if err != nil {
		if exactErr != nil {
			return nil, fmt.Errorf("find exact user: %v; list users: %w", exactErr, err)
		}
		return nil, fmt.Errorf("list users: %w", err)
	}

	exact = make([]IAMUser, 0)
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
	return c.listIAMBindingPoliciesWithProfile(ctx, profile, "")
}

func (c *VirtualClusterClient) ListIAMBindingPoliciesForProfile(ctx context.Context, profileName string) ([]IAMBindingPolicy, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.listIAMBindingPoliciesWithProfile(ctx, profile, "")
}

func (c *VirtualClusterClient) ListIAMBindingPoliciesForResourceProfile(ctx context.Context, profileName string, resourceName string) ([]IAMBindingPolicy, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	resourceName = escapeIAMFilterValue(resourceName)
	filter := ""
	if resourceName != "" {
		filter = fmt.Sprintf(
			`(member_identify="*%s*" OR role_name="*%s*" OR scope="*%s*")`,
			resourceName,
			resourceName,
			resourceName,
		)
	}
	return c.listIAMBindingPoliciesWithProfile(ctx, profile, filter)
}

func (c *VirtualClusterClient) listIAMBindingPoliciesWithProfile(ctx context.Context, profile clientProfile, filter string) ([]IAMBindingPolicy, error) {
	pageToken := "1"
	result := make([]IAMBindingPolicy, 0)
	for {
		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = "/iam/authz/v1/bindingPolicies"
		query := u.Query()
		query.Set("page_token", pageToken)
		query.Set("pageSize", fmt.Sprintf("%d", defaultPageLimit))
		if strings.TrimSpace(filter) != "" {
			query.Set("filter", filter)
		}
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

func (c *VirtualClusterClient) ListIAMRoles(ctx context.Context) ([]IAMRoleInfo, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}

	pageToken := "1"
	result := make([]IAMRoleInfo, 0)
	for {
		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = "/iam/authz/v1/roles"
		query := u.Query()
		query.Set("page_size", "200")
		query.Set("page_token", pageToken)
		query.Set("filter", "")
		u.RawQuery = query.Encode()

		var payload iamRoleListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return nil, err
		}
		roles := payload.Roles
		if len(roles) == 0 {
			roles = payload.RoleInfos
		}
		if len(roles) == 0 {
			roles = payload.Items
		}
		if len(roles) == 0 {
			break
		}
		result = append(result, roles...)
		nextToken := strings.TrimSpace(payload.NextPageToken)
		if nextToken != "" && nextToken != pageToken {
			pageToken = nextToken
			continue
		}
		if len(roles) < 200 {
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

func (c *VirtualClusterClient) ListOwnIAMRoles(ctx context.Context, scope string, bearerToken string) ([]IAMRoleInfo, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return nil, fmt.Errorf("scope is required")
	}

	pageToken := "1"
	result := make([]IAMRoleInfo, 0)
	for {
		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = "/iam/authz/v1/roles:own"
		query := u.Query()
		query.Set("page_size", "100")
		query.Set("page_token", pageToken)
		query.Set("scope", scope)
		query.Set("level", "resources")
		u.RawQuery = query.Encode()

		var payload iamRoleListResponse
		if err := c.getJSONWithBearerProfile(ctx, profile, u.String(), bearerToken, &payload); err != nil {
			return nil, err
		}
		roles := payload.Roles
		if len(roles) == 0 {
			roles = payload.RoleInfos
		}
		if len(roles) == 0 {
			roles = payload.Items
		}
		if len(roles) == 0 {
			break
		}
		result = append(result, roles...)
		nextToken := strings.TrimSpace(payload.NextPageToken)
		if nextToken != "" && nextToken != pageToken {
			pageToken = nextToken
			continue
		}
		if len(roles) < 100 {
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

func (c *VirtualClusterClient) FindIAMResourceScope(ctx context.Context, name string, searchTerm string, bearerToken string) (*StorageVolumeResource, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}
	name = strings.TrimSpace(name)
	searchTerm = strings.TrimSpace(searchTerm)
	bearerToken = strings.TrimSpace(bearerToken)
	if name == "" {
		return nil, fmt.Errorf("resource name is required")
	}
	if searchTerm == "" {
		searchTerm = name
	}
	if bearerToken == "" {
		return nil, fmt.Errorf("bearer token is required for IAM resource scope lookup")
	}

	pageToken := "1"
	for {
		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = "/iam/authz/v1/services/rm/levels/resources/scopes"
		query := u.Query()
		query.Set("filter", fmt.Sprintf(`name="*%s*" OR display_name="*%s*"`, searchTerm, searchTerm))
		query.Set("page_token", pageToken)
		query.Set("page_size", "50")
		query.Set("order_by", "create_time desc")
		u.RawQuery = query.Encode()

		var payload iamResourceScopeListResponse
		if err := c.getJSONWithBearerProfile(ctx, profile, u.String(), bearerToken, &payload); err != nil {
			return nil, err
		}
		items := payload.Scopes
		if len(items) == 0 {
			items = payload.ResourceScopes
		}
		if len(items) == 0 {
			items = payload.ScopeInfos
		}
		if len(items) == 0 {
			items = payload.Resources
		}
		if len(items) == 0 {
			items = payload.Items
		}
		for _, item := range items {
			itemName := firstNonEmpty(strings.TrimSpace(item.Name), strings.TrimSpace(item.ResourceName))
			displayName := firstNonEmpty(strings.TrimSpace(item.DisplayName), strings.TrimSpace(item.ResourceDisplayName))
			if !strings.EqualFold(itemName, name) && !strings.EqualFold(displayName, name) {
				continue
			}
			scope := firstNonEmpty(strings.TrimSpace(item.Scope), strings.TrimSpace(item.ResourceScope), strings.TrimSpace(item.RID))
			if scope == "" {
				continue
			}
			return &StorageVolumeResource{
				ID:           strings.TrimSpace(item.ID),
				RID:          scope,
				Name:         firstNonEmpty(itemName, name),
				DisplayName:  displayName,
				Type:         strings.TrimSpace(item.Type),
				ResourceType: strings.TrimSpace(item.ResourceType),
				ProfileName:  profile.Name,
			}, nil
		}

		nextPageToken := strings.TrimSpace(payload.NextPageToken)
		if nextPageToken == "" || nextPageToken == pageToken || len(items) == 0 {
			break
		}
		pageToken = nextPageToken
	}
	return nil, fmt.Errorf("IAM resource scope %q not found", name)
}

func (c *VirtualClusterClient) SetIAMPolicy(ctx context.Context, payload IAMSetPolicyRequest, bearerToken string) (*IAMSetPolicyResponse, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}

	u, _ := url.Parse(profile.IAMBaseURL)
	u.Path = "/iam/authz/v1/Policies:setUserPolicy"

	var out IAMSetPolicyResponse
	var err error
	if strings.TrimSpace(bearerToken) != "" {
		err = c.postJSONWithBearerProfile(ctx, profile, u.String(), payload, strings.TrimSpace(bearerToken), &out)
	} else {
		err = c.postJSONWithProfile(ctx, profile, u.String(), payload, &out)
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *VirtualClusterClient) BatchCreateIAMPolicies(ctx context.Context, payload IAMBatchCreatePoliciesRequest, bearerToken string) (*IAMBatchCreatePoliciesResponse, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}

	u, _ := url.Parse(profile.IAMBaseURL)
	u.Path = "/iam/authz/v1/policies:batchCreate"

	var out IAMBatchCreatePoliciesResponse
	var err error
	if strings.TrimSpace(bearerToken) != "" {
		err = c.postJSONWithBearerProfile(ctx, profile, u.String(), payload, strings.TrimSpace(bearerToken), &out)
	} else {
		err = c.postJSONWithProfile(ctx, profile, u.String(), payload, &out)
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *VirtualClusterClient) MemberRelationIAMPolicies(ctx context.Context, payload IAMMemberRelationPoliciesRequest, bearerToken string) (*IAMMemberRelationPoliciesResponse, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}

	u, _ := url.Parse(profile.IAMBaseURL)
	u.Path = "/iam/authz/v1/policies:memberRelationPolicies"

	var out IAMMemberRelationPoliciesResponse
	var err error
	if strings.TrimSpace(bearerToken) != "" {
		err = c.postJSONWithBearerProfile(ctx, profile, u.String(), payload, strings.TrimSpace(bearerToken), &out)
	} else {
		err = c.postJSONWithProfile(ctx, profile, u.String(), payload, &out)
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *VirtualClusterClient) DeleteIAMPolicy(ctx context.Context, policyID string, memberID string, memberType string, bearerToken string) error {
	profile, ok := c.currentClientProfile()
	if !ok {
		return fmt.Errorf("no current platform profile available")
	}
	policyID = strings.TrimSpace(policyID)
	memberID = strings.TrimSpace(memberID)
	memberType = strings.ToUpper(strings.TrimSpace(memberType))
	if policyID == "" {
		return fmt.Errorf("policy id is required")
	}
	if memberID == "" {
		return fmt.Errorf("member id is required")
	}
	if memberType == "" {
		return fmt.Errorf("member type is required")
	}

	u, _ := url.Parse(profile.IAMBaseURL)
	u.Path = "/iam/authz/v1/policies/" + url.PathEscape(policyID)
	query := u.Query()
	query.Set("policy_id", policyID)
	query.Set("member_id", memberID)
	query.Set("member_type", memberType)
	u.RawQuery = query.Encode()

	if strings.TrimSpace(bearerToken) != "" {
		return c.deleteWithBearerProfile(ctx, profile, u.String(), strings.TrimSpace(bearerToken))
	}
	_, err := c.doRequestWithProfile(ctx, profile, http.MethodDelete, u.String(), "", nil)
	return err
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

func (c *VirtualClusterClient) ResolveUserIDs(ctx context.Context, usernames []string) (map[string]string, error) {
	unique := make([]string, 0, len(usernames))
	seen := make(map[string]struct{}, len(usernames))
	for _, username := range usernames {
		username = strings.TrimSpace(username)
		key := strings.ToLower(username)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, username)
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
		resolved, err := c.resolveUserIDsWithProfile(ctx, profile, remaining)
		if err != nil {
			lastErr = err
			continue
		}
		success = true
		nextRemaining := make([]string, 0, len(remaining))
		for _, username := range remaining {
			key := strings.ToLower(strings.TrimSpace(username))
			if id := strings.TrimSpace(resolved[key]); id != "" {
				result[key] = id
				continue
			}
			nextRemaining = append(nextRemaining, username)
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
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return "", nil, clientProfile{}, fmt.Errorf("platform profile %q not found", profileName)
	}
	subscriptionID = firstNonEmpty(strings.TrimSpace(subscriptionID), strings.TrimSpace(profile.Subscription))
	region = firstNonEmpty(strings.TrimSpace(region), strings.TrimSpace(profile.Region))
	virtualClusterName = strings.TrimSpace(virtualClusterName)
	if subscriptionID == "" {
		return "", nil, clientProfile{}, fmt.Errorf("subscription id is required for virtual cluster node removal")
	}
	if virtualClusterName == "" {
		return "", nil, clientProfile{}, fmt.Errorf("virtual cluster name is required")
	}
	if len(acnUIDs) == 0 {
		return "", nil, clientProfile{}, fmt.Errorf("at least one acn uid is required")
	}

	u, _ := url.Parse(profile.BaseURL)
	u.Path = fmt.Sprintf(
		"/compute/ecp/v1/subscriptions/%s/resourceGroups/%s/regions/%s/virtualClusters/%s/AIComputeNodes:remove",
		subscriptionID,
		profile.ResourceGroup,
		region,
		virtualClusterName,
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
	return c.ListVolcanoJobsByLabelSelector(ctx, vclusterName, namespace, "")
}

func (c *VirtualClusterClient) ListVolcanoJobsByLabelSelector(ctx context.Context, vclusterName string, namespace string, labelSelector string) ([]unstructured.Unstructured, error) {
	query := url.Values{}
	if strings.TrimSpace(labelSelector) != "" {
		query.Set("labelSelector", strings.TrimSpace(labelSelector))
	}

	var lastErr error
	for _, profile := range c.orderedProfiles() {
		reqURL := c.kubernetesResourceURLForProfile(profile, vclusterName, fmt.Sprintf("/apis/batch.volcano.sh/v1alpha1/namespaces/%s/jobs", namespace), query)
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

func (c *VirtualClusterClient) SearchIAMUserGroupsForProfileToken(ctx context.Context, profileName string, searchQuery string, bearerToken string) ([]IAMUserGroupSearchItem, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	searchQuery = strings.TrimSpace(searchQuery)
	if searchQuery == "" {
		return nil, fmt.Errorf("member search query is required")
	}
	if strings.TrimSpace(bearerToken) == "" {
		return nil, fmt.Errorf("console bearer token is required")
	}

	u, _ := url.Parse(profile.IAMBaseURL)
	u.Path = "/iam/idp/v1/userGroups:search"
	query := u.Query()
	query.Set("page", "1")
	query.Set("page_size", "100")
	query.Set("query", searchQuery)
	u.RawQuery = query.Encode()

	var payload iamUserGroupSearchResponse
	if err := c.getJSONWithBearerProfile(ctx, profile, u.String(), bearerToken, &payload); err != nil {
		return nil, err
	}
	items := payload.Items
	items = append(items, payload.Users...)
	items = append(items, payload.Groups...)
	items = append(items, payload.UserGroups...)
	return items, nil
}

func (c *VirtualClusterClient) ReviewRBACCreateAccessForProfileToken(ctx context.Context, profileName string, vclusterName string, namespace string, resource string, bearerToken string) (*authorizationv1.SelfSubjectAccessReview, error) {
	return c.ReviewRBACAccessForProfileToken(ctx, profileName, vclusterName, namespace, "post", resource, bearerToken)
}

func (c *VirtualClusterClient) ReviewRBACAccessForProfileToken(ctx context.Context, profileName string, vclusterName string, namespace string, verb string, resource string, bearerToken string) (*authorizationv1.SelfSubjectAccessReview, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	if strings.TrimSpace(bearerToken) == "" {
		return nil, fmt.Errorf("console bearer token is required")
	}

	review := authorizationv1.SelfSubjectAccessReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "authorization.k8s.io/v1",
			Kind:       "SelfSubjectAccessReview",
		},
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace: strings.TrimSpace(namespace),
				Verb:      strings.TrimSpace(verb),
				Group:     "rbac.authorization.k8s.io",
				Resource:  strings.TrimSpace(resource),
			},
		},
	}
	reqURL := c.kubernetesClusterURLForProfile(profile, vclusterName, "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", nil)
	var out authorizationv1.SelfSubjectAccessReview
	if err := c.postJSONWithBearerProfile(ctx, profile, reqURL, review, bearerToken, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *VirtualClusterClient) ListClusterRoleBindingsForProfile(ctx context.Context, profileName string, vclusterName string, labelSelector string) ([]rbacv1.ClusterRoleBinding, error) {
	return c.ListClusterRoleBindingsForProfileToken(ctx, profileName, vclusterName, labelSelector, "")
}

func (c *VirtualClusterClient) ListClusterRoleBindingsForProfileToken(ctx context.Context, profileName string, vclusterName string, labelSelector string, bearerToken string) ([]rbacv1.ClusterRoleBinding, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
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

func (c *VirtualClusterClient) ListRoleBindingsForProfileToken(ctx context.Context, profileName string, vclusterName string, namespace string, labelSelector string, bearerToken string) ([]rbacv1.RoleBinding, error) {
	return c.listRoleBindingsForProfileToken(ctx, profileName, vclusterName, strings.TrimSpace(namespace), labelSelector, bearerToken)
}

func (c *VirtualClusterClient) ListAllRoleBindingsForProfileToken(ctx context.Context, profileName string, vclusterName string, labelSelector string, bearerToken string) ([]rbacv1.RoleBinding, error) {
	return c.listRoleBindingsForProfileToken(ctx, profileName, vclusterName, "", labelSelector, bearerToken)
}

func (c *VirtualClusterClient) listRoleBindingsForProfileToken(ctx context.Context, profileName string, vclusterName string, namespace string, labelSelector string, bearerToken string) ([]rbacv1.RoleBinding, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	query := url.Values{}
	if strings.TrimSpace(labelSelector) != "" {
		query.Set("labelSelector", strings.TrimSpace(labelSelector))
	}
	path := "/apis/rbac.authorization.k8s.io/v1/rolebindings"
	if namespace != "" {
		path = fmt.Sprintf("/apis/rbac.authorization.k8s.io/v1/namespaces/%s/rolebindings", url.PathEscape(namespace))
	}
	reqURL := c.kubernetesClusterURLForProfile(profile, vclusterName, path, query)
	var list rbacv1.RoleBindingList
	if err := c.getJSONWithBearerProfile(ctx, profile, reqURL, bearerToken, &list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (c *VirtualClusterClient) CreateClusterRoleBindingForProfileToken(ctx context.Context, profileName string, vclusterName string, binding rbacv1.ClusterRoleBinding, bearerToken string) (*rbacv1.ClusterRoleBinding, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	reqURL := c.kubernetesClusterURLForProfile(profile, vclusterName, "/apis/rbac.authorization.k8s.io/v1/clusterrolebindings", nil)
	var out rbacv1.ClusterRoleBinding
	if err := c.postJSONWithBearerProfile(ctx, profile, reqURL, binding, bearerToken, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *VirtualClusterClient) CreateRoleBindingForProfileToken(ctx context.Context, profileName string, vclusterName string, namespace string, binding rbacv1.RoleBinding, bearerToken string) (*rbacv1.RoleBinding, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	path := fmt.Sprintf("/apis/rbac.authorization.k8s.io/v1/namespaces/%s/rolebindings", url.PathEscape(strings.TrimSpace(namespace)))
	reqURL := c.kubernetesClusterURLForProfile(profile, vclusterName, path, nil)
	var out rbacv1.RoleBinding
	if err := c.postJSONWithBearerProfile(ctx, profile, reqURL, binding, bearerToken, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *VirtualClusterClient) DeleteClusterRoleBindingForProfileToken(ctx context.Context, profileName string, vclusterName string, bindingName string, bearerToken string) error {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return fmt.Errorf("platform profile %q not found", profileName)
	}
	bindingName = strings.TrimSpace(bindingName)
	if bindingName == "" {
		return fmt.Errorf("clusterrolebinding name is required")
	}
	path := fmt.Sprintf("/apis/rbac.authorization.k8s.io/v1/clusterrolebindings/%s", url.PathEscape(bindingName))
	reqURL := c.kubernetesClusterURLForProfile(profile, vclusterName, path, nil)
	return c.deleteWithBearerProfile(ctx, profile, reqURL, bearerToken)
}

func (c *VirtualClusterClient) DeleteRoleBindingForProfileToken(ctx context.Context, profileName string, vclusterName string, namespace string, bindingName string, bearerToken string) error {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return fmt.Errorf("platform profile %q not found", profileName)
	}
	namespace = strings.TrimSpace(namespace)
	bindingName = strings.TrimSpace(bindingName)
	if namespace == "" {
		return fmt.Errorf("rolebinding namespace is required")
	}
	if bindingName == "" {
		return fmt.Errorf("rolebinding name is required")
	}
	path := fmt.Sprintf(
		"/apis/rbac.authorization.k8s.io/v1/namespaces/%s/rolebindings/%s",
		url.PathEscape(namespace),
		url.PathEscape(bindingName),
	)
	reqURL := c.kubernetesClusterURLForProfile(profile, vclusterName, path, nil)
	return c.deleteWithBearerProfile(ctx, profile, reqURL, bearerToken)
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

	var lastErr error
	for _, profile := range c.orderedProfiles() {
		pageToken := "1"
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
				lastErr = err
				break
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
			if nextPageToken == "" || nextPageToken == pageToken || len(payload.Resources) == 0 {
				break
			}
			pageToken = nextPageToken
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

func (c *VirtualClusterClient) FindResourceByUID(ctx context.Context, uid string, resourceKinds ...string) (*StorageVolumeResource, error) {
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
	for _, profile := range c.orderedProfiles() {
		pageToken := "1"
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
			if nextPageToken == "" || nextPageToken == pageToken || len(payload.Resources) == 0 {
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

func (c *VirtualClusterClient) QueryECPWorkloadLogs(ctx context.Context, vclusterName string, query ECPWorkloadLogQuery) (*ECPWorkloadLogResult, error) {
	var lastErr error
	for _, profile := range c.orderedCMSProfiles() {
		clusterRef, err := c.cmsClusterRefForProfile(ctx, profile, vclusterName)
		if err != nil {
			lastErr = err
			continue
		}
		result, err := c.queryECPWorkloadLogsWithCMSProfile(ctx, profile, clusterRef, query)
		if err != nil {
			lastErr = err
			continue
		}
		result.VCluster = strings.TrimSpace(vclusterName)
		return result, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no cms profile available for ecp workload logs")
}

func (c *VirtualClusterClient) QueryCloudAuditEvents(ctx context.Context, query CloudAuditQuery, bearerToken string) (*CloudAuditResult, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("current platform profile is unavailable")
	}
	if strings.TrimSpace(bearerToken) == "" {
		return nil, fmt.Errorf("console bearer token is required; run rayctl auth login first")
	}

	reqURL := cloudAuditQueryURL(profile, query)
	var payload cloudAuditResponse
	if err := c.getJSONWithBearerProfile(ctx, profile, reqURL, bearerToken, &payload); err != nil {
		return nil, err
	}

	items := make([]CloudAuditEvent, 0, len(payload.AuditEvents))
	for _, item := range payload.AuditEvents {
		items = append(items, CloudAuditEvent{
			Time:          stringFromNested(item, "time", "event_time", "create_time"),
			ServiceType:   stringFromNested(item, "service_type"),
			ResourceType:  stringFromNested(item, "resource_type"),
			ResourceName:  stringFromNested(item, "resource_name", "resource.name", "object_name"),
			OperationType: stringFromNested(item, "operation_type"),
			UserName:      stringFromNested(item, "user_name", "username"),
			UserID:        stringFromNested(item, "user_id"),
			Code:          stringFromNested(item, "code"),
			Detail:        cloudAuditDetail(item["detail"]),
		})
	}

	return &CloudAuditResult{
		ProfileName: profile.Name,
		TotalSize:   payload.TotalSize,
		Items:       items,
	}, nil
}

func (c *VirtualClusterClient) queryECPWorkloadLogsWithCMSProfile(ctx context.Context, profile clientProfile, vclusterName string, query ECPWorkloadLogQuery) (*ECPWorkloadLogResult, error) {
	queryToken, err := c.cmsQueryTokenForProfile(ctx, profile, vclusterName)
	if err != nil {
		return nil, err
	}

	reqURL := c.cmsWorkloadLogQueryURL(profile, query)
	var payload cmsLogQueryResponse
	body, err := c.doRequestWithCMSProfile(ctx, profile, http.MethodGet, reqURL, "", nil, queryToken)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode cms workload log response: %w", err)
	}

	rawItems := payload.Logs
	if len(rawItems) == 0 {
		rawItems = payload.Items
	}
	if len(rawItems) == 0 {
		rawItems = payload.Data
	}

	items := make([]ECPWorkloadLogItem, 0, len(rawItems))
	for _, item := range rawItems {
		items = append(items, ECPWorkloadLogItem{
			Timestamp:    strings.TrimSpace(item.Timestamp),
			ObservedTS:   strings.TrimSpace(item.ObservedTS),
			Level:        strings.TrimSpace(item.Level),
			WorkloadName: firstNonEmpty(strings.TrimSpace(item.WorkloadName), stringFromAny(item.Resource["workload_name"])),
			Pod:          firstNonEmpty(strings.TrimSpace(item.Pod), stringFromAny(item.Resource["k8s.pod.name"]), stringFromAny(item.Resource["pod"])),
			Container:    firstNonEmpty(strings.TrimSpace(item.Container), stringFromAny(item.Resource["k8s.container.name"]), stringFromAny(item.Resource["container"])),
			Message:      strings.TrimSpace(item.Msg),
		})
	}

	return &ECPWorkloadLogResult{
		VCluster:    vclusterName,
		ProfileName: profile.Name,
		Items:       items,
	}, nil
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

func (c *VirtualClusterClient) cmsWorkloadLogQueryURL(profile clientProfile, query ECPWorkloadLogQuery) string {
	u, _ := url.Parse(profile.MonitorBaseURL)
	u.Path = "/query/v1/logs"

	values := u.Query()
	values.Set("model_name", "logs.compute.ecp.v1.virtualCluster.logs")
	for _, dimension := range []string{"resource_type", "resource_id", "id", "ts", "observed_ts", "workload_type", "workload_name", "namespace", "pod", "container", "node", "level", "msg"} {
		values.Add("dimensions", dimension)
	}
	values.Set("filter", buildECPWorkloadLogFilter(query))
	values.Set("time_dimension.dimension", "observed_ts")
	values.Set("start", formatMonitorTime(query.Start))
	values.Set("end", formatMonitorTime(query.End))
	if query.Limit <= 0 {
		query.Limit = 40
	}
	values.Set("page_size", fmt.Sprintf("%d", query.Limit))
	values.Set("skip", "0")
	values.Set("order_by", "ts desc,observed_ts desc")
	u.RawQuery = values.Encode()
	return u.String()
}

func cloudAuditQueryURL(profile clientProfile, query CloudAuditQuery) string {
	u, _ := url.Parse(trailBaseURLFromIAMBaseURL(profile.IAMBaseURL))
	u.Path = "/cts/data/v1/auditevents"

	values := u.Query()
	values.Set("trail_type", "res")
	values.Set("page_token", "1")
	if query.Limit <= 0 {
		query.Limit = 40
	}
	values.Set("page_size", strconv.Itoa(query.Limit))
	values.Set("filter", buildCloudAuditFilter(query))
	u.RawQuery = values.Encode()
	return u.String()
}

func buildCloudAuditFilter(query CloudAuditQuery) string {
	location := time.FixedZone("UTC+8", 8*60*60)
	parts := []string{
		fmt.Sprintf("time>='%s'", query.Start.In(location).Format(time.RFC3339)),
		fmt.Sprintf("time<='%s'", query.End.In(location).Format(time.RFC3339)),
	}
	if value := strings.TrimSpace(query.ServiceType); value != "" {
		parts = append(parts, fmt.Sprintf("service_type='%s'", escapeTrailFilterValue(value)))
	}
	if value := strings.TrimSpace(query.ResourceType); value != "" {
		parts = append(parts, fmt.Sprintf("resource_type='%s'", escapeTrailFilterValue(value)))
	}
	if value := strings.TrimSpace(query.ResourceName); value != "" {
		parts = append(parts, fmt.Sprintf("resource_name='%s'", escapeTrailFilterValue(value)))
	}
	if value := strings.TrimSpace(query.OperationType); value != "" {
		parts = append(parts, fmt.Sprintf("operation_type='%s'", escapeTrailFilterValue(value)))
	}
	userParts := make([]string, 0, len(query.UserNames))
	seenUsers := make(map[string]struct{}, len(query.UserNames))
	for _, userName := range query.UserNames {
		value := strings.TrimSpace(userName)
		if value == "" {
			continue
		}
		if _, ok := seenUsers[value]; ok {
			continue
		}
		seenUsers[value] = struct{}{}
		userParts = append(userParts, fmt.Sprintf("user_name='%s'", escapeTrailFilterValue(value)))
	}
	if len(userParts) > 0 {
		parts = append(parts, "("+strings.Join(userParts, " OR ")+")")
	}
	return strings.Join(parts, " AND ")
}

func escapeTrailFilterValue(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "'", "''")
}

func trailBaseURLFromIAMBaseURL(iamBaseURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(iamBaseURL))
	if err != nil || strings.TrimSpace(parsed.Host) == "" {
		return "https://trail.d.pjlab.org.cn"
	}
	switch {
	case strings.HasPrefix(parsed.Host, "iam."):
		parsed.Host = "trail." + strings.TrimPrefix(parsed.Host, "iam.")
	case strings.HasPrefix(parsed.Host, "iam-"):
		parsed.Host = "trail-" + strings.TrimPrefix(parsed.Host, "iam-")
	default:
		parsed.Host = "trail." + parsed.Host
	}
	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	return strings.TrimRight(parsed.String(), "/")
}

func cloudAuditDetail(value any) string {
	if text := stringFromAny(value); text != "" {
		return text
	}
	if value == nil {
		return ""
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(encoded))
}

func buildECPWorkloadLogFilter(query ECPWorkloadLogQuery) string {
	parts := []string{`resource_type="compute.ecp.v1.virtualCluster"`}
	if value := strings.TrimSpace(query.Keyword); value != "" {
		parts = append(parts, fmt.Sprintf(`matchPhrase(msg,"%s")`, escapeMonitorFilterValue(value)))
	}
	if value := strings.TrimSpace(query.WorkloadType); value != "" {
		parts = append(parts, fmt.Sprintf(`workload_type="%s"`, escapeMonitorFilterValue(value)))
	}
	if value := strings.TrimSpace(query.WorkloadName); value != "" {
		parts = append(parts, fmt.Sprintf(`workload_name="%s"`, escapeMonitorFilterValue(value)))
	}
	if value := strings.TrimSpace(query.Namespace); value != "" {
		parts = append(parts, fmt.Sprintf(`namespace="%s"`, escapeMonitorFilterValue(value)))
	}
	if value := strings.TrimSpace(query.Container); value != "" {
		parts = append(parts, fmt.Sprintf(`container="%s"`, escapeMonitorFilterValue(value)))
	}
	if value := strings.TrimSpace(query.Level); value != "" {
		parts = append(parts, fmt.Sprintf(`level="%s"`, escapeMonitorFilterValue(value)))
	}

	podParts := make([]string, 0, len(query.Pods))
	for _, pod := range query.Pods {
		if value := strings.TrimSpace(pod); value != "" {
			podParts = append(podParts, fmt.Sprintf(`pod="%s"`, escapeMonitorFilterValue(value)))
		}
	}
	if len(podParts) == 1 {
		parts = append(parts, podParts[0])
	} else if len(podParts) > 1 {
		parts = append(parts, "("+strings.Join(podParts, " OR ")+")")
	}

	return strings.Join(parts, " AND ")
}

func escapeMonitorFilterValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return value
}

func formatMonitorTime(value time.Time) string {
	if value.IsZero() {
		value = time.Now()
	}
	return value.UTC().Format("2006-01-02T15:04:05.000Z")
}

func virtualClusterUID(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "vc-") {
		return strings.TrimSpace(strings.TrimPrefix(value, "vc-"))
	}
	return value
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	switch value.(type) {
	case map[string]any, []any:
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func stringFromNested(item map[string]any, keys ...string) string {
	if len(item) == 0 {
		return ""
	}

	roots := []map[string]any{item}
	for _, key := range []string{"attributes", "resource", "fields", "labels", "dimensions", "data"} {
		if nested, ok := item[key].(map[string]any); ok {
			roots = append(roots, nested)
		}
	}

	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		for _, root := range roots {
			if value := stringAtPath(root, key); value != "" {
				return value
			}
		}
		for _, root := range roots {
			if value := findNestedString(root, key); value != "" {
				return value
			}
		}
	}
	return ""
}

func stringAtPath(root map[string]any, path string) string {
	parts := strings.Split(path, ".")
	var current any = root
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return ""
		}
		currentMap, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = currentMap[part]
		if !ok {
			return ""
		}
	}
	return stringFromAny(current)
}

func findNestedString(root map[string]any, key string) string {
	if value := stringFromAny(root[key]); value != "" {
		return value
	}
	for _, value := range root {
		nested, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if found := findNestedString(nested, key); found != "" {
			return found
		}
	}
	return ""
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

func (c *VirtualClusterClient) postJSONWithBearerProfile(ctx context.Context, profile clientProfile, reqURL string, payload any, bearerToken string, out any) error {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request for %s: %w", reqURL, err)
	}

	body, err := c.doSignedRequest(ctx, profile, http.MethodPost, reqURL, "application/json", bodyBytes, false, bearerToken)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	return nil
}

func (c *VirtualClusterClient) deleteWithBearerProfile(ctx context.Context, profile clientProfile, reqURL string, bearerToken string) error {
	_, err := c.doSignedRequest(ctx, profile, http.MethodDelete, reqURL, "", nil, false, bearerToken)
	return err
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
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
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
