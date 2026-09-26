package platform

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ClusterD                      = "d"
	ClusterDCloud                 = "dcloud"
	defaultSubscriptionDCloud     = "019a575c-9a53-71ab-8028-2b0383d7a02f"
	defaultAPIBaseURL             = "https://management.d.pjlab.org.cn"
	defaultKubernetesBaseURL      = "https://compute.d.pjlab.org.cn"
	defaultIAMBaseURL             = "https://iam.d.pjlab.org.cn"
	defaultPTIAMBaseURL           = "https://iam-api.pjlab.org.cn"
	defaultMonitorBaseURL         = "https://monitor.d.pjlab.org.cn"
	defaultCloudAPIBaseURL        = "https://management-cloud.d.pjlab.org.cn"
	defaultCloudKubernetesBaseURL = "https://compute-cloud.d.pjlab.org.cn"
	defaultCloudIAMBaseURL        = "https://iam-cloud.d.pjlab.org.cn"
	defaultCloudMonitorBaseURL    = "https://monitor-cloud.d.pjlab.org.cn"
	defaultResourceGroup          = "default"
	defaultRegion                 = "cn-pj-01"
	defaultPageLimit              = 100
	defaultConfigFileName         = "platform.json"
)

type VirtualClusterClient struct {
	accessKey         string
	secretKey         string
	baseURL           string
	kubernetesBaseURL string
	iamBaseURL        string
	subscription      string
	resourceGroup     string
	region            string
	metricsBaseURL    string
	metricsUsername   string
	metricsPassword   string
	currentProfile    string
	profiles          map[string]clientProfile
	httpClient        *http.Client
}

type VirtualCluster struct {
	UID         string `json:"uid"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	TenantID    string `json:"tenant_id"`
	Region      string `json:"region"`
	State       string `json:"state"`
	ProfileName string `json:"-"`
}

type StorageVolumeResource struct {
	ID                       string                    `json:"id"`
	RID                      string                    `json:"rid"`
	Name                     string                    `json:"name"`
	DisplayName              string                    `json:"display_name"`
	Type                     string                    `json:"type"`
	ResourceType             string                    `json:"resource_type"`
	State                    string                    `json:"state"`
	Zone                     string                    `json:"zone"`
	Region                   string                    `json:"region"`
	Properties               string                    `json:"properties"`
	CreatorID                string                    `json:"creator_id"`
	OwnerID                  string                    `json:"owner_id"`
	CreateTime               string                    `json:"create_time"`
	UpdateTime               string                    `json:"update_time"`
	ResourceGroupName        string                    `json:"resource_group_name"`
	ResourceGroupDisplayName string                    `json:"resource_group_display_name"`
	RelatedResources         []StorageResourceRelation `json:"related_resources"`
	ProfileName              string                    `json:"-"`
}

type StorageResourceRelation struct {
	RelationType string                `json:"relation_type"`
	RelationRole string                `json:"relation_role"`
	Resource     StorageVolumeResource `json:"resource"`
}

type ECSVirtualMachine struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	UID         string `json:"uid"`
	DisplayName string `json:"display_name"`
	CreatorID   string `json:"creator_id"`
	State       string `json:"state"`
	ProfileName string `json:"-"`
	Properties  struct {
		Hostname           string `json:"hostname"`
		MachineType        string `json:"machine_type"`
		VirtualMachineType string `json:"virtual_machine_type"`
		ImageID            string `json:"image_id"`
		Metadata           struct {
			Items []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"items"`
		} `json:"metadata"`
		NetworkInterfaces []struct {
			Properties struct {
				IPv4Addr string `json:"ip_v4_addr"`
				VPCInfo  struct {
					UID         string `json:"uid"`
					Name        string `json:"name"`
					DisplayName string `json:"display_name"`
				} `json:"vpc_info"`
			} `json:"properties"`
		} `json:"network_interfaces"`
	} `json:"properties"`
}

type AISpace struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	UID         string `json:"uid"`
	DisplayName string `json:"display_name"`
	CreatorID   string `json:"creator_id"`
	State       string `json:"state"`
	ProfileName string `json:"-"`
	Properties  struct {
		Type                     string `json:"type"`
		ImagePath                string `json:"image_path"`
		HostIP                   string `json:"host_ip"`
		VirtualMachineProperties struct {
			MachineType       string `json:"machine_type"`
			NetworkInterfaces []struct {
				Properties struct {
					IPv4Addr string `json:"ip_v4_addr"`
					VPCInfo  struct {
						UID         string `json:"uid"`
						Name        string `json:"name"`
						DisplayName string `json:"display_name"`
					} `json:"vpc_info"`
				} `json:"properties"`
			} `json:"network_interfaces"`
		} `json:"virtual_machine_properties"`
	} `json:"properties"`
}

type virtualClusterListResponse struct {
	VirtualClusters []VirtualCluster `json:"virtual_clusters"`
	Items           []VirtualCluster `json:"items"`
	NextPageToken   string           `json:"next_page_token"`
}

type ecsVirtualMachineListResponse struct {
	VirtualMachines []ECSVirtualMachine `json:"virtual_machines"`
}

type aiSpaceListResponse struct {
	AISpaces []AISpace `json:"ai_spaces"`
}

type storageVolumePageResponse struct {
	Resources     []StorageVolumeResource `json:"resources"`
	NextPageToken string                  `json:"next_page_token"`
	TotalSize     int                     `json:"total_size"`
}

type telemetryResourceListResponse struct {
	Resources []telemetryResource `json:"resources"`
}

type telemetryResource struct {
	RID        string          `json:"rid"`
	Properties json.RawMessage `json:"properties"`
}

type queryTokenResponse struct {
	Token string `json:"token"`
}

