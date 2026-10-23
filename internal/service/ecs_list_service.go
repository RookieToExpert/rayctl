package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"rayctl/internal/platform"
)

// ECSListPlatform queries all configured tenants in the selected environment.
// Listing remains independent of HC-wide Kubernetes discovery.
type ECSListPlatform interface {
	ListECSVirtualMachines(context.Context) ([]platform.ECSVirtualMachine, error)
	ListAISpaces(context.Context) ([]platform.AISpace, error)
	ResolveUsernamesForProfile(context.Context, string, []string) (map[string]string, error)
}

type ECSListService struct {
	client     ECSListPlatform
	kubeconfig *string
}

type ECSListOptions struct {
	ResourceType string
	Name         string
	State        string
	Limit        int
}

type ECSListItem struct {
	ResourceType string
	Name         string
	DisplayName  string
	UID          string
	State        string
	InternalIP   string
	Creator      string
	CreatorID    string
	MachineType  string
	VPC          string
	ProfileName  string
	Tenant       string
	TenantID     string
}

type ECSListResult struct {
	Items     []ECSListItem
	Total     int
	Truncated bool
	Warnings  []string
}

func NewECSListService(client ECSListPlatform) *ECSListService {
	return &ECSListService{client: client}
}

func (s *ECSListService) WithMachineSpecs(kubeconfig string) *ECSListService {
	s.kubeconfig = &kubeconfig
	return s
}

func (o ECSListOptions) Validate() error {
	switch strings.ToLower(strings.TrimSpace(o.ResourceType)) {
	case "", "all", "ecs", "ais":
	default:
		return fmt.Errorf("unsupported --type %q; expected all, ecs, or ais", o.ResourceType)
	}
	if o.Limit < 0 {
		return fmt.Errorf("--limit must be 0 or greater")
	}
	return nil
}

