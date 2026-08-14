package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"rayctl/internal/platform"
)

const (
	sspDefaultRegion           = "cn-pj-03"
	sspWorkloadTypeLabel       = "resource.compute.sensecore.cn/workload-type"
	sspWorkloadNameLabel       = "resource.compute.sensecore.cn/workload-name"
	sspWorkloadUIDLabel        = "resource.compute.sensecore.cn/workload-uid"
	sspWorkspaceNameLabel      = "resource.compute.sensecore.cn/workspace-name"
	sspTenantIDLabel           = "resource.compute.sensecore.cn/tenant-id"
	sspSubscriptionIDLabel     = "resource.compute.sensecore.cn/subscription-id"
	sspIdentityTenantIDLabel   = "identity.compute.sensecore.cn/tenant-id"
	sspVirtualNamespaceAnno    = "vcluster.loft.sh/object-name"
	sspVClusterNameAnno        = "vcluster.loft.sh/vcluster-name"
	sspClusterNameLabel        = "resource.compute.sensecore.cn/cluster-name"
	sspTrainingJobWorkloadType = "training-job"
	sspAIDWorkloadTypeValue    = "aid"
)

const (
	SSPWorkloadTypeTrainingJob = sspTrainingJobWorkloadType
	SSPWorkloadTypeAID         = sspAIDWorkloadTypeValue
	WorkloadTypeECPVCJob       = "ecp-vcjob"
)

type SSPJobService struct {
	clientset kubernetes.Interface
	platform  *platform.VirtualClusterClient
	jobHelper *JobService

	workspaceMu    sync.Mutex
	workspaceLoads map[string]*sspWorkspaceLoad
}

type sspWorkspaceLoad struct {
	done       chan struct{}
	workspaces []platform.SSPWorkspace
	err        error
}

type SSPWorkloadDetection struct {
	Type string
	pods []corev1.Pod
}

type SSPKubeconfigMismatchError struct {
	Job       string
	Workspace string
}

func (e *SSPKubeconfigMismatchError) Error() string {
	return fmt.Sprintf(
		"SSP TrainingJob %q 位于 workspace %q，但当前 HC kubeconfig 看不到对应 namespace；请切换到 PT HC kubeconfig",
		e.Job,
		e.Workspace,
	)
}

type SSPJobGetResult struct {
	Name                   string
	UID                    string
	Status                 string
	VCluster               string
	Workspace              string
	Queue                  string
	QueueType              string
	Namespace              string
	HostNamespace          string
	Submitter              string
	Framework              string
	Priority               string
	CreatedAt              string
	StartedAt              string
	EndedAt                string
	Terminal               bool
	Stage                  string
	Diagnosis              []string
	Instruction            string
	PodResources           []SSPJobPodResourceItem
	ImagePullSecrets       []string
	SecretChecks           []SecretCheckItem
	PersistentVolumeClaims []VolumeClaimRef
	Nodes                  []string
	InspectPod             string
	CheckEvidence          []CheckEvidenceItem
	RecentLogLines         []string
}

type SSPJobTaskItem struct {
	Name        string
	Role        string
	Replicas    int
	CPU         string
	Memory      string
	Accelerator string
	Model       string
	MachineType string
}

type SSPJobPodResourceItem struct {
	Pod         string
	Phase       string
	Node        string
	CPU         string
	Memory      string
	MachineType string
	Model       string
	Accelerator string
}

type sspWorkspaceRef struct {
	Name             string
	Subscription     string
	ProfileName      string
	HostNamespace    string
	VirtualNamespace string
}

type sspJobCandidate struct {
	Job       platform.SSPTrainingJob
	Workspace sspWorkspaceRef
}

func NewSSPJobService(clientset kubernetes.Interface, platformClient *platform.VirtualClusterClient) *SSPJobService {
	return &SSPJobService{
		clientset:      clientset,
		platform:       platformClient,
		jobHelper:      NewJobService(clientset, nil, platformClient),
		workspaceLoads: make(map[string]*sspWorkspaceLoad),
	}
}

