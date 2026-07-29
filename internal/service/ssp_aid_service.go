package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"rayctl/internal/platform"
)

const sspAIDWorkloadType = sspAIDWorkloadTypeValue

type SSPAIDService struct {
	clientset kubernetes.Interface
	platform  *platform.VirtualClusterClient
	sspBase   *SSPJobService
	jobHelper *JobService
}

type SSPAIDGetResult struct {
	Name                   string
	UID                    string
	State                  string
	VCluster               string
	Workspace              string
	Queue                  string
	QueueType              string
	Priority               string
	Submitter              string
	HostIP                 string
	SSHEnabled             string
	CodeServerEnabled      string
	Image                  string
	ImageType              string
	CreatedAt              string
	Terminal               bool
	Stage                  string
	Diagnosis              []string
	Instruction            string
	Resource               SSPAIDResourceItem
	ResourceSummary        string
	Volumes                []SSPAIDVolumeItem
	DNATRules              []SSPAIDDNATItem
	ImagePullSecrets       []string
	SecretChecks           []SecretCheckItem
	PersistentVolumeClaims []VolumeClaimRef
	Pods                   []JobPodItem
	Nodes                  []string
	InspectPod             string
	CheckEvidence          []CheckEvidenceItem
	RecentLogLines         []string
}

type SSPAIDResourceItem struct {
	CPU         string
	Memory      string
	Accelerator string
	GPUModel    string
	GPUMemory   string
	MachineType string
	RDMA        string
}

type SSPAIDVolumeItem struct {
	Type      string
	Name      string
	MountPath string
	Endpoint  string
}

type SSPAIDDNATItem struct {
	External string
	Internal string
	Protocol string
	State    string
}

type sspAIDCandidate struct {
	AID       platform.SSPAID
	Workspace sspWorkspaceRef
}

func NewSSPAIDService(clientset kubernetes.Interface, platformClient *platform.VirtualClusterClient) *SSPAIDService {
	return &SSPAIDService{
		clientset: clientset,
		platform:  platformClient,
		sspBase:   NewSSPJobService(clientset, platformClient),
		jobHelper: NewJobService(clientset, nil, platformClient),
	}
}

func (s *SSPAIDService) GetAID(ctx context.Context, identifier string, workspace string, includeLogs bool) (*SSPAIDGetResult, error) {
	if s.clientset == nil {
		return nil, fmt.Errorf("kubernetes client is required")
	}
	if s.platform == nil {
		return nil, fmt.Errorf("platform client is required; configure ~/.rayctl/platform.json first")
	}
	identifier = strings.TrimSpace(identifier)
	workspace = strings.TrimSpace(workspace)
	if identifier == "" {
		return nil, fmt.Errorf("AID name or uid is required")
	}
	if !looksLikeUUID(identifier) {
		identifier = strings.ToLower(identifier)
	}

	seedPods, err := s.findAIDPods(ctx, identifier, "")
	if err != nil {
		return nil, fmt.Errorf("locate AID pods in PT HC: %w", err)
	}
	workspaces, err := s.sspBase.resolveWorkspaceCandidates(ctx, workspace, seedPods)
	if err != nil {
		return nil, err
	}
	subscription, err := s.sspBase.resolveSubscription(ctx, seedPods)
	if err != nil {
		return nil, err
	}

	lookupIdentifier := identifier
	if looksLikeUUID(identifier) && len(seedPods) > 0 {
		lookupIdentifier = firstNonEmpty(seedPods[0].Labels[sspWorkloadNameLabel], identifier)
	}
	candidates, lookupErr := s.findPlatformAIDs(ctx, subscription, lookupIdentifier, workspaces)
	if len(candidates) == 0 {
		if lookupErr != nil {
			return nil, fmt.Errorf("query SSP AID API: %w", lookupErr)
		}
		if workspace != "" {
			return nil, fmt.Errorf("AID %q not found in workspace %q", identifier, workspace)
		}
		return nil, fmt.Errorf("AID %q not found in PT workspaces", identifier)
	}
	if len(candidates) > 1 {
		values := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			values = append(values, fmt.Sprintf("%s/%s (%s)", candidate.AID.Properties.Workload.WorkspaceName, candidate.AID.Name, candidate.AID.UID))
		}
		sort.Strings(values)
		return nil, fmt.Errorf("AID %q matches multiple workspaces: %s; use --workspace", identifier, strings.Join(values, ", "))
	}

	candidate := candidates[0]
	aid := candidate.AID
	pods := filterAIDPods(seedPods, aid)
	hostNamespace := ""
	if len(pods) > 0 {
		hostNamespace = pods[0].Namespace
	} else {
		hostNamespace = candidate.Workspace.HostNamespace
		pods, err = s.findAIDPods(ctx, firstNonEmpty(aid.UID, aid.Name), hostNamespace)
		if err != nil {
			return nil, fmt.Errorf("list AID pods: %w", err)
		}
	}

	dnatRules, _ := s.platform.FindSSPAIDDNATRules(ctx, aid)
	return s.buildResult(ctx, aid, hostNamespace, pods, dnatRules, includeLogs), nil
}

