package service

import (
	"context"
	"errors"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"rayctl/internal/podevidence"
	"sort"
	"strings"
	"sync"
	"time"
)

func (s *JobService) GetJobs(ctx context.Context, identifier string) ([]*JobGetResult, error) {
	return s.GetJobsWithLogs(ctx, identifier, false)
}

func (s *JobService) GetJobsWithLogs(ctx context.Context, identifier string, includeLogs bool) ([]*JobGetResult, error) {
	result, err := s.getJob(ctx, identifier, includeLogs)
	if err == nil {
		return []*JobGetResult{result}, nil
	}

	var ambiguousErr *ambiguousJobMatchError
	if !errors.As(err, &ambiguousErr) {
		return nil, err
	}

	results := make([]*JobGetResult, 0, len(ambiguousErr.Matches))
	seen := make(map[string]struct{}, len(ambiguousErr.Matches))
	for _, match := range ambiguousErr.Matches {
		key := strings.Join([]string{
			strings.TrimSpace(match.VClusterName),
			strings.TrimSpace(match.Namespace),
			strings.TrimSpace(match.Name),
			strings.TrimSpace(match.UID),
			strings.TrimSpace(match.HostNamespace),
			strings.TrimSpace(match.HostJobName),
		}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		jobResult, getErr := s.getJobByIdentity(ctx, match, includeLogs)
		if getErr != nil {
			return nil, getErr
		}
		results = append(results, jobResult)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].VClusterName != results[j].VClusterName {
			return results[i].VClusterName < results[j].VClusterName
		}
		if results[i].Namespace != results[j].Namespace {
			return results[i].Namespace < results[j].Namespace
		}
		if results[i].Name != results[j].Name {
			return results[i].Name < results[j].Name
		}
		return results[i].UID < results[j].UID
	})

	return results, nil
}

func (s *JobService) GetJob(ctx context.Context, identifier string) (*JobGetResult, error) {
	return s.getJob(ctx, identifier, false)
}

func (s *JobService) getJob(ctx context.Context, identifier string, includeLogs bool) (*JobGetResult, error) {
	startedAt := time.Now()
	if s.vcClient != nil {
		if result, err := s.getJobViaPlatform(ctx, identifier, includeLogs); err == nil {
			result.Timings.Total = time.Since(startedAt)
			return result, nil
		} else {
			var ambiguousErr *ambiguousJobMatchError
			if errors.As(err, &ambiguousErr) {
				return nil, err
			}
		}
	}

	locateBegin := time.Now()
	identity, pods, job, err := s.resolveJobInCurrentCluster(ctx, identifier)
	locateDuration := time.Since(locateBegin)
	if err != nil {
		return nil, err
	}

	return s.buildJobGetResultFromCurrentCluster(ctx, identifier, *identity, pods, job, locateDuration, startedAt, includeLogs)
}