type cmsLogQueryResponse struct {
	Logs  []cmsLogItem `json:"logs"`
	Items []cmsLogItem `json:"items"`
	Data  []cmsLogItem `json:"data"`
}

type cmsLogItem struct {
	Timestamp    string         `json:"ts"`
	ObservedTS   string         `json:"observed_ts"`
	Level        string         `json:"level"`
	Msg          string         `json:"msg"`
	WorkloadType string         `json:"workload_type"`
	WorkloadName string         `json:"workload_name"`
	Namespace    string         `json:"namespace"`
	Pod          string         `json:"pod"`
	Container    string         `json:"container"`
	Node         string         `json:"node"`
	Resource     map[string]any `json:"resource"`
}

type ECPWorkloadLogQuery struct {
	Start        time.Time
	End          time.Time
	WorkloadType string
	WorkloadName string
	Pods         []string
	Level        string
	Keyword      string
	Namespace    string
	Container    string
	Limit        int
}

type ECPWorkloadLogResult struct {
	VCluster    string
	ProfileName string
	Items       []ECPWorkloadLogItem
}

type ECPWorkloadLogItem struct {
	Timestamp    string
	ObservedTS   string
	Level        string
	WorkloadName string
	Pod          string
	Container    string
	Message      string
}

type CloudAuditQuery struct {
	Start         time.Time
	End           time.Time
	ServiceType   string
	ResourceType  string
	ResourceName  string
	OperationType string
	UserNames     []string
	Limit         int
}

type CloudAuditResult struct {
	ProfileName string
	TotalSize   int
	Items       []CloudAuditEvent
}

type CloudAuditEvent struct {
	Time          string
	ServiceType   string
	ResourceType  string
	ResourceName  string
	OperationType string
	UserName      string
	UserID        string
	Code          string
	Detail        string
}

type cloudAuditResponse struct {
	AuditEvents   []map[string]any `json:"audit_events"`
	NextPageToken string           `json:"next_page_token"`
	TotalSize     int              `json:"total_size"`
}

func (c *VirtualClusterClient) listStorageVolumeResourcesWithProfile(ctx context.Context, profile clientProfile, zone string) ([]StorageVolumeResource, error) {
	pageToken := "1"
	cursor := newPageCursor(pageToken)
	resources := make([]StorageVolumeResource, 0)
	for {
		u, _ := url.Parse(profile.BaseURL)
		u.Path = "/rmh/v1/resources:page"
		query := u.Query()
		query.Set("filter", fmt.Sprintf(`resource_type="storage.afs.v1.volume" OR resource_type="storage.afs.v2.volume"  AND zone="*%s*"`, zone))
		query.Set("page_size", "200")
		query.Set("page_token", pageToken)
		u.RawQuery = query.Encode()

		var payload storageVolumePageResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return nil, err
		}
		resources = append(resources, payload.Resources...)
		if !cursor.advance(payload.NextPageToken) {
			break
		}
		pageToken = payload.NextPageToken
	}
	return resources, nil
}

func (c *VirtualClusterClient) ListStorageVolumeResources(ctx context.Context, zone string) ([]StorageVolumeResource, error) {
	resources := make([]StorageVolumeResource, 0)
	seen := make(map[string]struct{})
	var lastErr error
	success := false
	queries := queryProfiles(ctx, c.orderedProfiles(), func(ctx context.Context, profile clientProfile) ([]StorageVolumeResource, error) {
		return c.listStorageVolumeResourcesWithProfile(ctx, profile, zone)
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, query := range queries {
		if query.err != nil {
			lastErr = query.err
			continue
		}
		success = true
		for _, item := range query.value {
			item.ProfileName = query.profile.Name
			key := firstNonEmpty(strings.TrimSpace(item.ID), strings.TrimSpace(item.RID), strings.TrimSpace(item.Name))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			resources = append(resources, item)
		}
	}
	if success {
		return resources, nil
	}
	return nil, lastErr
}

func (c *VirtualClusterClient) FindStorageVolumeResourceByUID(ctx context.Context, uid string) (*StorageVolumeResource, error) {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return nil, fmt.Errorf("storage volume uid is required")
	}

	for _, candidate := range uidSearchCandidates(uid) {
		resource, err := c.findStorageVolumeResourceByFragment(ctx, candidate)
		if err == nil && resource != nil {
			return resource, nil
		}
	}
	return nil, fmt.Errorf("storage volume resource with uid %q not found", uid)
}

func (c *VirtualClusterClient) FindStorageVolumeResource(ctx context.Context, identifier string) (*StorageVolumeResource, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("storage volume identifier is required")
	}

	if strings.HasPrefix(strings.ToLower(identifier), "afs-") {
		// AFS names are canonical resource identifiers. Do not retry the same
		// platform search through UID candidates and then by name again.
		return c.findStorageVolumeResourceByFieldFragment(ctx, "name", identifier)
	}
	if resource, err := c.FindStorageVolumeResourceByUID(ctx, identifier); err == nil && resource != nil {
		return resource, nil
	}
	return c.findStorageVolumeResourceByFieldFragment(ctx, "name", identifier)
}