// listPlatformWorkspaces shares one platform snapshot across concurrent
// queries in the same CLI invocation.
func (s *SSPJobService) listPlatformWorkspaces(ctx context.Context, region string) ([]platform.SSPWorkspace, error) {
	region = firstNonEmpty(strings.TrimSpace(region), sspDefaultRegion)
	s.workspaceMu.Lock()
	if s.workspaceLoads == nil {
		s.workspaceLoads = make(map[string]*sspWorkspaceLoad)
	}
	if load, ok := s.workspaceLoads[region]; ok {
		s.workspaceMu.Unlock()
		select {
		case <-load.done:
			return load.workspaces, load.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	load := &sspWorkspaceLoad{done: make(chan struct{})}
	s.workspaceLoads[region] = load
	s.workspaceMu.Unlock()

	load.workspaces, load.err = s.platform.ListSSPWorkspaces(ctx, region)
	close(load.done)
	return load.workspaces, load.err
}

func (s *SSPJobService) DetectWorkload(ctx context.Context, identifier string) (*SSPWorkloadDetection, error) {
	if s.clientset == nil {
		return nil, fmt.Errorf("kubernetes client is required")
	}
	identifier = strings.ToLower(strings.TrimSpace(identifier))
	if identifier == "" {
		return &SSPWorkloadDetection{}, nil
	}

	selectorKey := sspWorkloadNameLabel
	if looksLikeUUID(identifier) {
		selectorKey = sspWorkloadUIDLabel
	}
	options := metav1.ListOptions{
		LabelSelector:   labels.Set(map[string]string{selectorKey: identifier}).AsSelector().String(),
		ResourceVersion: "0",
		Limit:           5,
	}
	pods, err := s.listDetectionPodMetadata(ctx, options)
	if err != nil {
		return nil, err
	}
	matchedPods := filterPodsByLabel(pods, selectorKey, identifier)
	if workloadType := detectedWorkloadTypeFromPods(matchedPods); workloadType != "" {
		matchedPods, err = s.loadDetectionPods(ctx, options, func(items []corev1.Pod) []corev1.Pod {
			return filterPodsByLabel(items, selectorKey, identifier)
		})
		if err != nil {
			return nil, err
		}
		return &SSPWorkloadDetection{Type: workloadType, pods: matchedPods}, nil
	}

	options = metav1.ListOptions{
		FieldSelector:   fields.OneTermEqualSelector("metadata.name", identifier).String(),
		ResourceVersion: "0",
		Limit:           5,
	}
	pods, err = s.listDetectionPodMetadata(ctx, options)
	if err != nil {
		return nil, err
	}
	matchedPods = filterPodsByName(pods, identifier)
	if workloadType := detectedWorkloadTypeFromPods(matchedPods); workloadType != "" {
		matchedPods, err = s.loadDetectionPods(ctx, options, func(items []corev1.Pod) []corev1.Pod {
			return filterPodsByName(items, identifier)
		})
		if err != nil {
			return nil, err
		}
		return &SSPWorkloadDetection{Type: workloadType, pods: matchedPods}, nil
	}

	options = metav1.ListOptions{
		LabelSelector:   labels.Set(map[string]string{"volcano.sh/job-name": identifier}).AsSelector().String(),
		ResourceVersion: "0",
		Limit:           5,
	}
	pods, err = s.listDetectionPodMetadata(ctx, options)
	if err != nil {
		return nil, err
	}
	matchedPods = filterPodsByLabel(pods, "volcano.sh/job-name", identifier)
	if detectedWorkloadTypeFromPods(matchedPods) != "" {
		matchedPods, err = s.loadDetectionPods(ctx, options, func(items []corev1.Pod) []corev1.Pod {
			return filterPodsByLabel(items, "volcano.sh/job-name", identifier)
		})
		if err != nil {
			return nil, err
		}
	}
	return &SSPWorkloadDetection{Type: detectedWorkloadTypeFromPods(matchedPods), pods: matchedPods}, nil
}

func (s *SSPJobService) listDetectionPodMetadata(ctx context.Context, options metav1.ListOptions) ([]corev1.Pod, error) {
	items, _, err := s.jobHelper.listPodMetadataPage(ctx, options)
	if err != nil {
		return nil, err
	}
	pods := make([]corev1.Pod, 0, len(items))
	for _, item := range items {
		pods = append(pods, corev1.Pod{ObjectMeta: item.ObjectMeta})
	}
	return pods, nil
}

func (s *SSPJobService) loadDetectionPods(ctx context.Context, options metav1.ListOptions, filter func([]corev1.Pod) []corev1.Pod) ([]corev1.Pod, error) {
	list, err := s.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, options)
	if err != nil {
		return nil, err
	}
	return filter(list.Items), nil
}

func (s *SSPJobService) DetectWorkloadType(ctx context.Context, identifier string) (string, error) {
	detection, err := s.DetectWorkload(ctx, identifier)
	if err != nil || detection == nil {
		return "", err
	}
	return detection.Type, nil
}

func workloadTypeFromPods(pods []corev1.Pod) string {
	for _, pod := range pods {
		switch strings.ToLower(strings.TrimSpace(pod.Labels[sspWorkloadTypeLabel])) {
		case sspTrainingJobWorkloadType:
			return SSPWorkloadTypeTrainingJob
		case sspAIDWorkloadTypeValue:
			return SSPWorkloadTypeAID
		}
	}
	return ""
}

func detectedWorkloadTypeFromPods(pods []corev1.Pod) string {
	if workloadType := workloadTypeFromPods(pods); workloadType != "" {
		return workloadType
	}
	if len(pods) > 0 {
		return WorkloadTypeECPVCJob
	}
	return ""
}

func filterPodsByLabel(pods []corev1.Pod, key string, value string) []corev1.Pod {
	result := make([]corev1.Pod, 0, len(pods))
	for _, pod := range pods {
		if strings.EqualFold(strings.TrimSpace(pod.Labels[key]), strings.TrimSpace(value)) {
			result = append(result, pod)
		}
	}
	return result
}

func filterPodsByName(pods []corev1.Pod, name string) []corev1.Pod {
	result := make([]corev1.Pod, 0, len(pods))
	for _, pod := range pods {
		if strings.EqualFold(strings.TrimSpace(pod.Name), strings.TrimSpace(name)) {
			result = append(result, pod)
		}
	}
	return result
}

func (s *SSPJobService) GetJob(ctx context.Context, identifier string, workspace string, includeLogs bool) (*SSPJobGetResult, error) {
	return s.getJob(ctx, identifier, workspace, includeLogs, nil)
}

func (s *SSPJobService) GetJobWithDetection(ctx context.Context, identifier string, workspace string, includeLogs bool, detection *SSPWorkloadDetection) (*SSPJobGetResult, error) {
	if detection == nil || detection.Type != SSPWorkloadTypeTrainingJob {
		return s.GetJob(ctx, identifier, workspace, includeLogs)
	}
	return s.getJob(ctx, identifier, workspace, includeLogs, detection.pods)
}

func (s *SSPJobService) getJob(ctx context.Context, identifier string, workspace string, includeLogs bool, detectedPods []corev1.Pod) (*SSPJobGetResult, error) {
	if s.clientset == nil {
		return nil, fmt.Errorf("kubernetes client is required")
	}
	if s.platform == nil {
		return nil, fmt.Errorf("platform client is required; configure ~/.rayctl/platform.json first")
	}

	identifier = strings.TrimSpace(identifier)
	workspace = strings.TrimSpace(workspace)
	if identifier == "" {
		return nil, fmt.Errorf("training job name or uid is required")
	}
	if !looksLikeUUID(identifier) {
		identifier = strings.ToLower(identifier)
	}

	seedPods := detectedPods
	if seedPods == nil {
		seedPods = []corev1.Pod{}
	}

	workspaces, err := s.resolveWorkspaceCandidates(ctx, workspace, seedPods)
	if err != nil {
		return nil, err
	}
	subscription, err := s.resolveSubscription(ctx, seedPods)
	if err != nil {
		return nil, err
	}

	candidates, lookupErr := s.findPlatformJobs(ctx, subscription, identifier, workspaces)
	if len(candidates) == 0 {
		if lookupErr != nil {
			return nil, fmt.Errorf("query SSP TrainingJob API: %w", lookupErr)
		}
		if workspace != "" {
			return nil, fmt.Errorf("SSP training job %q not found in workspace %q", identifier, workspace)
		}
		return nil, fmt.Errorf("SSP training job %q not found in PT workspaces", identifier)
	}
	if len(candidates) > 1 {
		values := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			values = append(values, fmt.Sprintf("%s/%s (%s)", candidate.Job.WorkspaceName, candidate.Job.Name, candidate.Job.UID))
		}
		sort.Strings(values)
		return nil, fmt.Errorf("SSP training job %q matches multiple workspaces: %s; use --workspace", identifier, strings.Join(values, ", "))
	}

	candidate := candidates[0]
	job := candidate.Job
	hostNamespace := strings.TrimSpace(candidate.Workspace.HostNamespace)
	if hostNamespace == "" {
		hostNamespace = s.findHostNamespace(ctx, job.WorkspaceName, job.Namespace)
	}
	if hostNamespace == "" && !isTerminalSSPJobState(normalizeSSPJobState(job.Status.State)) {
		return nil, &SSPKubeconfigMismatchError{Job: job.Name, Workspace: job.WorkspaceName}
	}
	pods := filterSSPPodsForJob(seedPods, job)
	if len(pods) == 0 {
		var err error
		pods, err = s.findTrainingJobPods(ctx, firstNonEmpty(job.UID, job.Name), hostNamespace)
		if err != nil {
			return nil, fmt.Errorf("list SSP job pods: %w", err)
		}
	}

	return s.buildResult(ctx, job, hostNamespace, pods, includeLogs), nil
}

