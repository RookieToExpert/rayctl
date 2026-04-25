package service

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

var (
	volcanoJobGVR = schema.GroupVersionResource{
		Group:    "batch.volcano.sh",
		Version:  "v1alpha1",
		Resource: "jobs",
	}
	volcanoPodGroupGVR = schema.GroupVersionResource{
		Group:    "scheduling.volcano.sh",
		Version:  "v1beta1",
		Resource: "podgroups",
	}
)

const (
	defaultTailLogLines = int64(10)
	defaultEventLimit   = 10
)

type JobService struct {
	clientset     kubernetes.Interface
	dynamicClient dynamic.Interface
}

type JobGetResult struct {
	Name          string
	Namespace     string
	UID           string
	Submitter     string
	PodGroupName  string
	Pods          []JobPodItem
	Nodes         []string
	InspectPod    string
	RecentEvents  []EventItem
	RecentLogLines []string
}

type JobPodItem struct {
	Name      string
	Namespace string
	NodeName  string
	Phase     string
	TaskSpec  string
	TaskIndex string
}

type PodGroupGetResult struct {
	Name         string
	Namespace    string
	Status       string
	MinMember    string
	Queue        string
	CreatedAt    string
	RecentEvents []EventItem
}

type EventItem struct {
	Time    string
	Type    string
	Reason  string
	Message string
}

func NewJobService(clientset kubernetes.Interface, dynamicClient dynamic.Interface) *JobService {
	return &JobService{
		clientset:     clientset,
		dynamicClient: dynamicClient,
	}
}

func (s *JobService) GetJob(ctx context.Context, identifier string) (*JobGetResult, error) {
	job, err := s.findJob(ctx, identifier)
	if err != nil {
		return nil, err
	}

	namespace := job.GetNamespace()
	jobName := job.GetName()
	jobUID := string(job.GetUID())
	submitter := firstNonEmpty(
		getNestedString(job.Object, "metadata", "labels", "lepton.sensetime.com/submitter"),
		getNestedString(job.Object, "metadata", "annotations", "lepton.sensetime.com/submitter"),
		"-",
	)

	podGroupName, _ := s.findOwnedPodGroupName(ctx, namespace, jobUID)

	pods, err := s.listJobPods(ctx, namespace, jobName, jobUID)
	if err != nil {
		return nil, err
	}

	inspectPod := chooseInspectPod(pods)
	recentEvents := make([]EventItem, 0)
	recentLogLines := []string{"-"}
	if inspectPod != nil {
		recentEvents, err = s.listEventsForObject(ctx, inspectPod.Namespace, "Pod", inspectPod.Name, string(inspectPod.UID), defaultEventLimit)
		if err != nil {
			return nil, err
		}
		recentLogLines, err = s.tailPodLogs(ctx, inspectPod.Namespace, inspectPod.Name, defaultTailLogLines)
		if err != nil {
			recentLogLines = []string{fmt.Sprintf("log unavailable: %v", err)}
		}
	} else {
		recentEvents = []EventItem{{Time: "-", Type: "-", Reason: "-", Message: "no pods found for this job"}}
		recentLogLines = []string{"no pods found for this job"}
	}

	resultPods := make([]JobPodItem, 0, len(pods))
	nodeSet := make(map[string]struct{})
	for _, pod := range pods {
		resultPods = append(resultPods, JobPodItem{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			NodeName:  dashIfEmpty(pod.Spec.NodeName),
			Phase:     string(pod.Status.Phase),
			TaskSpec:  firstNonEmpty(pod.Labels["volcano.sh/task-spec"], pod.Annotations["volcano.sh/task-spec"]),
			TaskIndex: firstNonEmpty(pod.Labels["volcano.sh/task-index"], pod.Annotations["volcano.sh/task-index"]),
		})
		if strings.TrimSpace(pod.Spec.NodeName) != "" {
			nodeSet[pod.Spec.NodeName] = struct{}{}
		}
	}

	nodes := make([]string, 0, len(nodeSet))
	for nodeName := range nodeSet {
		nodes = append(nodes, nodeName)
	}
	sort.Strings(nodes)
	sort.Slice(resultPods, func(i, j int) bool {
		return jobPodLess(resultPods[i], resultPods[j])
	})

	inspectPodName := "-"
	if inspectPod != nil {
		inspectPodName = inspectPod.Name
	}

	return &JobGetResult{
		Name:           jobName,
		Namespace:      namespace,
		UID:            jobUID,
		Submitter:      submitter,
		PodGroupName:   dashIfEmpty(podGroupName),
		Pods:           resultPods,
		Nodes:          nodes,
		InspectPod:     inspectPodName,
		RecentEvents:   recentEvents,
		RecentLogLines: recentLogLines,
	}, nil
}

