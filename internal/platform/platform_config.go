package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

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
	MetricsBaseURL    string `json:"metrics_base_url"`
	MetricsUsername   string `json:"metrics_username"`
	MetricsPassword   string `json:"metrics_password"`
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
	MetricsBaseURL    string `json:"metrics_base_url"`
	MetricsUsername   string `json:"metrics_username"`
	MetricsPassword   string `json:"metrics_password"`
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
	MetricsBaseURL    string                   `json:"metrics_base_url"`
	MetricsUsername   string                   `json:"metrics_username"`
	MetricsPassword   string                   `json:"metrics_password"`
	ResourceGroup     string                   `json:"resource_group"`
	Region            string                   `json:"region"`
}

func NewVirtualClusterClientFromEnv() (*VirtualClusterClient, bool) {
	client, err := NewVirtualClusterClientFromEnvWithError()
	return client, err == nil && client != nil
}

func NewVirtualClusterClientFromEnvWithError() (*VirtualClusterClient, error) {
	client, err := newVirtualClusterClientFromFile(DefaultConfigPath())
	if err == nil {
		return client, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	accessKey := strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_ACCESS_KEY"))
	secretKey := strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_SECRET_KEY"))
	cluster := normalizeClusterName(os.Getenv("RAYCTL_PLATFORM_CLUSTER"))
	subscription := strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_SUBSCRIPTION_ID"))
	if subscription == "" {
		subscription = defaultSubscriptionForCluster(cluster)
	}
	if accessKey == "" || secretKey == "" || subscription == "" {
		return nil, nil
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
	iamBaseURL = normalizeIAMBaseURLForRegion(iamBaseURL, region)

	client = &VirtualClusterClient{
		accessKey:         accessKey,
		secretKey:         secretKey,
		baseURL:           baseURL,
		kubernetesBaseURL: kubernetesBaseURL,
		iamBaseURL:        iamBaseURL,
		subscription:      subscription,
		resourceGroup:     resourceGroup,
		region:            region,
		metricsBaseURL:    strings.TrimRight(strings.TrimSpace(os.Getenv("RAYCTL_METRICS_BASE_URL")), "/"),
		metricsUsername:   strings.TrimSpace(os.Getenv("RAYCTL_METRICS_USERNAME")),
		metricsPassword:   strings.TrimSpace(os.Getenv("RAYCTL_METRICS_PASSWORD")),
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
				MetricsBaseURL:    strings.TrimRight(strings.TrimSpace(os.Getenv("RAYCTL_METRICS_BASE_URL")), "/"),
				MetricsUsername:   strings.TrimSpace(os.Getenv("RAYCTL_METRICS_USERNAME")),
				MetricsPassword:   strings.TrimSpace(os.Getenv("RAYCTL_METRICS_PASSWORD")),
				Subscription:      subscription,
				ResourceGroup:     resourceGroup,
				Region:            region,
			},
		},
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
	selected, ok := applyProcessEnvironmentSelection(client)
	if !ok {
		return nil, fmt.Errorf("platform environment selection failed")
	}
	return selected, nil
}

func newVirtualClusterClientFromFile(configPath string) (*VirtualClusterClient, error) {
	content, err := os.ReadFile(filepath.Clean(configPath))
	if err != nil {
		return nil, err
	}

	var multiCfg multiProfileConfig
	if err := json.Unmarshal(content, &multiCfg); err == nil && len(multiCfg.Profiles) > 0 {
		profiles := make(map[string]clientProfile, len(multiCfg.Profiles))
		usableProfiles := make(map[string]config, len(multiCfg.Profiles))
		for name, raw := range multiCfg.Profiles {
			profile, ok := makeClientProfile(name, raw)
			if !ok {
				continue
			}
			profiles[name] = profile
			usableProfiles[name] = raw
		}
		currentProfile := normalizeCurrentProfileName(multiCfg.CurrentProfile, usableProfiles)
		current, ok := profiles[currentProfile]
		if !ok {
			return nil, fmt.Errorf("platform config %s has no usable profiles", configPath)
		}
		client := &VirtualClusterClient{
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
		}
		selected, ok := applyProcessEnvironmentSelection(client)
		if !ok {
			return nil, fmt.Errorf("platform config %s has no profile for the selected environment", configPath)
		}
		return selected, nil
	}

	var cfg config
	if err := json.Unmarshal(content, &cfg); err != nil {
		return nil, fmt.Errorf("parse platform config %s: %w", configPath, err)
	}
	profile, ok := makeClientProfile("default", cfg)
	if !ok {
		return nil, fmt.Errorf("platform config %s has no usable profile", configPath)
	}

	client := &VirtualClusterClient{
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
	}
	selected, ok := applyProcessEnvironmentSelection(client)
	if !ok {
		return nil, fmt.Errorf("platform config %s has no profile for the selected environment", configPath)
	}
	return selected, nil
}

func applyProcessEnvironmentSelection(client *VirtualClusterClient) (*VirtualClusterClient, bool) {
	if client == nil {
		return nil, false
	}
	environment := strings.ToLower(strings.TrimSpace(os.Getenv("RAYCTL_PLATFORM_ENVIRONMENT")))
	if environment == "" || environment == "auto" || environment == "all" {
		return client, true
	}
	if _, err := client.SelectProfileForProcess(environment); err != nil {
		return nil, false
	}
	return client, true
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
				MetricsBaseURL:    strings.TrimRight(strings.TrimSpace(raw.MetricsBaseURL), "/"),
				MetricsUsername:   strings.TrimSpace(raw.MetricsUsername),
				MetricsPassword:   strings.TrimSpace(raw.MetricsPassword),
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
			snapshot.MetricsBaseURL = current.MetricsBaseURL
			snapshot.MetricsUsername = current.MetricsUsername
			snapshot.MetricsPassword = current.MetricsPassword
			snapshot.ResourceGroup = current.ResourceGroup
			snapshot.Region = current.Region
		}
		return snapshot, nil
	}

	var cfg ConfigSnapshot
	if err := json.Unmarshal(content, &cfg); err != nil {
		return nil, fmt.Errorf("parse platform config %s: %w", configPath, err)
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
	return withConfigLock(configPath, func() error {
		return saveConfigSnapshot(configPath, cfg)
	})
}