func (s *SSPJobService) resolveWorkspaceCandidates(ctx context.Context, requested string, pods []corev1.Pod) ([]sspWorkspaceRef, error) {
	return s.resolveWorkspaceCandidatesForRegion(ctx, requested, pods, sspDefaultRegion)
}

func (s *SSPJobService) resolveWorkspaceCandidatesForRegion(ctx context.Context, requested string, pods []corev1.Pod, region string) ([]sspWorkspaceRef, error) {
	region = firstNonEmpty(strings.TrimSpace(region), sspDefaultRegion)
	if requested != "" {
		workspaces, err := s.listPlatformWorkspaces(ctx, region)
		if err == nil {
			for _, workspace := range workspaces {
				if strings.EqualFold(strings.TrimSpace(workspace.Name), requested) {
					return []sspWorkspaceRef{{
						Name:         workspace.Name,
						Subscription: workspace.Subscription,
						ProfileName:  workspace.ProfileName,
					}}, nil
				}
			}
		}
		return []sspWorkspaceRef{{Name: requested}}, nil
	}

	byName := make(map[string]sspWorkspaceRef)
	podWorkspaceNames := make(map[string]struct{})
	for _, pod := range pods {
		name := strings.TrimSpace(pod.Labels[sspWorkspaceNameLabel])
		if name == "" {
			continue
		}
		byName[name] = sspWorkspaceRef{
			Name: name,
			Subscription: firstNonEmpty(
				pod.Labels[sspSubscriptionIDLabel],
				pod.Annotations[sspSubscriptionIDLabel],
				pod.Labels[sspTenantIDLabel],
				pod.Annotations[sspTenantIDLabel],
				pod.Labels[sspIdentityTenantIDLabel],
			),
			HostNamespace:    pod.Namespace,
			VirtualNamespace: strings.TrimSpace(pod.Annotations[sspVirtualNamespaceAnno]),
		}
		podWorkspaceNames[name] = struct{}{}
	}
	if len(byName) == 0 {
		namespaces, err := s.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
			LabelSelector: sspWorkspaceNameLabel,
		})
		if err != nil {
			return nil, fmt.Errorf("list SSP workspace namespaces: %w", err)
		}
		for _, namespace := range namespaces.Items {
			name := strings.TrimSpace(namespace.Labels[sspWorkspaceNameLabel])
			if name == "" {
				continue
			}
			// A workspace can contain multiple virtual namespaces. Select the
			// exact host namespace only after the platform job reveals its namespace.
			byName[name] = sspWorkspaceRef{Name: name}
		}
	}
	platformWorkspaces, platformErr := s.listPlatformWorkspaces(ctx, region)
	for _, workspace := range platformWorkspaces {
		name := strings.TrimSpace(workspace.Name)
		if name == "" {
			continue
		}
		if existing, exists := byName[name]; exists {
			if existing.Subscription == "" {
				existing.Subscription = strings.TrimSpace(workspace.Subscription)
			}
			if existing.ProfileName == "" {
				existing.ProfileName = strings.TrimSpace(workspace.ProfileName)
			}
			byName[name] = existing
		} else {
			byName[name] = sspWorkspaceRef{
				Name:         name,
				Subscription: strings.TrimSpace(workspace.Subscription),
				ProfileName:  strings.TrimSpace(workspace.ProfileName),
			}
		}
	}
	if len(podWorkspaceNames) > 0 {
		podWorkspaces := make(map[string]sspWorkspaceRef, len(podWorkspaceNames))
		for name := range podWorkspaceNames {
			podWorkspaces[name] = byName[name]
		}
		return sortedSSPWorkspaceRefs(podWorkspaces), nil
	}
	if len(byName) == 0 {
		if platformErr != nil {
			return nil, fmt.Errorf("resolve SSP workspaces from current kubeconfig and platform API: %w", platformErr)
		}
		return nil, fmt.Errorf("no SSP workspace found in region %s", region)
	}

	return sortedSSPWorkspaceRefs(byName), nil
}

