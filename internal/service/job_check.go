package service

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sort"
	"strings"
)

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
	return s.checkPlatformJob(ctx, identity, job, pods)
}

func (s *JobService) checkPlatformJob(ctx context.Context, identity *jobIdentity, job *unstructured.Unstructured, pods []corev1.Pod) (*JobCheckResult, error) {
	podChecks := make([]JobPodCheckItem, 0, len(pods))
	readyPodCount := 0
	assignedPodCount := 0
	runningPodCount := 0
	failedPods := make([]JobPodCheckItem, 0)
	for _, pod := range pods {
		ready := isPodReady(pod)
		if ready {
			readyPodCount++
		}
		if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodSucceeded {
			runningPodCount++
		}
		if strings.TrimSpace(pod.Spec.NodeName) != "" {
			assignedPodCount++
		}
		reason := podFailureReason(pod)
		item := JobPodCheckItem{
			Name:     pod.Name,
			Phase:    dashIfEmpty(string(pod.Status.Phase)),
			Ready:    boolToYesNo(ready),
			NodeName: dashIfEmpty(pod.Spec.NodeName),
			Reason:   dashIfEmpty(reason),
		}
		podChecks = append(podChecks, item)
		if isFailedPod(pod) {
			failedPods = append(failedPods, item)
		}
	}
	sort.Slice(podChecks, func(i, j int) bool {
		return podChecks[i].Name < podChecks[j].Name
	})

	_, pvcRefs := extractJobSpecDetails(job)
	pvcRefs = s.resolveVolumeClaimRefs(ctx, identity, pvcRefs)
	failedPVCChecks := make([]PVCCheckItem, 0, len(pvcRefs))
	pendingPVCs := 0
	duplicatePVRefs := duplicatePVRefs(pvcRefs)
	for _, pvcRef := range pvcRefs {
		status := "-"
		message := "-"
		if pvcRef.PVName != "" {
			status = "Bound"
			message = pvcRef.PVName
		}
		if hostNamespace := strings.TrimSpace(identity.HostNamespace); hostNamespace != "" {
			pvc, pvcErr := s.clientset.CoreV1().PersistentVolumeClaims(hostNamespace).Get(ctx, pvcRef.ClaimName, metav1.GetOptions{})
			if pvcErr != nil {
				message = classifyPVCErrorMessage(pvcErr)
			} else {
				status = dashIfEmpty(string(pvc.Status.Phase))
				pvcRef.PVName = firstNonEmpty(strings.TrimSpace(pvc.Spec.VolumeName), pvcRef.PVName)
				message = dashIfEmpty(firstNonEmpty(firstPVCConditionMessage(pvc), pvcRef.PVName))
				if pvc.Status.Phase == corev1.ClaimPending {
					pendingPVCs++
					message = "PVC 的 AKSK 错误"
				}
			}
		} else if pvc, pvcErr := s.getPlatformPersistentVolumeClaim(ctx, identity, identity.Namespace, pvcRef.ClaimName); pvcErr != nil {
			message = classifyPVCErrorMessage(pvcErr)
		} else {
			status = dashIfEmpty(string(pvc.Status.Phase))
			pvcRef.PVName = firstNonEmpty(strings.TrimSpace(pvc.Spec.VolumeName), pvcRef.PVName)
			message = dashIfEmpty(firstNonEmpty(firstPVCConditionMessage(pvc), pvcRef.PVName))
			if pvc.Status.Phase == corev1.ClaimPending {
				pendingPVCs++
				message = "PVC 的 AKSK 错误"
			}
		}
		if status != "Bound" {
			failedPVCChecks = append(failedPVCChecks, PVCCheckItem{
				Name:             pvcRef.Name,
				ClaimName:        pvcRef.ClaimName,
				PVName:           pvcRef.PVName,
				BackendPV:        pvcRef.BackendPV,
				DisplayPV:        pvcRef.DisplayPV,
				HostPVCName:      pvcRef.HostPVCName,
				HostPVCNamespace: pvcRef.HostPVCNamespace,
				FrontendVolume:   pvcRef.FrontendVolume,
				Status:           status,
				Message:          message,
			})
		}
	}

	diagnosis := make([]string, 0, 3)
	secretChecks := []SecretCheckItem{}
	stage := "running"
	missingPVCs := 0
	for _, pvc := range failedPVCChecks {
		if pvc.Message == "PVC 在当前集群不存在" {
			missingPVCs++
		}
	}
	imagePullSecrets, _ := extractJobSpecDetails(job)
	secretChecks = s.checkImagePullSecrets(ctx, identity, imagePullSecrets)
	displaySecrets := make([]SecretCheckItem, 0, len(secretChecks))
	for _, secret := range secretChecks {
		if !strings.EqualFold(secret.Status, "OK") {
			displaySecrets = append(displaySecrets, secret)
		}
	}

	if len(failedPods) > 0 {
		stage = "failed"
		diagnosis = append(diagnosis, fmt.Sprintf("任务已经失败，不是还未拉起。失败 Pod: %s。", failedPods[0].Name))
	} else if len(pods) == 0 || assignedPodCount == 0 {
		stage = "scheduling"
		if missingPVCs > 0 {
			diagnosis = append(diagnosis, "存在 PVC 在当前集群不存在，任务因此无法继续调度。")
		} else if pendingPVCs > 0 {
			diagnosis = append(diagnosis, "PVC 仍处于 Pending，当前大概率是 PVC 的 AKSK 错误。")
		} else {
			diagnosis = append(diagnosis, "任务还没有调度到任何 host。")
		}
	} else if runningPodCount > 0 && hasFailedSecretCheck(secretChecks) {
		stage = "running"
		diagnosis = append(diagnosis, "imagePullSecret 错误，但任务当前已经在运行。")
	} else if readyPodCount == 0 {
		stage = "startup"
		if len(duplicatePVRefs) > 0 {
			diagnosis = append(diagnosis, "存在重复的 PV 挂载，当前任务可能卡在 PodInitiating。")
		} else if len(secretChecks) == 0 {
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

	displayPods := make([]JobPodCheckItem, 0, len(podChecks))
	if stage == "startup" || stage == "failed" || (stage == "running" && len(displaySecrets) > 0) {
		for _, pod := range podChecks {
			if stage == "failed" {
				if pod.Reason != "-" || strings.EqualFold(pod.Phase, "Failed") {
					displayPods = append(displayPods, pod)
				}
				continue
			}
			if stage == "running" && len(displaySecrets) > 0 {
				if strings.EqualFold(pod.Phase, "Running") || pod.Ready == "Yes" {
					displayPods = append(displayPods, pod)
				}
				continue
			}
			if pod.Ready != "Yes" {
				displayPods = append(displayPods, pod)
			}
		}
		if len(displayPods) == 0 {
			displayPods = podChecks
		}
	}

	instruction := ""

	podEvidence := make([]CheckEvidenceItem, 0)
	podGroupEvidence := make([]CheckEvidenceItem, 0)
	if stage == "startup" || stage == "failed" {
		diagnosticPods := selectDiagnosticPods(pods, stage)
		eventNamespace := firstNonEmpty(identity.HostNamespace, identity.Namespace)
		if strings.TrimSpace(eventNamespace) != "" {
			for _, pod := range diagnosticPods {
				eventUID := string(pod.UID)
				if strings.TrimSpace(identity.HostNamespace) != "" {
					hostPod, hostPodErr := s.findPodByNameInNamespace(ctx, identity.HostNamespace, pod.Name)
					if hostPodErr == nil && hostPod != nil {
						eventUID = string(hostPod.UID)
					}
				}
				events, eventErr := s.listEventsForObject(ctx, eventNamespace, "Pod", pod.Name, eventUID, 5)
				if eventErr != nil {
					continue
				}
				for _, event := range events {
					if strings.EqualFold(event.Message, "no events") {
						continue
					}
					podEvidence = append(podEvidence, CheckEvidenceItem{
						Source: pod.Name,
						Status: firstNonEmpty(event.Reason, event.Type, "-"),
						Detail: event.Message,
					})
				}
			}
		}
		if len(podEvidence) == 0 && s.vcClient != nil && strings.TrimSpace(identity.VClusterName) != "" && strings.TrimSpace(identity.Namespace) != "" {
			for _, pod := range diagnosticPods {
				podEvents, eventErr := s.vcClient.ListEvents(ctx, identity.VClusterName, identity.Namespace, pod.Name, "")
				if eventErr != nil {
					continue
				}
				for _, event := range formatEventItems(podEvents, 5) {
					if strings.EqualFold(event.Message, "no events") {
						continue
					}
					podEvidence = append(podEvidence, CheckEvidenceItem{
						Source: pod.Name,
						Status: firstNonEmpty(event.Reason, event.Type, "-"),
						Detail: event.Message,
					})
				}
			}
		}
	}
	if stage == "scheduling" && s.vcClient != nil && strings.TrimSpace(identity.VClusterName) != "" && strings.TrimSpace(identity.Namespace) != "" {
		eventName := firstNonEmpty(identity.PodGroupName, identity.Name)
		events, eventErr := s.vcClient.ListEvents(ctx, identity.VClusterName, identity.Namespace, eventName, "")
		if eventErr == nil {
			for _, event := range formatEventItems(events, defaultEventLimit) {
				if strings.EqualFold(event.Message, "no events") {
					continue
				}
				podGroupEvidence = append(podGroupEvidence, CheckEvidenceItem{
					Source: event.Time,
					Status: firstNonEmpty(event.Reason, event.Type, "-"),
					Detail: event.Message,
				})
			}
		}
	}
	if stage == "startup" && len(podEvidence) > 0 && len(diagnosis) > 0 {
		diagnosis[0] = diagnosis[0] + " 请进一步看下方 event 部分判断任务启动失败原因。"
	}
	if stage == "scheduling" && len(podGroupEvidence) > 0 && len(diagnosis) > 0 {
		diagnosis[0] = diagnosis[0] + " 请查看下方 PodGroup event。"
	}

	return &JobCheckResult{
		Name:             identity.Name,
		Namespace:        identity.Namespace,
		UID:              identity.UID,
		PodGroupName:     dashIfEmpty(identity.PodGroupName),
		Stage:            stage,
		Instruction:      instruction,
		Pods:             displayPods,
		PVCs:             append(failedPVCChecks, pvcRefsToChecks(duplicatePVRefs)...),
		SecretChecks:     displaySecrets,
		PodEvidence:      podEvidence,
		PodGroupEvidence: podGroupEvidence,
		Diagnosis:        diagnosis,
	}, nil
}

func selectDiagnosticPods(pods []corev1.Pod, stage string) []corev1.Pod {
	candidates := make([]corev1.Pod, 0, len(pods))
	for _, pod := range pods {
		switch stage {
		case "failed":
			if isFailedPod(pod) {
				candidates = append(candidates, pod)
			}
		case "startup":
			if !isPodReady(pod) {
				candidates = append(candidates, pod)
			}
		}
	}
	inspectPod := chooseInspectPod(candidates)
	if inspectPod == nil {
		return nil
	}
	return []corev1.Pod{*inspectPod}
}

func (s *JobService) findPodByNameInNamespace(ctx context.Context, namespace string, podName string) (*corev1.Pod, error) {
	namespace = strings.TrimSpace(namespace)
	podName = strings.TrimSpace(podName)
	if namespace == "" || podName == "" {
		return nil, fmt.Errorf("namespace and pod name are required")
	}

	pod, err := s.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return pod, nil
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
		podChecks := make([]JobPodCheckItem, 0, len(pods))
		podEvidence := make([]CheckEvidenceItem, 0)
		for _, pod := range pods {
			ready := isPodReady(pod)
			podChecks = append(podChecks, JobPodCheckItem{
				Name:     pod.Name,
				Phase:    dashIfEmpty(string(pod.Status.Phase)),
				Ready:    boolToYesNo(ready),
				NodeName: dashIfEmpty(pod.Spec.NodeName),
				Reason:   dashIfEmpty(podFailureReason(pod)),
			})
			if ready {
				continue
			}
			events, eventErr := s.listEventsForObject(ctx, namespace, "Pod", pod.Name, string(pod.UID), 5)
			if eventErr != nil {
				continue
			}
			for _, event := range events {
				if strings.EqualFold(event.Message, "no events") {
					continue
				}
				podEvidence = append(podEvidence, CheckEvidenceItem{
					Source: pod.Name,
					Status: firstNonEmpty(event.Reason, event.Type, "-"),
					Detail: event.Message,
				})
			}
		}
		sort.Slice(podChecks, func(i, j int) bool {
			return podChecks[i].Name < podChecks[j].Name
		})

		return &JobCheckResult{
			Name:         identity.Name,
			Namespace:    identity.Namespace,
			UID:          identity.UID,
			PodGroupName: dashIfEmpty(identity.PodGroupName),
			Stage:        "startup",
			Pods:         podChecks,
			PodEvidence:  podEvidence,
			Diagnosis: []string{
				"Pod 已经分配到 host，但还没有 Ready。下面附上 Pod 当前状态和最近事件，供你继续排查启动问题。",
			},
		}, nil
	}

	if s.vcClient != nil {
		return nil, fmt.Errorf("defer scheduling diagnosis to platform")
	}

	return &JobCheckResult{
		Name:         identity.Name,
		Namespace:    identity.Namespace,
		UID:          identity.UID,
		PodGroupName: dashIfEmpty(identity.PodGroupName),
		Stage:        "scheduling",
		Diagnosis: []string{
			"当前任务仍在等待调度。",
		},
	}, nil
}

func shouldAttachJobCheckSummary(status string, terminal bool) bool {
	if terminal {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(status), "Running")
}
