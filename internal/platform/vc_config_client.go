package platform

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/url"

	"k8s.io/client-go/rest"
)

// The operation API is reached through the selected HC's authenticated Service
// proxy. It owns the persistent flavor; editing a Kubernetes label does not.
const vcOperationProxy = "/api/v1/namespaces/prod-lepton/services/http:lepton-ecp-operation:8080/proxy/compute/ecp/interior/v1"

type VCControlPlaneFlavor struct {
	Name             string            `json:"name"`
	Replicas         int32             `json:"replicas"`
	ResourceRequests map[string]string `json:"resource_requests"`
	ResourceLimits   map[string]string `json:"resource_limits"`
	MaxNodeCount     int32             `json:"max_node_count"`
}

type VCOperationResource struct {
	UID                string                `json:"uid"`
	Name               string                `json:"name"`
	ControlPlaneFlavor *VCControlPlaneFlavor `json:"control_plane_flavor"`
}

type VCConfigClient struct{ client rest.Interface }

func NewVCConfigClient(client rest.Interface) *VCConfigClient {
	return &VCConfigClient{client: client}
}

func (c *VCConfigClient) Flavors(ctx context.Context) ([]VCControlPlaneFlavor, error) {
	var result struct {
		Flavors []VCControlPlaneFlavor `json:"available_control_plane_flavors"`
	}
	if err := c.get(ctx, "/virtualClusterConfig", &result); err != nil {
		return nil, err
	}
	return result.Flavors, nil
}

func (c *VCConfigClient) Get(ctx context.Context, uid string) (*VCOperationResource, error) {
	if !looksLikeClusterUUID(uid) {
		return nil, fmt.Errorf("invalid VC UID %q", uid)
	}
	var result VCOperationResource
	if err := c.get(ctx, "/virtualClusters/"+url.PathEscape(uid), &result); err != nil {
		return nil, err
	}
	if result.UID != uid {
		return nil, fmt.Errorf("VC configuration UID mismatch: requested %s, received %s", uid, result.UID)
	}
	return &result, nil
}

func (c *VCConfigClient) UpdateFlavor(ctx context.Context, uid, flavor string) (*VCOperationResource, error) {
	if !looksLikeClusterUUID(uid) {
		return nil, fmt.Errorf("invalid VC UID %q", uid)
	}
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("HC Service proxy client is unavailable")
	}
	var requestID [16]byte
	if _, err := rand.Read(requestID[:]); err != nil {
		return nil, fmt.Errorf("create VC update request ID: %w", err)
	}
	requestID[6] = (requestID[6] & 0x0f) | 0x40
	requestID[8] = (requestID[8] & 0x3f) | 0x80
	id := fmt.Sprintf("%x-%x-%x-%x-%x", requestID[:4], requestID[4:6], requestID[6:8], requestID[8:10], requestID[10:])
	// The gateway body is the virtual_cluster field, not the request envelope.
	// Only the flavor name is writable; replicas/resources are output-only.
	body, err := json.Marshal(map[string]any{"control_plane_flavor": map[string]string{"name": flavor}})
	if err != nil {
		return nil, err
	}
	raw, err := c.client.Verb("PATCH").AbsPath(vcOperationProxy+"/virtualClusters/"+url.PathEscape(uid)).
		Param("update_mask", "control_plane_flavor").Param("request_id", id).
		SetHeader("Content-Type", "application/json").Body(body).MaxRetries(0).DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("update persistent VC flavor: %w", err)
	}
	var result VCOperationResource
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode VC update response: %w", err)
	}
	if result.UID != uid {
		return nil, fmt.Errorf("VC update response UID mismatch: %s", result.UID)
	}
	return &result, nil
}

func (c *VCConfigClient) get(ctx context.Context, path string, result any) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("HC Service proxy client is unavailable")
	}
	raw, err := c.client.Get().AbsPath(vcOperationProxy + path).MaxRetries(0).DoRaw(ctx)
	if err != nil {
		return fmt.Errorf("read persistent VC configuration: %w", err)
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return fmt.Errorf("decode persistent VC configuration: %w", err)
	}
	return nil
}
