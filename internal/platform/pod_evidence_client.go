package platform

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// NewVCPodEvidenceClient uses the live Kubernetes proxy, rather than the
// virtualClusters resource index (which does not expose log subresources).
// The caller supplies the owning VC reference; container/previous/tail options
// and the original namespace are preserved.
func (c *VirtualClusterClient) NewVCPodEvidenceClient(ctx context.Context, profileName, vc string) (kubernetes.Interface, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	if strings.TrimSpace(vc) == "" {
		return nil, fmt.Errorf("VC is required for Pod evidence")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	endpoint := c.kubernetesClusterURLForProfile(profile, vc, "", nil)
	return kubernetes.NewForConfig(&rest.Config{Host: endpoint, Timeout: 3 * time.Second, QPS: 50, Burst: 100, Transport: &podEvidenceTransport{client: c, profile: profile, prefix: endpoint + "/"}})
}

type podEvidenceTransport struct {
	client  *VirtualClusterClient
	profile clientProfile
	prefix  string
}

func (t *podEvidenceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodGet || !strings.HasPrefix(request.URL.String(), t.prefix) {
		return nil, fmt.Errorf("Pod evidence transport only permits GET within its owning VC")
	}
	req := request.Clone(request.Context())
	headers, err := t.client.authHeadersForProfile(t.profile, req.URL.String(), req.Method)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return doPlatformRequest(t.client.httpClient, req)
}
