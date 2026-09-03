package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"rayctl/internal/platform"
)

const sspAIDWorkloadType = sspAIDWorkloadTypeValue

const sspNodeZoneLabel = "topology.sensecore.cn/zone"

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
	Namespace              string
	Queue                  string
	QueueType              string
	Priority               string
	Submitter              string
	InternalIP             string
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
	Timings                SSPAIDGetTimings
}

type SSPAIDGetTimings struct {
	LocatePods    time.Duration
	ResolveRegion time.Duration
	Workspace     time.Duration
	Subscription  time.Duration
	PlatformAID   time.Duration
	RefinePods    time.Duration
	BuildDetail   time.Duration
	DNAT          time.Duration
	Total         time.Duration
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
	return s.GetAIDInRegion(ctx, identifier, workspace, "", includeLogs)
}

func (s *SSPAIDService) GetAIDInRegion(ctx context.Context, identifier string, workspace string, requestedRegion string, includeLogs bool) (*SSPAIDGetResult, error) {
	startedAt := time.Now()
	timings := SSPAIDGetTimings{}
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

	stageStartedAt := time.Now()
	seedPods, err := s.findAIDPods(ctx, identifier, "")
	timings.LocatePods = time.Since(stageStartedAt)
	if err != nil {
		return nil, fmt.Errorf("locate AID pods in HC: %w", err)
	}
	stageStartedAt = time.Now()
	region := s.resolveAIDRegion(ctx, requestedRegion, seedPods)
	timings.ResolveRegion = time.Since(stageStartedAt)
	stageStartedAt = time.Now()
	workspaces, err := s.sspBase.resolveWorkspaceCandidatesForRegion(ctx, workspace, seedPods, region)
	timings.Workspace = time.Since(stageStartedAt)
	if err != nil {
		return nil, err
	}
	stageStartedAt = time.Now()
	subscription, err := s.sspBase.resolveSubscriptionForRegion(ctx, seedPods, region)
	timings.Subscription = time.Since(stageStartedAt)
	if err != nil {
		return nil, err
	}

	lookupIdentifier := identifier
	if looksLikeUUID(identifier) && len(seedPods) > 0 {
		lookupIdentifier = firstNonEmpty(seedPods[0].Labels[sspWorkloadNameLabel], identifier)
	}
	stageStartedAt = time.Now()
	candidates, lookupErr := s.findPlatformAIDs(ctx, subscription, region, lookupIdentifier, workspaces)
	timings.PlatformAID = time.Since(stageStartedAt)
	if len(candidates) == 0 {
		if lookupErr != nil {
			return nil, fmt.Errorf("query SSP AID API: %w", lookupErr)
		}
		if workspace != "" {
			return nil, fmt.Errorf("AID %q not found in workspace %q", identifier, workspace)
		}
		return nil, fmt.Errorf("AID %q not found in %s workspaces", identifier, region)
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
	stageStartedAt = time.Now()
	pods := filterAIDPods(seedPods, aid)
	hostNamespace := ""
	if len(pods) > 0 {
		hostNamespace = pods[0].Namespace
	} else {
		hostNamespace = candidate.Workspace.HostNamespace
		if hostNamespace == "" {
			hostNamespace = s.sspBase.findHostNamespace(ctx, aid.Properties.Workload.WorkspaceName, "")
		}
		pods, err = s.findAIDPods(ctx, firstNonEmpty(aid.UID, aid.Name), hostNamespace)
		if err != nil {
			return nil, fmt.Errorf("list AID pods: %w", err)
		}
		if hostNamespace == "" && len(pods) > 0 {
			hostNamespace = pods[0].Namespace
		}
	}
	timings.RefinePods = time.Since(stageStartedAt)

	type timedDNATResult struct {
		Rules    []platform.SSPAIDDNATRule
		Duration time.Duration
	}
	dnatResult := asyncCall(ctx, func(ctx context.Context) (timedDNATResult, error) {
		dnatStartedAt := time.Now()
		rules, err := s.platform.FindSSPAIDDNATRules(ctx, aid)
		return timedDNATResult{Rules: rules, Duration: time.Since(dnatStartedAt)}, err
	})
	stageStartedAt = time.Now()
	result := s.buildResult(ctx, aid, hostNamespace, pods, nil, includeLogs)
	timings.BuildDetail = time.Since(stageStartedAt)
	if resolved := <-dnatResult; resolved.Err == nil {
		appendSSPAIDDNATRules(result, resolved.Value.Rules)
		timings.DNAT = resolved.Value.Duration
	}
	timings.Total = time.Since(startedAt)
	result.Timings = timings
	return result, nil
}

func (s *SSPAIDService) findPlatformAIDs(ctx context.Context, subscription string, region string, identifier string, workspaces []sspWorkspaceRef) ([]sspAIDCandidate, error) {
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
			workspaceSubscription := firstNonEmpty(strings.TrimSpace(workspace.Subscription), subscription)
			var aids []platform.SSPAID
			var err error
			if workspace.ProfileName != "" {
				aids, err = s.platform.FindSSPAIDsForProfile(
					ctx, workspace.ProfileName, workspaceSubscription, region, workspace.Name, identifier,
				)
			} else {
				aids, err = s.platform.FindSSPAIDs(ctx, workspaceSubscription, region, workspace.Name, identifier)
			}
			results <- lookupResult{workspace: workspace, aids: aids, err: err}
		}()
	}
	wg.Wait()
	close(results)

	candidates := make([]sspAIDCandidate, 0)
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
		for _, aid := range result.aids {
			candidates = append(candidates, sspAIDCandidate{AID: aid, Workspace: result.workspace})
		}
	}
	if successfulLookups > 0 {
		firstErr = nil
	}
	return candidates, firstErr
}

