package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"rayctl/internal/platform"
)

type ecsListPlatformFake struct {
	ecs        []platform.ECSVirtualMachine
	ais        []platform.AISpace
	ecsErr     error
	aisErr     error
	creatorErr error
	ecsCalls   int
	aisCalls   int
	creatorIDs []string
	usernames  map[string]string
}

type ecsTenantCreatorFake struct {
	ecsListPlatformFake
	mu      sync.Mutex
	lookups map[string][]string
}

func (f *ecsTenantCreatorFake) ResolveUsernamesForProfile(_ context.Context, profile string, ids []string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups[profile] = append([]string(nil), ids...)
	return map[string]string{"same-creator-id": profile + "-creator"}, nil
}

func TestECSListKeepsSameNamesAndCreatorDirectoriesSeparatedByTenant(t *testing.T) {
	fake := &ecsTenantCreatorFake{ecsListPlatformFake: ecsListPlatformFake{ecs: []platform.ECSVirtualMachine{
		{Name: "same-name", UID: "a-uid", TenantID: "a", ProfileName: "tenant-a", CreatorID: "same-creator-id"},
		{Name: "same-name", UID: "b-uid", TenantID: "b", ProfileName: "tenant-b", CreatorID: "same-creator-id"},
	}}, lookups: make(map[string][]string)}
	result, err := NewECSListService(fake).List(context.Background(), ECSListOptions{ResourceType: "ecs"})
	if err != nil || len(result.Items) != 2 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	for _, item := range result.Items {
		if item.Creator != item.Tenant+"-creator" || !reflect.DeepEqual(fake.lookups[item.Tenant], []string{"same-creator-id"}) {
			t.Fatalf("creator crossed tenants: %#v lookups=%#v", item, fake.lookups)
		}
	}
}

func (f *ecsListPlatformFake) ListECSVirtualMachines(context.Context) ([]platform.ECSVirtualMachine, error) {
	f.ecsCalls++
	return f.ecs, f.ecsErr
}

func (f *ecsListPlatformFake) ListAISpaces(context.Context) ([]platform.AISpace, error) {
	f.aisCalls++
	return f.ais, f.aisErr
}

func (f *ecsListPlatformFake) ResolveUsernamesForProfile(_ context.Context, _ string, ids []string) (map[string]string, error) {
	f.creatorIDs = append([]string(nil), ids...)
	return f.usernames, f.creatorErr
}

