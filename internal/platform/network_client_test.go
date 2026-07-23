package platform

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestNetworkResourceAPIs(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Path {
		case "/network/vpc/v2/subscriptions/-/resourceGroups/-/regions/-/vpcs":
			body = `{"vpcs":[{"name":"vpc-test","uid":"vpc-uid","state":"ACTIVE","vpc_properties":{"cidr":"10.0.0.0/16","subnet_count":1}}]}`
		case "/network/vpc/v2/subscriptions/-/resourceGroups/-/zones/-/subnets":
			body = `{"Subnets":[{"name":"subnet-test","uid":"subnet-uid","state":"ACTIVE","subnet_properties":{"cidr":"10.0.1.0/24","vpc_info":{"name":"vpc-test"}}}]}`
		case "/network/vpc/v2/subscriptions/-/resourceGroups/-/zones/-/natGws":
			body = `{"nat_gws":[{"name":"nat-test","uid":"nat-uid","state":"ACTIVE","properties":{"vpc_info":{"name":"vpc-test"},"snat_rules_info":[{}],"dnat_rules_info":[{},{}]}}]}`
		case "/rmh/v1/resources:page":
			if r.Method != http.MethodPost {
				t.Errorf("AFS request method = %s, want POST", r.Method)
			}
			body = `{"resources":[{"id":"afs-uid","name":"afs-test","state":"ACTIVE","properties":"{\"storage_class\":\"OCEANSTOR\"}"}]}`
		default:
			return nil, fmt.Errorf("unexpected request path %s", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})}

	client := &VirtualClusterClient{
		accessKey:  "ak",
		secretKey:  "sk",
		baseURL:    "https://example.test",
		httpClient: httpClient,
	}
	ctx := context.Background()

	vpcs, err := client.ListVPCResources(ctx)
	if err != nil || len(vpcs) != 1 || vpcs[0].Name != "vpc-test" {
		t.Fatalf("ListVPCResources() = %#v, %v", vpcs, err)
	}
	subnets, err := client.ListSubnetResources(ctx)
	if err != nil || len(subnets) != 1 || subnets[0].Properties.VPCInfo.Name != "vpc-test" {
		t.Fatalf("ListSubnetResources() = %#v, %v", subnets, err)
	}
	natGateways, err := client.ListNATGatewayResources(ctx)
	if err != nil || len(natGateways) != 1 || len(natGateways[0].Properties.DNATRules) != 2 {
		t.Fatalf("ListNATGatewayResources() = %#v, %v", natGateways, err)
	}
	afsResources, err := client.ListCurrentStorageVolumeResources(ctx)
	if err != nil || len(afsResources) != 1 || afsResources[0].Name != "afs-test" {
		t.Fatalf("ListCurrentStorageVolumeResources() = %#v, %v", afsResources, err)
	}
}