func (c *VirtualClusterClient) FindResourceByName(ctx context.Context, name string, resourceKinds ...string) (*StorageVolumeResource, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("resource name is required")
	}

	kindSet := make(map[string]struct{}, len(resourceKinds))
	for _, kind := range resourceKinds {
		kind = strings.Trim(strings.TrimSpace(kind), "/")
		if kind != "" {
			kindSet[kind] = struct{}{}
		}
	}

	type profileResult struct {
		resource *StorageVolumeResource
		err      error
	}
	queryCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	profiles := c.orderedProfiles()
	resultCh := make(chan profileResult, len(profiles))
	for _, profile := range profiles {
		profile := profile
		go func() {
			profileCtx, profileCancel := context.WithTimeout(queryCtx, 2*time.Second)
			defer profileCancel()
			resource, err := c.findResourceByNameWithProfile(profileCtx, profile, name, kindSet)
			resultCh <- profileResult{resource: resource, err: err}
		}()
	}

	var lastErr error
	for range profiles {
		result := <-resultCh
		if result.resource != nil {
			cancel()
			return result.resource, nil
		}
		if result.err != nil {
			lastErr = result.err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	if len(resourceKinds) > 0 {
		return nil, fmt.Errorf("resource %q with kind %q not found", name, strings.Join(resourceKinds, ","))
	}
	return nil, fmt.Errorf("resource %q not found", name)
}

func (c *VirtualClusterClient) findResourceByNameWithProfile(ctx context.Context, profile clientProfile, name string, kindSet map[string]struct{}) (*StorageVolumeResource, error) {
	pageToken := "1"
	cursor := newPageCursor(pageToken)
	for {
		u, _ := url.Parse(profile.BaseURL)
		u.Path = "/rmh/v1/resources:page"
		query := u.Query()
		query.Set("filter", fmt.Sprintf(`name="%s"`, name))
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("page_token", pageToken)
		u.RawQuery = query.Encode()

		var payload storageVolumePageResponse
		if err := c.postJSONWithProfile(ctx, profile, u.String(), map[string]any{}, &payload); err != nil {
			return nil, err
		}

		for i := range payload.Resources {
			resource := &payload.Resources[i]
			resource.ProfileName = profile.Name
			if !strings.EqualFold(strings.TrimSpace(resource.Name), name) {
				continue
			}
			if len(kindSet) > 0 && !resourceRIDHasAnyKind(resource.RID, kindSet) {
				continue
			}
			return resource, nil
		}

		nextPageToken := strings.TrimSpace(payload.NextPageToken)
		if len(payload.Resources) == 0 || !cursor.advance(nextPageToken) {
			break
		}
		pageToken = nextPageToken
	}
	return nil, nil
}

func (c *VirtualClusterClient) FindResourceByUID(ctx context.Context, uid string, resourceKinds ...string) (*StorageVolumeResource, error) {
	return c.findResourceByUIDWithProfiles(ctx, c.orderedProfiles(), uid, resourceKinds...)
}

func (c *VirtualClusterClient) FindResourceByUIDForProfile(ctx context.Context, profileName string, uid string, resourceKinds ...string) (*StorageVolumeResource, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.findResourceByUIDWithProfiles(ctx, []clientProfile{profile}, uid, resourceKinds...)
}

func (c *VirtualClusterClient) findResourceByUIDWithProfiles(ctx context.Context, profiles []clientProfile, uid string, resourceKinds ...string) (*StorageVolumeResource, error) {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return nil, fmt.Errorf("resource uid is required")
	}

	kindSet := make(map[string]struct{}, len(resourceKinds))
	for _, kind := range resourceKinds {
		kind = strings.Trim(strings.TrimSpace(kind), "/")
		if kind != "" {
			kindSet[kind] = struct{}{}
		}
	}

	var lastErr error
	for _, profile := range profiles {
		pageToken := "1"
		cursor := newPageCursor(pageToken)
		for {
			u, _ := url.Parse(profile.BaseURL)
			u.Path = "/rmh/v1/resources:page"
			query := u.Query()
			query.Set("filter", fmt.Sprintf(`uid="*%s*"`, uid))
			query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
			query.Set("page_token", pageToken)
			u.RawQuery = query.Encode()

			var payload storageVolumePageResponse
			if err := c.postJSONWithProfile(ctx, profile, u.String(), map[string]any{}, &payload); err != nil {
				lastErr = err
				break
			}
			for i := range payload.Resources {
				resource := &payload.Resources[i]
				resource.ProfileName = profile.Name
				if !strings.EqualFold(strings.TrimSpace(resource.ID), uid) && !strings.Contains(strings.TrimSpace(resource.ID), uid) {
					continue
				}
				if len(kindSet) > 0 && !resourceRIDHasAnyKind(resource.RID, kindSet) {
					continue
				}
				return resource, nil
			}

			nextPageToken := strings.TrimSpace(payload.NextPageToken)
			if len(payload.Resources) == 0 || !cursor.advance(nextPageToken) {
				break
			}
			pageToken = nextPageToken
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	if len(resourceKinds) > 0 {
		return nil, fmt.Errorf("resource uid %q with kind %q not found", uid, strings.Join(resourceKinds, ","))
	}
	return nil, fmt.Errorf("resource uid %q not found", uid)
}

func resourceRIDHasAnyKind(rid string, kinds map[string]struct{}) bool {
	ridParts := strings.Split(strings.Trim(rid, "/"), "/")
	for _, part := range ridParts {
		if _, ok := kinds[part]; ok {
			return true
		}
	}
	return false
}

func (c *VirtualClusterClient) findStorageVolumeResourceByFragment(ctx context.Context, fragment string) (*StorageVolumeResource, error) {
	return c.findStorageVolumeResourceByFieldFragment(ctx, "uid", fragment)
}

func (c *VirtualClusterClient) findStorageVolumeResourceByFieldFragment(ctx context.Context, field string, fragment string) (*StorageVolumeResource, error) {
	fragment = strings.TrimSpace(fragment)
	if fragment == "" {
		return nil, fmt.Errorf("storage volume fragment is required")
	}

	type profileResult struct {
		resource *StorageVolumeResource
		err      error
	}
	queryCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	profiles := c.orderedProfiles()
	resultCh := make(chan profileResult, len(profiles))
	for _, profile := range profiles {
		profile := profile
		go func() {
			profileCtx, profileCancel := context.WithTimeout(queryCtx, 2*time.Second)
			defer profileCancel()
			resource, err := c.findStorageVolumeResourceByFieldFragmentWithProfile(profileCtx, profile, field, fragment)
			resultCh <- profileResult{resource: resource, err: err}
		}()
	}

	var lastErr error
	for range profiles {
		result := <-resultCh
		if result.resource != nil {
			cancel()
			return result.resource, nil
		}
		if result.err != nil {
			lastErr = result.err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("storage volume resource with fragment %q not found", fragment)
}

func (c *VirtualClusterClient) findStorageVolumeResourceByFieldFragmentWithProfile(ctx context.Context, profile clientProfile, field string, fragment string) (*StorageVolumeResource, error) {
	pageToken := "1"
	cursor := newPageCursor(pageToken)
	for {
		u, _ := url.Parse(profile.BaseURL)
		u.Path = "/rmh/v1/resources:page"
		query := u.Query()

		filter := fmt.Sprintf(`(resource_type="storage.afs.v1.volume" OR resource_type="storage.afs.v2.volume") AND %s="*%s*"`, field, fragment)
		query.Set("filter", filter)
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("page_token", pageToken)
		u.RawQuery = query.Encode()

		var payload storageVolumePageResponse
		if err := c.postJSONWithProfile(ctx, profile, u.String(), map[string]any{}, &payload); err != nil {
			return nil, err
		}

		for i := range payload.Resources {
			resource := &payload.Resources[i]
			resource.ProfileName = profile.Name
			if field == "name" {
				if strings.EqualFold(strings.TrimSpace(resource.Name), fragment) || strings.Contains(strings.TrimSpace(resource.Name), fragment) {
					return resource, nil
				}
				continue
			}
			if strings.EqualFold(strings.TrimSpace(resource.ID), fragment) || strings.Contains(strings.TrimSpace(resource.ID), fragment) {
				return resource, nil
			}
		}

		nextPageToken := strings.TrimSpace(payload.NextPageToken)
		if len(payload.Resources) == 0 || !cursor.advance(nextPageToken) {
			break
		}
		pageToken = nextPageToken
	}
	return nil, nil
}

func uidSearchCandidates(uid string) []string {
	parts := strings.Split(strings.TrimSpace(uid), "-")
	candidates := make([]string, 0, 4)
	seen := make(map[string]struct{})
	appendCandidate := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		candidates = append(candidates, value)
	}

	appendCandidate(uid)
	if len(parts) >= 3 {
		appendCandidate(strings.Join(parts[2:], "-"))
	}
	if len(parts) >= 4 {
		appendCandidate(strings.Join(parts[3:], "-"))
	}
	if len(parts) >= 2 {
		appendCandidate(strings.Join(parts[len(parts)-2:], "-"))
	}
	return candidates
}

func (c *VirtualClusterClient) GetPodLogs(ctx context.Context, vclusterName string, namespace string, podName string, tailLines int64) ([]string, error) {
	var lastErr error
	for _, profile := range c.orderedCMSProfiles() {
		clusterRef, err := c.cmsClusterRefForProfile(ctx, profile, vclusterName)
		if err != nil {
			lastErr = err
			continue
		}
		lines, err := c.queryPodLogsWithCMSProfile(ctx, profile, clusterRef, namespace, podName, tailLines)
		if err != nil {
			lastErr = err
			continue
		}
		return lines, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no cms profile available for pod logs")
}

func (c *VirtualClusterClient) queryPodLogsWithCMSProfile(ctx context.Context, profile clientProfile, vclusterName string, namespace string, podName string, tailLines int64) ([]string, error) {
	queryToken, err := c.cmsQueryTokenForProfile(ctx, profile, vclusterName)
	if err != nil {
		return nil, err
	}

	reqURL := c.cmsLogQueryURL(profile, namespace, podName, tailLines)
	var payload cmsLogQueryResponse
	body, err := c.doRequestWithCMSProfile(ctx, profile, http.MethodGet, reqURL, "", nil, queryToken)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode cms log response: %w", err)
	}

	lines := make([]string, 0, len(payload.Logs))
	for _, item := range payload.Logs {
		pod := emptyDash(fmt.Sprintf("%v", item.Resource["k8s.pod.name"]))
		container := emptyDash(fmt.Sprintf("%v", item.Resource["k8s.container.name"]))
		level := emptyDash(item.Level)
		msg := emptyDash(item.Msg)
		ts := emptyDash(item.ObservedTS)
		lines = append(lines, fmt.Sprintf("[%s] [%s] [%s/%s] %s", ts, level, pod, container, msg))
	}
	if len(lines) == 0 {
		return []string{"<empty log output>"}, nil
	}
	return lines, nil
}

func (c *VirtualClusterClient) QueryECPWorkloadLogs(ctx context.Context, vclusterName string, query ECPWorkloadLogQuery) (*ECPWorkloadLogResult, error) {
	var lastErr error
	for _, profile := range c.orderedCMSProfiles() {
		clusterRef, err := c.cmsClusterRefForProfile(ctx, profile, vclusterName)
		if err != nil {
			lastErr = err
			continue
		}
		result, err := c.queryECPWorkloadLogsWithCMSProfile(ctx, profile, clusterRef, query)
		if err != nil {
			lastErr = err
			continue
		}
		result.VCluster = strings.TrimSpace(vclusterName)
		return result, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no cms profile available for ecp workload logs")
}

func (c *VirtualClusterClient) QueryCloudAuditEvents(ctx context.Context, query CloudAuditQuery, bearerToken string) (*CloudAuditResult, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("current platform profile is unavailable")
	}
	if strings.TrimSpace(bearerToken) == "" {
		return nil, fmt.Errorf("console bearer token is required; run rayctl auth login first")
	}

	reqURL := cloudAuditQueryURL(profile, query)
	var payload cloudAuditResponse
	if err := c.getJSONWithBearerProfile(ctx, profile, reqURL, bearerToken, &payload); err != nil {
		return nil, err
	}

	items := make([]CloudAuditEvent, 0, len(payload.AuditEvents))
	for _, item := range payload.AuditEvents {
		items = append(items, CloudAuditEvent{
			Time:          stringFromNested(item, "time", "event_time", "create_time"),
			ServiceType:   stringFromNested(item, "service_type"),
			ResourceType:  stringFromNested(item, "resource_type"),
			ResourceName:  stringFromNested(item, "resource_name", "resource.name", "object_name"),
			OperationType: stringFromNested(item, "operation_type"),
			UserName:      stringFromNested(item, "user_name", "username"),
			UserID:        stringFromNested(item, "user_id"),
			Code:          stringFromNested(item, "code"),
			Detail:        cloudAuditDetail(item["detail"]),
		})
	}

	return &CloudAuditResult{
		ProfileName: profile.Name,
		TotalSize:   payload.TotalSize,
		Items:       items,
	}, nil
}

func (c *VirtualClusterClient) queryECPWorkloadLogsWithCMSProfile(ctx context.Context, profile clientProfile, vclusterName string, query ECPWorkloadLogQuery) (*ECPWorkloadLogResult, error) {
	queryToken, err := c.cmsQueryTokenForProfile(ctx, profile, vclusterName)
	if err != nil {
		return nil, err
	}

	reqURL := c.cmsWorkloadLogQueryURL(profile, query)
	var payload cmsLogQueryResponse
	body, err := c.doRequestWithCMSProfile(ctx, profile, http.MethodGet, reqURL, "", nil, queryToken)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode cms workload log response: %w", err)
	}

	rawItems := payload.Logs
	if len(rawItems) == 0 {
		rawItems = payload.Items
	}
	if len(rawItems) == 0 {
		rawItems = payload.Data
	}

	items := make([]ECPWorkloadLogItem, 0, len(rawItems))
	for _, item := range rawItems {
		items = append(items, ECPWorkloadLogItem{
			Timestamp:    strings.TrimSpace(item.Timestamp),
			ObservedTS:   strings.TrimSpace(item.ObservedTS),
			Level:        strings.TrimSpace(item.Level),
			WorkloadName: firstNonEmpty(strings.TrimSpace(item.WorkloadName), stringFromAny(item.Resource["workload_name"])),
			Pod:          firstNonEmpty(strings.TrimSpace(item.Pod), stringFromAny(item.Resource["k8s.pod.name"]), stringFromAny(item.Resource["pod"])),
			Container:    firstNonEmpty(strings.TrimSpace(item.Container), stringFromAny(item.Resource["k8s.container.name"]), stringFromAny(item.Resource["container"])),
			Message:      strings.TrimSpace(item.Msg),
		})
	}

	return &ECPWorkloadLogResult{
		VCluster:    vclusterName,
		ProfileName: profile.Name,
		Items:       items,
	}, nil
}

func (c *VirtualClusterClient) cmsQueryTokenForProfile(ctx context.Context, profile clientProfile, vclusterName string) (string, error) {
	telemetryRID, err := c.getPrivateTelemetryStationRID(ctx, profile)
	if err != nil {
		return "", err
	}
	return c.createCMSQueryToken(ctx, profile, telemetryRID, virtualClusterUID(vclusterName))
}

func (c *VirtualClusterClient) cmsClusterRefForProfile(ctx context.Context, profile clientProfile, vclusterName string) (string, error) {
	ref := strings.TrimSpace(vclusterName)
	if ref == "" {
		return "", fmt.Errorf("virtual cluster identifier is required")
	}

	items, err := c.listVirtualClustersForProfile(ctx, profile)
	if err == nil {
		for _, item := range items {
			uid := strings.TrimSpace(item.UID)
			if uid == "" {
				continue
			}
			if ref == uid || ref == "vc-"+uid || ref == strings.TrimSpace(item.Name) || ref == strings.TrimSpace(item.DisplayName) {
				return "vc-" + uid, nil
			}
		}
	}

	if strings.HasPrefix(ref, "vc-") {
		return ref, err
	}
	if looksLikeClusterUUID(ref) {
		return "vc-" + ref, err
	}
	return ref, err
}

func looksLikeClusterUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, ch := range value {
		switch i {
		case 8, 13, 18, 23:
			if ch != '-' {
				return false
			}
		default:
			if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') && (ch < 'A' || ch > 'F') {
				return false
			}
		}
	}
	return true
}

