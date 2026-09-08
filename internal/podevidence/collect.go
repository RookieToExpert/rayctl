package podevidence

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func termination(t *corev1.ContainerStateTerminated) *Termination {
	if t == nil {
		return nil
	}
	result := &Termination{Reason: t.Reason, ExitCode: t.ExitCode}
	if !t.StartedAt.IsZero() {
		value := t.StartedAt.Time
		result.StartedAt = &value
	}
	if !t.FinishedAt.IsZero() {
		value := t.FinishedAt.Time
		result.FinishedAt = &value
	}
	if result.StartedAt != nil && result.FinishedAt != nil && !result.FinishedAt.Before(*result.StartedAt) {
		instant := result.FinishedAt.Sub(*result.StartedAt) < time.Second
		result.InstantExit = &instant
	}
	return result
}

func statuses(pod corev1.Pod) []corev1.ContainerStatus {
	result := append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...)
	return append(result, pod.Status.ContainerStatuses...)
}

func RestartSummary(pods []corev1.Pod) (int64, *Termination) {
	var total int64
	var latest *Termination
	for _, pod := range pods {
		for _, status := range statuses(pod) {
			total += int64(status.RestartCount)
			t := termination(status.LastTerminationState.Terminated)
			if t != nil && t.FinishedAt != nil && (latest == nil || t.FinishedAt.After(*latest.FinishedAt)) {
				latest = t
			}
		}
	}
	return total, latest
}

func priority(pod corev1.Pod) (int, int32, time.Time) {
	score := 0
	if pod.Status.Phase == corev1.PodPending || pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodUnknown {
		score = 3
	}
	var restarts int32
	var earliest time.Time
	for _, c := range statuses(pod) {
		if !c.Ready && c.State.Terminated == nil && score < 2 {
			score = 2
		}
		if c.RestartCount > restarts {
			restarts = c.RestartCount
		}
		if t := c.LastTerminationState.Terminated; t != nil && !t.FinishedAt.IsZero() && (earliest.IsZero() || t.FinishedAt.Time.Before(earliest)) {
			earliest = t.FinishedAt.Time
		}
	}
	return score, restarts, earliest
}

func Select(pods []corev1.Pod, limit int) []corev1.Pod {
	items := append([]corev1.Pod{}, pods...)
	sort.SliceStable(items, func(i, j int) bool {
		a, ar, at := priority(items[i])
		b, br, bt := priority(items[j])
		if a != b {
			return a > b
		}
		if ar != br {
			return ar > br
		}
		if !at.Equal(bt) {
			if at.IsZero() {
				return false
			}
			if bt.IsZero() {
				return true
			}
			return at.Before(bt)
		}
		return items[i].Namespace+"/"+items[i].Name < items[j].Namespace+"/"+items[j].Name
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func Collect(ctx context.Context, client kubernetes.Interface, pods []corev1.Pod, options Options) *Result {
	result := &Result{Total: len(pods), Pods: []Pod{}, Warnings: []string{}}
	result.Restarts, _ = RestartSummary(pods)
	if options.PodLimit <= 0 {
		options.PodLimit = 3
	}
	selected := Select(pods, options.PodLimit)
	result.Omitted = len(pods) - len(selected)
	if client == nil {
		result.Warnings = append(result.Warnings, "Kubernetes client unavailable")
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result.Pods = make([]Pod, len(selected))
	images := &imageCache{loads: map[string]*imageLoad{}}
	var wg sync.WaitGroup
	// Three selected Pods run concurrently; each Pod reads its containers serially.
	for i, pod := range selected {
		wg.Add(1)
		go func(i int, pod corev1.Pod) {
			defer wg.Done()
			result.Pods[i] = collectPod(ctx, client, pod, options, images)
		}(i, pod)
	}
	wg.Wait()
	return result
}

func collectPod(ctx context.Context, client kubernetes.Interface, pod corev1.Pod, options Options, images *imageCache) Pod {
	result := Pod{Name: pod.Name, UID: string(pod.UID), Namespace: pod.Namespace, Phase: string(pod.Status.Phase), Node: pod.Spec.NodeName, Containers: []Container{}, Warnings: []string{}}
	if pod.Spec.NodeName != "" {
		callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		node, err := client.CoreV1().Nodes().Get(callCtx, pod.Spec.NodeName, metav1.GetOptions{})
		cancel()
		if err != nil {
			result.Warnings = append(result.Warnings, "node architecture: "+err.Error())
		} else if node.Status.NodeInfo.Architecture != "" {
			value := node.Status.NodeInfo.Architecture
			result.NodeArchitecture = &value
		}
	}
	statusByName := map[string]corev1.ContainerStatus{}
	for _, status := range statuses(pod) {
		statusByName[status.Name] = status
	}
	containers := append([]corev1.Container{}, pod.Spec.InitContainers...)
	containers = append(containers, pod.Spec.Containers...)
	for i, spec := range containers {
		status, exists := statusByName[spec.Name]
		item := Container{Name: spec.Name, Kind: "app", Ready: status.Ready, RestartCount: status.RestartCount, LastState: termination(status.LastTerminationState.Terminated), Image: Image{Ref: spec.Image, Digest: status.ImageID}, Comparison: Comparison{Item: "architecture", Right: result.NodeArchitecture}}
		if i < len(pod.Spec.InitContainers) {
			item.Kind = "init"
		}
		if exists {
			switch {
			case status.State.Terminated != nil:
				item.State = map[string]any{"terminated": termination(status.State.Terminated)}
			case status.State.Waiting != nil:
				item.State = map[string]any{"waiting": map[string]any{"reason": status.State.Waiting.Reason, "message": status.State.Waiting.Message}}
			case status.State.Running != nil:
				item.State = map[string]any{"running": map[string]any{"started_at": status.State.Running.StartedAt.Time}}
			}
		} else {
			result.Warnings = append(result.Warnings, spec.Name+": container status unavailable")
		}
		var err error
		item.Logs.Current, item.Logs.CurrentTruncated, err = readLogs(ctx, client, pod, spec.Name, false)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s current logs: %v", spec.Name, err))
		}
		if status.RestartCount > 0 {
			item.Logs.Previous, item.Logs.PreviousTruncated, err = readLogs(ctx, client, pod, spec.Name, true)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s previous logs: %v", spec.Name, err))
			}
		}
		if !options.NoImage {
			item.Image, err = images.lookup(ctx, client, pod, item.Image)
			if err != nil {
				result.Warnings = append(result.Warnings, spec.Name+" image architecture: "+err.Error())
			}
		} else {
			result.Warnings = append(result.Warnings, spec.Name+": image lookup skipped (--no-image)")
		}
		item.Comparison.Left = item.Image.Architecture
		if item.Comparison.Left != nil && item.Comparison.Right != nil {
			match := *item.Comparison.Left == *item.Comparison.Right
			item.Comparison.Match = &match
		}
		result.Containers = append(result.Containers, item)
	}
	return result
}
