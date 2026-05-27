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
	VClusterName  string
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
	PodEvidence         []CheckEvidenceItem
	PodGroupEvidence    []CheckEvidenceItem
	Diagnosis           []string
}

type JobCreateRequest struct {
	Name                string
	Namespace           string
	Submitter           string
	SPBlock             string
	FrameworkType       string
	MasterPort          string
	Replicas            int64
	MinAvailable        int64
	MasterReplicas      int64
	WorkerReplicas      int64
	Image               string
	Command             string
	ImagePullSecret     string
	CPU                 string
	Memory              string
	AcceleratorResource string
	AcceleratorCount    string
	ExtraResourceName   string
	ExtraResourceValue  string
	DataPVCName         string
	AOSSPVCName         string
	SHMSize             string
	MachineType         string
	HostArch            string
	AcceleratorType     string
	UseDefaultNodeSelector bool
	UsePCILinkVolume    bool
	RequireIPCLock      bool
	PriorityClass       string
	Queue               string
}

type VolumeClaimRef struct {
	Name             string
	ClaimName        string
	PVName           string
	BackendPV        string
	DisplayPV        string
	HostPVCName      string
	HostPVCNamespace string
	FrontendVolume   string
}

type PVCCheckItem struct {
	Name             string
	ClaimName        string
	PVName           string
	BackendPV        string
	DisplayPV        string
	HostPVCName      string
	HostPVCNamespace string
	FrontendVolume   string
	Status           string
	Message          string
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
	Reason    string
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

func (s *JobService) BuildJobManifest(req JobCreateRequest) (*unstructured.Unstructured, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("job name is required")
	}
	namespace := strings.TrimSpace(req.Namespace)
	if namespace == "" {
		namespace = "default"
	}
	submitter := strings.TrimSpace(req.Submitter)
	if submitter == "" {
		return nil, fmt.Errorf("submitter is required")
	}
	spBlock := strings.TrimSpace(req.SPBlock)
	frameworkType := normalizeJobFrameworkType(req.FrameworkType)
	masterPort := strings.TrimSpace(req.MasterPort)
	if masterPort == "" {
		return nil, fmt.Errorf("master port is required")
	}
	if req.Replicas <= 0 {
		return nil, fmt.Errorf("replicas must be greater than 0")
	}
	masterReplicas := req.MasterReplicas
	workerReplicas := req.WorkerReplicas
	minAvailable := req.MinAvailable
	isMultiNode := masterReplicas > 0 || workerReplicas > 0 || minAvailable > 1
	if isMultiNode {
		if masterReplicas <= 0 {
			masterReplicas = 1
		}
		if workerReplicas < 0 {
			return nil, fmt.Errorf("worker replicas must be greater than or equal to 0")
		}
		if minAvailable <= 0 {
			minAvailable = masterReplicas + workerReplicas
		}
		if minAvailable <= 0 {
			return nil, fmt.Errorf("minAvailable must be greater than 0")
		}
	} else {
		masterReplicas = 0
		workerReplicas = req.Replicas
		if minAvailable <= 0 {
			minAvailable = 1
		}
	}
	image := strings.TrimSpace(req.Image)
	if image == "" {
		return nil, fmt.Errorf("image is required")
	}
	command := strings.TrimSpace(req.Command)
	if command == "" {
		return nil, fmt.Errorf("command is required")
	}
	cpu := strings.TrimSpace(req.CPU)
	if cpu == "" {
		return nil, fmt.Errorf("cpu is required")
	}
	memory := strings.TrimSpace(req.Memory)
	if memory == "" {
		return nil, fmt.Errorf("memory is required")
	}
	acceleratorResource := strings.TrimSpace(req.AcceleratorResource)
	if acceleratorResource == "" {
		return nil, fmt.Errorf("accelerator resource is required")
	}
	acceleratorCount := strings.TrimSpace(req.AcceleratorCount)
	if acceleratorCount == "" {
		return nil, fmt.Errorf("accelerator count is required")
	}
	dataPVCName := strings.TrimSpace(req.DataPVCName)
	aossPVCName := strings.TrimSpace(req.AOSSPVCName)
	shmSize := strings.TrimSpace(req.SHMSize)
	if shmSize == "" {
		return nil, fmt.Errorf("shm size is required")
	}
	machineType := strings.TrimSpace(req.MachineType)
	if machineType == "" {
		return nil, fmt.Errorf("machine type is required")
	}
	hostArch := strings.TrimSpace(req.HostArch)
	if hostArch == "" {
		return nil, fmt.Errorf("host arch is required")
	}
	acceleratorType := strings.TrimSpace(req.AcceleratorType)
	if acceleratorType == "" {
		return nil, fmt.Errorf("accelerator type is required")
	}
	priorityClass := strings.TrimSpace(req.PriorityClass)
	if priorityClass == "" {
		priorityClass = "normal"
	}
	queue := strings.TrimSpace(req.Queue)
	if queue == "" {
		queue = "default"
	}

	volumeMounts := make([]interface{}, 0, 3)
	volumes := make([]interface{}, 0, 3)
	if dataPVCName != "" {
		volumeMounts = append(volumeMounts, map[string]interface{}{"name": "data-volume", "mountPath": "/data"})
		volumes = append(volumes, map[string]interface{}{
			"name": "data-volume",
			"persistentVolumeClaim": map[string]interface{}{
				"claimName": dataPVCName,
			},
		})
	}
	if aossPVCName != "" {
		volumeMounts = append(volumeMounts, map[string]interface{}{"name": "aoss-volume", "mountPath": "/mnt/test"})
		volumes = append(volumes, map[string]interface{}{
			"name": "aoss-volume",
			"persistentVolumeClaim": map[string]interface{}{
				"claimName": aossPVCName,
			},
		})
	}
	volumeMounts = append(volumeMounts, map[string]interface{}{"name": "shm-data", "mountPath": "/dev/shm"})
	volumes = append(volumes, map[string]interface{}{
		"name": "shm-data",
		"emptyDir": map[string]interface{}{
			"medium":    "Memory",
			"sizeLimit": shmSize,
		},
	})
	if req.UsePCILinkVolume {
		volumeMounts = append(volumeMounts, map[string]interface{}{"name": "pci-link", "mountPath": "/opt/pci_switch_link"})
		volumes = append(volumes, map[string]interface{}{
			"name": "pci-link",
			"hostPath": map[string]interface{}{
				"path": "/opt/pci_switch_link",
			},
		})
	}

	requests := map[string]interface{}{
		"cpu":               cpu,
		"memory":            memory,
		acceleratorResource: acceleratorCount,
	}
	limits := map[string]interface{}{
		"cpu":               cpu,
		"memory":            memory,
		acceleratorResource: acceleratorCount,
	}
	if extraName := strings.TrimSpace(req.ExtraResourceName); extraName != "" {
		extraValue := strings.TrimSpace(req.ExtraResourceValue)
		if extraValue == "" {
			extraValue = "1"
		}
		requests[extraName] = extraValue
		limits[extraName] = extraValue
	}

	buildTaskSpec := func(taskName string, replicas int64, includeCompletePolicy bool) map[string]interface{} {
		container := map[string]interface{}{
			"name":            taskName,
			"image":           image,
			"imagePullPolicy": "IfNotPresent",
			"command":         []interface{}{"bash", "-c"},
			"args":            []interface{}{command},
			"env":             []interface{}{},
			"volumeMounts":    volumeMounts,
			"resources": map[string]interface{}{
				"requests": requests,
				"limits":   limits,
			},
		}
		if req.RequireIPCLock {
			container["securityContext"] = map[string]interface{}{
				"capabilities": map[string]interface{}{
					"add": []interface{}{"IPC_LOCK"},
				},
			}
		}
		spec := map[string]interface{}{
			"containers": []interface{}{container},
			"restartPolicy": "Never",
			"volumes": volumes,
			"affinity": map[string]interface{}{
				"nodeAffinity": map[string]interface{}{
					"requiredDuringSchedulingIgnoredDuringExecution": map[string]interface{}{
						"nodeSelectorTerms": []interface{}{
							map[string]interface{}{
								"matchExpressions": []interface{}{
									map[string]interface{}{
										"key":      "resource.compute.sensecore.cn/machine-type",
										"operator": "In",
										"values":   []interface{}{machineType},
									},
								},
							},
						},
					},
				},
			},
		}
		if req.UseDefaultNodeSelector {
			spec["nodeSelector"] = map[string]interface{}{
				"host-arch":        hostArch,
				"accelerator-type": acceleratorType,
			}
		}
		if secretName := strings.TrimSpace(req.ImagePullSecret); secretName != "" {
			spec["imagePullSecrets"] = []interface{}{
				map[string]interface{}{"name": secretName},
			}
		} else {
			spec["imagePullSecrets"] = nil
		}
		policies := []interface{}{
			map[string]interface{}{
				"event":  "PodEvicted",
				"action": "RestartJob",
			},
		}
		if includeCompletePolicy {
			policies = append(policies, map[string]interface{}{
				"event":  "TaskCompleted",
				"action": "CompleteJob",
			})
		}
		return map[string]interface{}{
			"replicas": replicas,
			"name":     taskName,
			"policies": policies,
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"lepton.sensetime.com/submitter":      submitter,
						"lepton.sensetime.com/framework-type": frameworkType,
						"ring-controller.atlas":               "ascend-910b",
					},
				},
				"spec": spec,
			},
		}
	}

	topPolicies := []interface{}{
		map[string]interface{}{
			"event":  "PodEvicted",
			"action": "RestartJob",
		},
	}
	plugins := map[string]interface{}{
		"svc": []interface{}{},
	}
	tasks := make([]interface{}, 0, 2)
	if isMultiNode {
		tasks = append(tasks, buildTaskSpec("master", masterReplicas, frameworkType == "MPI"))
		if workerReplicas > 0 {
			tasks = append(tasks, buildTaskSpec("worker", workerReplicas, false))
		}
	} else {
		taskName := "worker"
		if frameworkType == "MPI" {
			taskName = "master"
		}
		tasks = append(tasks, buildTaskSpec(taskName, workerReplicas, frameworkType == "MPI"))
	}
	if frameworkType == "MPI" {
		plugins["ssh"] = []interface{}{}
		if isMultiNode {
			plugins["mpi"] = []interface{}{
				"--master=master",
				"--worker=worker",
				"--port=22",
			}
			plugins["hcclrank"] = []interface{}{}
		}
	} else {
		plugins["pytorch"] = []interface{}{
			"--master=master",
			"--worker=worker",
			fmt.Sprintf("--port=%s", masterPort),
		}
		plugins["hcclrank"] = []interface{}{}
	}

	metadataAnnotations := map[string]interface{}{}
	if spBlock != "" {
		metadataAnnotations["sp-block"] = spBlock
	}

	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind":       "Job",
			"metadata": map[string]interface{}{
				"name":         name,
				"generateName": "vcjob-",
				"namespace":    namespace,
				"labels": map[string]interface{}{
					"lepton.sensetime.com/submitter":      submitter,
					"lepton.sensetime.com/framework-type": frameworkType,
					"ring-controller.atlas":               "ascend-910b",
				},
				"annotations": metadataAnnotations,
			},
			"spec": map[string]interface{}{
				"minAvailable": minAvailable,
				"plugins":      plugins,
				"maxRetry": 1,
				"tasks":    tasks,
				"priorityClassName": priorityClass,
				"queue":             queue,
				"schedulerName":     "volcano",
				"policies":          topPolicies,
			},
		},
	}

	return job, nil
}

