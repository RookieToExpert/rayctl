package platform

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

type SSPAID struct {
	ID           string `json:"id"`
	UID          string `json:"uid"`
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	State        string `json:"state"`
	Zone         string `json:"zone"`
	ResourceType string `json:"resource_type"`
	CreatorID    string `json:"creator_id"`
	OwnerID      string `json:"owner_id"`
	TenantID     string `json:"tenant_id"`
	CreateTime   string `json:"create_time"`
	UpdateTime   string `json:"update_time"`
	ProfileName  string `json:"-"`
	Properties   struct {
		HostIP            string `json:"host_ip"`
		SSHEnabled        *bool  `json:"ssh_enabled"`
		CodeServerEnabled *bool  `json:"code_server_enabled"`
		ImageType         string `json:"image_type"`
		ImagePath         string `json:"image_path"`
		Ownership         struct {
			CreatorID   string `json:"creator_id"`
			CreatorName string `json:"creator_name"`
			OwnerID     string `json:"owner_id"`
			TenantID    string `json:"tenant_id"`
		} `json:"ownership"`
		VolumeMounts []SSPAIDVolumeMount `json:"volume_mounts"`
		DNATRules    []SSPAIDDNATRule    `json:"dnat_rules"`
		Workload     struct {
			WorkspaceName string `json:"workspace_name"`
			Priority      string `json:"priority"`
			Queue         struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Type string `json:"type"`
				UID  string `json:"uid"`
			} `json:"queue"`
			BaseSpec struct {
				MachineTypes          []string `json:"machine_types"`
				CPU                   any      `json:"cpu"`
				Memory                any      `json:"memory"`
				AccelerateDeviceCount any      `json:"accelerate_device_count"`
				GPUModel              string   `json:"gpu_model"`
				GPUMemorySize         any      `json:"gpu_memory_size"`
				RDMAName              string   `json:"rdma_name"`
				SharedMemorySize      any      `json:"shm_size"`
			} `json:"base_spec"`
			NetworkInterfaces []struct {
				Properties struct {
					VPCInfo struct {
						UID         string `json:"uid"`
						Name        string `json:"name"`
						DisplayName string `json:"display_name"`
					} `json:"vpc_info"`
				} `json:"properties"`
			} `json:"network_interfaces"`
		} `json:"workload_properties"`
	} `json:"properties"`
}

type SSPAIDVolumeMount struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	MountPath  string `json:"mount_path"`
	Endpoint   string `json:"endpoint"`
	Zone       string `json:"zone"`
	Region     string `json:"region"`
	AccessMode string `json:"access_mode"`
}

type SSPAIDDNATRule struct {
	Name         string `json:"name"`
	State        string `json:"state"`
	ExternalIP   string `json:"external_ip"`
	ExternalPort string `json:"external_port"`
	InternalIP   string `json:"internal_ip"`
	InternalPort string `json:"internal_port"`
	Protocol     string `json:"protocol"`
	Properties   struct {
		ExternalIP   string `json:"external_ip"`
		ExternalPort string `json:"external_port"`
		InternalIP   string `json:"internal_ip"`
		InternalPort string `json:"internal_port"`
		Protocol     string `json:"protocol"`
	} `json:"properties"`
	DNATRule struct {
		Name       string `json:"name"`
		State      string `json:"state"`
		Properties struct {
			ExternalIP   string `json:"external_ip"`
			ExternalPort string `json:"external_port"`
			InternalIP   string `json:"internal_ip"`
			InternalPort string `json:"internal_port"`
			Protocol     string `json:"protocol"`
		} `json:"properties"`
	} `json:"dnat_rule"`
}

type sspAIDListResponse struct {
	AIDs          []SSPAID `json:"aids"`
	TotalSize     int      `json:"total_size"`
	NextPageToken string   `json:"next_page_token"`
}

type sspAIDNATGateway struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	UID  string `json:"uid"`
	Zone string `json:"zone"`
}

type sspAIDNATGatewayResponse struct {
	NATGateways []sspAIDNATGateway `json:"nat_gws"`
}

