package service

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"rayctl/internal/platform"
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
	defaultRegistryHost = "registry2.d.pjlab.org.cn"
)

type JobService struct {
	clientset     kubernetes.Interface
	dynamicClient dynamic.Interface
	vcClient      *platform.VirtualClusterClient
}

type jobIdentity struct {
	Name         string
	Namespace    string
	UID          string
	Submitter    string
	PodGroupName string
	VClusterName string
	HostNamespace string
}

type JobGetResult struct {
	Name          string
	Namespace     string
	UID           string
	Submitter     string
	PodGroupName  string
	ImagePullSecrets []string
	PersistentVolumeClaims []VolumeClaimRef
	Pods          []JobPodItem
	Nodes         []string
	InspectPod    string
	RecentEvents  []EventItem
	RecentLogLines []string
	Timings       JobGetTimings
}

type JobCheckResult struct {
	Name                string
	Namespace           string
	UID                 string
	PodGroupName        string
	Stage               string
	Instruction         string
	Pods                []JobPodCheckItem
	PVCs                []PVCCheckItem
	SecretChecks        []SecretCheckItem
	PodGroupEvidence    []CheckEvidenceItem
	Diagnosis           []string
}

type VolumeClaimRef struct {
	Name      string
	ClaimName string
}

type PVCCheckItem struct {
	Name      string
	ClaimName string
	Status    string
	Message   string
}

type SecretCheckItem struct {
	SecretName string
	Registry   string
	Username   string
	Password   string
	Status     string
	Message    string
}

type CheckEvidenceItem struct {
	Source string
	Status string
	Detail string
}

type JobGetTimings struct {
	Locate       time.Duration
	PlatformJob  time.Duration
	PlatformPods time.Duration
	PlatformEvents time.Duration
	PlatformLogs time.Duration
	KubeJob      time.Duration
	KubePods     time.Duration
	KubeEvents   time.Duration
	KubeLogs     time.Duration
	Format       time.Duration
	Total        time.Duration
}

type JobPodItem struct {
	Name      string
	Namespace string
	NodeName  string
	Phase     string
	TaskSpec  string
	TaskIndex string
}

type JobPodCheckItem struct {
	Name      string
	Phase     string
	Ready     string
	NodeName  string
}

type PodGroupGetResult struct {
	Name         string
	Namespace    string
	Status       string
	MinMember    string
	Queue        string
	CreatedAt    string
	StatusMessages []string
	RecentEvents []EventItem
}

type EventItem struct {
	Time    string
	Type    string
	Reason  string
	Message string
}

type dockerCredential struct {
	Registry string
	Username string
	Password string
}

func NewJobService(clientset kubernetes.Interface, dynamicClient dynamic.Interface, vcClient *platform.VirtualClusterClient) *JobService {
	return &JobService{
		clientset:     clientset,
		dynamicClient: dynamicClient,
		vcClient:      vcClient,
	}
}

func (s *JobService) GetJob(ctx context.Context, identifier string) (*JobGetResult, error) {
	startedAt := time.Now()
	if s.vcClient != nil {
		if result, err := s.getJobViaPlatform(ctx, identifier); err == nil {
			result.Timings.Total = time.Since(startedAt)
			return result, nil
		}
	}

	locateBegin := time.Now()
	identity, pods, err := s.resolveJobIdentity(ctx, identifier)
	locateDuration := time.Since(locateBegin)
	if err != nil {
		return nil, err
	}

	inspectPod := chooseInspectPod(pods)
	recentLogLines := []string{"-"}
	var kubeLogsDuration time.Duration

	if inspectPod != nil {
		if podHasRunnableLogs(*inspectPod) {
			logsBegin := time.Now()
			recentLogLines, err = s.tailPodLogs(ctx, inspectPod.Namespace, inspectPod.Name, defaultTailLogLines)
			kubeLogsDuration = time.Since(logsBegin)
			if err != nil {
				recentLogLines = []string{fmt.Sprintf("log unavailable: %v", err)}
			}
		} else {
			recentLogLines = []string{fmt.Sprintf("pod phase is %s, logs are not available yet", inspectPod.Status.Phase)}
		}
	} else {
		recentLogLines = []string{"no pods found for this job"}
	}

	formatBegin := time.Now()
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
	formatDuration := time.Since(formatBegin)
	imagePullSecrets, pvcRefs := extractPodSpecDetailsFromPods(pods)
	imagePullSecrets = s.resolveImagePullSecretsFromKube(ctx, firstNonEmpty(identity.HostNamespace, identity.Namespace), imagePullSecrets)

	return &JobGetResult{
		Name:           identity.Name,
		Namespace:      identity.Namespace,
		UID:            identity.UID,
		Submitter:      identity.Submitter,
		PodGroupName:   dashIfEmpty(identity.PodGroupName),
		ImagePullSecrets: imagePullSecrets,
		PersistentVolumeClaims: pvcRefs,
		Pods:           resultPods,
		Nodes:          nodes,
		InspectPod:     inspectPodName,
		RecentLogLines: recentLogLines,
		Timings: JobGetTimings{
			Locate:     locateDuration,
			KubeLogs:   kubeLogsDuration,
			Format:     formatDuration,
			Total:      time.Since(startedAt),
		},
	}, nil
}