func sortedSSPWorkspaceRefs(byName map[string]sspWorkspaceRef) []sspWorkspaceRef {
	result := make([]sspWorkspaceRef, 0, len(byName))
	for _, ref := range byName {
		result = append(result, ref)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (s *SSPJobService) resolveSubscription(ctx context.Context, pods []corev1.Pod) (string, error) {
	return s.resolveSubscriptionForRegion(ctx, pods, sspDefaultRegion)
}

func (s *SSPJobService) resolveSubscriptionForRegion(ctx context.Context, pods []corev1.Pod, region string) (string, error) {
	region = firstNonEmpty(strings.TrimSpace(region), sspDefaultRegion)
	for _, pod := range pods {
		if value := firstNonEmpty(
			pod.Labels[sspSubscriptionIDLabel],
			pod.Annotations[sspSubscriptionIDLabel],
			pod.Labels[sspTenantIDLabel],
			pod.Annotations[sspTenantIDLabel],
			pod.Labels[sspIdentityTenantIDLabel],
		); value != "" {
			return value, nil
		}
	}
	if configured := s.platform.ConfiguredSubscriptionForRegion(region); configured != "" {
		return configured, nil
	}
	nodes, err := s.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 50})
	if err != nil {
		return "", fmt.Errorf("subscription_id is empty in the %s platform profile and node labels cannot be read: %w", region, err)
	}
	for _, node := range nodes.Items {
		if value := strings.TrimSpace(node.Labels[sspTenantIDLabel]); value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("cannot determine subscription id; set subscription_id on the %s profile in ~/.rayctl/platform.json", region)
}

func (s *SSPJobService) findPlatformJobs(ctx context.Context, subscription string, identifier string, workspaces []sspWorkspaceRef) ([]sspJobCandidate, error) {
	type lookupResult struct {
		workspace sspWorkspaceRef
		jobs      []platform.SSPTrainingJob
		err       error
	}
	results := make(chan lookupResult, len(workspaces))
	semaphore := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for _, workspace := range workspaces {
		workspace := workspace
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				results <- lookupResult{workspace: workspace, err: ctx.Err()}
				return
			}
			defer func() { <-semaphore }()
			workspaceSubscription := firstNonEmpty(strings.TrimSpace(workspace.Subscription), subscription)
			var jobs []platform.SSPTrainingJob
			var err error
			if workspace.ProfileName != "" {
				jobs, err = s.platform.FindSSPTrainingJobsForProfile(
					ctx, workspace.ProfileName, workspaceSubscription, sspDefaultRegion, workspace.Name, identifier,
				)
			} else {
				jobs, err = s.platform.FindSSPTrainingJobs(ctx, workspaceSubscription, sspDefaultRegion, workspace.Name, identifier)
			}
			results <- lookupResult{workspace: workspace, jobs: jobs, err: err}
		}()
	}
	wg.Wait()
	close(results)

	candidates := make([]sspJobCandidate, 0)
	var firstErr error
	successfulLookups := 0
	for result := range results {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		successfulLookups++
		for _, job := range result.jobs {
			candidates = append(candidates, sspJobCandidate{Job: job, Workspace: result.workspace})
		}
	}
	if successfulLookups > 0 {
		firstErr = nil
	}
	return candidates, firstErr
}

func (s *SSPJobService) findTrainingJobPods(ctx context.Context, identifier string, namespace string) ([]corev1.Pod, error) {
	identifier = strings.TrimSpace(identifier)
	selector := map[string]string{sspWorkloadTypeLabel: sspTrainingJobWorkloadType}
	if looksLikeUUID(identifier) {
		selector[sspWorkloadUIDLabel] = identifier
	} else {
		selector[sspWorkloadNameLabel] = identifier
	}
	options := metav1.ListOptions{LabelSelector: labels.Set(selector).AsSelector().String()}
	list, err := s.clientset.CoreV1().Pods(namespace).List(ctx, options)
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func filterSSPPodsForJob(pods []corev1.Pod, job platform.SSPTrainingJob) []corev1.Pod {
	result := make([]corev1.Pod, 0, len(pods))
	for _, pod := range pods {
		if strings.TrimSpace(pod.Labels[sspWorkloadUIDLabel]) == strings.TrimSpace(job.UID) ||
			strings.EqualFold(strings.TrimSpace(pod.Labels[sspWorkloadNameLabel]), strings.TrimSpace(job.Name)) {
			result = append(result, pod)
		}
	}
	return result
}

func (s *SSPJobService) findHostNamespace(ctx context.Context, workspace string, virtualNamespace string) string {
	selector := labels.Set(map[string]string{sspWorkspaceNameLabel: workspace}).AsSelector().String()
	namespaces, err := s.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return ""
	}
	for _, namespace := range namespaces.Items {
		if strings.TrimSpace(namespace.Annotations[sspVirtualNamespaceAnno]) == strings.TrimSpace(virtualNamespace) {
			return namespace.Name
		}
	}
	if len(namespaces.Items) == 1 {
		return namespaces.Items[0].Name
	}
	return ""
}