func (c *VirtualClusterClient) getPrivateTelemetryStationRID(ctx context.Context, profile clientProfile) (string, error) {
	u, _ := url.Parse(profile.BaseURL)
	u.Path = "/rmh/v1/resources"
	query := u.Query()
	query.Set("filter", `resource_type="monitor.ts.v1.telemetryStation"`)
	u.RawQuery = query.Encode()

	var payload telemetryResourceListResponse
	if err := c.getJSONWithCMSProfile(ctx, profile, u.String(), &payload); err != nil {
		return "", err
	}
	for _, resource := range payload.Resources {
		if strings.TrimSpace(resource.RID) == "" {
			continue
		}
		props := parseTelemetryProperties(resource.Properties)
		if strings.EqualFold(strings.TrimSpace(fmt.Sprintf("%v", props["station_type"])), "PRIVATE") {
			return strings.TrimSpace(resource.RID), nil
		}
	}
	return "", fmt.Errorf("private telemetryStation rid not found")
}

func parseTelemetryProperties(raw json.RawMessage) map[string]any {
	props := map[string]any{}
	if len(raw) == 0 {
		return props
	}
	if err := json.Unmarshal(raw, &props); err == nil {
		return props
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return props
	}
	_ = json.Unmarshal([]byte(encoded), &props)
	return props
}

