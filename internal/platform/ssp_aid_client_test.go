package platform

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFindSSPAIDsUsesNameFilter(t *testing.T) {
	var filter string
	client := testSSPAIDClient(func(request *http.Request) string {
		filter = request.URL.Query().Get("filter")
		return `{"aids":[{"name":"dev-demo","uid":"aid-uid","properties":{"workload_properties":{"workspace_name":"ws-demo"}}}],"total_size":1}`
	})

	items, err := client.FindSSPAIDs(context.Background(), "sub-1", "cn-pj-03", "ws-demo", "dev-demo")
	if err != nil {
		t.Fatalf("FindSSPAIDs returned error: %v", err)
	}
	if len(items) != 1 || items[0].ProfileName != "pt" {
		t.Fatalf("unexpected items: %#v", items)
	}
	if filter != `name="dev-demo"` {
		t.Fatalf("unexpected filter: %q", filter)
	}
}

func TestListSSPAIDsInWorkspaceUsesStateAndLimit(t *testing.T) {
	var filter, pageSize string
	client := testSSPAIDClient(func(request *http.Request) string {
		filter = request.URL.Query().Get("filter")
		pageSize = request.URL.Query().Get("page_size")
		return `{"aids":[{"name":"dev-demo","state":"Running"}],"total_size":1}`
	})
	items, err := client.ListSSPAIDsInWorkspace(context.Background(), SSPWorkspace{Name: "ws-demo", ProfileName: "pt", Subscription: "sub-1", Region: "cn-pj-03"}, "Running", 10)
	if err != nil {
		t.Fatalf("ListSSPAIDsInWorkspace() error = %v", err)
	}
	if len(items) != 1 || items[0].Properties.Workload.WorkspaceName != "ws-demo" {
		t.Fatalf("items = %#v", items)
	}
	if filter != `state="Running"` || pageSize != "10" {
		t.Fatalf("filter=%q page_size=%q", filter, pageSize)
	}
}

func TestFindSSPAIDsScansLocallyForUID(t *testing.T) {
	requests := 0
	client := testSSPAIDClient(func(request *http.Request) string {
		requests++
		if filter := request.URL.Query().Get("filter"); filter != "" {
			t.Fatalf("UID lookup must not send unsupported API filter: %q", filter)
		}
		if request.URL.Query().Get("skip") == "0" {
			return `{"aids":[{"name":"first","uid":"first-uid"}],"total_size":2}`
		}
		return `{"aids":[{"name":"wanted","uid":"0908d002-fc65-4291-a268-102360023265"}],"total_size":2}`
	})

	items, err := client.FindSSPAIDs(context.Background(), "sub-1", "cn-pj-03", "ws-demo", "0908d002-fc65-4291-a268-102360023265")
	if err != nil {
		t.Fatalf("FindSSPAIDs returned error: %v", err)
	}
	if len(items) != 1 || items[0].Name != "wanted" || requests != 2 {
		t.Fatalf("unexpected UID lookup result: items=%#v requests=%d", items, requests)
	}
}

func TestNormalizeSSPAIDNestedDNATRule(t *testing.T) {
	items := []SSPAIDDNATRule{{}}
	items[0].DNATRule.Name = "dnat-one"
	items[0].DNATRule.Properties.ExternalIP = "10.140.80.10"
	items[0].DNATRule.Properties.InternalPort = "22"
	items[0].DNATRule.Properties.Protocol = "TCP"
	result := normalizeSSPAIDDNATRules(items)
	if result[0].Name != "dnat-one" || result[0].ExternalIP != "10.140.80.10" || result[0].InternalPort != "22" {
		t.Fatalf("unexpected normalized rule: %#v", result[0])
	}
}

func TestFindSSPAIDDNATRulesLoadsDetailsWhenDirectRuleIsPartial(t *testing.T) {
	client := testSSPAIDClient(func(request *http.Request) string {
		switch {
		case strings.HasSuffix(request.URL.Path, "/natGws"):
			return `{"nat_gws":[{"id":"nat-rid","name":"nat-one","zone":"cn-pj-01a"}]}`
		case strings.HasSuffix(request.URL.Path, "/dnatRules"):
			return `{"dnat_rules":[{"properties":{"external_ip":"10.140.158.149","external_port":"11960","internal_ip":"10.119.138.158","internal_port":"22","protocol":"TCP"}}]}`
		default:
			t.Fatalf("unexpected request: %s", request.URL.String())
			return `{}`
		}
	})
	profile := client.profiles["pt"]
	profile.BaseURL = "https://management.d.pjlab.org.cn"
	profile.Region = "cn-pj-01"
	client.profiles["pt"] = profile
	aid := SSPAID{TenantID: "tenant-1", Zone: "cn-pj-01a"}
	aid.Properties.HostIP = "10.119.138.158"
	aid.Properties.DNATRules = []SSPAIDDNATRule{{ExternalIP: "10.140.158.149", ExternalPort: "11960", Protocol: "TCP"}}
	aid.Properties.Workload.NetworkInterfaces = append(aid.Properties.Workload.NetworkInterfaces, struct {
		Properties struct {
			VPCInfo struct {
				UID         string `json:"uid"`
				Name        string `json:"name"`
				DisplayName string `json:"display_name"`
			} `json:"vpc_info"`
		} `json:"properties"`
	}{})
	aid.Properties.Workload.NetworkInterfaces[0].Properties.VPCInfo.UID = "vpc-uid"

	rules, err := client.FindSSPAIDDNATRules(t.Context(), aid)
	if err != nil {
		t.Fatalf("FindSSPAIDDNATRules returned error: %v", err)
	}
	if len(rules) != 1 || rules[0].InternalIP != "10.119.138.158" || rules[0].InternalPort != "22" {
		t.Fatalf("unexpected DNAT rules: %#v", rules)
	}
}

func testSSPAIDClient(response func(*http.Request) string) *VirtualClusterClient {
	return &VirtualClusterClient{
		currentProfile: "pt",
		profiles: map[string]clientProfile{
			"pt": {
				Name:              "pt",
				AccessKey:         "ak",
				SecretKey:         "sk",
				KubernetesBaseURL: "https://compute.pjlab.org.cn",
				ResourceGroup:     "default",
				Region:            "cn-pj-03",
			},
		},
		httpClient: &http.Client{Transport: sspRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(response(request))),
				Request:    request,
			}, nil
		})},
	}
}