func (s *SSPAIDService) resolveAIDRegion(ctx context.Context, requested string, pods []corev1.Pod) string {
	if region := strings.TrimSpace(requested); region != "" {
		return region
	}
	seenNodes := make(map[string]struct{})
	for _, pod := range pods {
		nodeName := strings.TrimSpace(pod.Spec.NodeName)
		if nodeName == "" {
			continue
		}
		if _, exists := seenNodes[nodeName]; exists {
			continue
		}
		seenNodes[nodeName] = struct{}{}
		node, err := s.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			continue
		}
		if region := regionFromSSPZone(node.Labels[sspNodeZoneLabel]); region != "" {
			return region
		}
	}
	if nodes, err := s.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 10}); err == nil {
		for _, node := range nodes.Items {
			if region := regionFromSSPZone(node.Labels[sspNodeZoneLabel]); region != "" {
				return region
			}
		}
	}
	if s.platform != nil {
		if region := s.platform.CurrentRegion(); region != "" {
			return region
		}
	}
	return sspDefaultRegion
}

func regionFromSSPZone(zone string) string {
	zone = strings.TrimSpace(zone)
	if len(zone) >= 2 {
		last := zone[len(zone)-1]
		previous := zone[len(zone)-2]
		if ((last >= 'a' && last <= 'z') || (last >= 'A' && last <= 'Z')) && previous >= '0' && previous <= '9' {
			return zone[:len(zone)-1]
		}
	}
	return zone
}

