package platform

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// SSPQueueQuota is the queue's quota, not a sum of its VC's node capacity.
type SSPQueueQuota struct {
	Resources    []SSPResourceSummary       `json:"resources"`
	MachineTypes []SSPQueueMachineTypeQuota `json:"machine_types"`
}

type SSPQueueMachineTypeQuota struct {
	MachineType string               `json:"machine_type"`
	Resources   []SSPResourceSummary `json:"resources"`
}

// GetSSPQueueQuota reads the same queue detail fields as the resource page.
// The /summary endpoint's NODE allocation can be zero for an ELASTIC queue
// even when machine_type_allocation and properties.quota report used nodes.
func (c *VirtualClusterClient) GetSSPQueueQuota(ctx context.Context, profileName, subscription, resourceGroup, region, cluster, queue string) (*SSPQueueQuota, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	subscription = firstNonEmpty(strings.TrimSpace(subscription), profile.Subscription)
	resourceGroup = firstNonEmpty(strings.TrimSpace(resourceGroup), profile.ResourceGroup, defaultResourceGroup)
	region = firstNonEmpty(strings.TrimSpace(region), profile.Region)
	cluster, queue = strings.TrimSpace(cluster), strings.TrimSpace(queue)
	if subscription == "" || region == "" || cluster == "" || queue == "" {
		return nil, fmt.Errorf("subscription, region, cluster and queue are required for queue quota lookup")
	}
	endpoint, err := url.Parse(profile.BaseURL)
	if err != nil {
		return nil, err
	}
	endpoint.Path = fmt.Sprintf("/compute/ssp/v1/subscriptions/%s/resourceGroups/%s/regions/%s/clusters/%s/queues/%s",
		url.PathEscape(subscription), url.PathEscape(resourceGroup), url.PathEscape(region), url.PathEscape(cluster), url.PathEscape(queue))
	var payload struct {
		Properties struct {
			Quota                 []SSPResourceSummary `json:"quota"`
			MachineTypeAllocation []struct {
				MachineType string `json:"machine_type"`
				QueueQuota  struct {
					NodeMaxNum    *int                 `json:"node_max_num"`
					NodeAllocated *int                 `json:"node_allocated"`
					ResourceUsage []SSPResourceSummary `json:"resource_usage"`
				} `json:"queue_quota"`
			} `json:"machine_type_allocation"`
		} `json:"properties"`
	}
	if err := c.getJSONWithProfile(ctx, profile, endpoint.String(), &payload); err != nil {
		return nil, err
	}
	result := &SSPQueueQuota{Resources: payload.Properties.Quota}
	for _, allocation := range payload.Properties.MachineTypeAllocation {
		resources := make([]SSPResourceSummary, 0, len(allocation.QueueQuota.ResourceUsage)+1)
		if total, allocated := allocation.QueueQuota.NodeMaxNum, allocation.QueueQuota.NodeAllocated; total != nil && allocated != nil {
			resources = append(resources, SSPResourceSummary{
				ResourceType: "NODE", Unit: "node", Total: strconv.Itoa(*total),
				Allocated: strconv.Itoa(*allocated), Unallocated: strconv.Itoa(*total - *allocated),
			})
		}
		resources = append(resources, allocation.QueueQuota.ResourceUsage...)
		if len(resources) > 0 {
			result.MachineTypes = append(result.MachineTypes, SSPQueueMachineTypeQuota{MachineType: allocation.MachineType, Resources: resources})
		}
	}
	if len(result.Resources) == 0 && len(result.MachineTypes) == 0 {
		return nil, fmt.Errorf("queue %q has no quota data", queue)
	}
	return result, nil
}