func TestECSListMergesFiltersDeduplicatesAndLimitsBeforeCreatorLookup(t *testing.T) {
	vm := platform.ECSVirtualMachine{Name: "ecs-zeta", DisplayName: "Production Zeta", UID: "vm-z", State: "RUNNING", CreatorID: "creator-z"}
	vm.Properties.MachineType = "ecs-flavor"
	a := platform.AISpace{Name: "ais-raw-a", DisplayName: "Alpha", UID: "ais-a", State: "RUNNING", CreatorID: "creator-a"}
	a.Properties.HostIP = "10.0.0.1"
	a.Properties.VirtualMachineProperties.MachineType = "ais-flavor"
	b := platform.AISpace{Name: "ais-raw-b", DisplayName: "Beta", UID: "ais-b", State: "STOPPED", CreatorID: "creator-b"}
	fake := &ecsListPlatformFake{
		ecs: []platform.ECSVirtualMachine{vm, vm}, ais: []platform.AISpace{b, a},
		usernames: map[string]string{"creator-a": "alice"},
	}
	result, err := NewECSListService(fake).List(context.Background(), ECSListOptions{State: " running ", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 || !result.Truncated || len(result.Items) != 1 {
		t.Fatalf("result = %#v", result)
	}
	item := result.Items[0]
	if item.ResourceType != "AIS" || item.Name != "Alpha" || item.Creator != "alice" || item.InternalIP != "10.0.0.1" || item.MachineType != "ais-flavor" {
		t.Fatalf("item = %#v", item)
	}
	if !reflect.DeepEqual(fake.creatorIDs, []string{"creator-a"}) {
		t.Fatalf("creator IDs = %v, want only the displayed creator", fake.creatorIDs)
	}
	for _, name := range []string{"AIS-RAW-A", "alpha", "AIS-A"} {
		result, err := NewECSListService(fake).List(context.Background(), ECSListOptions{Name: name})
		if err != nil || len(result.Items) != 1 || result.Items[0].UID != "ais-a" || result.Truncated {
			t.Fatalf("name filter %q: result=%#v error=%v", name, result, err)
		}
	}
	result, err = NewECSListService(fake).List(context.Background(), ECSListOptions{Name: "PRODUCTION"})
	if err != nil || len(result.Items) != 1 || result.Items[0].UID != "vm-z" {
		t.Fatalf("ECS display-name filter: result=%#v error=%v", result, err)
	}
}

func TestECSListTypeSkipsUnrequestedAPI(t *testing.T) {
	for _, resourceType := range []string{"ECS", " ais "} {
		t.Run(resourceType, func(t *testing.T) {
			fake := &ecsListPlatformFake{
				ecs: []platform.ECSVirtualMachine{{Name: "ecs-a", UID: "uid-a"}},
				ais: []platform.AISpace{{Name: "ais-a", UID: "uid-a"}},
			}
			result, err := NewECSListService(fake).List(context.Background(), ECSListOptions{ResourceType: resourceType})
			if err != nil || len(result.Items) != 1 || !strings.EqualFold(result.Items[0].ResourceType, strings.TrimSpace(resourceType)) {
				t.Fatalf("result=%#v error=%v", result, err)
			}
			if strings.TrimSpace(resourceType) == "ECS" && (fake.ecsCalls != 1 || fake.aisCalls != 0) {
				t.Fatalf("unexpected API calls: ecs=%d ais=%d", fake.ecsCalls, fake.aisCalls)
			}
			if strings.TrimSpace(resourceType) == "ais" && (fake.aisCalls != 1 || fake.ecsCalls != 0) {
				t.Fatalf("unexpected API calls: ecs=%d ais=%d", fake.ecsCalls, fake.aisCalls)
			}
		})
	}
}

func TestECSListRetainsSuccessfulSourceAndCreatorIDOnFailures(t *testing.T) {
	aisErr := errors.New("AIS access denied")
	fake := &ecsListPlatformFake{
		ecs:    []platform.ECSVirtualMachine{{Name: "ecs-a", UID: "uid-a", CreatorID: "creator-a"}},
		aisErr: aisErr, creatorErr: errors.New("IAM unavailable"),
	}
	result, err := NewECSListService(fake).List(context.Background(), ECSListOptions{})
	if !errors.Is(err, aisErr) || len(result.Items) != 1 || result.Items[0].Creator != "creator-a" || len(result.Warnings) != 2 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	if !strings.Contains(strings.Join(result.Warnings, "\n"), "IAM unavailable") {
		t.Fatalf("missing creator warning: %v", result.Warnings)
	}
	fake.aisErr = nil
	result, err = NewECSListService(fake).List(context.Background(), ECSListOptions{})
	if err != nil || len(result.Items) != 1 || len(result.Warnings) != 1 {
		t.Fatalf("creator enrichment failure should retain inventory: result=%#v error=%v", result, err)
	}
}

func TestECSListRejectsInvalidOptionsAndCancellationBeforeAPICalls(t *testing.T) {
	fake := &ecsListPlatformFake{}
	svc := NewECSListService(fake)
	for _, options := range []ECSListOptions{{ResourceType: "pod"}, {Limit: -1}} {
		if _, err := svc.List(context.Background(), options); err == nil {
			t.Fatalf("invalid options accepted: %#v", options)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.List(ctx, ECSListOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	if fake.ecsCalls != 0 || fake.aisCalls != 0 {
		t.Fatalf("API calls after validation failure: ecs=%d ais=%d", fake.ecsCalls, fake.aisCalls)
	}
}
