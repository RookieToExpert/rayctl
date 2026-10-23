package platform

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

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
	MetricsBaseURL    string
	MetricsUsername   string
	MetricsPassword   string
	Subscription      string
	ResourceGroup     string
	Region            string
}

func (c *VirtualClusterClient) CurrentProfileName() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.currentProfile)
}

// ProfileNames returns configured profile names in the selected environment.
// An empty environment means the current profile's environment; "all" keeps
// every configured profile.
func (c *VirtualClusterClient) ProfileNames(environment string) []string {
	if c == nil {
		return nil
	}
	environment = normalizeProfileEnvironment(environment)
	if environment == "" {
		environment = c.ProfileEnvironment(c.CurrentProfileName())
	}
	names := make([]string, 0)
	for _, profile := range c.orderedProfiles() {
		if environment != "all" && profileEnvironment(profile) != environment {
			continue
		}
		names = append(names, profile.Name)
	}
	return names
}

// ResolveProfileName maps a user-facing environment name to a configured
// profile without changing current_profile. This keeps one-off cross-region
// queries isolated from the rest of the CLI process.
func (c *VirtualClusterClient) ResolveProfileName(environment string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("platform client is unavailable")
	}
	environment = normalizeProfileEnvironment(environment)
	if environment == "" {
		if profile, ok := c.currentClientProfile(); ok {
			return profile.Name, nil
		}
		return "", fmt.Errorf("current platform profile is unavailable")
	}

	current, _ := c.currentClientProfile()
	candidates := make([]clientProfile, 0)
	for _, profile := range c.orderedProfiles() {
		if profileEnvironment(profile) == environment {
			candidates = append(candidates, profile)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no platform profile configured for environment %q", environment)
	}
	if profileEnvironment(current) == environment {
		return current.Name, nil
	}

	currentFamily := profileTenantFamily(current.Name)
	for _, profile := range candidates {
		if profileTenantFamily(profile.Name) == currentFamily {
			return profile.Name, nil
		}
	}
	if len(candidates) == 1 {
		return candidates[0].Name, nil
	}

	names := make([]string, 0, len(candidates))
	for _, profile := range candidates {
		names = append(names, profile.Name)
	}
	sort.Strings(names)
	return "", fmt.Errorf("environment %q matches multiple profiles (%s); please select the matching tenant as current_profile", environment, strings.Join(names, ", "))
}

// SelectProfileForProcess switches only this in-memory client. It does not
// rewrite platform.json and is useful for one-off commands such as auth login
// that need to cache credentials for a non-current environment.
func (c *VirtualClusterClient) SelectProfileForProcess(environment string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("platform client is unavailable")
	}
	profileName, err := c.ResolveProfileName(environment)
	if err != nil {
		return "", err
	}
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return "", fmt.Errorf("platform profile %q not found", profileName)
	}
	c.currentProfile = profile.Name
	c.accessKey = profile.AccessKey
	c.secretKey = profile.SecretKey
	c.baseURL = profile.BaseURL
	c.kubernetesBaseURL = profile.KubernetesBaseURL
	c.iamBaseURL = profile.IAMBaseURL
	c.subscription = profile.Subscription
	c.resourceGroup = profile.ResourceGroup
	c.region = profile.Region
	return profile.Name, nil
}

// SelectProfileNameForProcess switches to an exact configured profile name.
// Resource discovery returns ProfileName, so write operations must keep all
// subsequent IAM lookups and requests in that same environment.
func (c *VirtualClusterClient) SelectProfileNameForProcess(profileName string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("platform client is unavailable")
	}
	profileName = strings.TrimSpace(profileName)
	profile, ok := c.profiles[profileName]
	if !ok {
		return "", fmt.Errorf("platform profile %q not found", profileName)
	}
	c.currentProfile = profile.Name
	c.accessKey = profile.AccessKey
	c.secretKey = profile.SecretKey
	c.baseURL = profile.BaseURL
	c.kubernetesBaseURL = profile.KubernetesBaseURL
	c.iamBaseURL = profile.IAMBaseURL
	c.subscription = profile.Subscription
	c.resourceGroup = profile.ResourceGroup
	c.region = profile.Region
	return profile.Name, nil
}

// ProfileEnvironment returns the user-facing environment for a configured
// profile. It is intended for diagnostics and ambiguity messages.
func (c *VirtualClusterClient) ProfileEnvironment(profileName string) string {
	if c == nil {
		return ""
	}
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return ""
	}
	return profileEnvironment(profile)
}

func normalizeProfileEnvironment(environment string) string {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "", "current", "default":
		return ""
	case "d":
		return "d"
	case "p", "pt":
		return "pt"
	case "dcloud", "cloud":
		return "dcloud"
	default:
		return strings.ToLower(strings.TrimSpace(environment))
	}
}