func (s *SSPAIDService) findPlatformAIDs(ctx context.Context, subscription string, identifier string, workspaces []sspWorkspaceRef) ([]sspAIDCandidate, error) {
	type lookupResult struct {
		workspace sspWorkspaceRef
		aids      []platform.SSPAID
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
			aids, err := s.platform.FindSSPAIDs(ctx, subscription, sspDefaultRegion, workspace.Name, identifier)
			results <- lookupResult{workspace: workspace, aids: aids, err: err}
		}()
	}
	wg.Wait()
	close(results)

	candidates := make([]sspAIDCandidate, 0)
	var firstErr error
	for result := range results {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		for _, aid := range result.aids {
			candidates = append(candidates, sspAIDCandidate{AID: aid, Workspace: result.workspace})
		}
	}
	return candidates, firstErr
}

func (s *SSPAIDService) findAIDPods(ctx context.Context, identifier string, namespace string) ([]corev1.Pod, error) {
	selector := map[string]string{sspWorkloadTypeLabel: sspAIDWorkloadType}
	if looksLikeUUID(identifier) {
		selector[sspWorkloadUIDLabel] = identifier
	} else {
		selector[sspWorkloadNameLabel] = identifier
	}
	list, err := s.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set(selector).AsSelector().String(),
	})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func filterAIDPods(pods []corev1.Pod, aid platform.SSPAID) []corev1.Pod {
	result := make([]corev1.Pod, 0, len(pods))
	for _, pod := range pods {
		if strings.TrimSpace(pod.Labels[sspWorkloadUIDLabel]) == strings.TrimSpace(aid.UID) ||
			strings.EqualFold(strings.TrimSpace(pod.Labels[sspWorkloadNameLabel]), strings.TrimSpace(aid.Name)) {
			result = append(result, pod)
		}
	}
	return result
}

