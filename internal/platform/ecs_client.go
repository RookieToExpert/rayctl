package platform

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

func (c *VirtualClusterClient) listECSVirtualMachinesWithProfile(ctx context.Context, profile clientProfile, filter string) ([]ECSVirtualMachine, error) {
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
		query.Set("filter", strings.TrimSpace(filter))
		u.RawQuery = query.Encode()

		var payload ecsVirtualMachineListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return result, err
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

func (c *VirtualClusterClient) listAISpacesWithProfile(ctx context.Context, profile clientProfile, filter string) ([]AISpace, error) {
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
		query.Set("filter", strings.TrimSpace(filter))
		u.RawQuery = query.Encode()

		var payload aiSpaceListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return result, err
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

func (c *VirtualClusterClient) ListECSVirtualMachines(ctx context.Context) ([]ECSVirtualMachine, error) {
	return c.listEnvironmentECSVirtualMachines(ctx, "")
}

func (c *VirtualClusterClient) ListAISpaces(ctx context.Context) ([]AISpace, error) {
	return c.listEnvironmentAISpaces(ctx, "")
}

func (c *VirtualClusterClient) FindECSVirtualMachines(ctx context.Context, identifier string) ([]ECSVirtualMachine, error) {
	return c.listEnvironmentECSVirtualMachines(ctx, exactECSResourceFilter(identifier))
}

func (c *VirtualClusterClient) FindAISpaces(ctx context.Context, identifier string) ([]AISpace, error) {
	return c.listEnvironmentAISpaces(ctx, exactECSResourceFilter(identifier))
}

func (c *VirtualClusterClient) listEnvironmentECSVirtualMachines(ctx context.Context, filter string) ([]ECSVirtualMachine, error) {
	return listProfileInventory(ctx, c.currentEnvironmentProfiles(), "ECS", func(ctx context.Context, profile clientProfile) ([]ECSVirtualMachine, error) {
		return c.listECSVirtualMachinesWithProfile(ctx, profile, filter)
	}, func(item *ECSVirtualMachine, profile clientProfile) { item.ProfileName = profile.Name }, func(item ECSVirtualMachine) string {
		return firstNonEmpty(item.TenantID, item.ProfileName) + "|" + firstNonEmpty(item.UID, item.ID, item.Name)
	})
}

func (c *VirtualClusterClient) listEnvironmentAISpaces(ctx context.Context, filter string) ([]AISpace, error) {
	return listProfileInventory(ctx, c.currentEnvironmentProfiles(), "AIS", func(ctx context.Context, profile clientProfile) ([]AISpace, error) {
		return c.listAISpacesWithProfile(ctx, profile, filter)
	}, func(item *AISpace, profile clientProfile) { item.ProfileName = profile.Name }, func(item AISpace) string {
		return firstNonEmpty(item.TenantID, item.ProfileName) + "|" + firstNonEmpty(item.UID, item.ID, item.Name)
	})
}

func (c *VirtualClusterClient) ListCurrentProfileECSVirtualMachines(ctx context.Context) ([]ECSVirtualMachine, error) {
	return c.listCurrentProfileECSVirtualMachines(ctx, "")
}

func (c *VirtualClusterClient) ListCurrentProfileAISpaces(ctx context.Context) ([]AISpace, error) {
	return c.listCurrentProfileAISpaces(ctx, "")
}

func (c *VirtualClusterClient) FindCurrentProfileECSVirtualMachines(ctx context.Context, identifier string) ([]ECSVirtualMachine, error) {
	return c.listCurrentProfileECSVirtualMachines(ctx, exactECSResourceFilter(identifier))
}

func (c *VirtualClusterClient) listCurrentProfileECSVirtualMachines(ctx context.Context, filter string) ([]ECSVirtualMachine, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}
	items, err := c.listECSVirtualMachinesWithProfile(ctx, profile, filter)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].ProfileName = profile.Name
	}
	return items, nil
}

func (c *VirtualClusterClient) FindCurrentProfileAISpaces(ctx context.Context, identifier string) ([]AISpace, error) {
	return c.listCurrentProfileAISpaces(ctx, exactECSResourceFilter(identifier))
}

func (c *VirtualClusterClient) listCurrentProfileAISpaces(ctx context.Context, filter string) ([]AISpace, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}
	items, err := c.listAISpacesWithProfile(ctx, profile, filter)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].ProfileName = profile.Name
	}
	return items, nil
}

func exactECSResourceFilter(identifier string) string {
	identifier = strings.ReplaceAll(strings.TrimSpace(identifier), `\`, `\\`)
	identifier = strings.ReplaceAll(identifier, `"`, `\"`)
	if looksLikeClusterUUID(identifier) {
		return fmt.Sprintf(`uid="%s"`, identifier)
	}
	return fmt.Sprintf(`name="%s" OR display_name="%s"`, identifier, identifier)
}

type ECSVirtualMachine struct {
	Zone        string `json:"zone"`
	TenantID    string `json:"tenant_id"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	UID         string `json:"uid"`
	DisplayName string `json:"display_name"`
	CreatorID   string `json:"creator_id"`
	State       string `json:"state"`
	ProfileName string `json:"-"`
	Properties  struct {
		DNATRules          []DNATRule `json:"dnat_rules"`
		Hostname           string     `json:"hostname"`
		MachineType        string     `json:"machine_type"`
		VirtualMachineType string     `json:"virtual_machine_type"`
		ImageID            string     `json:"image_id"`
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

type ecsVirtualMachineListResponse struct {
	VirtualMachines []ECSVirtualMachine `json:"virtual_machines"`
}