func (s *JobService) GetPodGroup(ctx context.Context, identifier string) (*PodGroupGetResult, error) {
	pg, err := s.findPodGroup(ctx, identifier)
	if err != nil {
		return nil, err
	}

	status := firstNonEmpty(
		getNestedText(pg.Object, "status", "phase"),
		getNestedText(pg.Object, "status", "state"),
		"-",
	)
	minMember := firstNonEmpty(
		getNestedText(pg.Object, "spec", "minMember"),
		getNestedText(pg.Object, "spec", "minMemberCount"),
		"-",
	)
	queue := firstNonEmpty(
		getNestedText(pg.Object, "spec", "queue"),
		getNestedText(pg.Object, "spec", "queueName"),
		"-",
	)

	events, err := s.listEventsForObject(ctx, pg.GetNamespace(), "PodGroup", pg.GetName(), string(pg.GetUID()), defaultEventLimit)
	if err != nil {
		return nil, err
	}

	return &PodGroupGetResult{
		Name:         pg.GetName(),
		Namespace:    pg.GetNamespace(),
		Status:       status,
		MinMember:    minMember,
		Queue:        queue,
		CreatedAt:    pg.GetCreationTimestamp().Local().Format(time.RFC3339),
		RecentEvents: events,
	}, nil
}

func (s *JobService) findJob(ctx context.Context, identifier string) (*unstructured.Unstructured, error) {
	list, err := s.dynamicClient.Resource(volcanoJobGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list volcano jobs: %w", err)
	}

	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("job identifier is required")
	}

	var exact []*unstructured.Unstructured
	var fuzzy []*unstructured.Unstructured
	for i := range list.Items {
		item := &list.Items[i]
		switch {
		case item.GetName() == identifier || string(item.GetUID()) == identifier:
			exact = append(exact, item)
		case strings.HasPrefix(item.GetName(), identifier), strings.HasPrefix(item.GetGenerateName(), identifier):
			fuzzy = append(fuzzy, item)
		}
	}

	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return nil, fmt.Errorf("multiple volcano jobs matched %q exactly", identifier)
	}
	if len(fuzzy) == 1 {
		return fuzzy[0], nil
	}
	if len(fuzzy) > 1 {
		names := make([]string, 0, len(fuzzy))
		for _, item := range fuzzy {
			names = append(names, fmt.Sprintf("%s/%s", item.GetNamespace(), item.GetName()))
		}
		sort.Strings(names)
		return nil, fmt.Errorf("multiple volcano jobs matched %q: %s", identifier, strings.Join(names, ", "))
	}

	return nil, fmt.Errorf("volcano job %q not found in current cluster", identifier)
}

func (s *JobService) findPodGroup(ctx context.Context, identifier string) (*unstructured.Unstructured, error) {
	list, err := s.dynamicClient.Resource(volcanoPodGroupGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list podgroups: %w", err)
	}

	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("podgroup identifier is required")
	}

	var matched []*unstructured.Unstructured
	for i := range list.Items {
		item := &list.Items[i]
		if item.GetName() == identifier || string(item.GetUID()) == identifier {
			matched = append(matched, item)
		}
	}

	if len(matched) == 1 {
		return matched[0], nil
	}
	if len(matched) > 1 {
		names := make([]string, 0, len(matched))
		for _, item := range matched {
			names = append(names, fmt.Sprintf("%s/%s", item.GetNamespace(), item.GetName()))
		}
		sort.Strings(names)
		return nil, fmt.Errorf("multiple podgroups matched %q: %s", identifier, strings.Join(names, ", "))
	}

	return nil, fmt.Errorf("podgroup %q not found in current cluster", identifier)
}

func (s *JobService) findOwnedPodGroupName(ctx context.Context, namespace string, ownerUID string) (string, error) {
	list, err := s.dynamicClient.Resource(volcanoPodGroupGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("list podgroups in namespace %q: %w", namespace, err)
	}

	for i := range list.Items {
		item := &list.Items[i]
		owners, found, err := unstructured.NestedSlice(item.Object, "metadata", "ownerReferences")
		if !found || err != nil {
			continue
		}
		for _, owner := range owners {
			ownerMap, ok := owner.(map[string]any)
			if !ok {
				continue
			}
			if fmt.Sprintf("%v", ownerMap["uid"]) == ownerUID {
				return item.GetName(), nil
			}
		}
	}

	return "", nil
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

func getNestedString(obj map[string]any, fields ...string) string {
	value, found, err := unstructured.NestedString(obj, fields...)
	if err != nil || !found {
		return ""
	}
	return strings.TrimSpace(value)
}

func getNestedText(obj map[string]any, fields ...string) string {
	if value := getNestedString(obj, fields...); value != "" {
		return value
	}
	raw, found, err := unstructured.NestedFieldNoCopy(obj, fields...)
	if err != nil || !found || raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", raw))
}

func eventTimestamp(event corev1.Event) time.Time {
	switch {
	case !event.EventTime.IsZero():
		return event.EventTime.Time
	case !event.LastTimestamp.IsZero():
		return event.LastTimestamp.Time
	case !event.FirstTimestamp.IsZero():
		return event.FirstTimestamp.Time
	default:
		return event.CreationTimestamp.Time
	}
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func dashIfEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