func (s *JobService) CheckJob(ctx context.Context, identifier string) (*JobCheckResult, error) {
	if result, err := s.checkJobInCurrentCluster(ctx, identifier); err == nil {
		return result, nil
	}

	if s.vcClient == nil {
		return nil, fmt.Errorf("job check requires platform configuration")
	}

	identity, err := s.locateJobForPlatform(ctx, identifier)
	if err != nil {
		return nil, err
	}

	job, err := s.vcClient.GetVolcanoJob(ctx, identity.VClusterName, identity.Namespace, identity.Name)
	if err != nil {
		return nil, err
	}

	identity.UID = firstNonEmpty(identity.UID, string(job.GetUID()))
	if strings.TrimSpace(identity.PodGroupName) == "" && strings.TrimSpace(identity.UID) != "" {
		identity.PodGroupName = fmt.Sprintf("%s-%s", identity.Name, identity.UID)
	}

	pods, err := s.vcClient.ListJobPods(ctx, identity.VClusterName, identity.Namespace, identity.Name)
	if err != nil {
		return nil, err
	}

	podChecks := make([]JobPodCheckItem, 0, len(pods))
	readyPodCount := 0
	assignedPodCount := 0
	for _, pod := range pods {
		ready := isPodReady(pod)
		if ready {
			readyPodCount++
		}
		if strings.TrimSpace(pod.Spec.NodeName) != "" {
			assignedPodCount++
		}
		podChecks = append(podChecks, JobPodCheckItem{
			Name:     pod.Name,
			Phase:    dashIfEmpty(string(pod.Status.Phase)),
			Ready:    boolToYesNo(ready),
			NodeName: dashIfEmpty(pod.Spec.NodeName),
		})
	}
	sort.Slice(podChecks, func(i, j int) bool {
		return podChecks[i].Name < podChecks[j].Name
	})

	_, pvcRefs := extractJobSpecDetails(job)
	failedPVCChecks := make([]PVCCheckItem, 0, len(pvcRefs))
	pendingPVCs := 0
	for _, pvcRef := range pvcRefs {
		status := "-"
		message := "-"
		pvc, pvcErr := s.vcClient.GetPersistentVolumeClaim(ctx, identity.VClusterName, identity.Namespace, pvcRef.ClaimName)
		if pvcErr != nil {
			message = classifyPVCErrorMessage(pvcErr)
		} else {
			status = dashIfEmpty(string(pvc.Status.Phase))
			message = dashIfEmpty(firstPVCConditionMessage(pvc))
			if pvc.Status.Phase == corev1.ClaimPending {
				pendingPVCs++
				message = "PVC 的 AKSK 错误"
			}
		}
		if status != "Bound" {
			failedPVCChecks = append(failedPVCChecks, PVCCheckItem{
				Name:      pvcRef.Name,
				ClaimName: pvcRef.ClaimName,
				Status:    status,
				Message:   message,
			})
		}
	}

	diagnosis := make([]string, 0, 3)
	secretChecks := []SecretCheckItem{}
	stage := "running"
	missingPVCs := 0
	for _, pvc := range failedPVCChecks {
		if pvc.Message == "pvc 不存在于当前分区" {
			missingPVCs++
		}
	}
	if len(pods) == 0 || assignedPodCount == 0 {
		stage = "scheduling"
		if missingPVCs > 0 {
			diagnosis = append(diagnosis, "存在 PVC 不在当前分区，任务因此无法继续调度。")
		} else if pendingPVCs > 0 {
			diagnosis = append(diagnosis, "PVC 仍处于 Pending，当前大概率是 PVC 的 AKSK 错误。")
		} else {
			diagnosis = append(diagnosis, "任务还没有调度到任何 host。")
		}
	} else if readyPodCount == 0 {
		stage = "startup"
		imagePullSecrets, _ := extractJobSpecDetails(job)
		secretChecks = s.checkImagePullSecrets(ctx, identity, imagePullSecrets)
		if len(secretChecks) == 0 {
			diagnosis = append(diagnosis, "Pod 已经分配到 host，但还没有 Ready，且任务里没有配置 imagePullSecret。")
		} else if hasFailedSecretCheck(secretChecks) {
			diagnosis = append(diagnosis, "Pod 已经分配到 host，但还没有 Ready，当前 imagePullSecret 看起来是错误的。")
		} else if hasErroredSecretCheck(secretChecks) {
			diagnosis = append(diagnosis, "Pod 已经分配到 host，但还没有 Ready，当前机器无法完成 imagePullSecret 校验。")
		} else {
			diagnosis = append(diagnosis, "Pod 已经分配到 host，但还没有 Ready。imagePullSecret 看起来没问题，请继续检查容器启动本身。")
		}
	} else {
		diagnosis = append(diagnosis, "至少有一个 Pod 已经 Ready，当前任务不属于常见的“起不来”问题。")
	}

	displaySecrets := make([]SecretCheckItem, 0, len(secretChecks))
	for _, secret := range secretChecks {
		if !strings.EqualFold(secret.Status, "OK") {
			displaySecrets = append(displaySecrets, secret)
		}
	}

	displayPods := make([]JobPodCheckItem, 0, len(podChecks))
	if stage == "startup" {
		for _, pod := range podChecks {
			if pod.Ready != "Yes" {
				displayPods = append(displayPods, pod)
			}
		}
		if len(displayPods) == 0 {
			displayPods = podChecks
		}
	}

	instruction := ""
	if stage == "scheduling" {
		instruction = fmt.Sprintf("当前任务还没调度成功，请带上目标 vcluster 的 kubeconfig 重新执行：rayctl job check %s -k <vcluster-kubeconfig-path>", identity.Name)
	}

	return &JobCheckResult{
		Name:             identity.Name,
		Namespace:        identity.Namespace,
		UID:              identity.UID,
		PodGroupName:     dashIfEmpty(identity.PodGroupName),
		Stage:            stage,
		Instruction:      instruction,
		Pods:             displayPods,
		PVCs:             failedPVCChecks,
		SecretChecks:     displaySecrets,
		PodGroupEvidence: nil,
		Diagnosis:        diagnosis,
	}, nil
}

