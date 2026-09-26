package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"sort"
	"strings"
	"time"
)

func extractVolcanoJobPhase(job *unstructured.Unstructured) string {
	if job == nil {
		return "-"
	}
	return dashIfEmpty(firstNonEmpty(
		getNestedText(job.Object, "status", "state", "phase"),
		getNestedText(job.Object, "status", "phase"),
		getNestedText(job.Object, "status", "state"),
	))
}

func isActiveVolcanoJobPhase(phase string) bool {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "running", "pending":
		return true
	default:
		return false
	}
}

func isTerminalVolcanoJobPhase(phase string) bool {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "aborted", "completed", "failed", "terminated", "succeeded":
		return true
	default:
		return false
	}
}

func summarizeJobPods(pods []corev1.Pod) (runningCount int, pendingCount int, activeCount int) {
	for _, pod := range pods {
		switch pod.Status.Phase {
		case corev1.PodRunning:
			runningCount++
			activeCount++
		case corev1.PodPending:
			pendingCount++
			activeCount++
		case corev1.PodUnknown:
			activeCount++
		}
	}
	return runningCount, pendingCount, activeCount
}

func desiredReplicaTotal(job *unstructured.Unstructured) int {
	if job == nil {
		return 0
	}
	total := 0
	tasks, found, err := unstructured.NestedSlice(job.Object, "spec", "tasks")
	if err == nil && found {
		for _, task := range tasks {
			taskMap, ok := task.(map[string]any)
			if !ok {
				continue
			}
			replicas, found, err := unstructured.NestedInt64(taskMap, "replicas")
			if err == nil && found {
				total += int(replicas)
			}
		}
	}
	if total > 0 {
		return total
	}
	if minAvailable, found, err := unstructured.NestedInt64(job.Object, "spec", "minAvailable"); err == nil && found {
		return int(minAvailable)
	}
	return 0
}

func (s *JobService) resolveJobFromPods(ctx context.Context, identifier string, jobLookupErr error) (*jobIdentity, []corev1.Pod, error) {
	allPods, err := s.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("%v; fallback list pods failed: %w", jobLookupErr, err)
	}

	candidates := make([]corev1.Pod, 0)
	for _, pod := range allPods.Items {
		if !isSSPManagedWorkloadPod(pod) && podMatchesIdentifier(pod, identifier) {
			candidates = append(candidates, pod)
		}
	}

	if len(candidates) == 0 {
		return nil, nil, jobLookupErr
	}

	groups := groupPodsByDerivedJob(candidates)
	if len(groups) == 0 {
		return nil, nil, jobLookupErr
	}
	if len(groups) > 1 {
		return nil, nil, &ambiguousJobMatchError{
			Identifier: identifier,
			Matches:    buildAmbiguousMatchesFromPodGroups(groups),
		}
	}

	var selectedKey string
	var selectedPods []corev1.Pod
	for key, pods := range groups {
		selectedKey = key
		selectedPods = pods
	}

	sort.Slice(selectedPods, func(i, j int) bool {
		return podInspectLess(selectedPods[i], selectedPods[j])
	})

	seed := selectedPods[0]
	identity := &jobIdentity{
		Name:         deriveJobName(seed),
		Namespace:    deriveLogicalNamespace(seed),
		UID:          deriveJobUID(seed),
		Submitter:    firstNonEmpty(seed.Labels["lepton.sensetime.com/submitter"], seed.Annotations["lepton.sensetime.com/submitter"], "-"),
		PodGroupName: firstNonEmpty(seed.Annotations["scheduling.k8s.io/group-name"], seed.Labels["scheduling.k8s.io/group-name"]),
		VClusterName: firstNonEmpty(seed.Annotations["vcluster.loft.sh/vcluster-name"], seed.Labels["vcluster.loft.sh/vcluster-name"]),
	}

	_ = selectedKey
	siblingPods, err := s.listSiblingPods(ctx, seed, identity)
	if err != nil {
		return identity, selectedPods, nil
	}
	if len(siblingPods) == 0 {
		return identity, selectedPods, nil
	}
	return identity, siblingPods, nil
}

func (s *JobService) listSiblingPods(ctx context.Context, seed corev1.Pod, identity *jobIdentity) ([]corev1.Pod, error) {
	namespace := seed.Namespace
	if strings.TrimSpace(namespace) == "" {
		return []corev1.Pod{seed}, nil
	}

	if jobName := strings.TrimSpace(seed.Labels["volcano.sh/job-name"]); jobName != "" {
		pods, err := s.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("volcano.sh/job-name=%s", jobName),
		})
		if err == nil && len(pods.Items) > 0 {
			return pods.Items, nil
		}
	}

	allPods, err := s.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	jobName := identity.Name
	jobUID := identity.UID
	matched := make([]corev1.Pod, 0)
	for _, pod := range allPods.Items {
		if deriveJobName(pod) == jobName {
			matched = append(matched, pod)
			continue
		}
		if jobUID != "" && deriveJobUID(pod) == jobUID {
			matched = append(matched, pod)
		}
	}
	return matched, nil
}

