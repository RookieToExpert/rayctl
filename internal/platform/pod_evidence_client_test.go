package platform

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestVCPodEvidencePreservesPreviousContainerLogOptionsAndProfile(t *testing.T) {
	calls := 0
	c := &VirtualClusterClient{profiles: map[string]clientProfile{"owner": {Name: "owner", KubernetesBaseURL: "https://compute.test", AccessKey: "owner-ak", SecretKey: "sk"}}, httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Path != "/ecp/v1/clusters/vc-test/api/v1/namespaces/ns/pods/p/log" || req.URL.Query().Get("previous") != "true" || req.URL.Query().Get("container") != "worker" || req.URL.Query().Get("tailLines") != "30" || !strings.Contains(req.Header.Get("Authorization"), "owner-ak") {
			t.Fatalf("bad signed request: %s", req.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/plain"}}, Body: io.NopCloser(strings.NewReader("previous log\n")), Request: req}, nil
	})}}
	client, err := c.NewVCPodEvidenceClient(context.Background(), "owner", "vc-test")
	if err != nil {
		t.Fatal(err)
	}
	tail := int64(30)
	logs, err := client.CoreV1().Pods("ns").GetLogs("p", &corev1.PodLogOptions{Container: "worker", Previous: true, TailLines: &tail}).DoRaw(context.Background())
	if err != nil || string(logs) != "previous log\n" || calls != 1 {
		t.Fatalf("calls=%d logs=%q err=%v", calls, logs, err)
	}
}

func TestVCPodEvidenceTransportRejectsWritesAndForeignVC(t *testing.T) {
	tpt := &podEvidenceTransport{prefix: "https://compute.test/vc/owner/"}
	for _, tc := range []struct{ method, url string }{{"POST", "https://compute.test/vc/owner/api/v1/pods"}, {"GET", "https://compute.test/vc/other/api/v1/pods"}, {"GET", "https://other.test/vc/owner/api/v1/pods"}} {
		req, _ := http.NewRequest(tc.method, tc.url, nil)
		if _, err := tpt.RoundTrip(req); err == nil {
			t.Fatal(tc)
		}
	}
}