func profileEnvironment(profile clientProfile) string {
	hosts := strings.ToLower(strings.Join([]string{profile.BaseURL, profile.KubernetesBaseURL, profile.IAMBaseURL}, " "))
	if strings.Contains(hosts, "-cloud.d.pjlab.org.cn") || strings.Contains(hosts, "cloud.d.pjlab.org.cn") {
		return "dcloud"
	}
	if strings.EqualFold(strings.TrimSpace(profile.Region), "cn-pj-03") ||
		(strings.Contains(hosts, ".pjlab.org.cn") && !strings.Contains(hosts, ".d.pjlab.org.cn")) {
		return "pt"
	}
	return "d"
}

func profileTenantFamily(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, suffix := range []string{"-dcloud", "_dcloud", "-cloud", "_cloud", "-pt", "_pt", "-p", "_p", "-d", "_d"} {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix)
		}
	}
	return name
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

type MetricsEndpointConfig struct {
	Profile     string
	Environment string
	BaseURL     string
	Username    string
	Password    string
}

// CurrentMetricsConfig returns the VictoriaMetrics connection for the active
// platform profile. Environment variables intentionally override file values
// so credentials can be injected without persisting them to platform.json.
func (c *VirtualClusterClient) CurrentMetricsConfig() (MetricsEndpointConfig, error) {
	if c == nil {
		return MetricsEndpointConfig{}, fmt.Errorf("platform client is unavailable")
	}
	profile, ok := c.currentClientProfile()
	if !ok {
		return MetricsEndpointConfig{}, fmt.Errorf("current platform profile is unavailable")
	}
	environment := profileEnvironment(profile)
	baseURL := strings.TrimRight(strings.TrimSpace(profile.MetricsBaseURL), "/")
	if baseURL == "" {
		switch environment {
		case "pt":
			baseURL = "k8s://prod-datalake/vmauth-datalake-vm-cms:8427"
		case "dcloud":
			baseURL = "k8s://prod-datalake/vmauth-victoria-metrics-datalake-vm:8427"
		default:
			baseURL = "https://vm-cms.d.pjlab.org.cn"
		}
	}
	username := strings.TrimSpace(profile.MetricsUsername)
	password := strings.TrimSpace(profile.MetricsPassword)
	if value := strings.TrimRight(strings.TrimSpace(os.Getenv("RAYCTL_METRICS_BASE_URL")), "/"); value != "" {
		baseURL = value
	}
	if value := strings.TrimSpace(os.Getenv("RAYCTL_METRICS_USERNAME")); value != "" {
		username = value
	}
	if value := strings.TrimSpace(os.Getenv("RAYCTL_METRICS_PASSWORD")); value != "" {
		password = value
	}
	return MetricsEndpointConfig{
		Profile:     profile.Name,
		Environment: environment,
		BaseURL:     baseURL,
		Username:    username,
		Password:    password,
	}, nil
}

func normalizeCurrentProfileName(current string, profiles map[string]config) string {
	current = strings.TrimSpace(current)
	if current != "" {
		if _, ok := profiles[current]; ok {
			return current
		}
	}
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	fmt.Fprintf(os.Stderr, "Warning: current_profile %q not found in platform.json; falling back to %q\n", current, names[0])
	return names[0]
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
			MetricsBaseURL:    c.metricsBaseURL,
			MetricsUsername:   c.metricsUsername,
			MetricsPassword:   c.metricsPassword,
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

// currentEnvironmentProfiles keeps aggregate queries inside the active
// platform endpoint and region. Profiles for other environments remain
// available to exact-name lookups, but must not delay every list operation.
func (c *VirtualClusterClient) currentEnvironmentProfiles() []clientProfile {
	profiles := c.orderedProfiles()
	current, ok := c.currentClientProfile()
	if !ok || len(profiles) <= 1 {
		return profiles
	}

	currentBaseURL := strings.TrimRight(strings.TrimSpace(current.BaseURL), "/")
	currentRegion := strings.TrimSpace(current.Region)
	result := make([]clientProfile, 0, len(profiles))
	for _, profile := range profiles {
		if !strings.EqualFold(strings.TrimRight(strings.TrimSpace(profile.BaseURL), "/"), currentBaseURL) {
			continue
		}
		if currentRegion != "" && !strings.EqualFold(strings.TrimSpace(profile.Region), currentRegion) {
			continue
		}
		result = append(result, profile)
	}
	if len(result) == 0 {
		return []clientProfile{current}
	}
	return result
}

// A resolved region may differ from the current profile (for example when
// inspecting PT through an HC kubeconfig). Inventory still includes every
// configured tenant at the selected region's platform endpoint.
func (c *VirtualClusterClient) inventoryProfilesForRegion(region string) []clientProfile {
	region = strings.TrimSpace(region)
	if region == "" {
		return c.currentEnvironmentProfiles()
	}
	current, ok := c.currentClientProfile()
	if ok && strings.EqualFold(strings.TrimSpace(current.Region), region) {
		return c.currentEnvironmentProfiles()
	}
	return c.profilesForRegion(region)
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
			MetricsBaseURL:    c.metricsBaseURL,
			MetricsUsername:   c.metricsUsername,
			MetricsPassword:   c.metricsPassword,
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
