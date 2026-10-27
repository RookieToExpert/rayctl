package platform

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Keep list metadata so callers can prove that the ownership scan is complete.
func (c *VirtualClusterClient) ListSSPQueueOwnershipNodesPage(ctx context.Context, profile, vc string, options metav1.ListOptions) (*corev1.NodeList, error) {
	var result corev1.NodeList
	if err := c.getSSPQueueOwnershipPage(ctx, profile, vc, "nodes", options, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *VirtualClusterClient) ListSSPQueueOwnershipPodsPage(ctx context.Context, profile, vc string, options metav1.ListOptions) (*corev1.PodList, error) {
	var result corev1.PodList
	if err := c.getSSPQueueOwnershipPage(ctx, profile, vc, "pods", options, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *VirtualClusterClient) getSSPQueueOwnershipPage(ctx context.Context, profileName, vc, resource string, options metav1.ListOptions, out any) error {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return fmt.Errorf("platform profile %q not found", profileName)
	}
	if strings.TrimSpace(vc) == "" || vc == "-" {
		return fmt.Errorf("a specific VC is required for queue ownership lookup")
	}
	query := url.Values{}
	query.Set("limit", strconv.FormatInt(options.Limit, 10))
	if options.Continue != "" {
		query.Set("continue", options.Continue)
	}
	if options.FieldSelector != "" {
		query.Set("fieldSelector", options.FieldSelector)
	}
	reqURL := c.kubernetesResourceURLForProfile(profile, vc, "/api/v1/"+resource, query)
	return c.getJSONWithProfile(ctx, profile, reqURL, out)
}