func (s *JobService) getJobViaPlatform(ctx context.Context, identifier string) (*JobGetResult, error) {
	startedAt := time.Now()
	locateBegin := time.Now()
	identity, err := s.locateJobForPlatform(ctx, identifier)
	locateDuration := time.Since(locateBegin)
	if err != nil {
		return nil, err
	}

	jobBegin := time.Now()
	job, err := s.vcClient.GetVolcanoJob(ctx, identity.VClusterName, identity.Namespace, identity.Name)
	platformJobDuration := time.Since(jobBegin)
	if err != nil {
		return nil, err
	}

	podsBegin := time.Now()
	pods, err := s.vcClient.ListJobPods(ctx, identity.VClusterName, identity.Namespace, identity.Name)
	platformPodsDuration := time.Since(podsBegin)
	if err != nil {
		return nil, err
	}

	identity.UID = firstNonEmpty(identity.UID, string(job.GetUID()))
	identity.Submitter = firstNonEmpty(
		identity.Submitter,
		getNestedString(job.Object, "metadata", "labels", "lepton.sensetime.com/submitter"),
		getNestedString(job.Object, "metadata", "annotations", "lepton.sensetime.com/submitter"),
		"-",
	)
	if strings.TrimSpace(identity.PodGroupName) == "" && strings.TrimSpace(identity.UID) != "" {
		identity.PodGroupName = fmt.Sprintf("%s-%s", identity.Name, identity.UID)
	}

	inspectPod := chooseInspectPod(pods)
	recentLogLines := []string{"no pods found for this job"}
	var platformLogsDuration time.Duration
	if inspectPod != nil {
		if podHasRunnableLogs(*inspectPod) {
			logsBegin := time.Now()
			logs, logErr := s.vcClient.GetPodLogs(ctx, identity.VClusterName, inspectPod.Namespace, inspectPod.Name, defaultTailLogLines)
			platformLogsDuration = time.Since(logsBegin)
			if logErr == nil {
				recentLogLines = logs
			} else if strings.TrimSpace(identity.HostNamespace) != "" {
				fallbackBegin := time.Now()
				fallbackLogs, fallbackErr := s.tailPodLogs(ctx, identity.HostNamespace, inspectPod.Name, defaultTailLogLines)
				platformLogsDuration += time.Since(fallbackBegin)
				if fallbackErr == nil {
					recentLogLines = fallbackLogs
				} else {
					recentLogLines = []string{fmt.Sprintf("log unavailable: %v", logErr)}
				}
			} else {
				recentLogLines = []string{fmt.Sprintf("log unavailable: %v", logErr)}
			}
		} else {
			recentLogLines = []string{fmt.Sprintf("pod phase is %s, logs are not available yet", inspectPod.Status.Phase)}
		}
	}

	formatBegin := time.Now()
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
	formatDuration := time.Since(formatBegin)
	imagePullSecrets, pvcRefs := extractJobSpecDetails(job)
	imagePullSecrets = s.resolveImagePullSecrets(ctx, identity, imagePullSecrets)

	return &JobGetResult{
		Name:           identity.Name,
		Namespace:      identity.Namespace,
		UID:            identity.UID,
		Submitter:      identity.Submitter,
		PodGroupName:   dashIfEmpty(identity.PodGroupName),
		ImagePullSecrets: imagePullSecrets,
		PersistentVolumeClaims: pvcRefs,
		Pods:           resultPods,
		Nodes:          nodes,
		InspectPod:     inspectPodName,
		RecentLogLines: recentLogLines,
		Timings: JobGetTimings{
			Locate:         locateDuration,
			PlatformJob:    platformJobDuration,
			PlatformPods:   platformPodsDuration,
			PlatformLogs:   platformLogsDuration,
			Format:         formatDuration,
			Total:          time.Since(startedAt),
		},
	}, nil
}