func (c *VirtualClusterClient) createCMSQueryToken(ctx context.Context, profile clientProfile, telemetryRID string, vcUUID string) (string, error) {
	if strings.TrimSpace(telemetryRID) == "" {
		return "", fmt.Errorf("telemetry station rid is required")
	}
	if strings.TrimSpace(vcUUID) == "" {
		return "", fmt.Errorf("virtual cluster uuid is required")
	}

	u, _ := url.Parse(profile.MonitorBaseURL)
	u.Path = fmt.Sprintf("/monitor/ts/data/v1%s/query/token", telemetryRID)
	body := map[string]any{
		"expire_time":   time.Now().UTC().Add(30 * 24 * time.Hour).Format("2006-01-02T15:04:05Z"),
		"scope":         "RESOURCE",
		"resource_type": "compute.ecp.v1.virtualCluster",
		"resource_ids":  []string{vcUUID},
	}

	var payload queryTokenResponse
	if err := c.postJSONWithCMSProfile(ctx, profile, u.String(), body, &payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.Token) == "" {
		return "", fmt.Errorf("cms query token is empty")
	}
	return strings.TrimSpace(payload.Token), nil
}

func (c *VirtualClusterClient) postJSONWithCMSProfile(ctx context.Context, profile clientProfile, reqURL string, payload any, out any) error {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request for %s: %w", reqURL, err)
	}
	body, err := c.doRequestWithCMSProfile(ctx, profile, http.MethodPost, reqURL, "application/json", bodyBytes)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	return nil
}