func (s *SSPJobService) buildResult(ctx context.Context, job platform.SSPTrainingJob, hostNamespace string, pods []corev1.Pod, includeLogs bool) *SSPJobGetResult {
	status := normalizeSSPJobState(job.Status.State)
	terminal := isTerminalSSPJobState(status)
	vcResult := asyncCall(ctx, func(ctx context.Context) (string, error) {
		return s.resolveSSPVClusterName(ctx, pods, hostNamespace, job.ProfileName), nil
	})
	result := &SSPJobGetResult{
		Name:          job.Name,
		UID:           job.UID,
		Status:        status,
		VCluster:      "-",
		Workspace:     job.WorkspaceName,
		Queue:         job.Spec.Queue.Name,
		QueueType:     job.Spec.Queue.Type,
		Namespace:     job.Namespace,
		HostNamespace: hostNamespace,
		Submitter:     firstNonEmpty(job.Ownership.CreatorName, job.Ownership.CreatorID),
		Framework:     job.Spec.Framework,
		Priority:      job.Spec.Priority,
		CreatedAt:     formatSSPTime(job.Status.CreateTime),
		StartedAt:     formatSSPTime(job.Status.StartTime),
		EndedAt:       formatSSPTime(job.Status.EndTime),
		Terminal:      terminal,
		InspectPod:    "-",
	}
	result.PodResources, result.Nodes = makeSSPPodResourceItems(pods, job.Spec.VCJob.Tasks)
	inspectPod := chooseInspectPod(append([]corev1.Pod(nil), pods...))
	logPod := chooseMasterLogPod(pods)
	if inspectPod != nil {
		result.InspectPod = inspectPod.Name
	}
	if terminal {
		result.VCluster = (<-vcResult).Value
		result.Stage = "terminal"
		result.Diagnosis = []string{fmt.Sprintf("任务已经结束，平台状态为 %s。", status)}
		return result
	}

	imagePullSecrets, pvcRefs := extractPodSpecDetailsFromPods(pods)
	identity := &jobIdentity{Name: job.Name, Namespace: job.Namespace, UID: job.UID, HostNamespace: hostNamespace}
	result.PersistentVolumeClaims = s.resolveSSPVolumeClaimRefs(ctx, hostNamespace, pvcRefs)
	volumeDescriptors := s.resolveSSPVolumeDescriptors(ctx, trainingJobVolumeDescriptors(job.Spec.VolumeMounts))
	result.PersistentVolumeClaims = enrichSSPVolumeClaims(result.PersistentVolumeClaims, volumeDescriptors)
	result.ImagePullSecrets = s.jobHelper.resolveImagePullSecretsFromKube(ctx, hostNamespace, imagePullSecrets)
	result.VCluster = (<-vcResult).Value

	assigned := 0
	ready := 0
	for _, pod := range pods {
		if strings.TrimSpace(pod.Spec.NodeName) != "" {
			assigned++
		}
		if isPodReady(pod) {
			ready++
		}
	}
	switch {
	case len(pods) == 0:
		result.Stage = "scheduling"
		result.Diagnosis = []string{"任务处于 Pending，但 PT HC 尚未观察到 Pod，当前更可能仍在队列、控制器或 Gang 调度阶段。"}
		result.Instruction = "确认队列是否可用，并检查 TrainingJob 平台状态；Pod 创建后可再次执行本命令查看具体调度原因。"
		result.CheckEvidence = platformConditionEvidence(job.Status.Conditions)
	case assigned == 0:
		result.Stage = "scheduling"
		reason, detail := summarizeSSPSchedulingFailure(pods)
		result.Diagnosis = []string{firstNonEmpty(reason, "Pod 已创建但尚未分配到节点。")}
		result.Instruction = schedulingInstruction(detail)
		result.CheckEvidence = podConditionEvidence(pods, corev1.PodScheduled)
	case ready == 0:
		result.Stage = "startup"
		reason := summarizeSSPStartupFailure(pods)
		result.Diagnosis = []string{firstNonEmpty(reason, "Pod 已分配到节点，但尚未 Ready。")}
		result.Instruction = "根据下方 Pod 事件检查镜像、挂载卷和容器启动配置。"
		result.CheckEvidence = s.podEventEvidence(ctx, inspectPod)
		if hasImagePullProblem(pods) {
			result.SecretChecks = s.jobHelper.checkImagePullSecrets(ctx, identity, imagePullSecrets)
		}
	default:
		result.Stage = "running"
		result.Diagnosis = []string{fmt.Sprintf("任务已有 %d/%d 个 Pod Ready。", ready, len(pods))}
		if includeLogs && logPod != nil && podHasRunnableLogs(*logPod) {
			lines, err := s.jobHelper.tailPodLogs(ctx, logPod.Namespace, logPod.Name, defaultTailLogLines)
			if err != nil {
				result.RecentLogLines = []string{fmt.Sprintf("log unavailable: %v", err)}
			} else {
				result.RecentLogLines = lines
			}
		}
	}
	result.Stage, result.Diagnosis = ensurePVCGetDiagnosis(status, terminal, result.Stage, result.Diagnosis, result.PersistentVolumeClaims)
	return result
}