func (s *JobService) checkJobInCurrentCluster(ctx context.Context, identifier string) (*JobCheckResult, error) {
	job, err := s.getJobInCurrentCluster(ctx, identifier)
	if err != nil {
		return nil, err
	}

	namespace := job.GetNamespace()
	jobName := job.GetName()
	jobUID := string(job.GetUID())

	podGroupName := derivePodGroupName(jobName, jobUID)
	pods, err := s.listJobPods(ctx, namespace, jobName, jobUID)
	if err != nil {
		return nil, err
	}

	assignedPodCount := 0
	for _, pod := range pods {
		if strings.TrimSpace(pod.Spec.NodeName) != "" {
			assignedPodCount++
		}
	}

	identity := &jobIdentity{
		Name:         jobName,
		Namespace:    namespace,
		UID:          jobUID,
		PodGroupName: podGroupName,
	}

	if len(pods) > 0 && assignedPodCount > 0 {
		return &JobCheckResult{
			Name:         identity.Name,
			Namespace:    identity.Namespace,
			UID:          identity.UID,
			PodGroupName: dashIfEmpty(identity.PodGroupName),
			Stage:        "startup",
			Diagnosis: []string{
				"Pod 已经分配到 host。请切回 host-cluster kubeconfig 重新执行 job check，继续排查 imagePullSecret 等启动问题。",
			},
		}, nil
	}

	if strings.TrimSpace(podGroupName) == "" {
		return &JobCheckResult{
			Name:         identity.Name,
			Namespace:    identity.Namespace,
			UID:          identity.UID,
			PodGroupName: "-",
			Stage:        "scheduling",
			Diagnosis:    []string{"当前 vcluster kubeconfig 里没有找到对应的 PodGroup。"},
		}, nil
	}

	pg, err := s.dynamicClient.Resource(volcanoPodGroupGVR).Namespace(namespace).Get(ctx, podGroupName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get podgroup %q in namespace %q: %w", podGroupName, namespace, err)
	}

	evidence := make([]CheckEvidenceItem, 0)
	for _, message := range extractPodGroupMessages(pg) {
		evidence = append(evidence, CheckEvidenceItem{
			Source: "status",
			Status: extractPodGroupPhase(pg),
			Detail: message,
		})
	}

	events, err := s.listEventsForObject(ctx, namespace, "PodGroup", podGroupName, string(pg.GetUID()), defaultEventLimit)
	if err == nil {
		for _, event := range events {
			evidence = append(evidence, CheckEvidenceItem{
				Source: "event",
				Status: firstNonEmpty(event.Reason, event.Type, "-"),
				Detail: event.Message,
			})
		}
	}

	return &JobCheckResult{
		Name:             identity.Name,
		Namespace:        identity.Namespace,
		UID:              identity.UID,
		PodGroupName:     dashIfEmpty(identity.PodGroupName),
		Stage:            "scheduling",
		PodGroupEvidence: evidence,
		Diagnosis: []string{
			"当前任务仍在等待调度，下面是 vcluster 里的 PodGroup 状态和事件。",
		},
	}, nil
}

func (s *JobService) getJobInCurrentCluster(ctx context.Context, identifier string) (*unstructured.Unstructured, error) {
	id := strings.TrimSpace(identifier)
	if id == "" {
		return nil, fmt.Errorf("job identifier is required")
	}

	if !looksLikeUUID(id) {
		job, err := s.dynamicClient.Resource(volcanoJobGVR).Namespace("default").Get(ctx, id, metav1.GetOptions{})
		if err == nil {
			return job, nil
		}
	}

	return s.findJob(ctx, identifier)
}

func (s *JobService) resolveJobIdentity(ctx context.Context, identifier string) (*jobIdentity, []corev1.Pod, error) {
	job, err := s.findJob(ctx, identifier)
	if err == nil {
		namespace := job.GetNamespace()
		jobName := job.GetName()
		jobUID := string(job.GetUID())
		submitter := firstNonEmpty(
			getNestedString(job.Object, "metadata", "labels", "lepton.sensetime.com/submitter"),
			getNestedString(job.Object, "metadata", "annotations", "lepton.sensetime.com/submitter"),
			"-",
		)

		podGroupName, _ := s.findOwnedPodGroupName(ctx, namespace, jobUID)
		pods, podErr := s.listJobPods(ctx, namespace, jobName, jobUID)
		if podErr != nil {
			return nil, nil, podErr
		}

		return &jobIdentity{
			Name:         jobName,
			Namespace:    namespace,
			UID:          jobUID,
			Submitter:    submitter,
			PodGroupName: podGroupName,
			VClusterName: "",
		}, pods, nil
	}

	return s.resolveJobFromPods(ctx, identifier, err)
}

func (s *JobService) locateJobForPlatform(ctx context.Context, identifier string) (*jobIdentity, error) {
	if shouldLookupJobByPlatform(identifier) {
		if pod, err := s.findSinglePodByJobName(ctx, identifier); err == nil && pod != nil {
			return jobIdentityFromPod(*pod), nil
		}
		if pod, err := s.findSeedPodForJobName(ctx, identifier); err == nil && pod != nil {
			return jobIdentityFromPod(*pod), nil
		}
	}

	if s.vcClient != nil && shouldLookupJobByPlatform(identifier) {
		if identity, err := s.findPlatformJobIdentity(ctx, identifier); err == nil {
			return identity, nil
		}
	}

	if pod, err := s.findHostPodForIdentifier(ctx, identifier); err == nil && pod != nil {
		return jobIdentityFromPod(*pod), nil
	}

	identity, _, err := s.resolveJobIdentity(ctx, identifier)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(identity.VClusterName) == "" {
		return nil, fmt.Errorf("unable to determine vcluster for %q", identifier)
	}
	return identity, nil
}

