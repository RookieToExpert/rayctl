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