func (s *SSPAIDService) buildResult(ctx context.Context, aid platform.SSPAID, hostNamespace string, pods []corev1.Pod, dnatRules []platform.SSPAIDDNATRule, includeLogs bool) *SSPAIDGetResult {
	state := normalizeSSPJobState(aid.State)
	terminal := isTerminalSSPAIDState(state)
	queue := firstNonEmpty(aid.Properties.Workload.Queue.Name, lastResourceSegment(aid.Properties.Workload.Queue.ID))
	resource := aid.Properties.Workload.BaseSpec
	result := &SSPAIDGetResult{
		Name:              aid.Name,
		UID:               aid.UID,
		State:             state,
		VCluster:          s.sspBase.resolveSSPVClusterName(ctx, pods, hostNamespace),
		Workspace:         aid.Properties.Workload.WorkspaceName,
		Queue:             queue,
		QueueType:         aid.Properties.Workload.Queue.Type,
		Priority:          aid.Properties.Workload.Priority,
		Submitter:         firstNonEmpty(aid.Properties.Ownership.CreatorName, aid.CreatorID),
		HostIP:            aid.Properties.HostIP,
		SSHEnabled:        boolPointerText(aid.Properties.SSHEnabled),
		CodeServerEnabled: boolPointerText(aid.Properties.CodeServerEnabled),
		Image:             aid.Properties.ImagePath,
		ImageType:         aid.Properties.ImageType,
		CreatedAt:         formatSSPTime(aid.CreateTime),
		Terminal:          terminal,
		InspectPod:        "-",
		Resource: SSPAIDResourceItem{
			CPU:         formatSSPResource(resource.CPU, ""),
			Memory:      formatSSPResource(resource.Memory, ""),
			Accelerator: formatSSPResource(resource.AccelerateDeviceCount, ""),
			GPUModel:    resource.GPUModel,
			GPUMemory:   formatSSPResource(resource.GPUMemorySize, "Gi"),
			MachineType: strings.Join(resource.MachineTypes, ", "),
			RDMA:        resource.RDMAName,
		},
	}
	result.ResourceSummary = formatSSPAIDResourceSummary(result.Resource)
	for _, volume := range aid.Properties.VolumeMounts {
		result.Volumes = append(result.Volumes, SSPAIDVolumeItem{
			Type:      volume.Type,
			Name:      volume.Name,
			MountPath: volume.MountPath,
			Endpoint:  volume.Endpoint,
		})
	}
	for _, rule := range dnatRules {
		result.DNATRules = append(result.DNATRules, SSPAIDDNATItem{
			External: endpointText(rule.ExternalIP, rule.ExternalPort),
			Internal: endpointText(rule.InternalIP, rule.InternalPort),
			Protocol: rule.Protocol,
			State:    rule.State,
		})
	}
	result.Pods, result.Nodes = makeSSPPodItems(pods)
	inspectPod := chooseInspectPod(append([]corev1.Pod(nil), pods...))
	if inspectPod != nil {
		result.InspectPod = inspectPod.Name
	}
	if terminal {
		result.Stage = "terminal"
		result.Diagnosis = []string{fmt.Sprintf("开发机已经停止，平台状态为 %s。", state)}
		return result
	}

	imagePullSecrets, pvcRefs := extractPodSpecDetailsFromPods(pods)
	identity := &jobIdentity{Name: aid.Name, UID: aid.UID, HostNamespace: hostNamespace}
	result.PersistentVolumeClaims = s.jobHelper.resolveVolumeClaimRefs(ctx, identity, pvcRefs)
	volumeDescriptors := s.sspBase.resolveSSPVolumeDescriptors(ctx, aidVolumeDescriptors(aid.Properties.VolumeMounts))
	result.PersistentVolumeClaims = enrichSSPVolumeClaims(result.PersistentVolumeClaims, volumeDescriptors)
	result.ImagePullSecrets = s.jobHelper.resolveImagePullSecretsFromKube(ctx, hostNamespace, imagePullSecrets)

	assigned := 0
	ready := 0
	for _, pod := range pods {
		if pod.Spec.NodeName != "" {
			assigned++
		}
		if isPodReady(pod) {
			ready++
		}
	}
	switch {
	case len(pods) == 0:
		result.Stage = "scheduling"
		result.Diagnosis = []string{"平台尚未在 PT HC 创建 AID Pod，当前更可能仍在开发机控制器或队列处理阶段。"}
		result.Instruction = "确认所选队列可用并稍后重试；Pod 创建后本命令会展示具体调度原因。"
	case assigned == 0:
		result.Stage = "scheduling"
		reason, detail := summarizeSSPSchedulingFailure(pods)
		result.Diagnosis = []string{firstNonEmpty(reason, "AID Pod 已创建但尚未分配到节点。")}
		result.Instruction = schedulingInstruction(detail)
		result.CheckEvidence = podConditionEvidence(pods, corev1.PodScheduled)
	case ready == 0:
		result.Stage = "startup"
		result.Diagnosis = []string{summarizeSSPStartupFailure(pods)}
		result.Instruction = "根据下方 Pod 事件检查镜像、挂载卷、探针和容器启动配置。"
		result.CheckEvidence = s.sspBase.podEventEvidence(ctx, inspectPod)
		if hasImagePullProblem(pods) {
			result.SecretChecks = s.jobHelper.checkImagePullSecrets(ctx, identity, imagePullSecrets)
		}
	default:
		result.Stage = "running"
		result.Diagnosis = []string{"开发机 Pod 已 Ready，可以正常使用。"}
		if includeLogs && inspectPod != nil && podHasRunnableLogs(*inspectPod) {
			lines, err := s.jobHelper.tailPodLogs(ctx, inspectPod.Namespace, inspectPod.Name, defaultTailLogLines)
			if err != nil {
				result.RecentLogLines = []string{fmt.Sprintf("log unavailable: %v", err)}
			} else {
				result.RecentLogLines = lines
			}
		}
	}
	result.Stage, result.Diagnosis = ensurePVCGetDiagnosis(state, terminal, result.Stage, result.Diagnosis, result.PersistentVolumeClaims)
	return result
}

func formatSSPAIDResourceSummary(resource SSPAIDResourceItem) string {
	parts := make([]string, 0, 7)
	appendPart := func(value string, suffix string) {
		value = strings.TrimSpace(value)
		if value == "" || value == "-" || value == "0" {
			return
		}
		parts = append(parts, value+suffix)
	}
	appendPart(resource.CPU, " CPU")
	appendPart(resource.Memory, " Memory")
	if value := strings.TrimSpace(resource.Accelerator); value != "" && value != "-" && value != "0" {
		model := strings.TrimSpace(resource.GPUModel)
		if model != "" && model != "-" {
			parts = append(parts, value+" "+model)
		} else {
			parts = append(parts, value+" Accelerator")
		}
	}
	appendPart(resource.GPUMemory, " GPU Memory")
	appendPart(resource.MachineType, " Machine Type")
	appendPart(resource.RDMA, " RDMA")
	return dashIfEmpty(strings.Join(parts, " / "))
}

func boolPointerText(value *bool) string {
	if value == nil {
		return "-"
	}
	if *value {
		return "Y"
	}
	return "N"
}

func endpointText(host string, port string) string {
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	if host == "" {
		return "-"
	}
	if port == "" {
		return host
	}
	return host + ":" + port
}

func lastResourceSegment(value string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(value), "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func isTerminalSSPAIDState(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "stopped", "stopping", "deleted", "failed", "terminated", "aborted", "canceled", "cancelled":
		return true
	default:
		return false
	}
}
