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

func (c *VirtualClusterClient) ListECSVirtualMachines(ctx context.Context) ([]ECSVirtualMachine, error) {
	result := make([]ECSVirtualMachine, 0)
	seen := make(map[string]struct{})
	var lastErr error
	success := false
	for _, profile := range c.orderedProfiles() {
		items, err := c.listECSVirtualMachinesWithProfile(ctx, profile, "")
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
		items, err := c.listAISpacesWithProfile(ctx, profile, "")
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

func (c *VirtualClusterClient) FindCurrentProfileECSVirtualMachines(ctx context.Context, identifier string) ([]ECSVirtualMachine, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}
	items, err := c.listECSVirtualMachinesWithProfile(ctx, profile, exactECSResourceFilter(identifier))
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].ProfileName = profile.Name
	}
	return items, nil
}

func (c *VirtualClusterClient) FindCurrentProfileAISpaces(ctx context.Context, identifier string) ([]AISpace, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}
	items, err := c.listAISpacesWithProfile(ctx, profile, exactECSResourceFilter(identifier))
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

type ecsVirtualMachineListResponse struct {
	VirtualMachines []ECSVirtualMachine `json:"virtual_machines"`
}