func (s *JobService) getJobByIdentity(ctx context.Context, identity jobIdentity, includeLogs bool) (*JobGetResult, error) {
	startedAt := time.Now()
	if s.vcClient != nil && strings.TrimSpace(identity.VClusterName) != "" {
		return s.getJobViaPlatformByIdentity(ctx, identity, identity.Name, 0, startedAt, includeLogs)
	}

	hostNamespace := strings.TrimSpace(identity.HostNamespace)
	hostJobName := strings.TrimSpace(identity.HostJobName)
	if hostNamespace == "" || hostJobName == "" {
		return nil, fmt.Errorf("unable to fetch job %q exactly from current cluster", identity.Name)
	}

	job, err := s.dynamicClient.Resource(volcanoJobGVR).Namespace(hostNamespace).Get(ctx, hostJobName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	resolvedIdentity, pods, err := s.resolveJobIdentityFromJobObject(ctx, job)
	if err != nil {
		return nil, err
	}

	return s.buildJobGetResultFromCurrentCluster(ctx, identity.Name, *resolvedIdentity, pods, job, 0, startedAt, includeLogs)
}

func (s *JobService) buildJobGetResultFromCurrentCluster(ctx context.Context, identifier string, identity jobIdentity, pods []corev1.Pod, job *unstructured.Unstructured, locateDuration time.Duration, startedAt time.Time, includeLogs bool) (*JobGetResult, error) {
	status := "-"
	terminal := false
	if job != nil {
		status = extractVolcanoJobPhase(job)
		terminal = isTerminalVolcanoJobPhase(status)
	}

	stage := ""
	instruction := ""
	diagnosis := []string(nil)
	secretChecks := []SecretCheckItem(nil)
	detailEvidence := []CheckEvidenceItem(nil)
	diagnosisBegin := time.Now()
	if shouldAttachJobCheckSummary(status, terminal) {
		if checkResult, err := s.CheckJob(ctx, identifier); err == nil && checkResult != nil {
			stage = strings.TrimSpace(checkResult.Stage)
			instruction = strings.TrimSpace(checkResult.Instruction)
			if len(checkResult.Diagnosis) > 0 {
				diagnosis = append([]string{}, checkResult.Diagnosis...)
			}
			if len(checkResult.SecretChecks) > 0 {
				secretChecks = append([]SecretCheckItem{}, checkResult.SecretChecks...)
			}
			switch stage {
			case "scheduling":
				if len(checkResult.PodGroupEvidence) > 0 {
					detailEvidence = append([]CheckEvidenceItem{}, checkResult.PodGroupEvidence...)
				}
			case "startup", "failed":
				if len(checkResult.PodEvidence) > 0 {
					detailEvidence = append([]CheckEvidenceItem{}, checkResult.PodEvidence...)
				}
			}
		}
	}
	diagnosisDuration := time.Since(diagnosisBegin)

	inspectPod := chooseInspectPod(pods)
	var recentLogLines []string
	var podEvidence *podevidence.Result
	var kubeLogsDuration time.Duration

	if includeLogs {
		evidenceBegin := time.Now()
		podEvidence = s.collectATEEvidence(ctx, identity, pods, false)
		kubeLogsDuration = time.Since(evidenceBegin)
		recentLogLines = ateRecentEvidenceLogs(podEvidence)
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
			Reason:    pod.Status.Reason, Message: pod.Status.Message,
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
	enrichmentBegin := time.Now()
	imagePullSecrets, pvcRefs := extractPodSpecDetailsFromPods(pods)
	var enrichmentWG sync.WaitGroup
	enrichmentWG.Add(2)
	go func() {
		defer enrichmentWG.Done()
		pvcRefs = s.resolveVolumeClaimRefs(ctx, &identity, pvcRefs)
	}()
	go func() {
		defer enrichmentWG.Done()
		imagePullSecrets = s.resolveImagePullSecretsFromKube(ctx, firstNonEmpty(identity.HostNamespace, identity.Namespace), imagePullSecrets)
	}()
	enrichmentWG.Wait()
	stage, diagnosis = ensurePVCGetDiagnosis(status, terminal, stage, diagnosis, pvcRefs)
	createdAt, jobStartedAt, endedAt := extractJobLifecycleTimes(job, pods, status)
	enrichmentDuration := time.Since(enrichmentBegin)

	return &JobGetResult{
		Name:                   identity.Name,
		WorkloadType:           jobWorkloadType(job, pods),
		Namespace:              identity.Namespace,
		HostNamespace:          identity.HostNamespace,
		UID:                    identity.UID,
		Status:                 status,
		CreatedAt:              createdAt,
		StartedAt:              jobStartedAt,
		EndedAt:                endedAt,
		Terminal:               terminal,
		Stage:                  stage,
		Instruction:            instruction,
		Diagnosis:              diagnosis,
		VClusterName:           dashIfEmpty(identity.VClusterName),
		Submitter:              identity.Submitter,
		PodGroupName:           dashIfEmpty(identity.PodGroupName),
		ImagePullSecrets:       imagePullSecrets,
		SecretChecks:           secretChecks,
		PersistentVolumeClaims: pvcRefs,
		Pods:                   resultPods,
		Nodes:                  nodes,
		InspectPod:             inspectPodName,
		CheckEvidence:          detailEvidence,
		RecentLogLines:         recentLogLines,
		PodEvidence:            podEvidence,
		ResourceSpecs:          jobResourceSpecsFromVolcanoJob(job),
		Timings: JobGetTimings{
			Locate:     locateDuration,
			Diagnosis:  diagnosisDuration,
			Enrichment: enrichmentDuration,
			KubeLogs:   kubeLogsDuration,
			Format:     formatDuration,
			Total:      time.Since(startedAt),
		},
	}, nil
}

func (s *JobService) getJobViaPlatform(ctx context.Context, identifier string, includeLogs bool) (*JobGetResult, error) {
	startedAt := time.Now()
	locateBegin := time.Now()
	identity, err := s.locateJobForPlatform(ctx, identifier)
	locateDuration := time.Since(locateBegin)
	if err != nil {
		return nil, err
	}

	return s.getJobViaPlatformByIdentity(ctx, *identity, identifier, locateDuration, startedAt, includeLogs)
}

func (s *JobService) getJobViaPlatformByIdentity(ctx context.Context, identity jobIdentity, _ string, locateDuration time.Duration, startedAt time.Time, includeLogs bool) (*JobGetResult, error) {
	jobBegin := time.Now()
	var job *unstructured.Unstructured
	var err error
	if strings.TrimSpace(identity.ProfileName) != "" {
		job, err = s.vcClient.GetVolcanoJobForProfile(ctx, identity.ProfileName, identity.VClusterName, identity.Namespace, identity.Name)
	} else {
		job, err = s.vcClient.GetVolcanoJob(ctx, identity.VClusterName, identity.Namespace, identity.Name)
	}
	platformJobDuration := time.Since(jobBegin)
	if err != nil {
		return nil, err
	}

	podsBegin := time.Now()
	var pods []corev1.Pod
	if strings.TrimSpace(identity.ProfileName) != "" {
		pods, err = s.vcClient.ListJobPodsForProfile(ctx, identity.ProfileName, identity.VClusterName, identity.Namespace, identity.Name)
	} else {
		pods, err = s.vcClient.ListJobPods(ctx, identity.VClusterName, identity.Namespace, identity.Name)
	}
	platformPodsDuration := time.Since(podsBegin)
	if err != nil {
		return nil, err
	}

	identity.UID = firstNonEmpty(identity.UID, string(job.GetUID()))
	if strings.TrimSpace(identity.PodGroupName) == "" && strings.TrimSpace(identity.UID) != "" {
		identity.PodGroupName = fmt.Sprintf("%s-%s", identity.Name, identity.UID)
	}
	status := extractVolcanoJobPhase(job)
	terminal := isTerminalVolcanoJobPhase(status)
	stage := ""
	instruction := ""
	diagnosis := []string(nil)
	secretChecks := []SecretCheckItem(nil)
	detailEvidence := []CheckEvidenceItem(nil)
	diagnosisBegin := time.Now()
	if shouldAttachJobCheckSummary(status, terminal) {
		if checkResult, err := s.checkPlatformJob(ctx, &identity, job, pods); err == nil && checkResult != nil {
			stage = strings.TrimSpace(checkResult.Stage)
			instruction = strings.TrimSpace(checkResult.Instruction)
			if len(checkResult.Diagnosis) > 0 {
				diagnosis = append([]string{}, checkResult.Diagnosis...)
			}
			if len(checkResult.SecretChecks) > 0 {
				secretChecks = append([]SecretCheckItem{}, checkResult.SecretChecks...)
			}
			switch stage {
			case "scheduling":
				if len(checkResult.PodGroupEvidence) > 0 {
					detailEvidence = append([]CheckEvidenceItem{}, checkResult.PodGroupEvidence...)
				}
			case "startup", "failed":
				if len(checkResult.PodEvidence) > 0 {
					detailEvidence = append([]CheckEvidenceItem{}, checkResult.PodEvidence...)
				}
			}
		}
	}
	diagnosisDuration := time.Since(diagnosisBegin)
	identity.Submitter = firstNonEmpty(
		identity.Submitter,
		getNestedString(job.Object, "metadata", "labels", "lepton.sensetime.com/submitter"),
		getNestedString(job.Object, "metadata", "annotations", "lepton.sensetime.com/submitter"),
		"-",
	)
	inspectPod := chooseInspectPod(pods)
	var recentLogLines []string
	var podEvidence *podevidence.Result
	var platformLogsDuration time.Duration
	if includeLogs {
		evidenceBegin := time.Now()
		podEvidence = s.collectATEEvidence(ctx, identity, pods, true)
		platformLogsDuration = time.Since(evidenceBegin)
		recentLogLines = ateRecentEvidenceLogs(podEvidence)
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
			Reason:    pod.Status.Reason, Message: pod.Status.Message,
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
	enrichmentBegin := time.Now()
	imagePullSecrets, pvcRefs := extractJobSpecDetails(job)
	var enrichmentWG sync.WaitGroup
	enrichmentWG.Add(2)
	go func() {
		defer enrichmentWG.Done()
		pvcRefs = s.resolveVolumeClaimRefs(ctx, &identity, pvcRefs)
	}()
	go func() {
		defer enrichmentWG.Done()
		imagePullSecrets = s.resolveImagePullSecrets(ctx, &identity, imagePullSecrets)
	}()
	enrichmentWG.Wait()
	stage, diagnosis = ensurePVCGetDiagnosis(status, terminal, stage, diagnosis, pvcRefs)
	createdAt, jobStartedAt, endedAt := extractJobLifecycleTimes(job, pods, status)
	enrichmentDuration := time.Since(enrichmentBegin)

	return &JobGetResult{
		Name:                   identity.Name,
		WorkloadType:           jobWorkloadType(job, pods),
		Namespace:              identity.Namespace,
		HostNamespace:          identity.HostNamespace,
		UID:                    identity.UID,
		Status:                 status,
		CreatedAt:              createdAt,
		StartedAt:              jobStartedAt,
		EndedAt:                endedAt,
		Terminal:               terminal,
		Stage:                  stage,
		Instruction:            instruction,
		Diagnosis:              diagnosis,
		VClusterName:           dashIfEmpty(identity.VClusterName),
		Submitter:              identity.Submitter,
		PodGroupName:           dashIfEmpty(identity.PodGroupName),
		ImagePullSecrets:       imagePullSecrets,
		SecretChecks:           secretChecks,
		PersistentVolumeClaims: pvcRefs,
		Pods:                   resultPods,
		Nodes:                  nodes,
		InspectPod:             inspectPodName,
		CheckEvidence:          detailEvidence,
		RecentLogLines:         recentLogLines,
		PodEvidence:            podEvidence,
		ResourceSpecs:          jobResourceSpecsFromVolcanoJob(job),
		Timings: JobGetTimings{
			Locate:       locateDuration,
			PlatformJob:  platformJobDuration,
			PlatformPods: platformPodsDuration,
			PlatformLogs: platformLogsDuration,
			Diagnosis:    diagnosisDuration,
			Enrichment:   enrichmentDuration,
			Format:       formatDuration,
			Total:        time.Since(startedAt),
		},
	}, nil
}

func jobWorkloadType(job *unstructured.Unstructured, pods []corev1.Pod) string {
	if job != nil {
		if value := strings.TrimSpace(getNestedString(job.Object, "metadata", "labels", sspWorkloadTypeLabel)); value != "" {
			return strings.ToLower(value)
		}
	}
	return workloadTypeFromPods(pods)
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

func extractJobLifecycleTimes(job *unstructured.Unstructured, pods []corev1.Pod, status string) (string, string, string) {
	createdAt := time.Time{}
	startedAt := time.Time{}
	endedAt := time.Time{}
	if job != nil {
		createdAt = job.GetCreationTimestamp().Time
		conditions, _, _ := unstructured.NestedSlice(job.Object, "status", "conditions")
		for _, raw := range conditions {
			condition, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			phase := strings.ToLower(firstNonEmpty(
				getNestedText(condition, "phase"),
				getNestedText(condition, "type"),
				getNestedText(condition, "status"),
			))
			transition := parseJobTimestamp(firstNonEmpty(
				getNestedText(condition, "lastTransitionTime"),
				getNestedText(condition, "transitionTime"),
				getNestedText(condition, "lastUpdateTime"),
			))
			if phase == "running" && !transition.IsZero() && (startedAt.IsZero() || transition.Before(startedAt)) {
				startedAt = transition
			}
			if isTerminalVolcanoJobPhase(phase) && transition.After(endedAt) {
				endedAt = transition
			}
		}
		stateTransition := parseJobTimestamp(getNestedText(job.Object, "status", "state", "lastTransitionTime"))
		if strings.EqualFold(status, "Running") && startedAt.IsZero() {
			startedAt = stateTransition
		}
		if isTerminalVolcanoJobPhase(status) && endedAt.IsZero() {
			endedAt = stateTransition
		}
	}

	for _, pod := range pods {
		if pod.Status.StartTime != nil {
			value := pod.Status.StartTime.Time
			if startedAt.IsZero() || value.Before(startedAt) {
				startedAt = value
			}
		}
		if !isTerminalVolcanoJobPhase(status) {
			continue
		}
		statuses := append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...)
		statuses = append(statuses, pod.Status.ContainerStatuses...)
		for _, container := range statuses {
			if container.State.Terminated == nil {
				continue
			}
			value := container.State.Terminated.FinishedAt.Time
			if value.After(endedAt) {
				endedAt = value
			}
		}
	}

	return formatJobTime(createdAt), formatJobTime(startedAt), formatJobTime(endedAt)
}

func parseJobTimestamp(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func formatJobTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.In(utcPlus8).Format("2006-01-02 15:04:05")
}