func (c *VirtualClusterClient) cmsLogQueryURL(profile clientProfile, namespace string, podName string, tailLines int64) string {
	u, _ := url.Parse(profile.MonitorBaseURL)
	u.Path = "/query/v1/logs"
	query := u.Query()
	query.Set("model_name", "logs.compute.ecp.v1.virtualCluster.logs")
	for _, dimension := range []string{"resource_type", "resource_id", "id", "ts", "observed_ts", "workload_type", "workload_name", "namespace", "pod", "container", "node", "level", "msg"} {
		query.Add("dimensions", dimension)
	}
	query.Set("filter", fmt.Sprintf(`namespace="%s" AND pod="%s"`, strings.TrimSpace(namespace), strings.TrimSpace(podName)))
	query.Set("time_dimension.dimension", "observed_ts")
	query.Set("start", time.Now().UTC().Add(-7*24*time.Hour).Format(time.RFC3339))
	query.Set("end", time.Now().UTC().Add(5*time.Minute).Format(time.RFC3339))
	if tailLines <= 0 {
		tailLines = 10
	}
	query.Set("page_size", fmt.Sprintf("%d", tailLines))
	query.Set("skip", "0")
	query.Set("order_by", "observed_ts desc")
	u.RawQuery = query.Encode()
	return u.String()
}

func (c *VirtualClusterClient) cmsWorkloadLogQueryURL(profile clientProfile, query ECPWorkloadLogQuery) string {
	u, _ := url.Parse(profile.MonitorBaseURL)
	u.Path = "/query/v1/logs"

	values := u.Query()
	values.Set("model_name", "logs.compute.ecp.v1.virtualCluster.logs")
	for _, dimension := range []string{"resource_type", "resource_id", "id", "ts", "observed_ts", "workload_type", "workload_name", "namespace", "pod", "container", "node", "level", "msg"} {
		values.Add("dimensions", dimension)
	}
	values.Set("filter", buildECPWorkloadLogFilter(query))
	values.Set("time_dimension.dimension", "observed_ts")
	values.Set("start", formatMonitorTime(query.Start))
	values.Set("end", formatMonitorTime(query.End))
	if query.Limit <= 0 {
		query.Limit = 40
	}
	values.Set("page_size", fmt.Sprintf("%d", query.Limit))
	values.Set("skip", "0")
	values.Set("order_by", "ts desc,observed_ts desc")
	u.RawQuery = values.Encode()
	return u.String()
}

func cloudAuditQueryURL(profile clientProfile, query CloudAuditQuery) string {
	u, _ := url.Parse(trailBaseURLFromIAMBaseURL(profile.IAMBaseURL))
	u.Path = "/cts/data/v1/auditevents"

	values := u.Query()
	values.Set("trail_type", "res")
	values.Set("page_token", "1")
	if query.Limit <= 0 {
		query.Limit = 40
	}
	values.Set("page_size", strconv.Itoa(query.Limit))
	values.Set("filter", buildCloudAuditFilter(query))
	u.RawQuery = values.Encode()
	return u.String()
}

func buildCloudAuditFilter(query CloudAuditQuery) string {
	location := time.FixedZone("UTC+8", 8*60*60)
	parts := []string{
		fmt.Sprintf("time>='%s'", query.Start.In(location).Format(time.RFC3339)),
		fmt.Sprintf("time<='%s'", query.End.In(location).Format(time.RFC3339)),
	}
	if value := strings.TrimSpace(query.ServiceType); value != "" {
		parts = append(parts, fmt.Sprintf("service_type='%s'", escapeTrailFilterValue(value)))
	}
	if value := strings.TrimSpace(query.ResourceType); value != "" {
		parts = append(parts, fmt.Sprintf("resource_type='%s'", escapeTrailFilterValue(value)))
	}
	if value := strings.TrimSpace(query.ResourceName); value != "" {
		parts = append(parts, fmt.Sprintf("resource_name='%s'", escapeTrailFilterValue(value)))
	}
	if value := strings.TrimSpace(query.OperationType); value != "" {
		parts = append(parts, fmt.Sprintf("operation_type='%s'", escapeTrailFilterValue(value)))
	}
	userParts := make([]string, 0, len(query.UserNames))
	seenUsers := make(map[string]struct{}, len(query.UserNames))
	for _, userName := range query.UserNames {
		value := strings.TrimSpace(userName)
		if value == "" {
			continue
		}
		if _, ok := seenUsers[value]; ok {
			continue
		}
		seenUsers[value] = struct{}{}
		userParts = append(userParts, fmt.Sprintf("user_name='%s'", escapeTrailFilterValue(value)))
	}
	if len(userParts) > 0 {
		parts = append(parts, "("+strings.Join(userParts, " OR ")+")")
	}
	return strings.Join(parts, " AND ")
}

