package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"rayctl/internal/platform"
)

type LogsService struct {
	vcClient *platform.VirtualClusterClient
}

type ECPWorkloadLogOptions struct {
	VCluster     string
	Since        time.Duration
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
	VCluster     string
	ProfileName  string
	WorkloadType string
	WorkloadName string
	Since        time.Duration
	Start        time.Time
	End          time.Time
	Items        []ECPWorkloadLogItem
}

type ECPWorkloadLogItem struct {
	Time         string
	Level        string
	WorkloadName string
	Pod          string
	Container    string
	Message      string
}

type CloudAuditLogOptions struct {
	Since        string
	Start        string
	End          string
	ServiceType  string
	ResourceType string
	Limit        int
	BearerToken  string
}

type CloudAuditLogResult struct {
	ProfileName  string
	ServiceType  string
	ResourceType string
	Start        time.Time
	End          time.Time
	TotalSize    int
	Items        []CloudAuditLogItem
}

type CloudAuditLogItem struct {
	Time          string
	ServiceType   string
	ResourceType  string
	ResourceName  string
	OperationType string
	UserName      string
	UserID        string
}

func NewLogsService(vcClient *platform.VirtualClusterClient) *LogsService {
	return &LogsService{vcClient: vcClient}
}

func (s *LogsService) GetECPWorkloadLogs(ctx context.Context, opts ECPWorkloadLogOptions) (*ECPWorkloadLogResult, error) {
	if s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required")
	}
	opts.VCluster = strings.TrimSpace(opts.VCluster)
	if opts.VCluster == "" {
		return nil, fmt.Errorf("vc name is required")
	}
	if opts.Since <= 0 {
		opts.Since = 24 * time.Hour
	}
	if opts.Limit <= 0 {
		opts.Limit = 40
	}

	end := time.Now()
	start := end.Add(-opts.Since)
	workloadType := normalizeECPWorkloadType(opts.WorkloadType)
	level := strings.ToUpper(strings.TrimSpace(opts.Level))

	query := platform.ECPWorkloadLogQuery{
		Start:        start,
		End:          end,
		WorkloadType: workloadType,
		WorkloadName: strings.TrimSpace(opts.WorkloadName),
		Pods:         trimNonEmpty(opts.Pods),
		Level:        level,
		Keyword:      strings.TrimSpace(opts.Keyword),
		Namespace:    strings.TrimSpace(opts.Namespace),
		Container:    strings.TrimSpace(opts.Container),
		Limit:        opts.Limit,
	}

	platformResult, err := s.vcClient.QueryECPWorkloadLogs(ctx, opts.VCluster, query)
	if err != nil {
		return nil, err
	}

	items := make([]ECPWorkloadLogItem, 0, len(platformResult.Items))
	for _, item := range platformResult.Items {
		items = append(items, ECPWorkloadLogItem{
			Time:         formatLogTimestamp(item.ObservedTS, item.Timestamp),
			Level:        emptyDashService(item.Level),
			WorkloadName: emptyDashService(item.WorkloadName),
			Pod:          emptyDashService(item.Pod),
			Container:    emptyDashService(item.Container),
			Message:      emptyDashService(item.Message),
		})
	}

	return &ECPWorkloadLogResult{
		VCluster:     firstNonEmptyService(platformResult.VCluster, opts.VCluster),
		ProfileName:  platformResult.ProfileName,
		WorkloadType: workloadType,
		WorkloadName: strings.TrimSpace(opts.WorkloadName),
		Since:        opts.Since,
		Start:        start,
		End:          end,
		Items:        items,
	}, nil
}

