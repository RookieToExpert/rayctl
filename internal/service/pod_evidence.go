package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"rayctl/internal/kube"
	"rayctl/internal/metrics"
	"rayctl/internal/platform"
	"rayctl/internal/podevidence"
)

type evidenceOptionsKey struct{}

func WithPodEvidenceOptions(ctx context.Context, noImage bool, kubeconfig string) context.Context {
	return context.WithValue(ctx, evidenceOptionsKey{}, podevidence.Options{NoImage: noImage, PodLimit: 3, Kubeconfig: kubeconfig})
}

func collectWorkloadEvidence(ctx context.Context, client kubernetes.Interface, platformClient *platform.VirtualClusterClient, pods []corev1.Pod, name, kind, workspace string) *podevidence.Result {
	options, _ := ctx.Value(evidenceOptionsKey{}).(podevidence.Options)
	result := podevidence.Collect(ctx, client, pods, options)
	return enrichRestartHistory(ctx, platformClient, pods, name, kind, workspace, result)
}

func enrichRestartHistory(ctx context.Context, platformClient *platform.VirtualClusterClient, pods []corev1.Pod, name, kind, workspace string, result *podevidence.Result) *podevidence.Result {
	if result == nil || platformClient == nil || len(pods) == 0 {
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	config, err := platformClient.CurrentMetricsConfig()
	if err != nil {
		result.Warnings = append(result.Warnings, "24h restart history: "+err.Error())
		return result
	}
	if config.Username == "" || config.Password == "" {
		result.Warnings = append(result.Warnings, "24h restart history: metrics credentials unavailable; showing current Pod cumulative restarts")
		return result
	}
	options, _ := ctx.Value(evidenceOptionsKey{}).(podevidence.Options)
	metricsClient, err := metrics.NewClient(ctx, metrics.EndpointConfig{BaseURL: config.BaseURL, Username: config.Username, Password: config.Password, Kubeconfig: kube.ResolvedKubeconfigPath(options.Kubeconfig), RequestTimeout: 3 * time.Second})
	if err != nil {
		result.Warnings = append(result.Warnings, "24h restart history: "+err.Error())
		return result
	}
	defer metricsClient.Close()
	names := make([]string, 0, len(pods))
	if kind != "aid" {
		pods = podevidence.Select(pods, 3)
	}
	for _, pod := range pods {
		names = append(names, pod.Name)
	}
	result.HistoryPods = names
	end := time.Now()
	history, err := metrics.NewService(metricsClient, config.Environment).RestartForPods(ctx, metrics.QueryOptions{Workload: name, Type: kind, Workspace: workspace, Pods: names, By: "container", Start: end.Add(-24 * time.Hour), End: end})
	if err != nil {
		result.Warnings = append(result.Warnings, "24h restart history: "+err.Error())
		return result
	}
	if len(history.Samples) == 0 {
		result.Warnings = append(result.Warnings, "24h restart history: no samples available")
		return result
	}
	count := int64(history.Summary.EventCount)
	result.Restarts24h = &count
	for _, sample := range history.Samples {
		if sample.Event != "restart" {
			continue
		}
		observedAt, err := time.Parse(time.RFC3339Nano, sample.Timestamp)
		if err != nil || (result.LastRestart24h != nil && !observedAt.After(result.LastRestart24h.ObservedAt)) {
			continue
		}
		pod, container, _ := strings.Cut(sample.Series, "/")
		result.LastRestart24h = &podevidence.RestartObservation{
			ObservedAt: observedAt, Pod: pod, Container: container, UID: sample.UID,
			RestartCount: int64(sample.Value), Reason: sample.Reason,
		}
	}
	result.Warnings = append(result.Warnings, history.Warnings...)
	return result
}

func aidRestartDiagnosis(pods []corev1.Pod, evidence *podevidence.Result) string {
	count, latest := podevidence.RestartSummary(pods)
	text := "开发机 Pod 当前已 Ready"
	var observed *podevidence.RestartObservation
	if evidence != nil && evidence.Restarts24h != nil {
		text += fmt.Sprintf("，近 24h 监控观察到重启 %d 次，当前 Pod 累计重启 %d 次", *evidence.Restarts24h, count)
		observed = evidence.LastRestart24h
	} else if count > 0 || evidence != nil {
		text += fmt.Sprintf("，当前 Pod 累计重启 %d 次", count)
		if evidence != nil && len(evidence.Warnings) > 0 {
			text += "；近 24h 重启历史暂不可用"
		}
	}
	if observed != nil && (latest == nil || observed.ObservedAt.After(*latest.FinishedAt)) && !hasObservedRestartTermination(pods, observed) {
		text += fmt.Sprintf("；最近一次重启监控观测于 %s (UTC+8)，原因 %s", observed.ObservedAt.In(utcPlus8).Format("2006-01-02 15:04:05"), firstNonEmpty(observed.Reason, "未采集到"))
	} else if latest != nil && latest.FinishedAt != nil {
		text += fmt.Sprintf("；最近一次重启于 %s (UTC+8)，原因 %s", latest.FinishedAt.In(utcPlus8).Format("2006-01-02 15:04:05"), firstNonEmpty(latest.Reason, "未采集到"))
	}
	return strings.TrimSpace(text) + "。"
}

// Keep the precise Kubernetes termination time when its counter already
// includes the observed restart; a scrape may record it a little later.
func hasObservedRestartTermination(pods []corev1.Pod, observed *podevidence.RestartObservation) bool {
	if observed.UID == "" {
		return false
	}
	for _, pod := range pods {
		if pod.Name != observed.Pod || string(pod.UID) != observed.UID {
			continue
		}
		statuses := append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...)
		statuses = append(statuses, pod.Status.ContainerStatuses...)
		for _, status := range statuses {
			if status.Name == observed.Container && int64(status.RestartCount) >= observed.RestartCount {
				if termination := status.LastTerminationState.Terminated; termination != nil && !termination.FinishedAt.IsZero() {
					return true
				}
			}
		}
	}
	return false
}