func makeSSPPodItems(pods []corev1.Pod) ([]JobPodItem, []string) {
	items := make([]JobPodItem, 0, len(pods))
	nodeSet := make(map[string]struct{})
	for _, pod := range pods {
		items = append(items, JobPodItem{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			NodeName:  dashIfEmpty(pod.Spec.NodeName),
			Phase:     dashIfEmpty(string(pod.Status.Phase)),
			TaskSpec:  firstNonEmpty(pod.Labels["volcano.sh/task-spec"], pod.Labels["resource.compute.sensecore.cn/task-name"]),
			TaskIndex: firstNonEmpty(pod.Labels["volcano.sh/task-index"], pod.Labels["resource.compute.sensecore.cn/task-index"]),
		})
		if pod.Spec.NodeName != "" {
			nodeSet[pod.Spec.NodeName] = struct{}{}
		}
	}
	sort.Slice(items, func(i, j int) bool { return jobPodLess(items[i], items[j]) })
	nodes := make([]string, 0, len(nodeSet))
	for node := range nodeSet {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)
	return items, nodes
}

func makeSSPPodResourceItems(pods []corev1.Pod, tasks []platform.SSPTrainingJobTask) ([]SSPJobPodResourceItem, []string) {
	podItems, nodes := makeSSPPodItems(pods)
	tasksByName := make(map[string]platform.SSPTrainingJobTask, len(tasks)*2)
	for _, task := range tasks {
		for _, value := range []string{task.Name, task.Role} {
			if key := strings.ToLower(strings.TrimSpace(value)); key != "" {
				tasksByName[key] = task
			}
		}
	}

	items := make([]SSPJobPodResourceItem, 0, len(podItems))
	for _, pod := range podItems {
		task, found := tasksByName[strings.ToLower(strings.TrimSpace(pod.TaskSpec))]
		if !found && len(tasks) == 1 {
			task = tasks[0]
			found = true
		}
		item := SSPJobPodResourceItem{
			Pod:   pod.Name,
			Phase: pod.Phase,
			Node:  pod.NodeName,
		}
		if found {
			item.CPU = formatSSPResource(task.ResourceSpec.CPUCount, "")
			item.Memory = formatSSPResource(task.ResourceSpec.MemoryGiB, "Gi")
			item.MachineType = strings.Join(task.ResourceSpec.MachineTypes, ", ")
			item.Model = task.ResourceSpec.AccelerateDeviceModel
			item.Accelerator = formatSSPResource(task.ResourceSpec.AccelerateDeviceCount, "")
		}
		items = append(items, item)
	}
	return items, nodes
}

type sspVolumeDescriptor struct {
	Type     string
	ID       string
	Name     string
	Endpoint string
}

func trainingJobVolumeDescriptors(mounts []platform.SSPTrainingJobVolumeMount) []sspVolumeDescriptor {
	result := make([]sspVolumeDescriptor, 0, len(mounts))
	for _, mount := range mounts {
		result = append(result, sspVolumeDescriptor{Type: mount.Type, ID: mount.ID, Name: mount.Name, Endpoint: mount.Endpoint})
	}
	return result
}

func aidVolumeDescriptors(mounts []platform.SSPAIDVolumeMount) []sspVolumeDescriptor {
	result := make([]sspVolumeDescriptor, 0, len(mounts))
	for _, mount := range mounts {
		result = append(result, sspVolumeDescriptor{Type: mount.Type, ID: mount.ID, Name: mount.Name, Endpoint: mount.Endpoint})
	}
	return result
}

func (s *SSPJobService) resolveSSPVolumeDescriptors(ctx context.Context, volumes []sspVolumeDescriptor) []sspVolumeDescriptor {
	result := append([]sspVolumeDescriptor(nil), volumes...)
	if s.platform == nil {
		return result
	}
	var wait sync.WaitGroup
	for index := range result {
		volumeType := strings.ToLower(strings.TrimSpace(result[index].Type))
		if strings.TrimSpace(result[index].Name) != "" || strings.Contains(volumeType, "aoss") || strings.Contains(volumeType, "object") {
			continue
		}
		uid := firstNonEmpty(strings.TrimSpace(result[index].ID), strings.TrimPrefix(strings.TrimSpace(result[index].Endpoint), "csi://"))
		if uid == "" {
			continue
		}
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			resource, err := s.platform.FindResourceByUID(ctx, uid, "virtualVolumes")
			if err != nil || resource == nil {
				return
			}
			result[index].Name = firstNonEmpty(strings.TrimSpace(resource.Name), strings.TrimSpace(resource.DisplayName))
		}()
	}
	wait.Wait()
	return result
}

func enrichSSPVolumeClaims(claims []VolumeClaimRef, mounts []sspVolumeDescriptor) []VolumeClaimRef {
	if len(claims) == 0 || len(mounts) == 0 {
		return claims
	}
	result := append([]VolumeClaimRef(nil), claims...)
	for index := range result {
		if value := strings.TrimSpace(result[index].FrontendVolume); value != "" && value != "-" {
			continue
		}
		mountIndex := index
		if parsed, ok := trailingNumericIndex(result[index].ClaimName); ok {
			mountIndex = parsed
		}
		if mountIndex < 0 || mountIndex >= len(mounts) {
			continue
		}
		result[index].FrontendVolume = formatSSPVolumeLocation(mounts[mountIndex])
	}
	return result
}