type sspAIDDNATResponse struct {
	DNATRules []SSPAIDDNATRule `json:"dnat_rules"`
}

func (c *VirtualClusterClient) FindSSPAIDs(ctx context.Context, subscription string, region string, workspace string, identifier string) ([]SSPAID, error) {
	return c.findSSPAIDs(ctx, "", subscription, region, workspace, identifier)
}

func (c *VirtualClusterClient) FindSSPAIDsForProfile(ctx context.Context, profileName string, subscription string, region string, workspace string, identifier string) ([]SSPAID, error) {
	return c.findSSPAIDs(ctx, profileName, subscription, region, workspace, identifier)
}

func (c *VirtualClusterClient) findSSPAIDs(ctx context.Context, profileName string, subscription string, region string, workspace string, identifier string) ([]SSPAID, error) {
	subscription = strings.TrimSpace(subscription)
	region = strings.TrimSpace(region)
	workspace = strings.TrimSpace(workspace)
	identifier = strings.TrimSpace(identifier)
	if subscription == "" || region == "" || workspace == "" || identifier == "" {
		return nil, fmt.Errorf("subscription, region, workspace and AID identifier are required")
	}

	profiles := c.profilesForRegion(region)
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
	filter := ""
	if !looksLikeClusterUUID(identifier) {
		filter = fmt.Sprintf(`name="%s"`, escapeSSPFilterValue(identifier))
	}
	var lastErr error
	for _, profile := range profiles {
		endpoint, err := sspAIDsURL(profile, subscription, region, workspace)
		if err != nil {
			lastErr = err
			continue
		}
		matches := make([]SSPAID, 0, 1)
		skip := 0
		for {
			query := endpoint.Query()
			query.Set("page_size", "100")
			query.Set("skip", fmt.Sprintf("%d", skip))
			query.Set("order_by", "created_at desc")
			if filter != "" {
				query.Set("filter", filter)
			}
			endpoint.RawQuery = query.Encode()

			var payload sspAIDListResponse
			if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &payload); err != nil {
				lastErr = err
				break
			}
			for _, item := range payload.AIDs {
				if !strings.EqualFold(strings.TrimSpace(item.Name), identifier) && !strings.EqualFold(strings.TrimSpace(item.UID), identifier) {
					continue
				}
				item.ProfileName = profile.Name
				matches = append(matches, item)
			}
			if len(matches) > 0 || filter != "" || len(payload.AIDs) == 0 || skip+len(payload.AIDs) >= payload.TotalSize {
				break
			}
			skip += len(payload.AIDs)
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

func (c *VirtualClusterClient) FindSSPAIDDNATRules(ctx context.Context, aid SSPAID) ([]SSPAIDDNATRule, error) {
	directRules := normalizeSSPAIDDNATRules(aid.Properties.DNATRules)
	for _, rule := range directRules {
		if strings.TrimSpace(rule.ExternalIP) != "" && strings.TrimSpace(rule.ExternalPort) != "" {
			return directRules, nil
		}
	}
	vpcUID := ""
	for _, networkInterface := range aid.Properties.Workload.NetworkInterfaces {
		if value := strings.TrimSpace(networkInterface.Properties.VPCInfo.UID); value != "" {
			vpcUID = value
			break
		}
	}
	if vpcUID == "" || strings.TrimSpace(aid.Properties.HostIP) == "" {
		return nil, nil
	}

	profiles := c.profilesForRegion(sspAIDRegion(aid))
	if len(profiles) == 0 {
		return nil, fmt.Errorf("platform profile %q is unavailable", aid.ProfileName)
	}
	profile := profiles[0]
	for _, candidate := range profiles {
		if candidate.Name == aid.ProfileName {
			profile = candidate
			break
		}
	}

	managementURL, err := url.Parse(strings.TrimRight(profile.BaseURL, "/") + "/network/vpc/v2/subscriptions/-/resourceGroups/-/zones/-/natGws")
	if err != nil {
		return nil, err
	}
	query := managementURL.Query()
	query.Set("filter", fmt.Sprintf(`vpc_id="%s"`, escapeSSPFilterValue(vpcUID)))
	managementURL.RawQuery = query.Encode()
	var gateways sspAIDNATGatewayResponse
	if err := c.getJSONWithProfile(ctx, profile, managementURL.String(), &gateways); err != nil {
		return nil, err
	}

	result := make([]SSPAIDDNATRule, 0)
	for _, gateway := range gateways.NATGateways {
		endpoint, err := sspAIDDNATURL(profile, aid, gateway)
		if err != nil {
			return nil, err
		}
		var payload sspAIDDNATResponse
		if err := c.getJSONWithProfile(ctx, profile, endpoint, &payload); err != nil {
			return nil, err
		}
		result = append(result, normalizeSSPAIDDNATRules(payload.DNATRules)...)
	}
	return result, nil
}

func sspAIDsURL(profile clientProfile, subscription string, region string, workspace string) (*url.URL, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(profile.KubernetesBaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("profile %q has no kubernetes_base_url", profile.Name)
	}
	resourceGroup := firstNonEmpty(strings.TrimSpace(profile.ResourceGroup), defaultResourceGroup)
	return url.Parse(fmt.Sprintf(
		"%s/aid/v1/subscriptions/%s/resourceGroups/%s/regions/%s/workspaces/%s/aids",
		baseURL,
		url.PathEscape(subscription),
		url.PathEscape(resourceGroup),
		url.PathEscape(region),
		url.PathEscape(workspace),
	))
}

func sspAIDDNATURL(profile clientProfile, aid SSPAID, gateway sspAIDNATGateway) (string, error) {
	management, err := url.Parse(profile.BaseURL)
	if err != nil {
		return "", err
	}
	management.Host = strings.Replace(management.Host, "management", "network", 1)
	management.Path = fmt.Sprintf(
		"/network/vpc/data/v2/subscriptions/%s/resourceGroups/%s/zones/%s/natGws/%s/dnatRules",
		url.PathEscape(firstNonEmpty(aid.TenantID, aid.Properties.Ownership.TenantID)),
		url.PathEscape(firstNonEmpty(profile.ResourceGroup, defaultResourceGroup)),
		url.PathEscape(firstNonEmpty(gateway.Zone, aid.Zone)),
		url.PathEscape(gateway.Name),
	)
	query := management.Query()
	query.Set("rid", gateway.ID)
	query.Set("filter", fmt.Sprintf(`internal_ip="%s"`, escapeSSPFilterValue(aid.Properties.HostIP)))
	management.RawQuery = query.Encode()
	return management.String(), nil
}

func normalizeSSPAIDDNATRules(items []SSPAIDDNATRule) []SSPAIDDNATRule {
	for i := range items {
		items[i].Name = firstNonEmpty(items[i].Name, items[i].DNATRule.Name)
		items[i].State = firstNonEmpty(items[i].State, items[i].DNATRule.State)
		items[i].ExternalIP = firstNonEmpty(items[i].ExternalIP, items[i].Properties.ExternalIP, items[i].DNATRule.Properties.ExternalIP)
		items[i].ExternalPort = firstNonEmpty(items[i].ExternalPort, items[i].Properties.ExternalPort, items[i].DNATRule.Properties.ExternalPort)
		items[i].InternalIP = firstNonEmpty(items[i].InternalIP, items[i].Properties.InternalIP, items[i].DNATRule.Properties.InternalIP)
		items[i].InternalPort = firstNonEmpty(items[i].InternalPort, items[i].Properties.InternalPort, items[i].DNATRule.Properties.InternalPort)
		items[i].Protocol = firstNonEmpty(items[i].Protocol, items[i].Properties.Protocol, items[i].DNATRule.Properties.Protocol)
	}
	return items
}

func sspAIDRegion(aid SSPAID) string {
	if strings.TrimSpace(aid.Zone) != "" && len(strings.TrimSpace(aid.Zone)) > 1 {
		return strings.TrimSuffix(strings.TrimSpace(aid.Zone), "a")
	}
	return "cn-pj-03"
}