func (s *JobService) findHostPodForIdentifier(ctx context.Context, identifier string) (*corev1.Pod, error) {
	id := strings.TrimSpace(identifier)
	if id == "" {
		return nil, fmt.Errorf("job identifier is required")
	}

	if !looksLikeUUID(id) {
		if pod, err := s.findSinglePodByName(ctx, id); err == nil && pod != nil && !isSSPManagedWorkloadPod(*pod) {
			return pod, nil
		}
		if pod, err := s.findSinglePodByJobName(ctx, id); err == nil && pod != nil && !isSSPManagedWorkloadPod(*pod) {
			return pod, nil
		}
	}

	allPods, err := s.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("fallback list pods: %w", err)
	}

	matched := make([]corev1.Pod, 0)
	for _, pod := range allPods.Items {
		if !isSSPManagedWorkloadPod(pod) && podMatchesIdentifier(pod, id) {
			matched = append(matched, pod)
		}
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("host cluster pod for %q not found", id)
	}

	groups := groupPodsByDerivedJob(matched)
	if len(groups) > 1 {
		return nil, &ambiguousJobMatchError{
			Identifier: id,
			Matches:    buildAmbiguousMatchesFromPodGroups(groups),
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		return podInspectLess(matched[i], matched[j])
	})
	return &matched[0], nil
}

func (s *JobService) findSinglePodByName(ctx context.Context, podName string) (*corev1.Pod, error) {
	pods, err := s.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector:   fields.OneTermEqualSelector("metadata.name", podName).String(),
		ResourceVersion: "0",
	})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("pod %q not found", podName)
	}
	if len(pods.Items) > 1 {
		return nil, fmt.Errorf("multiple pods named %q found", podName)
	}
	return &pods.Items[0], nil
}

func (s *JobService) findSinglePodByJobName(ctx context.Context, jobName string) (*corev1.Pod, error) {
	pods, err := s.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		LabelSelector:   fmt.Sprintf("volcano.sh/job-name=%s", jobName),
		ResourceVersion: "0",
	})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("job pod %q not found", jobName)
	}
	sort.Slice(pods.Items, func(i, j int) bool {
		return podInspectLess(pods.Items[i], pods.Items[j])
	})
	return &pods.Items[0], nil
}

func (s *JobService) listJobPods(ctx context.Context, namespace string, jobName string, jobUID string) ([]corev1.Pod, error) {
	options := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("volcano.sh/job-name=%s", jobName),
	}
	podList, err := s.clientset.CoreV1().Pods(namespace).List(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("list pods for volcano job %q in namespace %q: %w", jobName, namespace, err)
	}

	if len(podList.Items) > 0 {
		return podList.Items, nil
	}

	allPods, err := s.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("fallback list pods for namespace %q: %w", namespace, err)
	}

	matched := make([]corev1.Pod, 0)
	for _, pod := range allPods.Items {
		if pod.Labels["volcano.sh/job-name"] == jobName {
			matched = append(matched, pod)
			continue
		}
		if hasOwnerUID(pod.OwnerReferences, jobUID) {
			matched = append(matched, pod)
		}
	}
	return matched, nil
}

func (s *JobService) listEventsForObject(ctx context.Context, namespace string, kind string, name string, uid string, limit int) ([]EventItem, error) {
	fieldSelector := fields.Set{
		"involvedObject.kind": kind,
		"involvedObject.name": name,
	}.AsSelector().String()

	events, err := s.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list events for %s %q in namespace %q: %w", kind, name, namespace, err)
	}

	sort.Slice(events.Items, func(i, j int) bool {
		return eventTimestamp(events.Items[i]).After(eventTimestamp(events.Items[j]))
	})

	result := make([]EventItem, 0, min(limit, len(events.Items)))
	for _, event := range events.Items {
		if uid != "" && string(event.InvolvedObject.UID) != "" && string(event.InvolvedObject.UID) != uid {
			continue
		}
		result = append(result, EventItem{
			Time:    eventTimestamp(event).Local().Format(time.RFC3339),
			Type:    dashIfEmpty(event.Type),
			Reason:  dashIfEmpty(event.Reason),
			Message: dashIfEmpty(event.Message),
		})
		if len(result) >= limit {
			break
		}
	}

	if len(result) == 0 {
		return []EventItem{{Time: "-", Type: "-", Reason: "-", Message: "no events"}}, nil
	}

	return result, nil
}