func trailingNumericIndex(value string) (int, bool) {
	value = strings.TrimSpace(value)
	separator := strings.LastIndex(value, "-")
	if separator < 0 || separator == len(value)-1 {
		return 0, false
	}
	index, err := strconv.Atoi(value[separator+1:])
	return index, err == nil
}

func formatSSPVolumeLocation(volume sspVolumeDescriptor) string {
	volumeType := strings.ToLower(strings.TrimSpace(volume.Type))
	if strings.Contains(volumeType, "aoss") || strings.Contains(volumeType, "object") || strings.Contains(volumeType, "s3") {
		return formatObjectStorageLocation(volume.Endpoint, volume.Name)
	}
	return dashIfEmpty(firstNonEmpty(volume.Name, volume.Endpoint))
}

func (s *SSPJobService) resolveSSPVolumeClaimRefs(ctx context.Context, hostNamespace string, refs []VolumeClaimRef) []VolumeClaimRef {
	hostNamespace = strings.TrimSpace(hostNamespace)
	if hostNamespace == "" || len(refs) == 0 {
		return refs
	}
	return boundedMap(ctx, refs, 4, func(ctx context.Context, ref VolumeClaimRef) VolumeClaimRef {
		pvc, err := s.clientset.CoreV1().PersistentVolumeClaims(hostNamespace).Get(ctx, ref.ClaimName, metav1.GetOptions{})
		if err != nil {
			ref.Status = "Unknown"
			ref.Message = classifyPVCErrorMessage(err)
			if ref.Message == "PVC 在当前集群不存在" {
				ref.Status = "NotFound"
			}
			return ref
		}
		ref.Status = dashIfEmpty(string(pvc.Status.Phase))
		ref.Message = firstNonEmpty(firstPVCConditionMessage(pvc), strings.TrimSpace(pvc.Spec.VolumeName))
		ref.BackendPV = strings.TrimSpace(pvc.Spec.VolumeName)
		ref.DisplayPV = ref.BackendPV
		return ref
	})
}

func (s *SSPJobService) resolveSSPVClusterName(ctx context.Context, pods []corev1.Pod, hostNamespace string, profileName string) string {
	vcReference := ""
	clusterName := ""
	for _, pod := range pods {
		vcReference = firstNonEmpty(
			strings.TrimSpace(pod.Annotations[sspVClusterNameAnno]),
			strings.TrimSpace(pod.Labels[sspVClusterNameAnno]),
			vcReference,
		)
		clusterName = firstNonEmpty(strings.TrimSpace(pod.Labels[sspClusterNameLabel]), clusterName)
		if vcReference != "" {
			break
		}
	}
	if vcReference == "" && strings.TrimSpace(hostNamespace) != "" {
		if namespace, err := s.clientset.CoreV1().Namespaces().Get(ctx, hostNamespace, metav1.GetOptions{}); err == nil {
			vcReference = firstNonEmpty(
				strings.TrimSpace(namespace.Labels[sspVClusterNameAnno]),
				strings.TrimSpace(namespace.Labels["vcluster.loft.sh/vcluster-namespace"]),
			)
			clusterName = firstNonEmpty(strings.TrimSpace(namespace.Labels[sspClusterNameLabel]), clusterName)
		}
	}
	uid := strings.TrimPrefix(strings.TrimSpace(vcReference), "vc-")
	if looksLikeUUID(uid) && s.platform != nil {
		var resource *platform.StorageVolumeResource
		var err error
		if strings.TrimSpace(profileName) != "" {
			resource, err = s.platform.FindResourceByUIDForProfile(ctx, profileName, uid, "virtualClusters")
		} else {
			resource, err = s.platform.FindResourceByUID(ctx, uid, "virtualClusters")
		}
		if err == nil {
			if name := firstNonEmpty(strings.TrimSpace(resource.Name), strings.TrimSpace(resource.DisplayName)); name != "" {
				return name
			}
		}
	}
	return dashIfEmpty(firstNonEmpty(vcReference, clusterName))
}

func summarizeSSPSchedulingFailure(pods []corev1.Pod) (string, string) {
	details := make([]string, 0)
	for _, pod := range pods {
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse {
				detail := firstNonEmpty(strings.TrimSpace(condition.Message), strings.TrimSpace(condition.Reason))
				if detail != "" {
					details = append(details, detail)
				}
			}
		}
	}
	details = uniqueStrings(details)
	if len(details) == 0 {
		return "Pod 已创建但尚未分配到节点。", ""
	}
	detail := strings.Join(details, " | ")

	reasons := make([]string, 0, 5)
	lower := strings.ToLower(detail)
	appendReason := func(match string, text string) {
		if strings.Contains(lower, match) {
			reasons = append(reasons, text)
		}
	}
	appendReason("insufficient cpu", "CPU 不足")
	appendReason("insufficient memory", "内存不足")
	if strings.Contains(lower, "insufficient nvidia.com/gpu") || strings.Contains(lower, "insufficient huawei.com/ascend") || strings.Contains(lower, "insufficient metax-tech.com/gpu") {
		reasons = append(reasons, "加速卡不足")
	}
	appendReason("didn't match pod's node affinity/selector", "节点亲和性或 selector 不匹配")
	appendReason("untolerated taint", "存在未容忍的节点污点")
	if strings.Contains(lower, "pod group is not ready") || strings.Contains(lower, "gang") {
		reasons = append(reasons, "Gang/PodGroup 尚未满足")
	}
	if len(reasons) == 0 {
		return "Pod 调度失败，具体原因见下方调度证据。", detail
	}
	return "Pod 尚未调度，检测到：" + strings.Join(uniqueStrings(reasons), "、") + "。", detail
}

