package platform

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestResourceInventoriesIncludeOtherTenantsOnlyAtCurrentEndpointAndRegion(t *testing.T) {
	for _, kind := range []string{"ecs", "ais", "vpc", "subnet", "nat", "afs"} {
		t.Run(kind, func(t *testing.T) {
			client := &VirtualClusterClient{
				currentProfile: "tenant-a",
				profiles: map[string]clientProfile{
					"tenant-a":       {Name: "tenant-a", AccessKey: "a", SecretKey: "sk", BaseURL: "https://d.test", Region: "cn-pj-01"},
					"tenant-b":       {Name: "tenant-b", AccessKey: "b", SecretKey: "sk", BaseURL: "https://d.test", Region: "cn-pj-01"},
					"alias-a":        {Name: "alias-a", AccessKey: "a", SecretKey: "sk", BaseURL: "https://d.test", Region: "cn-pj-01"},
					"other-region":   {Name: "other-region", AccessKey: "other", SecretKey: "sk", BaseURL: "https://d.test", Region: "cn-pj-03"},
					"other-endpoint": {Name: "other-endpoint", AccessKey: "other", SecretKey: "sk", BaseURL: "https://cloud.test", Region: "cn-pj-01"},
				},
				httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					key := "a"
					if strings.Contains(r.Header.Get("Authorization"), `accesskey="b"`) {
						key = "b"
					}
					if r.URL.Host != "d.test" || strings.Contains(r.Header.Get("Authorization"), `accesskey="other"`) {
						return nil, fmt.Errorf("cross-environment query: %s", r.URL)
					}
					responseKey := map[string]string{"ecs": "virtual_machines", "ais": "ai_spaces", "vpc": "vpcs", "subnet": "Subnets", "nat": "nat_gws", "afs": "resources"}[kind]
					body := fmt.Sprintf(`{"%s":[{"id":"uid-%s","uid":"uid-%s","name":"same-name","tenant_id":"owner-%s","rid":"/subscriptions/owner-%s/regions/cn-pj-01/volumes/same-name"}]}`, responseKey, key, key, key, key)
					return jsonHTTPResponse(r, body), nil
				})},
			}
			var count int
			var err error
			switch kind {
			case "ecs":
				values, e := client.ListECSVirtualMachines(context.Background())
				count, err = len(values), e
			case "ais":
				values, e := client.ListAISpaces(context.Background())
				count, err = len(values), e
			case "vpc":
				values, e := client.ListVPCResources(context.Background())
				count, err = len(values), e
			case "subnet":
				values, e := client.ListSubnetResources(context.Background())
				count, err = len(values), e
			case "nat":
				values, e := client.ListNATGatewayResources(context.Background())
				count, err = len(values), e
			case "afs":
				values, e := client.ListEnvironmentStorageVolumeResources(context.Background())
				count, err = len(values), e
			}
			if err != nil || count != 2 {
				t.Fatalf("inventory count=%d err=%v; want both tenants without alias duplication", count, err)
			}
		})
	}
}

func TestResourceInventoryPreservesSuccessfulTenantsWhenAnotherFails(t *testing.T) {
	client := &VirtualClusterClient{
		currentProfile: "a", profiles: map[string]clientProfile{
			"a": {Name: "a", AccessKey: "a", SecretKey: "sk", BaseURL: "https://d.test", Region: "cn-pj-01"},
			"b": {Name: "b", AccessKey: "b", SecretKey: "sk", BaseURL: "https://d.test", Region: "cn-pj-01"},
		},
		httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if strings.Contains(r.Header.Get("Authorization"), `accesskey="b"`) {
				return nil, fmt.Errorf("tenant permission unavailable")
			}
			return jsonHTTPResponse(r, `{"virtual_machines":[{"uid":"a-uid","name":"available","tenant_id":"owner-a"}]}`), nil
		})},
	}
	items, err := client.ListECSVirtualMachines(context.Background())
	if len(items) != 1 || items[0].ProfileName != "a" || err == nil || !strings.Contains(err.Error(), `profile "b"`) {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}

func TestSSPInventoryProfilesIncludeDifferentTenantFamiliesInSameEnvironment(t *testing.T) {
	client := &VirtualClusterClient{currentProfile: "a", profiles: map[string]clientProfile{
		"a":  {Name: "a", Region: "cn-pj-01", BaseURL: "https://d.test", AccessKey: "a"},
		"b":  {Name: "b", Region: "cn-pj-01", BaseURL: "https://d.test", AccessKey: "b"},
		"pt": {Name: "pt", Region: "cn-pj-03", BaseURL: "https://pt.test"},
	}}
	profiles := client.inventoryProfilesForRegion("cn-pj-01")
	if len(profiles) != 2 || profiles[0].Name != "a" || profiles[1].Name != "b" {
		t.Fatalf("inventory profiles=%#v", profiles)
	}
	profiles = client.inventoryProfilesForRegion("cn-pj-03")
	if len(profiles) != 1 || profiles[0].Name != "pt" {
		t.Fatalf("resolved region profiles=%#v", profiles)
	}
}