func (s *JobService) tailPodLogs(ctx context.Context, namespace string, podName string, tailLines int64) ([]string, error) {
	stream, err := s.clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		TailLines: &tailLines,
	}).Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("stream logs for pod %q in namespace %q: %w", podName, namespace, err)
	}
	defer stream.Close()

	lines := make([]string, 0)
	reader := bufio.NewScanner(stream)
	for reader.Scan() {
		lines = append(lines, reader.Text())
	}
	if err := reader.Err(); err != nil && err != io.EOF {
		return nil, fmt.Errorf("read logs for pod %q in namespace %q: %w", podName, namespace, err)
	}
	if len(lines) == 0 {
		return []string{"<empty log output>"}, nil
	}
	return lines, nil
}

func chooseInspectPod(pods []corev1.Pod) *corev1.Pod {
	if len(pods) == 0 {
		return nil
	}

	sort.Slice(pods, func(i, j int) bool {
		return podInspectLess(pods[i], pods[j])
	})
	return &pods[0]
}

func chooseMasterLogPod(pods []corev1.Pod) *corev1.Pod {
	masters := make([]corev1.Pod, 0, 1)
	for _, pod := range pods {
		taskSpec := firstNonEmpty(pod.Labels["volcano.sh/task-spec"], pod.Annotations["volcano.sh/task-spec"])
		if strings.EqualFold(strings.TrimSpace(taskSpec), "master") || strings.Contains(strings.ToLower(pod.Name), "-master-") {
			masters = append(masters, pod)
		}
	}
	if len(masters) > 0 {
		return chooseInspectPod(masters)
	}
	return chooseInspectPod(append([]corev1.Pod(nil), pods...))
}

func podInspectLess(left corev1.Pod, right corev1.Pod) bool {
	leftScore := podInspectScore(left)
	rightScore := podInspectScore(right)
	if leftScore != rightScore {
		return leftScore < rightScore
	}
	return left.Name < right.Name
}

func podInspectScore(pod corev1.Pod) int {
	taskSpec := firstNonEmpty(pod.Labels["volcano.sh/task-spec"], pod.Annotations["volcano.sh/task-spec"])
	taskIndex := firstNonEmpty(pod.Labels["volcano.sh/task-index"], pod.Annotations["volcano.sh/task-index"])

	switch {
	case strings.EqualFold(taskSpec, "master"), strings.EqualFold(taskSpec, "chief"), strings.EqualFold(taskSpec, "launcher"):
		return 0
	case taskIndex == "0":
		return 1
	case strings.Contains(strings.ToLower(pod.Name), "master"):
		return 2
	default:
		return 3
	}
}

func jobPodLess(left JobPodItem, right JobPodItem) bool {
	if left.TaskSpec != right.TaskSpec {
		return left.TaskSpec < right.TaskSpec
	}
	if left.TaskIndex != right.TaskIndex {
		return left.TaskIndex < right.TaskIndex
	}
	return left.Name < right.Name
}

func hasOwnerUID(owners []metav1.OwnerReference, uid string) bool {
	for _, owner := range owners {
		if string(owner.UID) == uid {
			return true
		}
	}
	return false
}

func podMatchesIdentifier(pod corev1.Pod, identifier string) bool {
	id := strings.TrimSpace(identifier)
	if id == "" {
		return false
	}

	jobName := deriveJobName(pod)
	jobUID := deriveJobUID(pod)
	logicalNamespace := deriveLogicalNamespace(pod)

	switch {
	case pod.Name == id:
		return true
	case string(pod.UID) == id:
		return true
	case jobName == id:
		return true
	case jobUID == id:
		return true
	case strings.HasPrefix(pod.Name, id):
		return true
	case strings.HasPrefix(jobName, id):
		return true
	case strings.HasPrefix(jobUID, id):
		return true
	case logicalNamespace == id:
		return true
	default:
		return false
	}
}

func isSSPManagedWorkloadPod(pod corev1.Pod) bool {
	switch strings.ToLower(strings.TrimSpace(pod.Labels[sspWorkloadTypeLabel])) {
	case sspTrainingJobWorkloadType, sspAIDWorkloadTypeValue:
		return true
	default:
		return false
	}
}

func groupPodsByDerivedJob(pods []corev1.Pod) map[string][]corev1.Pod {
	grouped := make(map[string][]corev1.Pod)
	for _, pod := range pods {
		jobName := deriveJobName(pod)
		if jobName == "" {
			jobName = pod.Name
		}
		key := fmt.Sprintf("%s/%s", pod.Namespace, jobName)
		grouped[key] = append(grouped[key], pod)
	}
	return grouped
}