func (s *JobService) findSeedPodForJobName(ctx context.Context, jobName string) (*corev1.Pod, error) {
	candidates := []string{
		jobName + "-worker-0",
		jobName + "-master-0",
		jobName + "-launcher-0",
		jobName + "-chief-0",
	}

	for _, candidate := range candidates {
		pod, err := s.findSinglePodByName(ctx, candidate)
		if err == nil && pod != nil {
			return pod, nil
		}
	}

	return nil, fmt.Errorf("seed pod for job %q not found", jobName)
}

func (s *JobService) findPlatformJobIdentity(ctx context.Context, identifier string) (*jobIdentity, error) {
	vclusters, err := s.vcClient.ListVirtualClusters(ctx)
	if err != nil {
		return nil, err
	}

	searchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type searchResult struct {
		identity *jobIdentity
		err      error
	}

	results := make(chan searchResult, len(vclusters))
	var wg sync.WaitGroup

	for _, vc := range vclusters {
		vc := vc
		wg.Add(1)
		go func() {
			defer wg.Done()

			job, getErr := s.vcClient.GetVolcanoJob(searchCtx, vc.Name, "default", identifier)
			if getErr != nil {
				results <- searchResult{}
				return
			}

			results <- searchResult{
				identity: &jobIdentity{
					Name:         job.GetName(),
					Namespace:    job.GetNamespace(),
					UID:          string(job.GetUID()),
					Submitter:    firstNonEmpty(getNestedString(job.Object, "metadata", "labels", "lepton.sensetime.com/submitter"), "-"),
					PodGroupName: fmt.Sprintf("%s-%s", job.GetName(), string(job.GetUID())),
					VClusterName: vc.Name,
				},
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for result := range results {
		if result.err != nil {
			continue
		}
		if result.identity != nil {
			cancel()
			return result.identity, nil
		}
	}

	return nil, fmt.Errorf("platform volcano job %q not found", identifier)
}

func (s *JobService) locatePodGroupForPlatform(ctx context.Context, identifier string) (*jobIdentity, error) {
	pod, err := s.findHostPodForIdentifier(ctx, identifier)
	if err == nil && pod != nil {
		identity := jobIdentityFromPod(*pod)
		if strings.TrimSpace(identity.PodGroupName) != "" {
			return identity, nil
		}
	}

	return nil, fmt.Errorf("unable to determine vcluster podgroup identity for %q from host cluster pod data", identifier)
}

func (s *JobService) GetPodGroup(ctx context.Context, identifier string) (*PodGroupGetResult, error) {
	if s.vcClient != nil {
		if result, err := s.getPodGroupViaPlatform(ctx, identifier); err == nil {
			return result, nil
		}
	}

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

	return &PodGroupGetResult{
		Name:           pg.GetName(),
		Namespace:      pg.GetNamespace(),
		Status:         status,
		MinMember:      minMember,
		Queue:          queue,
		CreatedAt:      pg.GetCreationTimestamp().Local().Format(time.RFC3339),
		StatusMessages: extractPodGroupMessages(pg),
	}, nil
}

func (s *JobService) getPodGroupViaPlatform(ctx context.Context, identifier string) (*PodGroupGetResult, error) {
	identity, err := s.locatePodGroupForPlatform(ctx, identifier)
	if err != nil {
		return nil, err
	}

	pg, err := s.vcClient.GetPodGroup(ctx, identity.VClusterName, identity.Namespace, identity.PodGroupName)
	if err != nil {
		return nil, err
	}

	return &PodGroupGetResult{
		Name:      pg.GetName(),
		Namespace: pg.GetNamespace(),
		Status: firstNonEmpty(
			getNestedText(pg.Object, "status", "phase"),
			getNestedText(pg.Object, "status", "state"),
			"-",
		),
		MinMember: firstNonEmpty(
			getNestedText(pg.Object, "spec", "minMember"),
			getNestedText(pg.Object, "spec", "minMemberCount"),
			"-",
		),
		Queue: firstNonEmpty(
			getNestedText(pg.Object, "spec", "queue"),
			getNestedText(pg.Object, "spec", "queueName"),
			"-",
		),
		CreatedAt:      pg.GetCreationTimestamp().Local().Format(time.RFC3339),
		StatusMessages: extractPodGroupMessages(pg),
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

func (s *JobService) resolveJobFromPods(ctx context.Context, identifier string, jobLookupErr error) (*jobIdentity, []corev1.Pod, error) {
	allPods, err := s.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("%v; fallback list pods failed: %w", jobLookupErr, err)
	}

	candidates := make([]corev1.Pod, 0)
	for _, pod := range allPods.Items {
		if podMatchesIdentifier(pod, identifier) {
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
		keys := make([]string, 0, len(groups))
		for key := range groups {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return nil, nil, fmt.Errorf("multiple pod-backed jobs matched %q: %s", identifier, strings.Join(keys, ", "))
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
		if pod, err := s.findSinglePodByName(ctx, id); err == nil && pod != nil {
			return pod, nil
		}
		if pod, err := s.findSinglePodByJobName(ctx, id); err == nil && pod != nil {
			return pod, nil
		}
	}

	allPods, err := s.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("fallback list pods: %w", err)
	}

	matched := make([]corev1.Pod, 0)
	for _, pod := range allPods.Items {
		if podMatchesIdentifier(pod, id) {
			matched = append(matched, pod)
		}
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("host cluster pod for %q not found", id)
	}

	groups := groupPodsByDerivedJob(matched)
	if len(groups) > 1 {
		keys := make([]string, 0, len(groups))
		for key := range groups {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("multiple pod-backed jobs matched %q: %s", id, strings.Join(keys, ", "))
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

func derivePodGroupName(jobName string, jobUID string) string {
	jobName = strings.TrimSpace(jobName)
	jobUID = strings.TrimSpace(jobUID)
	if jobName == "" || jobUID == "" {
		return ""
	}
	return jobName + "-" + jobUID
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
		Name:         deriveJobName(pod),
		Namespace:    deriveLogicalNamespace(pod),
		UID:          deriveJobUID(pod),
		Submitter:    firstNonEmpty(pod.Labels["lepton.sensetime.com/submitter"], pod.Annotations["lepton.sensetime.com/submitter"], "-"),
		PodGroupName: firstNonEmpty(pod.Annotations["scheduling.k8s.io/group-name"], pod.Labels["scheduling.k8s.io/group-name"]),
		VClusterName: firstNonEmpty(pod.Annotations["vcluster.loft.sh/vcluster-name"], pod.Labels["vcluster.loft.sh/vcluster-name"]),
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

func extractPodGroupStatus(obj *unstructured.Unstructured) (string, []string) {
	return firstNonEmpty(
			getNestedText(obj.Object, "status", "phase"),
			getNestedText(obj.Object, "status", "state"),
			"-",
		),
		extractPodGroupMessages(obj)
}

func extractPodGroupPhase(obj *unstructured.Unstructured) string {
	return firstNonEmpty(
		getNestedText(obj.Object, "status", "phase"),
		getNestedText(obj.Object, "status", "state"),
		"-",
	)
}

func extractPodGroupMessages(obj *unstructured.Unstructured) []string {
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found || len(conditions) == 0 {
		return []string{"no podgroup status message"}
	}

	messages := make([]string, 0, len(conditions))
	seen := make(map[string]struct{})
	for i := len(conditions) - 1; i >= 0; i-- {
		condition, ok := conditions[i].(map[string]any)
		if !ok {
			continue
		}
		message := strings.TrimSpace(fmt.Sprintf("%v", condition["message"]))
		reason := strings.TrimSpace(fmt.Sprintf("%v", condition["reason"]))
		combined := joinMessageReason(message, reason)
		if combined == "" {
			continue
		}
		if _, exists := seen[combined]; exists {
			continue
		}
		seen[combined] = struct{}{}
		messages = append(messages, combined)
	}
	if len(messages) == 0 {
		return []string{"no podgroup status message"}
	}
	return messages
}

func podHasRunnableLogs(pod corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodSucceeded
}

func isPodReady(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func looksLikeUUID(value string) bool {
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

func shouldLookupJobByPlatform(identifier string) bool {
	id := strings.TrimSpace(identifier)
	if id == "" {
		return false
	}
	if looksLikeUUID(id) {
		return false
	}
	lowerID := strings.ToLower(id)
	if strings.Contains(lowerID, "-worker-") || strings.Contains(lowerID, "-master-") || strings.Contains(lowerID, "-launcher-") {
		return false
	}
	return true
}

func extractJobSpecDetails(job *unstructured.Unstructured) ([]string, []VolumeClaimRef) {
	tasks, found, err := unstructured.NestedSlice(job.Object, "spec", "tasks")
	if err != nil || !found {
		return nil, nil
	}

	secretSet := make(map[string]struct{})
	claimSet := make(map[string]VolumeClaimRef)

	for _, task := range tasks {
		taskMap, ok := task.(map[string]any)
		if !ok {
			continue
		}
		spec, found, err := unstructured.NestedMap(taskMap, "template", "spec")
		if err != nil || !found {
			continue
		}
		collectPodSpecDetails(spec, secretSet, claimSet)
	}

	return normalizeSpecDetails(secretSet, claimSet)
}

func extractPodSpecDetailsFromPods(pods []corev1.Pod) ([]string, []VolumeClaimRef) {
	secretSet := make(map[string]struct{})
	claimSet := make(map[string]VolumeClaimRef)

	for _, pod := range pods {
		for _, secret := range pod.Spec.ImagePullSecrets {
			if strings.TrimSpace(secret.Name) != "" {
				secretSet[secret.Name] = struct{}{}
			}
		}
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim == nil {
				continue
			}
			claimSet[volume.Name] = VolumeClaimRef{
				Name:      volume.Name,
				ClaimName: volume.PersistentVolumeClaim.ClaimName,
			}
		}
	}

	return normalizeSpecDetails(secretSet, claimSet)
}

func collectPodSpecDetails(spec map[string]any, secretSet map[string]struct{}, claimSet map[string]VolumeClaimRef) {
	if imagePullSecrets, found, err := unstructured.NestedSlice(spec, "imagePullSecrets"); err == nil && found {
		for _, secret := range imagePullSecrets {
			secretMap, ok := secret.(map[string]any)
			if !ok {
				continue
			}
			name := strings.TrimSpace(fmt.Sprintf("%v", secretMap["name"]))
			if name != "" {
				secretSet[name] = struct{}{}
			}
		}
	}

	if volumes, found, err := unstructured.NestedSlice(spec, "volumes"); err == nil && found {
		for _, volume := range volumes {
			volumeMap, ok := volume.(map[string]any)
			if !ok {
				continue
			}
			pvc, ok := volumeMap["persistentVolumeClaim"].(map[string]any)
			if !ok {
				continue
			}
			name := strings.TrimSpace(fmt.Sprintf("%v", volumeMap["name"]))
			claimName := strings.TrimSpace(fmt.Sprintf("%v", pvc["claimName"]))
			if name == "" || claimName == "" {
				continue
			}
			claimSet[name] = VolumeClaimRef{Name: name, ClaimName: claimName}
		}
	}
}

func normalizeSpecDetails(secretSet map[string]struct{}, claimSet map[string]VolumeClaimRef) ([]string, []VolumeClaimRef) {
	secrets := make([]string, 0, len(secretSet))
	for secret := range secretSet {
		secrets = append(secrets, secret)
	}
	sort.Strings(secrets)

	claims := make([]VolumeClaimRef, 0, len(claimSet))
	for _, claim := range claimSet {
		claims = append(claims, claim)
	}
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].Name == claims[j].Name {
			return claims[i].ClaimName < claims[j].ClaimName
		}
		return claims[i].Name < claims[j].Name
	})

	return secrets, claims
}

func (s *JobService) resolveImagePullSecretsFromPlatform(ctx context.Context, vclusterName string, namespace string, secretNames []string) []string {
	if s.vcClient == nil || strings.TrimSpace(vclusterName) == "" || strings.TrimSpace(namespace) == "" {
		return secretNames
	}

	resolved := make([]string, 0, len(secretNames))
	for _, secretName := range secretNames {
		secret, err := s.vcClient.GetSecret(ctx, vclusterName, namespace, secretName)
		if err != nil {
			resolved = append(resolved, secretName)
			continue
		}
		resolved = append(resolved, formatImagePullSecret(secretName, secret))
	}
	return resolved
}

func (s *JobService) resolveImagePullSecrets(ctx context.Context, identity *jobIdentity, secretNames []string) []string {
	hostNamespace := ""
	vclusterName := ""
	namespace := ""
	if identity != nil {
		hostNamespace = strings.TrimSpace(identity.HostNamespace)
		vclusterName = strings.TrimSpace(identity.VClusterName)
		namespace = strings.TrimSpace(identity.Namespace)
	}

	if hostNamespace != "" {
		return s.resolveImagePullSecretsFromKube(ctx, hostNamespace, secretNames)
	}
	if vclusterName != "" && namespace != "" {
		return s.resolveImagePullSecretsFromPlatform(ctx, vclusterName, namespace, secretNames)
	}
	return secretNames
}

func (s *JobService) resolveImagePullSecretsFromKube(ctx context.Context, namespace string, secretNames []string) []string {
	if strings.TrimSpace(namespace) == "" {
		return secretNames
	}

	resolved := make([]string, 0, len(secretNames))
	for _, secretName := range secretNames {
		secret, err := s.clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			resolved = append(resolved, secretName)
			continue
		}
		resolved = append(resolved, formatImagePullSecret(secretName, secret))
	}
	return resolved
}

func formatImagePullSecret(secretName string, secret *corev1.Secret) string {
	creds := decodeDockerSecret(secret)
	if len(creds) == 0 {
		return secretName
	}
	return fmt.Sprintf("%s | %s", secretName, strings.Join(creds, "; "))
}

func decodeDockerSecret(secret *corev1.Secret) []string {
	credentials := decodeDockerCredentials(secret)
	results := make([]string, 0, len(credentials))
	for _, credential := range credentials {
		if credential.Registry != "" {
			results = append(results, fmt.Sprintf("%s username: %s password: %s", credential.Registry, credential.Username, credential.Password))
		} else {
			results = append(results, fmt.Sprintf("username: %s password: %s", credential.Username, credential.Password))
		}
	}
	return results
}

func decodeDockerCredentials(secret *corev1.Secret) []dockerCredential {
	if secret == nil {
		return nil
	}

	type dockerConfigEntry struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Auth     string `json:"auth"`
	}
	type dockerConfigJSON struct {
		Auths map[string]dockerConfigEntry `json:"auths"`
	}

	var raw []byte
	switch {
	case len(secret.Data[corev1.DockerConfigJsonKey]) > 0:
		raw = secret.Data[corev1.DockerConfigJsonKey]
	case len(secret.Data[corev1.DockerConfigKey]) > 0:
		raw = secret.Data[corev1.DockerConfigKey]
	default:
		return nil
	}

	var payload dockerConfigJSON
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}

	registries := make([]string, 0, len(payload.Auths))
	for registry := range payload.Auths {
		registries = append(registries, registry)
	}
	sort.Strings(registries)

	results := make([]dockerCredential, 0, len(registries))
	for _, registry := range registries {
		entry := payload.Auths[registry]
		username := strings.TrimSpace(entry.Username)
		password := strings.TrimSpace(entry.Password)
		if (username == "" || password == "") && strings.TrimSpace(entry.Auth) != "" {
			decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
			if err == nil {
				parts := strings.SplitN(string(decoded), ":", 2)
				if len(parts) == 2 {
					if username == "" {
						username = parts[0]
					}
					if password == "" {
						password = parts[1]
					}
				}
			}
		}
		if username == "" && password == "" {
			continue
		}
		results = append(results, dockerCredential{
			Registry: registry,
			Username: username,
			Password: password,
		})
	}

	return results
}

func joinMessageReason(message string, reason string) string {
	message = strings.TrimSpace(message)
	reason = strings.TrimSpace(reason)
	switch {
	case message == "" && reason == "":
		return ""
	case message == "":
		return reason
	case reason == "", strings.EqualFold(message, reason):
		return message
	default:
		return fmt.Sprintf("%s | %s", message, reason)
	}
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

func boolToYesNo(value bool) string {
	if value {
		return "Yes"
	}
	return "No"
}

func firstPVCConditionMessage(pvc *corev1.PersistentVolumeClaim) string {
	if pvc == nil {
		return ""
	}
	for i := len(pvc.Status.Conditions) - 1; i >= 0; i-- {
		message := strings.TrimSpace(pvc.Status.Conditions[i].Message)
		reason := strings.TrimSpace(pvc.Status.Conditions[i].Reason)
		if combined := joinMessageReason(message, reason); combined != "" {
			return combined
		}
	}
	return ""
}

func classifyPVCErrorMessage(err error) string {
	if err == nil {
		return "-"
	}
	message := strings.TrimSpace(err.Error())
	if strings.Contains(message, "persistentvolumeclaims") && strings.Contains(message, "not found") {
		return "pvc 不存在于当前分区"
	}
	return message
}

func hasFailedSecretCheck(items []SecretCheckItem) bool {
	for _, item := range items {
		if strings.EqualFold(item.Status, "FAIL") {
			return true
		}
	}
	return false
}

func hasErroredSecretCheck(items []SecretCheckItem) bool {
	for _, item := range items {
		if strings.EqualFold(item.Status, "ERROR") {
			return true
		}
	}
	return false
}

func (s *JobService) checkImagePullSecrets(ctx context.Context, identity *jobIdentity, secretNames []string) []SecretCheckItem {
	if len(secretNames) == 0 {
		return nil
	}

	namespace := ""
	useHostSecret := false
	if identity != nil && strings.TrimSpace(identity.HostNamespace) != "" {
		namespace = identity.HostNamespace
		useHostSecret = true
	} else if identity != nil {
		namespace = identity.Namespace
	}

	results := make([]SecretCheckItem, 0, len(secretNames))
	for _, secretName := range secretNames {
		var secret *corev1.Secret
		var err error

		switch {
		case useHostSecret:
			secret, err = s.clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
		case s.vcClient != nil && identity != nil && strings.TrimSpace(identity.VClusterName) != "" && strings.TrimSpace(namespace) != "":
			secret, err = s.vcClient.GetSecret(ctx, identity.VClusterName, namespace, secretName)
		default:
			err = fmt.Errorf("unable to determine secret namespace")
		}
		if err != nil {
			results = append(results, SecretCheckItem{
				SecretName: secretName,
				Status:     "FAIL",
				Message:    err.Error(),
			})
			continue
		}

		credentials := decodeDockerCredentials(secret)
		if len(credentials) == 0 {
			results = append(results, SecretCheckItem{
				SecretName: secretName,
				Status:     "FAIL",
				Message:    "secret does not contain docker registry credentials",
			})
			continue
		}

		credential := credentials[0]
		registry := strings.TrimSpace(credential.Registry)
		if registry == "" {
			registry = defaultRegistryHost
		}
		if err := verifyDockerLogin(ctx, registry, credential.Username, credential.Password); err != nil {
			status := "FAIL"
			if isDockerUnavailableError(err) {
				status = "ERROR"
			}
			results = append(results, SecretCheckItem{
				SecretName: secretName,
				Registry:   registry,
				Username:   credential.Username,
				Password:   credential.Password,
				Status:     status,
				Message:    err.Error(),
			})
			continue
		}

		results = append(results, SecretCheckItem{
			SecretName: secretName,
			Registry:   registry,
			Username:   credential.Username,
			Password:   credential.Password,
			Status:     "OK",
			Message:    "docker login succeeded",
		})
	}

	return results
}

func verifyDockerLogin(ctx context.Context, registry string, username string, password string) error {
	if strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
		return fmt.Errorf("missing username or password")
	}

	tmpDir, err := os.MkdirTemp("", "rayctl-docker-login-*")
	if err != nil {
		return fmt.Errorf("create temp docker config: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	cmd := exec.CommandContext(ctx, "docker", "--config", tmpDir, "login", registry, "--username", username, "--password-stdin")
	cmd.Stdin = strings.NewReader(password)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf(message)
	}
	return nil
}

func isDockerUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "executable file not found") || strings.Contains(message, "no such file or directory")
}