func schedulingInstruction(detail string) string {
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "insufficient"):
		return "检查所选队列的空闲整机资源，或降低副本数/资源规格后重试。"
	case strings.Contains(lower, "affinity") || strings.Contains(lower, "selector"):
		return "检查队列绑定节点、machine type 与任务节点选择条件是否一致。"
	case strings.Contains(lower, "taint"):
		return "检查目标节点污点与任务 tolerations 是否匹配。"
	default:
		return "根据下方调度证据检查队列容量、节点标签和任务资源规格。"
	}
}

func summarizeSSPStartupFailure(pods []corev1.Pod) string {
	for _, pod := range pods {
		for _, status := range append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...) {
			if status.State.Waiting != nil {
				reason := firstNonEmpty(status.State.Waiting.Message, status.State.Waiting.Reason)
				if reason != "" {
					return fmt.Sprintf("Pod 已分配到节点，但容器 %s 启动受阻：%s。", status.Name, reason)
				}
			}
		}
	}
	return "Pod 已分配到节点，但尚未 Ready。"
}

func hasImagePullProblem(pods []corev1.Pod) bool {
	for _, pod := range pods {
		for _, status := range append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...) {
			if status.State.Waiting == nil {
				continue
			}
			reason := strings.ToLower(status.State.Waiting.Reason + " " + status.State.Waiting.Message)
			if strings.Contains(reason, "imagepull") || strings.Contains(reason, "errimagepull") || strings.Contains(reason, "unauthorized") {
				return true
			}
		}
	}
	return false
}

func podConditionEvidence(pods []corev1.Pod, conditionType corev1.PodConditionType) []CheckEvidenceItem {
	items := make([]CheckEvidenceItem, 0)
	for _, pod := range pods {
		var selected *corev1.PodCondition
		for _, condition := range pod.Status.Conditions {
			if condition.Type != conditionType || condition.Status == corev1.ConditionTrue {
				continue
			}
			current := condition
			if selected == nil || podConditionInformationScore(current) > podConditionInformationScore(*selected) {
				selected = &current
			}
		}
		if selected != nil {
			items = append(items, CheckEvidenceItem{
				Source: pod.Name,
				Status: firstNonEmpty(selected.Reason, string(selected.Status)),
				Detail: firstNonEmpty(selected.Message, "no condition message"),
			})
		}
	}
	return items
}

func podConditionInformationScore(condition corev1.PodCondition) int {
	message := strings.ToLower(condition.Message)
	score := len(message) / 100
	for _, marker := range []string{"insufficient", "affinity", "selector", "taint", "imagepull", "mount", "failed"} {
		if strings.Contains(message, marker) {
			score += 10
		}
	}
	if strings.Contains(message, "pod group is not ready") {
		score++
	}
	return score
}

func platformConditionEvidence(conditions []map[string]any) []CheckEvidenceItem {
	items := make([]CheckEvidenceItem, 0, len(conditions))
	for _, condition := range conditions {
		items = append(items, CheckEvidenceItem{
			Source: "platform",
			Status: firstNonEmpty(stringFromMap(condition, "reason"), stringFromMap(condition, "type"), stringFromMap(condition, "state")),
			Detail: firstNonEmpty(stringFromMap(condition, "message"), stringFromMap(condition, "detail"), "state transition only"),
		})
	}
	return items
}

func (s *SSPJobService) podEventEvidence(ctx context.Context, pod *corev1.Pod) []CheckEvidenceItem {
	if pod == nil {
		return nil
	}
	events, err := s.jobHelper.listEventsForObject(ctx, pod.Namespace, "Pod", pod.Name, string(pod.UID), defaultEventLimit)
	if err == nil && len(events) > 0 && !(len(events) == 1 && events[0].Message == "no events") {
		items := make([]CheckEvidenceItem, 0, len(events))
		for _, event := range events {
			items = append(items, CheckEvidenceItem{Source: event.Reason, Status: event.Type, Detail: event.Message})
		}
		return items
	}
	return podConditionEvidence([]corev1.Pod{*pod}, corev1.PodReady)
}

func normalizeSSPJobState(value string) string {
	value = strings.TrimSpace(value)
	upper := strings.ToUpper(value)
	for _, prefix := range []string{"TRAINING_JOB_STATE_", "TRAINING_JOB_", "JOB_STATE_", "JOB_", "STATE_"} {
		upper = strings.TrimPrefix(upper, prefix)
	}
	if upper == "" || upper == "UNSPECIFIED" {
		return "-"
	}
	return strings.ToUpper(upper[:1]) + strings.ToLower(upper[1:])
}

func isTerminalSSPJobState(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "succeeded", "completed", "failed", "aborted", "terminated", "stopped", "deleted", "canceled", "cancelled":
		return true
	default:
		return false
	}
}

func formatSSPTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return parsed.In(utcPlus8).Format("2006-01-02 15:04:05")
}

func formatSSPResource(value any, suffix string) string {
	if value == nil {
		return "-"
	}
	text := strings.TrimSpace(fmt.Sprintf("%v", value))
	if text == "" || text == "0" || text == "<nil>" {
		return dashIfEmpty(text)
	}
	return text + suffix
}

func stringFromMap(value map[string]any, key string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value[key]))
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