func (s *LogsService) GetCloudAuditLogs(ctx context.Context, opts CloudAuditLogOptions) (*CloudAuditLogResult, error) {
	if s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required")
	}
	if opts.Limit <= 0 {
		opts.Limit = 40
	}

	start, end, err := cloudAuditTimeRange(opts.Since, opts.Start, opts.End)
	if err != nil {
		return nil, err
	}
	serviceType := strings.ToUpper(strings.TrimSpace(opts.ServiceType))
	resourceType := normalizeCloudAuditResourceType(serviceType, opts.ResourceType)
	query := platform.CloudAuditQuery{
		Start:        start,
		End:          end,
		ServiceType:  serviceType,
		ResourceType: resourceType,
		Limit:        opts.Limit,
	}

	platformResult, err := s.vcClient.QueryCloudAuditEvents(ctx, query, opts.BearerToken)
	if err != nil {
		return nil, err
	}

	userIDs := make([]string, 0)
	for _, item := range platformResult.Items {
		_, userID := cloudAuditUserIdentity(item.UserName, item.UserID, nil)
		if isUUIDValue(userID) {
			userIDs = append(userIDs, userID)
		}
	}
	resolvedUsernames, resolveErr := s.vcClient.ResolveUsernames(ctx, userIDs)
	if resolveErr != nil {
		// Username enrichment is best-effort; audit events remain useful with raw user IDs.
		resolvedUsernames = nil
	}

	items := make([]CloudAuditLogItem, 0, len(platformResult.Items))
	for _, item := range platformResult.Items {
		userName, userID := cloudAuditUserIdentity(item.UserName, item.UserID, resolvedUsernames)
		items = append(items, CloudAuditLogItem{
			Time:          formatLogTimestamp(item.Time),
			ServiceType:   emptyDashService(item.ServiceType),
			ResourceType:  emptyDashService(item.ResourceType),
			ResourceName:  emptyDashService(item.ResourceName),
			OperationType: emptyDashService(item.OperationType),
			UserName:      emptyDashService(userName),
			UserID:        emptyDashService(userID),
		})
	}

	return &CloudAuditLogResult{
		ProfileName:  platformResult.ProfileName,
		ServiceType:  serviceType,
		ResourceType: resourceType,
		Start:        start,
		End:          end,
		TotalSize:    platformResult.TotalSize,
		Items:        items,
	}, nil
}

func cloudAuditUserIdentity(userName string, userID string, resolved map[string]string) (string, string) {
	userName = strings.TrimSpace(userName)
	userID = strings.TrimSpace(userID)
	if isUUIDValue(userName) {
		if userID == "" {
			userID = userName
		}
		userName = ""
	}
	if userName == "" && isUUIDValue(userID) {
		userName = strings.TrimSpace(resolved[userID])
	}
	if userName == "" {
		userName = userID
	}
	return userName, userID
}

func isUUIDValue(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 36 {
		return false
	}
	for index, char := range value {
		switch index {
		case 8, 13, 18, 23:
			if char != '-' {
				return false
			}
		default:
			if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
				return false
			}
		}
	}
	return true
}

func cloudAuditTimeRange(sinceValue string, startValue string, endValue string) (time.Time, time.Time, error) {
	now := time.Now()
	startValue = strings.TrimSpace(startValue)
	endValue = strings.TrimSpace(endValue)
	if startValue == "" && endValue == "" {
		since, err := time.ParseDuration(strings.TrimSpace(sinceValue))
		if err != nil || since <= 0 {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --since %q, examples: 30m, 2h, 24h", sinceValue)
		}
		return now.Add(-since), now, nil
	}

	end := now
	var err error
	if endValue != "" {
		end, err = parseCloudAuditTime(endValue)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --end %q: %w", endValue, err)
		}
	}
	if startValue == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("--start is required when --end is set")
	}
	start, err := parseCloudAuditTime(startValue)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid --start %q: %w", startValue, err)
	}
	if !start.Before(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("--start must be earlier than --end")
	}
	return start, end, nil
}

func parseCloudAuditTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		var parsed time.Time
		var err error
		if layout == time.RFC3339Nano {
			parsed, err = time.Parse(layout, value)
		} else {
			parsed, err = time.ParseInLocation(layout, value, time.FixedZone("UTC+8", 8*60*60))
		}
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("expected RFC3339 or 2006-01-02 15:04:05 (UTC+8)")
}

func normalizeCloudAuditResourceType(serviceType string, value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.ToUpper(strings.TrimSpace(serviceType)) != "ECP" {
		return trimmed
	}
	switch strings.ToLower(trimmed) {
	case "vc", "vcluster", "virtualcluster":
		return "compute.ecp.v1.virtualCluster"
	case "node", "aicomputenode", "compute-node":
		return "compute.ecp.v1.aiComputeNode"
	case "job", "vcjob":
		return "vcjob"
	default:
		return trimmed
	}
}

func normalizeECPWorkloadType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "vcjob", "job":
		return "VCJob"
	case "deploy", "deployment":
		return "Deployment"
	case "sts", "statefulset":
		return "StatefulSet"
	case "ds", "daemonset":
		return "DaemonSet"
	default:
		return strings.TrimSpace(value)
	}
}

func trimNonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func formatLogTimestamp(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return t.In(time.FixedZone("UTC+8", 8*60*60)).Format("2006-01-02 15:04:05")
		}
		return value
	}
	return "-"
}

func emptyDashService(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return strings.TrimSpace(value)
}

func firstNonEmptyService(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
