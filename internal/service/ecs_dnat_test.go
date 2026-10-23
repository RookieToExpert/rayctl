package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"rayctl/internal/platform"
)

type dnatPlatformFake struct {
	fakeECSPlatform
	mu      sync.Mutex
	queries []platform.InstanceDNATQuery
}

func (f *dnatPlatformFake) FindInstanceDNATRules(_ context.Context, q platform.InstanceDNATQuery) ([]platform.DNATRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	if q.ProfileName == "b" {
		return nil, fmt.Errorf("DNAT unavailable in b")
	}
	return []platform.DNATRule{{ExternalIP: "198.51.100.1", ExternalPort: "22225", InternalIP: q.InternalIP, InternalPort: "22", Protocol: "TCP", State: "ACTIVE", NATGateway: "nat-a"}}, nil
}

func TestECSDNATKeepsTenantScopeAndPartialLookupStatus(t *testing.T) {
	var a, b platform.ECSVirtualMachine
	if err := json.Unmarshal([]byte(`{"uid":"uid-a","name":"ecs-a","tenant_id":"owner-a","zone":"cn-pj-01a","properties":{"network_interfaces":[{"properties":{"ip_v4_addr":"10.0.0.1","vpc_info":{"uid":"vpc-a"}}}]}}`), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"uid":"uid-b","name":"ecs-b","tenant_id":"owner-b","zone":"cn-pj-01a","properties":{"network_interfaces":[{"properties":{"ip_v4_addr":"10.0.0.2","vpc_info":{"uid":"vpc-b"}}}]}}`), &b); err != nil {
		t.Fatal(err)
	}
	a.ProfileName = "a"
	b.ProfileName = "b"
	items := []ECSCheckItem{{ResourceType: "ECS", UID: "uid-a", Name: "ecs-a", Tenant: "a", InternalIP: "10.0.0.1"}, {ResourceType: "ECS", UID: "uid-b", Name: "ecs-b", Tenant: "b", InternalIP: "10.0.0.2"}}
	fake := &dnatPlatformFake{}
	s := NewECSServiceWithPlatform(nil, fake)
	s.enrichDNAT(context.Background(), items, []platform.ECSVirtualMachine{a, b}, nil)
	if !items[0].DNATComplete || len(items[0].DNATRules) != 1 || items[0].DNATRules[0].InternalIP != "10.0.0.1" || items[1].DNATComplete || len(items[1].DNATRules) != 0 || len(items[1].Warnings) != 1 {
		t.Fatalf("wrong lookup results: %#v", items)
	}
	if len(fake.queries) != 2 {
		t.Fatal(fake.queries)
	}
	for _, q := range fake.queries {
		if q.ProfileName == "a" && (q.TenantID != "owner-a" || q.VPCUID != "vpc-a" || q.InternalIP != "10.0.0.1") {
			t.Fatalf("wrong tenant scope: %#v", q)
		}
	}
}
