package platform

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestLookupVCInventoryPreservesPartialProfileErrorsAndScope(t *testing.T) {
	client := &VirtualClusterClient{
		currentProfile: "good",
		profiles: map[string]clientProfile{
			"good":  {Name: "good", AccessKey: "good", SecretKey: "sk", BaseURL: "https://d.test", Region: "d"},
			"bad":   {Name: "bad", AccessKey: "bad", SecretKey: "sk", BaseURL: "https://d.test", Region: "d"},
			"other": {Name: "other", AccessKey: "other", SecretKey: "sk", BaseURL: "https://pt.test", Region: "pt"},
		},
		httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Host != "d.test" {
				t.Errorf("lookup escaped current environment: %s", r.URL.Host)
			}
			if r.Context().Err() != nil {
				return nil, r.Context().Err()
			}
			if strings.Contains(r.Header.Get("Authorization"), `accesskey="bad"`) {
				response := jsonHTTPResponse(r, `{"message":"inventory forbidden"}`)
				response.StatusCode = http.StatusForbidden
				return response, nil
			}
			return jsonHTTPResponse(r, `{"virtual_clusters":[{"uid":"one","name":"vc-one"}]}`), nil
		})},
	}
	items, err := client.ListLookupVirtualClusters(context.Background())
	if len(items) != 1 || items[0].ProfileName != "good" || err == nil || !strings.Contains(err.Error(), `profile "bad"`) || !strings.Contains(err.Error(), "403") {
		t.Fatalf("partial inventory lost: items=%v err=%v", items, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.ListLookupVirtualClusters(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