func escapeTrailFilterValue(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "'", "''")
}

func trailBaseURLFromIAMBaseURL(iamBaseURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(iamBaseURL))
	if err != nil || strings.TrimSpace(parsed.Host) == "" {
		return "https://trail.d.pjlab.org.cn"
	}
	switch {
	case strings.HasPrefix(parsed.Host, "iam."):
		parsed.Host = "trail." + strings.TrimPrefix(parsed.Host, "iam.")
	case strings.HasPrefix(parsed.Host, "iam-"):
		parsed.Host = "trail-" + strings.TrimPrefix(parsed.Host, "iam-")
	default:
		parsed.Host = "trail." + parsed.Host
	}
	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	return strings.TrimRight(parsed.String(), "/")
}

func cloudAuditDetail(value any) string {
	if text := stringFromAny(value); text != "" {
		return text
	}
	if value == nil {
		return ""
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(encoded))
}

func buildECPWorkloadLogFilter(query ECPWorkloadLogQuery) string {
	parts := []string{`resource_type="compute.ecp.v1.virtualCluster"`}
	if value := strings.TrimSpace(query.Keyword); value != "" {
		parts = append(parts, fmt.Sprintf(`matchPhrase(msg,"%s")`, escapeMonitorFilterValue(value)))
	}
	if value := strings.TrimSpace(query.WorkloadType); value != "" {
		parts = append(parts, fmt.Sprintf(`workload_type="%s"`, escapeMonitorFilterValue(value)))
	}
	if value := strings.TrimSpace(query.WorkloadName); value != "" {
		parts = append(parts, fmt.Sprintf(`workload_name="%s"`, escapeMonitorFilterValue(value)))
	}
	if value := strings.TrimSpace(query.Namespace); value != "" {
		parts = append(parts, fmt.Sprintf(`namespace="%s"`, escapeMonitorFilterValue(value)))
	}
	if value := strings.TrimSpace(query.Container); value != "" {
		parts = append(parts, fmt.Sprintf(`container="%s"`, escapeMonitorFilterValue(value)))
	}
	if value := strings.TrimSpace(query.Level); value != "" {
		parts = append(parts, fmt.Sprintf(`level="%s"`, escapeMonitorFilterValue(value)))
	}

	podParts := make([]string, 0, len(query.Pods))
	for _, pod := range query.Pods {
		if value := strings.TrimSpace(pod); value != "" {
			podParts = append(podParts, fmt.Sprintf(`pod="%s"`, escapeMonitorFilterValue(value)))
		}
	}
	if len(podParts) == 1 {
		parts = append(parts, podParts[0])
	} else if len(podParts) > 1 {
		parts = append(parts, "("+strings.Join(podParts, " OR ")+")")
	}

	return strings.Join(parts, " AND ")
}

func escapeMonitorFilterValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return value
}

func formatMonitorTime(value time.Time) string {
	if value.IsZero() {
		value = time.Now()
	}
	return value.UTC().Format("2006-01-02T15:04:05.000Z")
}

func virtualClusterUID(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "vc-") {
		return strings.TrimSpace(strings.TrimPrefix(value, "vc-"))
	}
	return value
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	switch value.(type) {
	case map[string]any, []any:
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func stringFromNested(item map[string]any, keys ...string) string {
	if len(item) == 0 {
		return ""
	}

	roots := []map[string]any{item}
	for _, key := range []string{"attributes", "resource", "fields", "labels", "dimensions", "data"} {
		if nested, ok := item[key].(map[string]any); ok {
			roots = append(roots, nested)
		}
	}

	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		for _, root := range roots {
			if value := stringAtPath(root, key); value != "" {
				return value
			}
		}
		for _, root := range roots {
			if value := findNestedString(root, key); value != "" {
				return value
			}
		}
	}
	return ""
}

func stringAtPath(root map[string]any, path string) string {
	parts := strings.Split(path, ".")
	var current any = root
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return ""
		}
		currentMap, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = currentMap[part]
		if !ok {
			return ""
		}
	}
	return stringFromAny(current)
}

