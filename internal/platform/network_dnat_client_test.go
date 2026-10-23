package platform

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestInstanceDNATUsesOwningProfileVPCAndIPAndReadsEveryPage(t *testing.T) {
	dataCalls := 0
	c := &VirtualClusterClient{currentProfile: "a", profiles: map[string]clientProfile{
		"a": {Name: "a", BaseURL: "https://management.d.test", AccessKey: "a-ak", SecretKey: "sk"},
		"b": {Name: "b", BaseURL: "https://management.d.test", AccessKey: "b-ak", SecretKey: "sk", Subscription: "stale-subscription"},
	}, httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.Header.Get("Authorization"), `accesskey="b-ak"`) {
			return nil, fmt.Errorf("wrong tenant credentials")
		}
		if strings.HasSuffix(r.URL.Path, "/natGws") {
			if r.URL.Host != "management.d.test" || r.URL.Query().Get("filter") != `vpc_id="wanted-vpc"` {
				return nil, fmt.Errorf("unscoped NAT query: %s", r.URL)
			}
			return jsonHTTPResponse(r, `{"nat_gws":[{"name":"wanted","id":"rid-wanted","tenant_id":"owner-b","zone":"cn-pj-01a","properties":{"vpc_info":{"uid":"wanted-vpc"}}},{"name":"other-vpc","tenant_id":"owner-b","zone":"cn-pj-01a","properties":{"vpc_info":{"uid":"other-vpc"}}},{"name":"other-tenant","tenant_id":"owner-a","zone":"cn-pj-01a","properties":{"vpc_info":{"uid":"wanted-vpc"}}}]}`), nil
		}
		dataCalls++
		if r.URL.Host != "network.d.test" || r.URL.Path != "/network/vpc/data/v2/subscriptions/owner-b/resourceGroups/default/zones/cn-pj-01a/natGws/wanted/dnatRules" || r.URL.Query().Get("filter") != `internal_ip="10.0.0.8"` || r.URL.Query().Get("rid") != "rid-wanted" {
			return nil, fmt.Errorf("wrong DNAT scope: %s", r.URL)
		}
		if r.URL.Query().Get("page_token") == "1" {
			return jsonHTTPResponse(r, `{"dnat_rules":[{"state":"ACTIVE","properties":{"external_ip":"198.51.100.8","external_port":"22225","internal_ip":"10.0.0.8","internal_port":"22","protocol":"TCP"}},{"properties":{"external_ip":"198.51.100.8","external_port":"9999","internal_ip":"10.0.0.9","internal_port":"22","protocol":"TCP"}}],"next_page_token":"2"}`), nil
		}
		return jsonHTTPResponse(r, `{"dnat_rules":[{"dnat_rule":{"state":"ACTIVE","properties":{"external_ip":"198.51.100.8","external_port":"22225","internal_ip":"10.0.0.8","internal_port":"22","protocol":"TCP"}}},{"properties":{"external_ip":"198.51.100.8","external_port":"8080","internal_ip":"10.0.0.8","internal_port":"80","protocol":"TCP"}}]}`), nil
	})}}
	rules, err := c.FindInstanceDNATRules(context.Background(), InstanceDNATQuery{ProfileName: "b", TenantID: "owner-b", VPCUID: "wanted-vpc", InternalIP: "10.0.0.8"})
	if err != nil || len(rules) != 2 || dataCalls != 2 {
		t.Fatalf("rules=%#v err=%v data calls=%d", rules, err, dataCalls)
	}
	for _, rule := range rules {
		if rule.InternalIP != "10.0.0.8" || rule.NATGateway != "wanted" {
			t.Fatalf("unrelated or unnormalized rule: %#v", rule)
		}
	}
}

func TestInstanceDNATPreservesMappingsWhenAnotherGatewayFails(t *testing.T) {
	c := &VirtualClusterClient{currentProfile: "a", profiles: map[string]clientProfile{"a": {Name: "a", BaseURL: "https://management.d.test", AccessKey: "ak", SecretKey: "sk"}}, httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/natGws") {
			return jsonHTTPResponse(r, `{"nat_gws":[{"name":"good","zone":"cn-pj-01a"},{"name":"bad","zone":"cn-pj-01a"}]}`), nil
		}
		if strings.Contains(r.URL.Path, "/bad/") {
			response := jsonHTTPResponse(r, `{"message":"permission denied"}`)
			response.StatusCode = http.StatusForbidden
			return response, nil
		}
		return jsonHTTPResponse(r, `{"dnat_rules":[{"external_ip":"198.51.100.8","external_port":"22","internal_ip":"10.0.0.8","internal_port":"22","protocol":"TCP"}]}`), nil
	})}}
	rules, err := c.FindInstanceDNATRules(context.Background(), InstanceDNATQuery{ProfileName: "a", TenantID: "owner", VPCUID: "vpc", InternalIP: "10.0.0.8"})
	if len(rules) != 1 || err == nil || !strings.Contains(err.Error(), `gateway "bad"`) {
		t.Fatalf("lost partial mapping or error: %#v %v", rules, err)
	}
}

func TestInstanceDNATDistinguishesCompleteRuleFromEIPOnly(t *testing.T) {
	c := &VirtualClusterClient{}
	query := InstanceDNATQuery{InternalIP: "10.0.0.8", DirectRules: []DNATRule{{ExternalIP: "198.51.100.8"}}}
	rules, err := c.FindInstanceDNATRules(context.Background(), query)
	if len(rules) != 0 || err == nil {
		t.Fatalf("EIP only became a mapping: %#v %v", rules, err)
	}
	query.DirectRules[0].ExternalPort = "22225"
	query.DirectRules[0].InternalPort = "22"
	rules, err = c.FindInstanceDNATRules(context.Background(), query)
	if err != nil || len(rules) != 1 || rules[0].InternalIP != "10.0.0.8" {
		t.Fatalf("direct rule was not usable: %#v %v", rules, err)
	}
}