func (s *JobService) CreateJob(ctx context.Context, req JobCreateRequest) (*unstructured.Unstructured, error) {
	job, err := s.BuildJobManifest(req)
	if err != nil {
		return nil, err
	}

	namespace := strings.TrimSpace(req.Namespace)
	if namespace == "" {
		namespace = "default"
	}
	created, err := s.dynamicClient.Resource(volcanoJobGVR).Namespace(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create volcano job %s/%s: %w", namespace, job.GetName(), err)
	}
	return created, nil
}

func normalizeJobFrameworkType(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "MPI":
		return "MPI"
	default:
		return "PyTorch"
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
	pvcRefs = s.resolveVolumeClaimRefs(ctx, identity, pvcRefs)
	imagePullSecrets = s.resolveImagePullSecretsFromKube(ctx, firstNonEmpty(identity.HostNamespace, identity.Namespace), imagePullSecrets)

	return &JobGetResult{
		Name:           identity.Name,
		Namespace:      identity.Namespace,
		UID:            identity.UID,
		VClusterName:   dashIfEmpty(identity.VClusterName),
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
		} else if pvc, pvcErr := s.vcClient.GetPersistentVolumeClaim(ctx, identity.VClusterName, identity.Namespace, pvcRef.ClaimName); pvcErr != nil {
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
		if pvc.Message == "pvc 不存在于当前分区" {
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
			diagnosis = append(diagnosis, "存在 PVC 不在当前分区，任务因此无法继续调度。")
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
	if stage == "scheduling" {
		instruction = fmt.Sprintf("当前任务还没调度成功，请带上目标 vcluster 的 kubeconfig 重新执行：rayctl job check %s -k <vcluster-kubeconfig-path>", identity.Name)
	}

	podEvidence := make([]CheckEvidenceItem, 0)
	if stage == "startup" || stage == "failed" {
		eventNamespace := firstNonEmpty(identity.HostNamespace, identity.Namespace)
		if strings.TrimSpace(eventNamespace) != "" {
			for _, pod := range pods {
				if stage == "startup" && isPodReady(pod) {
					continue
				}
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
			jobEvents, eventErr := s.vcClient.ListEvents(ctx, identity.VClusterName, identity.Namespace, identity.Name, "")
			if eventErr == nil {
				formatted := formatEventItems(jobEvents, 5)
				for _, event := range formatted {
					if strings.EqualFold(event.Message, "no events") {
						continue
					}
					podEvidence = append(podEvidence, CheckEvidenceItem{
						Source: "job",
						Status: firstNonEmpty(event.Reason, event.Type, "-"),
						Detail: event.Message,
					})
				}
			}
		}
	}
	if stage == "startup" && len(podEvidence) > 0 && len(diagnosis) > 0 {
		diagnosis[0] = diagnosis[0] + " 请进一步看下方 event 部分判断任务启动失败原因。"
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
		PodGroupEvidence: nil,
		Diagnosis:        diagnosis,
	}, nil
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
	pvcRefs = s.resolveVolumeClaimRefs(ctx, identity, pvcRefs)
	imagePullSecrets = s.resolveImagePullSecrets(ctx, identity, imagePullSecrets)

	return &JobGetResult{
		Name:           identity.Name,
		Namespace:      identity.Namespace,
		UID:            identity.UID,
		VClusterName:   dashIfEmpty(identity.VClusterName),
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

func (s *JobService) resolveVolumeClaimRefs(ctx context.Context, identity *jobIdentity, refs []VolumeClaimRef) []VolumeClaimRef {
	if len(refs) == 0 {
		return refs
	}

	resolved := make([]VolumeClaimRef, 0, len(refs))
	hostNamespace := ""
	vclusterName := ""
	namespace := ""
	if identity != nil {
		hostNamespace = strings.TrimSpace(identity.HostNamespace)
		vclusterName = strings.TrimSpace(identity.VClusterName)
		namespace = strings.TrimSpace(identity.Namespace)
	}

	for _, ref := range refs {
		current := ref
		switch {
		case s.vcClient != nil && vclusterName != "" && namespace != "":
			pvc, err := s.vcClient.GetPersistentVolumeClaim(ctx, vclusterName, namespace, ref.ClaimName)
			if err == nil {
				current.PVName = strings.TrimSpace(pvc.Spec.VolumeName)
				if current.PVName != "" {
					if pv, pvErr := s.clientset.CoreV1().PersistentVolumes().Get(ctx, current.PVName, metav1.GetOptions{}); pvErr == nil {
						current.BackendPV = strings.TrimSpace(pv.Labels["source-pv"])
					} else if pv, pvErr := s.vcClient.GetPersistentVolume(ctx, vclusterName, current.PVName); pvErr == nil {
						current.BackendPV = strings.TrimSpace(pv.Labels["source-pv"])
					}
					if current.BackendPV == "" {
						current.BackendPV = strings.TrimSpace(current.PVName)
					}
				}
			}
		case hostNamespace != "":
			pvc, err := s.clientset.CoreV1().PersistentVolumeClaims(hostNamespace).Get(ctx, ref.ClaimName, metav1.GetOptions{})
			if err == nil {
				current.BackendPV = strings.TrimSpace(pvc.Spec.VolumeName)
			}
		}
		s.enrichObjectStorageVolumeClaimRef(ctx, hostNamespace, &current)
		s.enrichHostVolumeClaimRef(ctx, &current)
		current.DisplayPV = firstNonEmpty(current.BackendPV, current.PVName)
		resolved = append(resolved, current)
	}

	return resolved
}

func (s *JobService) enrichHostVolumeClaimRef(ctx context.Context, ref *VolumeClaimRef) {
	if ref == nil {
		return
	}

	hostPVName := strings.TrimSpace(ref.BackendPV)
	if hostPVName == "" {
		return
	}

	pv, err := s.clientset.CoreV1().PersistentVolumes().Get(ctx, hostPVName, metav1.GetOptions{})
	if err != nil {
		return
	}

	if pv.Spec.ClaimRef != nil {
		ref.HostPVCName = strings.TrimSpace(pv.Spec.ClaimRef.Name)
		ref.HostPVCNamespace = strings.TrimSpace(pv.Spec.ClaimRef.Namespace)
	}

	if s.vcClient == nil || strings.TrimSpace(ref.HostPVCName) == "" {
		return
	}

	resourceUID := extractResourceUIDFromName(ref.HostPVCName)
	if resourceUID == "" {
		return
	}

	resource, err := s.vcClient.FindStorageVolumeResourceByUID(ctx, resourceUID)
	if err != nil || resource == nil {
		return
	}
	ref.FrontendVolume = firstNonEmpty(resource.Name, resource.DisplayName, resource.ID)
}

func (s *JobService) enrichObjectStorageVolumeClaimRef(ctx context.Context, hostNamespace string, ref *VolumeClaimRef) {
	if ref == nil || strings.TrimSpace(ref.FrontendVolume) != "" {
		return
	}
	hostNamespace = strings.TrimSpace(hostNamespace)
	if hostNamespace == "" || strings.TrimSpace(ref.ClaimName) == "" {
		return
	}

	pvc, err := s.clientset.CoreV1().PersistentVolumeClaims(hostNamespace).Get(ctx, ref.ClaimName, metav1.GetOptions{})
	if err != nil || pvc == nil {
		return
	}

	storageClassName := ""
	if pvc.Spec.StorageClassName != nil {
		storageClassName = strings.TrimSpace(*pvc.Spec.StorageClassName)
	}
	storageClass := strings.TrimSpace(firstNonEmpty(storageClassName, pvc.Annotations["volume.kubernetes.io/storage-provisioner"], pvc.Annotations["volume.beta.kubernetes.io/storage-provisioner"]))
	if !looksLikeObjectStoragePVC(storageClass, pvc.Annotations) {
		return
	}

	secretName := strings.TrimSpace(pvc.Annotations["secretName"])
	if secretName == "" {
		return
	}

	secret, err := s.clientset.CoreV1().Secrets(hostNamespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil || secret == nil {
		return
	}

	if endpoint := decodeObjectStorageEndpoint(secret); endpoint != "" {
		ref.FrontendVolume = endpoint
	}
}

func looksLikeObjectStoragePVC(storageClass string, annotations map[string]string) bool {
	storageClass = strings.ToLower(strings.TrimSpace(storageClass))
	if strings.Contains(storageClass, "aoss") || strings.Contains(storageClass, "s3") {
		return true
	}
	for key, value := range annotations {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.ToLower(strings.TrimSpace(value))
		if strings.Contains(key, "bucket") || strings.Contains(key, "secretname") || strings.Contains(value, "aoss") || strings.Contains(value, "s3") {
			return true
		}
	}
	return false
}

func decodeObjectStorageEndpoint(secret *corev1.Secret) string {
	if secret == nil {
		return ""
	}

	preferredKeys := []string{"endpoint", "ENDPOINT", "Endpoint", "domain", "DOMAIN", "host", "HOST", "url", "URL"}
	for _, key := range preferredKeys {
		if value, ok := secret.Data[key]; ok {
			endpoint := normalizeEndpointString(string(value))
			if endpoint != "" {
				return endpoint
			}
		}
	}

	for key, value := range secret.Data {
		lowerKey := strings.ToLower(strings.TrimSpace(key))
		if strings.Contains(lowerKey, "endpoint") || strings.Contains(lowerKey, "domain") || strings.Contains(lowerKey, "host") || strings.Contains(lowerKey, "url") {
			endpoint := normalizeEndpointString(string(value))
			if endpoint != "" {
				return endpoint
			}
		}
	}

	for _, value := range secret.Data {
		endpoint := normalizeEndpointString(string(value))
		if endpoint != "" {
			return endpoint
		}
	}

	return ""
}

func normalizeEndpointString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.TrimSuffix(value, "/")
	if strings.Contains(value, " ") {
		return ""
	}
	if strings.Contains(value, ".") || strings.Contains(value, ":") {
		return value
	}
	return ""
}

func duplicatePVRefs(refs []VolumeClaimRef) []VolumeClaimRef {
	byPV := make(map[string][]VolumeClaimRef)
	for _, ref := range refs {
		pvName := strings.TrimSpace(firstNonEmpty(ref.BackendPV, ref.PVName))
		if pvName == "" {
			continue
		}
		byPV[pvName] = append(byPV[pvName], ref)
	}

	duplicates := make([]VolumeClaimRef, 0)
	for _, group := range byPV {
		if len(group) < 2 {
			continue
		}
		duplicates = append(duplicates, group...)
	}

	sort.Slice(duplicates, func(i, j int) bool {
		leftPV := firstNonEmpty(duplicates[i].BackendPV, duplicates[i].PVName)
		rightPV := firstNonEmpty(duplicates[j].BackendPV, duplicates[j].PVName)
		if leftPV == rightPV {
			if duplicates[i].ClaimName == duplicates[j].ClaimName {
				return duplicates[i].Name < duplicates[j].Name
			}
			return duplicates[i].ClaimName < duplicates[j].ClaimName
		}
		return leftPV < rightPV
	})
	return duplicates
}

func pvcRefsToChecks(refs []VolumeClaimRef) []PVCCheckItem {
	if len(refs) == 0 {
		return nil
	}
	items := make([]PVCCheckItem, 0, len(refs))
	for _, ref := range refs {
		items = append(items, PVCCheckItem{
			Name:             ref.Name,
			ClaimName:        ref.ClaimName,
			PVName:           ref.PVName,
			BackendPV:        ref.BackendPV,
			DisplayPV:        firstNonEmpty(ref.DisplayPV, ref.BackendPV, ref.PVName),
			HostPVCName:      ref.HostPVCName,
			HostPVCNamespace: ref.HostPVCNamespace,
			FrontendVolume:   ref.FrontendVolume,
			Status:           "Bound",
			Message:          "重复的 PV 挂载有冲突",
		})
	}
	return items
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

func extractResourceUIDFromName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if looksLikeUUID(value) {
		return value
	}
	for start := 0; start+36 <= len(value); start++ {
		candidate := value[start : start+36]
		if looksLikeUUID(candidate) {
			return candidate
		}
	}
	return ""
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