func deriveJobName(pod corev1.Pod) string {
	if value := strings.TrimSpace(pod.Labels["volcano.sh/job-name"]); value != "" {
		return value
	}
	if value := strings.TrimSpace(pod.Annotations["volcano.sh/job-name"]); value != "" {
		return value
	}

	name, _, _ := parseOwnerReferenceAnnotation(pod.Annotations["vcluster.loft.sh/owner-references"])
	return name
}

func deriveJobUID(pod corev1.Pod) string {
	_, uid, _ := parseOwnerReferenceAnnotation(pod.Annotations["vcluster.loft.sh/owner-references"])
	return uid
}

func deriveLogicalNamespace(pod corev1.Pod) string {
	return firstNonEmpty(
		pod.Annotations["vcluster.loft.sh/namespace"],
		pod.Labels["volcano.sh/job-namespace"],
		pod.Annotations["volcano.sh/job-namespace"],
		pod.Namespace,
	)
}

func parseOwnerReferenceAnnotation(raw string) (string, string, string) {
	if strings.TrimSpace(raw) == "" {
		return "", "", ""
	}

	var refs []map[string]any
	if err := json.Unmarshal([]byte(raw), &refs); err != nil {
		return "", "", ""
	}
	for _, ref := range refs {
		kind := strings.TrimSpace(fmt.Sprintf("%v", ref["kind"]))
		if kind != "Job" {
			continue
		}
		return strings.TrimSpace(fmt.Sprintf("%v", ref["name"])), strings.TrimSpace(fmt.Sprintf("%v", ref["uid"])), kind
	}
	return "", "", ""
}

func jobIdentityFromPod(pod corev1.Pod) *jobIdentity {
	return &jobIdentity{
		Name:          deriveJobName(pod),
		Namespace:     deriveLogicalNamespace(pod),
		UID:           deriveJobUID(pod),
		Submitter:     firstNonEmpty(pod.Labels["lepton.sensetime.com/submitter"], pod.Annotations["lepton.sensetime.com/submitter"], "-"),
		PodGroupName:  firstNonEmpty(pod.Annotations["scheduling.k8s.io/group-name"], pod.Labels["scheduling.k8s.io/group-name"]),
		VClusterName:  firstNonEmpty(pod.Annotations["vcluster.loft.sh/vcluster-name"], pod.Labels["vcluster.loft.sh/vcluster-name"]),
		HostNamespace: pod.Namespace,
	}
}

func formatEventItems(events []corev1.Event, limit int) []EventItem {
	if len(events) == 0 {
		return []EventItem{{Time: "-", Type: "-", Reason: "-", Message: "no events"}}
	}

	sort.Slice(events, func(i, j int) bool {
		return eventTimestamp(events[i]).After(eventTimestamp(events[j]))
	})

	items := make([]EventItem, 0, min(limit, len(events)))
	for _, event := range events {
		items = append(items, EventItem{
			Time:    eventTimestamp(event).Local().Format(time.RFC3339),
			Type:    dashIfEmpty(event.Type),
			Reason:  dashIfEmpty(event.Reason),
			Message: dashIfEmpty(event.Message),
		})
		if len(items) >= limit {
			break
		}
	}
	return items
}

func podHasRunnableLogs(pod corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodSucceeded
}

func isFailedPod(pod corev1.Pod) bool {
	if pod.Status.Phase == corev1.PodFailed {
		return true
	}
	for _, status := range pod.Status.InitContainerStatuses {
		if isFailedContainerStatus(status) {
			return true
		}
	}
	for _, status := range pod.Status.ContainerStatuses {
		if isFailedContainerStatus(status) {
			return true
		}
	}
	return false
}

func podFailureReason(pod corev1.Pod) string {
	if pod.Status.Phase == corev1.PodFailed {
		if strings.TrimSpace(pod.Status.Message) != "" {
			return pod.Status.Message
		}
		if strings.TrimSpace(pod.Status.Reason) != "" {
			return pod.Status.Reason
		}
	}
	for _, status := range pod.Status.InitContainerStatuses {
		if reason := containerFailureReason(status); reason != "" {
			return reason
		}
	}
	for _, status := range pod.Status.ContainerStatuses {
		if reason := containerFailureReason(status); reason != "" {
			return reason
		}
	}
	return ""
}

func isFailedContainerStatus(status corev1.ContainerStatus) bool {
	return containerFailureReason(status) != ""
}

func containerFailureReason(status corev1.ContainerStatus) string {
	if status.State.Terminated == nil {
		return ""
	}
	terminated := status.State.Terminated
	if terminated.ExitCode == 0 {
		return ""
	}
	return firstNonEmpty(
		strings.TrimSpace(terminated.Message),
		strings.TrimSpace(terminated.Reason),
		fmt.Sprintf("exit code %d", terminated.ExitCode),
	)
}

func isPodReady(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}
