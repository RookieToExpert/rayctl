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
	if result.Restarts == 0 || platformClient == nil {
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
		result.Warnings = append(result.Warnings, "24h restart history: metrics credentials unavailable; showing cumulative restarts")
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
	result.Warnings = append(result.Warnings, history.Warnings...)
	return result
}

func aidRestartDiagnosis(pods []corev1.Pod, evidence *podevidence.Result) string {
	count, latest := podevidence.RestartSummary(pods)
	if count == 0 {
		return "开发机 Pod 当前已 Ready。"
	}
	text := fmt.Sprintf("Pod 当前已 Ready，历史累计重启 %d 次", count)
	if evidence != nil && evidence.Restarts24h != nil {
		text = fmt.Sprintf("Pod 当前已 Ready，近 24h 监控观察到重启 %d 次，历史累计 %d 次", *evidence.Restarts24h, count)
	}
	if latest != nil && latest.FinishedAt != nil {
		text += fmt.Sprintf("；最近一次 %s %s (UTC+8)", latest.Reason, latest.FinishedAt.In(utcPlus8).Format("2006-01-02 15:04:05"))
	}
	return strings.TrimSpace(text) + "。"
}