func (s *SSPAIDService) findAIDPods(ctx context.Context, identifier string, namespace string) ([]corev1.Pod, error) {
	selector := map[string]string{sspWorkloadTypeLabel: sspAIDWorkloadType}
	if looksLikeUUID(identifier) {
		selector[sspWorkloadUIDLabel] = identifier
	} else {
		selector[sspWorkloadNameLabel] = identifier
	}
	list, err := s.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector:   labels.Set(selector).AsSelector().String(),
		ResourceVersion: "0",
		Limit:           20,
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
	vcResult := asyncCall(ctx, func(ctx context.Context) (string, error) {
		return s.resolveAIDVClusterName(ctx, aid, queue, pods, hostNamespace), nil
	})
	result := &SSPAIDGetResult{
		Name:              aid.Name,
		UID:               aid.UID,
		State:             state,
		VCluster:          "-",
		Workspace:         aid.Properties.Workload.WorkspaceName,
		Namespace:         aidWorkspaceNamespace(pods, hostNamespace),
		Queue:             queue,
		QueueType:         aid.Properties.Workload.Queue.Type,
		Priority:          aid.Properties.Workload.Priority,
		Submitter:         firstNonEmpty(aid.Properties.Ownership.CreatorName, aid.CreatorID),
		InternalIP:        aid.Properties.HostIP,
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
	appendSSPAIDDNATRules(result, dnatRules)
	result.Pods, result.Nodes = makeSSPPodItems(pods)
	inspectPod := chooseInspectPod(append([]corev1.Pod(nil), pods...))
	if inspectPod != nil {
		result.InspectPod = inspectPod.Name
	}
	if terminal {
		result.VCluster = (<-vcResult).Value
		result.Stage = "terminal"
		result.Diagnosis = []string{fmt.Sprintf("开发机已经停止，平台状态为 %s。", state)}
		return result
	}

	imagePullSecrets, pvcRefs := extractPodSpecDetailsFromPods(pods)
	identity := &jobIdentity{Name: aid.Name, UID: aid.UID, HostNamespace: hostNamespace}
	result.PersistentVolumeClaims = s.sspBase.resolveSSPVolumeClaimRefs(ctx, hostNamespace, pvcRefs)
	volumeDescriptors := s.sspBase.resolveSSPVolumeDescriptors(ctx, aid.ProfileName, aidVolumeDescriptors(aid.Properties.VolumeMounts))
	result.PersistentVolumeClaims = enrichSSPVolumeClaims(result.PersistentVolumeClaims, volumeDescriptors)
	result.ImagePullSecrets = s.jobHelper.resolveImagePullSecretsFromKube(ctx, hostNamespace, imagePullSecrets)
	result.VCluster = (<-vcResult).Value

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
		result.Diagnosis = []string{"平台尚未在当前 HC 创建 AID Pod，当前更可能仍在开发机控制器或队列处理阶段。"}
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

func (s *SSPAIDService) resolveAIDVClusterName(ctx context.Context, aid platform.SSPAID, queue string, pods []corev1.Pod, hostNamespace string) string {
	vcName := s.sspBase.resolveSSPVClusterName(ctx, pods, hostNamespace, aid.ProfileName)
	if vcName != "-" || s.platform == nil || strings.TrimSpace(aid.ProfileName) == "" || strings.TrimSpace(queue) == "" {
		return vcName
	}
	details, err := s.platform.GetSSPQueueResource(ctx, aid.ProfileName, queue)
	if err != nil {
		return vcName
	}
	return dashIfEmpty(firstNonEmpty(strings.TrimSpace(details.VClusterName), vcName))
}

func aidWorkspaceNamespace(pods []corev1.Pod, hostNamespace string) string {
	for _, pod := range pods {
		if namespace := firstNonEmpty(
			pod.Annotations["vcluster.loft.sh/object-namespace"],
			pod.Annotations["vcluster.loft.sh/namespace"],
		); namespace != "" {
			return namespace
		}
	}
	return hostNamespace
}

func appendSSPAIDDNATRules(result *SSPAIDGetResult, rules []platform.SSPAIDDNATRule) {
	if result == nil {
		return
	}
	for _, rule := range rules {
		// AID payloads can contain an associated EIP without an actual DNAT
		// mapping. Only render complete port mappings as DNAT rules.
		if strings.TrimSpace(rule.ExternalIP) == "" || strings.TrimSpace(rule.ExternalPort) == "" || strings.TrimSpace(rule.InternalPort) == "" {
			continue
		}
		internalIP := firstNonEmpty(strings.TrimSpace(rule.InternalIP), strings.TrimSpace(result.InternalIP))
		result.DNATRules = append(result.DNATRules, SSPAIDDNATItem{
			External: endpointText(rule.ExternalIP, rule.ExternalPort),
			Internal: endpointText(internalIP, rule.InternalPort),
			Protocol: rule.Protocol,
			State:    firstNonEmpty(strings.TrimSpace(rule.State), "UNKNOWN"),
		})
	}
}

func formatSSPAIDResourceSummary(resource SSPAIDResourceItem) string {
	parts := make([]string, 0, 7)
	appendPart := func(value string, suffix string) {
		value = strings.TrimSpace(value)
		if isEmptySSPResourceValue(value) {
			return
		}
		parts = append(parts, value+suffix)
	}
	appendPart(resource.CPU, " CPU")
	appendPart(resource.Memory, " Memory")
	if value := strings.TrimSpace(resource.Accelerator); !isEmptySSPResourceValue(value) {
		model := strings.TrimSpace(resource.GPUModel)
		if !isEmptySSPResourceValue(model) {
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

func isEmptySSPResourceValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "-", "0", "none", "null", "<nil>":
		return true
	default:
		return false
	}
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
