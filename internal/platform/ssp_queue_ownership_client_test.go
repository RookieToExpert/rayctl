package platform

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestQueueOwnershipPagePreservesScopeAndContinuation(t *testing.T) {
	client := &VirtualClusterClient{currentProfile: "d", profiles: map[string]clientProfile{
		"d":  {Name: "d", AccessKey: "d-ak", SecretKey: "sk", KubernetesBaseURL: "https://d.test"},
		"pt": {Name: "pt", AccessKey: "pt-ak", SecretKey: "sk", KubernetesBaseURL: "https://pt.test"},
	}, httpClient: &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "pt.test" || !strings.HasPrefix(r.URL.Path, "/ecp/v1/kubernetes/virtualClusters/vc-exact/api/v1/") || r.URL.Query().Get("limit") != "500" || r.URL.Query().Get("continue") != "opaque token" || !strings.Contains(r.Header.Get("Authorization"), `accesskey="pt-ak"`) {
			t.Errorf("wrong scope %s", r.URL)
		}
		if strings.HasSuffix(r.URL.Path, "/pods") && r.URL.Query().Get("fieldSelector") != "active" {
			t.Error(r.URL.Query())
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"metadata":{"continue":"next","resourceVersion":"version"},"items":[]}`)), Request: r}, nil
	})}}
	options := metav1.ListOptions{Limit: 500, Continue: "opaque token"}
	nodes, err := client.ListSSPQueueOwnershipNodesPage(context.Background(), "pt", "vc-exact", options)
	if err != nil || nodes.Continue != "next" || nodes.ResourceVersion != "version" {
		t.Fatal(nodes, err)
	}
	options.FieldSelector = "active"
	pods, err := client.ListSSPQueueOwnershipPodsPage(context.Background(), "pt", "vc-exact", options)
	if err != nil || pods.Continue != "next" {
		t.Fatal(pods, err)
	}
	if _, err := client.ListSSPQueueOwnershipNodesPage(context.Background(), "pt", "", options); err == nil {
		t.Fatal("accepted unscoped ownership query")
	}
}
