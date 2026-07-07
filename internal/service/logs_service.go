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
