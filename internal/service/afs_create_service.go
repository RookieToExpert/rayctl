package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"rayctl/internal/platform"
)

var afsNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var afsSizePattern = regexp.MustCompile(`(?i)^([1-9][0-9]*)\s*(GB|GIB|TB|TIB|PB|PIB)$`)

type AFSCreateService struct {
	client *platform.VirtualClusterClient
}

type AFSCreateOptions struct {
	Name         string
	DisplayName  string
	Size         string
	StorageClass string
	Zone         string
	Years        int
	AutoRenew    bool
}

type AFSCreatePlan struct {
	Name, DisplayName, Size, StorageClass, Zone, Region, SKU, SKUName, Profile, Subscription, ResourceGroup string
	Years                                                                                                   int
	AutoRenew                                                                                               bool
	RequestURL, Payload                                                                                     string

	platformPlan *platform.AFSCreatePlan
}

type AFSCreateResult struct {
	Name, UID, State, OrderID, OrderState, Amount, PaymentResult, Profile, Warning string
}

func NewAFSCreateService(client *platform.VirtualClusterClient) *AFSCreateService {
	return &AFSCreateService{client: client}
}

func (s *AFSCreateService) Prepare(ctx context.Context, options AFSCreateOptions) (*AFSCreatePlan, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("platform client is required")
	}
	options.Name = strings.ToLower(strings.TrimSpace(options.Name))
	if !afsNamePattern.MatchString(options.Name) {
		return nil, fmt.Errorf("AFS name %q must contain only lowercase letters, digits, and hyphens, and cannot end with a hyphen", options.Name)
	}
	capacityGB, err := parseAFSCapacityGB(options.Size)
	if err != nil {
		return nil, err
	}
	platformPlan, err := s.client.PrepareAFSCreate(ctx, platform.AFSCreateRequest{
		Name: options.Name, DisplayName: strings.TrimSpace(options.DisplayName), CapacityGB: capacityGB,
		StorageClass: options.StorageClass, Zone: options.Zone, Years: options.Years, AutoRenew: options.AutoRenew,
	})
	if err != nil {
		return nil, err
	}
	payloadBytes, err := json.MarshalIndent(platformPlan.Payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal AFS creation payload: %w", err)
	}
	return &AFSCreatePlan{
		Name: platformPlan.Name, DisplayName: platformPlan.DisplayName,
		Size: formatAFSCapacity(capacityGB), StorageClass: platformPlan.StorageClass,
		Zone: platformPlan.Zone, Region: platformPlan.Region, Years: platformPlan.Years, AutoRenew: platformPlan.AutoRenew,
		SKU: platformPlan.SKU, SKUName: platformPlan.SKUName, Profile: platformPlan.ProfileName,
		Subscription: platformPlan.Subscription, ResourceGroup: platformPlan.ResourceGroup,
		RequestURL: platformPlan.RequestURL, Payload: string(payloadBytes), platformPlan: platformPlan,
	}, nil
}

func (s *AFSCreateService) Apply(ctx context.Context, plan *AFSCreatePlan) (*AFSCreateResult, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("platform client is required")
	}
	if plan == nil || plan.platformPlan == nil {
		return nil, fmt.Errorf("prepared AFS creation plan is required")
	}
	created, err := s.client.CreateAFS(ctx, plan.platformPlan)
	if created == nil {
		return nil, err
	}
	result := &AFSCreateResult{
		Name: created.Name, UID: created.UID, State: created.State, OrderID: created.OrderID,
		OrderState: created.OrderState, Amount: created.Amount, PaymentResult: created.PaymentResult,
		Profile: created.ProfileName, Warning: created.Warning,
	}
	return result, err
}

func parseAFSCapacityGB(value string) (int64, error) {
	match := afsSizePattern.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 3 {
		return 0, fmt.Errorf("invalid AFS size %q; use values such as 500GB, 1TB, or 2PB", value)
	}
	number, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid AFS size %q", value)
	}
	multiplier := int64(1)
	switch strings.ToUpper(match[2]) {
	case "TB", "TIB":
		multiplier = 1024
	case "PB", "PIB":
		multiplier = 1024 * 1024
	}
	if number > (1<<63-1)/multiplier {
		return 0, fmt.Errorf("AFS size %q is too large", value)
	}
	return number * multiplier, nil
}

func formatAFSCapacity(capacityGB int64) string {
	if capacityGB > 0 && capacityGB%(1024*1024) == 0 {
		return fmt.Sprintf("%dPB (%dGB)", capacityGB/(1024*1024), capacityGB)
	}
	if capacityGB > 0 && capacityGB%1024 == 0 {
		return fmt.Sprintf("%dTB (%dGB)", capacityGB/1024, capacityGB)
	}
	return fmt.Sprintf("%dGB", capacityGB)
}
