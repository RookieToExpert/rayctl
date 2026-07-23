package platform

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

type VPCResource struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	UID          string `json:"uid"`
	ResourceType string `json:"resource_type"`
	TenantID     string `json:"tenant_id"`
	Region       string `json:"region"`
	State        string `json:"state"`
	CreateTime   string `json:"create_time"`
	UpdateTime   string `json:"update_time"`
	Properties   struct {
		CIDR              string `json:"cidr"`
		IsDefault         bool   `json:"is_default"`
		EnableRDMANetwork bool   `json:"enable_RDMA_network"`
		SubnetCount       int    `json:"subnet_count"`
		NATGatewayInfos   []struct {
			UID         string `json:"uid"`
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
		} `json:"nat_gw_infos"`
	} `json:"vpc_properties"`
}

type SubnetResource struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	UID          string `json:"uid"`
	ResourceType string `json:"resource_type"`
	TenantID     string `json:"tenant_id"`
	Zone         string `json:"zone"`
	State        string `json:"state"`
	CreateTime   string `json:"create_time"`
	UpdateTime   string `json:"update_time"`
	Properties   struct {
		Scope          string `json:"scope"`
		Provider       string `json:"provider"`
		NetworkType    string `json:"network_type"`
		CIDR           string `json:"cidr"`
		GatewayIP      string `json:"gateway_ip"`
		V4AvailableIPs string `json:"v4_available_ips"`
		VPCInfo        struct {
			UID         string `json:"uid"`
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
		} `json:"vpc_info"`
	} `json:"subnet_properties"`
}

type NATGatewayResource struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	UID          string `json:"uid"`
	ResourceType string `json:"resource_type"`
	TenantID     string `json:"tenant_id"`
	Zone         string `json:"zone"`
	State        string `json:"state"`
	CreateTime   string `json:"create_time"`
	UpdateTime   string `json:"update_time"`
	Properties   struct {
		GatewayType string `json:"gateway_type"`
		SubnetInfo  struct {
			UID         string `json:"uid"`
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
		} `json:"subnet_info"`
		VPCInfo struct {
			UID         string `json:"uid"`
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
		} `json:"vpc_info"`
		SNATRules []struct{} `json:"snat_rules_info"`
		DNATRules []struct{} `json:"dnat_rules_info"`
		EIPs      []struct {
			Name string `json:"eip_name"`
			IP   string `json:"eip_ip"`
			UID  string `json:"eip_uid"`
		} `json:"eips_info"`
	} `json:"properties"`
}

type vpcResourceListResponse struct {
	VPCs          []VPCResource `json:"vpcs"`
	NextPageToken string        `json:"next_page_token"`
}

type subnetResourceListResponse struct {
	Subnets       []SubnetResource `json:"Subnets"`
	NextPageToken string           `json:"next_page_token"`
}

type natGatewayResourceListResponse struct {
	NATGateways   []NATGatewayResource `json:"nat_gws"`
	NextPageToken string               `json:"next_page_token"`
}

func (c *VirtualClusterClient) ListVPCResources(ctx context.Context) ([]VPCResource, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("current platform profile not found")
	}

	pageToken := "1"
	resources := make([]VPCResource, 0)
	for {
		u, _ := url.Parse(profile.BaseURL)
		u.Path = "/network/vpc/v2/subscriptions/-/resourceGroups/-/regions/-/vpcs"
		query := u.Query()
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("page_token", pageToken)
		u.RawQuery = query.Encode()

		var payload vpcResourceListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return nil, err
		}
		resources = append(resources, payload.VPCs...)
		if paginationComplete(pageToken, payload.NextPageToken, len(payload.VPCs)) {
			break
		}
		pageToken = strings.TrimSpace(payload.NextPageToken)
	}
	return resources, nil
}

func (c *VirtualClusterClient) ListSubnetResources(ctx context.Context) ([]SubnetResource, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("current platform profile not found")
	}

	pageToken := "1"
	resources := make([]SubnetResource, 0)
	for {
		u, _ := url.Parse(profile.BaseURL)
		u.Path = "/network/vpc/v2/subscriptions/-/resourceGroups/-/zones/-/subnets"
		query := u.Query()
		query.Set("filter", `vpc_id="**" AND scope="DATA" OR scope="BMS_DATA"`)
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("page_token", pageToken)
		u.RawQuery = query.Encode()

		var payload subnetResourceListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return nil, err
		}
		resources = append(resources, payload.Subnets...)
		if paginationComplete(pageToken, payload.NextPageToken, len(payload.Subnets)) {
			break
		}
		pageToken = strings.TrimSpace(payload.NextPageToken)
	}
	return resources, nil
}

func (c *VirtualClusterClient) ListNATGatewayResources(ctx context.Context) ([]NATGatewayResource, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("current platform profile not found")
	}

	pageToken := "1"
	resources := make([]NATGatewayResource, 0)
	for {
		u, _ := url.Parse(profile.BaseURL)
		u.Path = "/network/vpc/v2/subscriptions/-/resourceGroups/-/zones/-/natGws"
		query := u.Query()
		query.Set("filter", `vpc_id='**'`)
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("page_token", pageToken)
		u.RawQuery = query.Encode()

		var payload natGatewayResourceListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return nil, err
		}
		resources = append(resources, payload.NATGateways...)
		if paginationComplete(pageToken, payload.NextPageToken, len(payload.NATGateways)) {
			break
		}
		pageToken = strings.TrimSpace(payload.NextPageToken)
	}
	return resources, nil
}

func (c *VirtualClusterClient) ListCurrentStorageVolumeResources(ctx context.Context) ([]StorageVolumeResource, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("current platform profile not found")
	}

	pageToken := "1"
	resources := make([]StorageVolumeResource, 0)
	for {
		u, _ := url.Parse(profile.BaseURL)
		u.Path = "/rmh/v1/resources:page"
		query := u.Query()
		query.Set("filter", `resource_type="storage.afs.v1.volume" OR resource_type="storage.afs.v2.volume"`)
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("page_token", pageToken)
		u.RawQuery = query.Encode()

		var payload storageVolumePageResponse
		if err := c.postJSONWithProfile(ctx, profile, u.String(), map[string]any{}, &payload); err != nil {
			return nil, err
		}
		for i := range payload.Resources {
			payload.Resources[i].ProfileName = profile.Name
		}
		resources = append(resources, payload.Resources...)
		if paginationComplete(pageToken, payload.NextPageToken, len(payload.Resources)) {
			break
		}
		pageToken = strings.TrimSpace(payload.NextPageToken)
	}
	return resources, nil
}

func paginationComplete(current string, next string, itemCount int) bool {
	next = strings.TrimSpace(next)
	return itemCount == 0 || next == "" || next == current
}
