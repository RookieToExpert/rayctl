package service

import (
	"context"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"rayctl/internal/podevidence"
)

func (s *JobService) collectATEEvidence(ctx context.Context, identity jobIdentity, pods []corev1.Pod, viaPlatform bool) *podevidence.Result {
	if len(pods) == 0 {
		return &podevidence.Result{Pods: []podevidence.Pod{}, Warnings: []string{"未找到可检查的 Pod；容器状态与日志尚不可用"}}
	}
	options, _ := ctx.Value(evidenceOptionsKey{}).(podevidence.Options)
	options.PodLimit = 3
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	client := s.clientset
	if viaPlatform && client != nil && identity.HostNamespace != "" {
		selected := podevidence.Select(pods, 3)
		hostPods := []corev1.Pod{}
		missing := []corev1.Pod{}
		for _, pod := range selected {
			hostName := firstNonEmpty(pod.Annotations["vcluster.loft.sh/object-host-name"], pod.Name)
			host, err := client.CoreV1().Pods(identity.HostNamespace).Get(ctx, hostName, metav1.GetOptions{})
			if err == nil && ateHostPodMatches(*host, pod) {
				hostPods = append(hostPods, *host)
			} else {
				missing = append(missing, pod)
			}
		}
		if len(hostPods) > 0 {
			result := podevidence.Collect(ctx, client, hostPods, options)
			if len(missing) > 0 {
				extra := s.collectATEProxyEvidence(ctx, identity, missing, options)
				result.Pods = append(result.Pods, extra.Pods...)
				result.Warnings = append(result.Warnings, extra.Warnings...)
			}
			result.Total = len(pods)
			result.Omitted = len(pods) - len(result.Pods)
			result.Restarts, _ = podevidence.RestartSummary(pods)
			return result
		}
	}
	if viaPlatform && s.vcClient != nil {
		return s.collectATEProxyEvidence(ctx, identity, pods, options)
	}
	return podevidence.Collect(ctx, client, pods, options)
}

func ateHostPodMatches(host, virtual corev1.Pod) bool {
	name := firstNonEmpty(host.Annotations["vcluster.loft.sh/object-name"], host.Annotations["vcluster.loft.sh/name"], host.Name)
	namespace := firstNonEmpty(host.Annotations["vcluster.loft.sh/object-namespace"], host.Annotations["vcluster.loft.sh/namespace"], host.Labels["volcano.sh/job-namespace"])
	if name != virtual.Name || namespace != "" && namespace != virtual.Namespace {
		return false
	}
	uid := firstNonEmpty(host.Annotations["vcluster.loft.sh/object-uid"], host.Annotations["vcluster.loft.sh/uid"])
	return uid == "" || virtual.UID == "" || uid == string(virtual.UID)
}

func (s *JobService) collectATEProxyEvidence(ctx context.Context, identity jobIdentity, pods []corev1.Pod, options podevidence.Options) *podevidence.Result {
	if s.vcClient != nil {
		profile := firstNonEmpty(identity.ProfileName, s.vcClient.CurrentProfileName())
		ref := firstNonEmpty(identity.VClusterUID, identity.VClusterName)
		if !strings.HasPrefix(ref, "vc-") {
			ref = "vc-" + ref
		}
		vcClient, err := s.vcClient.NewVCPodEvidenceClient(ctx, profile, ref)
		if err != nil {
			return &podevidence.Result{Total: len(pods), Pods: []podevidence.Pod{}, Warnings: []string{"VC Pod evidence: " + err.Error()}}
		}
		return podevidence.Collect(ctx, vcClient, pods, options)
	}
	return &podevidence.Result{Total: len(pods), Pods: []podevidence.Pod{}, Warnings: []string{"VC client unavailable for Pod evidence"}}
}

func ateRecentEvidenceLogs(evidence *podevidence.Result) []string {
	if evidence == nil {
		return nil
	}
	for _, pod := range evidence.Pods {
		for _, container := range pod.Containers {
			if container.Kind == "app" && len(container.Logs.Current) > 0 {
				return container.Logs.Current
			}
		}
	}
	return nil
}