func findNestedString(root map[string]any, key string) string {
	if value := stringFromAny(root[key]); value != "" {
		return value
	}
	for _, value := range root {
		nested, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if found := findNestedString(nested, key); found != "" {
			return found
		}
	}
	return ""
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func (c *VirtualClusterClient) authHeadersForProfile(profile clientProfile, reqURL string, method string) (map[string]string, error) {
	return authHeadersForCredentials(profile.AccessKey, profile.SecretKey, reqURL, method)
}

func (c *VirtualClusterClient) authHeadersForCMSProfile(profile clientProfile, reqURL string, method string) (map[string]string, error) {
	return authHeadersForCredentials(profile.CMSAccessKey, profile.CMSSecretKey, reqURL, method)
}

func authHeadersForCredentials(accessKey string, secretKey string, reqURL string, method string) (map[string]string, error) {
	parsedURL, err := url.Parse(reqURL)
	if err != nil {
		return nil, fmt.Errorf("parse auth url: %w", err)
	}

	dateHeader := time.Now().UTC().Format(http.TimeFormat)
	requestTarget := parsedURL.Path
	if parsedURL.RawQuery != "" {
		requestTarget += "?" + parsedURL.RawQuery
	}

	signContent := fmt.Sprintf("date: %s\nhost: %s\n@request-target: %s %s", dateHeader, parsedURL.Host, strings.ToLower(method), requestTarget)

	mac := hmac.New(sha256.New, []byte(secretKey))
	_, _ = mac.Write([]byte(signContent))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return map[string]string{
		"Date":          dateHeader,
		"Host":          parsedURL.Host,
		"Accept":        "application/json",
		"Authorization": fmt.Sprintf(`hmac accesskey="%s", algorithm="hmac-sha256", headers="date host @request-target", signature="%s"`, accessKey, signature),
	}, nil
}

func (c *VirtualClusterClient) getJSONWithProfile(ctx context.Context, profile clientProfile, reqURL string, out any) error {
	return c.getJSONWithAuth(ctx, profile, reqURL, false, "", out)
}

func (c *VirtualClusterClient) postJSONWithProfile(ctx context.Context, profile clientProfile, reqURL string, payload any, out any) error {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request for %s: %w", reqURL, err)
	}

	body, err := c.doRequestWithProfile(ctx, profile, http.MethodPost, reqURL, "application/json", bodyBytes)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	return nil
}

func (c *VirtualClusterClient) postJSONWithBearerProfile(ctx context.Context, profile clientProfile, reqURL string, payload any, bearerToken string, out any) error {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request for %s: %w", reqURL, err)
	}

	body, err := c.doSignedRequest(ctx, profile, http.MethodPost, reqURL, "application/json", bodyBytes, false, bearerToken)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	return nil
}

func (c *VirtualClusterClient) deleteWithBearerProfile(ctx context.Context, profile clientProfile, reqURL string, bearerToken string) error {
	_, err := c.doSignedRequest(ctx, profile, http.MethodDelete, reqURL, "", nil, false, bearerToken)
	return err
}

func (c *VirtualClusterClient) getTextWithProfile(ctx context.Context, profile clientProfile, reqURL string) (string, error) {
	body, err := c.doRequestWithProfile(ctx, profile, http.MethodGet, reqURL, "", nil)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *VirtualClusterClient) getJSONWithCMSProfile(ctx context.Context, profile clientProfile, reqURL string, out any) error {
	return c.getJSONWithAuth(ctx, profile, reqURL, true, "", out)
}

func (c *VirtualClusterClient) getJSONWithCMSProfileToken(ctx context.Context, profile clientProfile, reqURL string, bearerToken string, out any) error {
	return c.getJSONWithAuth(ctx, profile, reqURL, true, bearerToken, out)
}

func (c *VirtualClusterClient) getJSONWithBearerProfile(ctx context.Context, profile clientProfile, reqURL string, bearerToken string, out any) error {
	return c.getJSONWithAuth(ctx, profile, reqURL, false, bearerToken, out)
}

func (c *VirtualClusterClient) getJSONWithAuth(ctx context.Context, profile clientProfile, reqURL string, useCMS bool, bearerToken string, out any) error {
	body, err := c.doSignedRequest(ctx, profile, http.MethodGet, reqURL, "", nil, useCMS, bearerToken)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", reqURL, err)
	}
	return nil
}

func (c *VirtualClusterClient) getTextWithCMSProfile(ctx context.Context, profile clientProfile, reqURL string, bearerToken string) (string, error) {
	body, err := c.doRequestWithCMSProfile(ctx, profile, http.MethodGet, reqURL, "", nil, bearerToken)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *VirtualClusterClient) doRequestWithProfile(ctx context.Context, profile clientProfile, method string, reqURL string, contentType string, requestBody []byte) ([]byte, error) {
	return c.doSignedRequest(ctx, profile, method, reqURL, contentType, requestBody, false, "")
}

func (c *VirtualClusterClient) doRequestWithCMSProfile(ctx context.Context, profile clientProfile, method string, reqURL string, contentType string, requestBody []byte, bearerToken ...string) ([]byte, error) {
	token := ""
	if len(bearerToken) > 0 {
		token = bearerToken[0]
	}
	return c.doSignedRequest(ctx, profile, method, reqURL, contentType, requestBody, true, token)
}

func (c *VirtualClusterClient) doSignedRequest(ctx context.Context, profile clientProfile, method string, reqURL string, contentType string, requestBody []byte, useCMS bool, bearerToken string, extraHeaders ...map[string]string) ([]byte, error) {
	var bodyReader io.Reader
	if len(requestBody) > 0 {
		bodyReader = bytes.NewReader(requestBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	headers := map[string]string{}
	if strings.TrimSpace(bearerToken) != "" {
		headers["Accept"] = "application/json"
		headers["Authorization"] = "Bearer " + strings.TrimSpace(bearerToken)
	} else {
		var err error
		if useCMS {
			headers, err = c.authHeadersForCMSProfile(profile, reqURL, method)
		} else {
			headers, err = c.authHeadersForProfile(profile, reqURL, method)
		}
		if err != nil {
			return nil, err
		}
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	for _, extra := range extraHeaders {
		for key, value := range extra {
			req.Header.Set(key, value)
		}
	}
	if strings.TrimSpace(contentType) != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := doPlatformRequest(c.httpClient, req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", reqURL, err)
	}

	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read response from %s: %w", reqURL, readErr)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("request %s returned %d: %s", reqURL, resp.StatusCode, responseBodyExcerpt(body))
	}
	return body, nil
}

func responseBodyExcerpt(body []byte) string {
	const limit = 512
	if len(body) > limit {
		prefix := body[:limit]
		for len(prefix) > 0 && !utf8.Valid(prefix) {
			prefix = prefix[:len(prefix)-1]
		}
		return strings.TrimSpace(string(prefix)) + "..."
	}
	return strings.TrimSpace(string(body))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