// UpdateConfigSnapshot keeps the read-modify-write cycle under the same lock.
func UpdateConfigSnapshot(configPath string, update func(*ConfigSnapshot) error) error {
	return withConfigLock(configPath, func() error {
		cfg, err := LoadConfigSnapshot(configPath)
		if os.IsNotExist(err) {
			cfg = &ConfigSnapshot{}
		} else if err != nil {
			return err
		}
		if err := update(cfg); err != nil {
			return err
		}
		return saveConfigSnapshot(configPath, cfg)
	})
}

func ValidateConfigFile(configPath string) error {
	content, err := os.ReadFile(filepath.Clean(configPath))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read platform config %s: %w", configPath, err)
	}
	var value json.RawMessage
	if err := json.Unmarshal(content, &value); err != nil {
		return fmt.Errorf("parse platform config %s: %w", configPath, err)
	}
	if trimmed := bytes.TrimSpace(value); len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("platform config %s must be a JSON object", configPath)
	}
	return nil
}

func withConfigLock(configPath string, fn func() error) error {
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if filepath.Base(dir) == ".rayctl" {
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	lock, err := os.OpenFile(configPath+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return fn()
}

func writeConfigAtomically(configPath string, content []byte) error {
	dir := filepath.Dir(configPath)
	f, err := os.CreateTemp(dir, ".platform-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), configPath); err != nil {
		return err
	}
	if dirFile, err := os.Open(dir); err == nil {
		defer dirFile.Close()
		_ = dirFile.Sync()
	}
	return nil
}

func saveConfigSnapshot(configPath string, cfg *ConfigSnapshot) error {
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
		if strings.TrimSpace(cfg.MetricsBaseURL) != "" {
			current.MetricsBaseURL = cfg.MetricsBaseURL
		}
		if strings.TrimSpace(cfg.MetricsUsername) != "" {
			current.MetricsUsername = cfg.MetricsUsername
		}
		if strings.TrimSpace(cfg.MetricsPassword) != "" {
			current.MetricsPassword = cfg.MetricsPassword
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
				MetricsBaseURL:    profile.MetricsBaseURL,
				MetricsUsername:   profile.MetricsUsername,
				MetricsPassword:   profile.MetricsPassword,
				ResourceGroup:     profile.ResourceGroup,
				Region:            profile.Region,
			}
		}

		content, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		return writeConfigAtomically(configPath, append(content, '\n'))
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

	return writeConfigAtomically(configPath, append(content, '\n'))
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

func normalizeIAMBaseURLForRegion(iamBaseURL string, region string) string {
	iamBaseURL = strings.TrimRight(strings.TrimSpace(iamBaseURL), "/")
	if !strings.EqualFold(strings.TrimSpace(region), "cn-pj-03") {
		return iamBaseURL
	}
	if iamBaseURL == "" {
		return defaultPTIAMBaseURL
	}
	parsed, err := url.Parse(iamBaseURL)
	if err != nil {
		return iamBaseURL
	}
	if strings.EqualFold(parsed.Hostname(), "iam.pjlab.org.cn") {
		parsed.Host = "iam-api.pjlab.org.cn"
		return strings.TrimRight(parsed.String(), "/")
	}
	return iamBaseURL
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
	case strings.EqualFold(parsed.Hostname(), "iam-api.pjlab.org.cn"):
		host = "signin.pjlab.org.cn"
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
	iamBaseURL = normalizeIAMBaseURLForRegion(iamBaseURL, region)
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
		MetricsBaseURL:    strings.TrimRight(strings.TrimSpace(cfg.MetricsBaseURL), "/"),
		MetricsUsername:   strings.TrimSpace(cfg.MetricsUsername),
		MetricsPassword:   strings.TrimSpace(cfg.MetricsPassword),
		Subscription:      subscription,
		ResourceGroup:     resourceGroup,
		Region:            region,
	}, true
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
			MetricsBaseURL:    value.MetricsBaseURL,
			MetricsUsername:   value.MetricsUsername,
			MetricsPassword:   value.MetricsPassword,
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
	if strings.TrimSpace(profile.Region) == "" {
		profile.Region = defaultRegion
	}
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
		profile.IAMBaseURL = normalizeIAMBaseURLForRegion("", profile.Region)
		if profile.IAMBaseURL == "" {
			profile.IAMBaseURL = defaultIAMBaseURLForCluster(profile.Cluster)
		}
	}
	profile.IAMBaseURL = normalizeIAMBaseURLForRegion(profile.IAMBaseURL, profile.Region)
	if strings.TrimSpace(profile.MonitorBaseURL) == "" {
		profile.MonitorBaseURL = defaultMonitorBaseURLForCluster(profile.Cluster)
	}
	if strings.TrimSpace(profile.ResourceGroup) == "" {
		profile.ResourceGroup = defaultResourceGroup
	}
}

func cloneConfigProfiles(values map[string]ConfigProfile) map[string]ConfigProfile {
	result := make(map[string]ConfigProfile, len(values))
	for name, value := range values {
		result[name] = value
	}
	return result
}
