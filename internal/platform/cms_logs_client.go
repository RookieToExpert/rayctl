package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

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