func (s *ECSListService) List(ctx context.Context, options ECSListOptions) (*ECSListResult, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("platform client is required for ecs list")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resourceType := strings.ToLower(strings.TrimSpace(options.ResourceType))
	var ecsCall <-chan asyncResult[[]platform.ECSVirtualMachine]
	var aisCall <-chan asyncResult[[]platform.AISpace]
	if resourceType != "ais" {
		ecsCall = asyncCall(ctx, s.client.ListECSVirtualMachines)
	}
	if resourceType != "ecs" {
		aisCall = asyncCall(ctx, s.client.ListAISpaces)
	}

	result := &ECSListResult{Items: []ECSListItem{}, Warnings: []string{}}
	queryErrors := make([]error, 0)
	seen := make(map[string]struct{})
	nameFilter := strings.ToLower(strings.TrimSpace(options.Name))
	stateFilter := strings.TrimSpace(options.State)
	add := func(item ECSListItem, resourceName string) {
		if stateFilter != "" && !strings.EqualFold(item.State, stateFilter) {
			return
		}
		if nameFilter != "" && !strings.Contains(strings.ToLower(resourceName), nameFilter) &&
			!strings.Contains(strings.ToLower(item.Name), nameFilter) &&
			!strings.Contains(strings.ToLower(item.DisplayName), nameFilter) &&
			!strings.Contains(strings.ToLower(item.UID), nameFilter) {
			return
		}
		key := item.ResourceType + "\x00" + firstNonEmpty(item.TenantID, item.ProfileName) + "\x00" + firstNonEmpty(item.UID, resourceName, item.Name)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		result.Items = append(result.Items, item)
	}
	if ecsCall != nil {
		response := <-ecsCall
		if response.Err != nil {
			queryErrors = append(queryErrors, fmt.Errorf("list ECS: %w", response.Err))
		}
		for _, resource := range response.Value {
			add(ECSListItem{
				ResourceType: "ECS",
				Name:         firstNonEmpty(strings.TrimSpace(resource.Name), strings.TrimSpace(resource.DisplayName), resource.UID, resource.ID),
				DisplayName:  strings.TrimSpace(resource.DisplayName),
				UID:          firstNonEmpty(strings.TrimSpace(resource.UID), strings.TrimSpace(resource.ID)),
				State:        strings.TrimSpace(resource.State),
				InternalIP:   ecsPlatformIP(resource),
				CreatorID:    strings.TrimSpace(resource.CreatorID),
				MachineType:  ecsMachineType(resource),
				VPC:          ecsVPC(resource),
				ProfileName:  resource.ProfileName,
				Tenant:       resource.ProfileName,
				TenantID:     resource.TenantID,
			}, strings.TrimSpace(resource.Name))
		}
	}
	if aisCall != nil {
		response := <-aisCall
		if response.Err != nil {
			queryErrors = append(queryErrors, fmt.Errorf("list AIS: %w", response.Err))
		}
		for _, resource := range response.Value {
			add(ECSListItem{
				ResourceType: "AIS",
				Name:         firstNonEmpty(strings.TrimSpace(resource.DisplayName), strings.TrimSpace(resource.Name), resource.UID, resource.ID),
				DisplayName:  strings.TrimSpace(resource.DisplayName),
				UID:          firstNonEmpty(strings.TrimSpace(resource.UID), strings.TrimSpace(resource.ID)),
				State:        strings.TrimSpace(resource.State),
				InternalIP:   aisPlatformIP(resource),
				CreatorID:    strings.TrimSpace(resource.CreatorID),
				MachineType:  aisMachineType(resource),
				VPC:          aisVPC(resource),
				ProfileName:  resource.ProfileName,
				Tenant:       resource.ProfileName,
				TenantID:     resource.TenantID,
			}, strings.TrimSpace(resource.Name))
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, err := range queryErrors {
		result.Warnings = append(result.Warnings, err.Error())
	}
	sort.Slice(result.Items, func(i, j int) bool {
		a, b := result.Items[i], result.Items[j]
		if a.ResourceType != b.ResourceType {
			return a.ResourceType < b.ResourceType
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Tenant != b.Tenant {
			return a.Tenant < b.Tenant
		}
		return a.UID < b.UID
	})
	result.Total = len(result.Items)
	if options.Limit > 0 && len(result.Items) > options.Limit {
		result.Items = result.Items[:options.Limit]
		result.Truncated = true
	}

	if s.kubeconfig != nil {
		result.Warnings = append(result.Warnings, enrichECSMachineSpecs(ctx, result.Items, *s.kubeconfig)...)
	}

	// Creator IDs are resolved in their resource's tenant; identical IDs in
	// different tenant directories must not overwrite each other.
	byProfile := make(map[string][]string)
	seenCreators := make(map[string]map[string]bool)
	for _, item := range result.Items {
		if seenCreators[item.ProfileName] == nil {
			seenCreators[item.ProfileName] = make(map[string]bool)
		}
		if item.CreatorID != "" && !seenCreators[item.ProfileName][item.CreatorID] {
			byProfile[item.ProfileName] = append(byProfile[item.ProfileName], item.CreatorID)
			seenCreators[item.ProfileName][item.CreatorID] = true
		}
	}
	profiles := make([]string, 0, len(byProfile))
	for profile := range byProfile {
		profiles = append(profiles, profile)
	}
	sort.Strings(profiles)
	enrichCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	type creatorResult struct {
		profile   string
		usernames map[string]string
		err       error
	}
	lookups := boundedMap(enrichCtx, profiles, 4, func(ctx context.Context, profile string) creatorResult {
		users, err := s.client.ResolveUsernamesForProfile(ctx, profile, byProfile[profile])
		return creatorResult{profile: profile, usernames: users, err: err}
	})
	usernames := make(map[string]map[string]string)
	for _, lookup := range lookups {
		usernames[lookup.profile] = lookup.usernames
		if lookup.err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("resolve creator usernames in profile %q: %v; retaining creator IDs", lookup.profile, lookup.err))
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for index := range result.Items {
		item := &result.Items[index]
		item.Creator = firstNonEmpty(strings.TrimSpace(usernames[item.ProfileName][item.CreatorID]), item.CreatorID)
	}
	return result, errors.Join(queryErrors...)
}

func aisPlatformIP(resource platform.AISpace) string {
	if ip := strings.TrimSpace(resource.Properties.HostIP); ip != "" {
		return ip
	}
	for _, nic := range resource.Properties.VirtualMachineProperties.NetworkInterfaces {
		if ip := strings.TrimSpace(nic.Properties.IPv4Addr); ip != "" {
			return ip
		}
	}
	return ""
}
