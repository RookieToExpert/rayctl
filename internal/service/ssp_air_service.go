package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"rayctl/internal/platform"
	"rayctl/internal/podevidence"
)

type SSPAIRResourceItem struct {
	MachineType string
	CPU         string
	Memory      string
	Accelerator string
	Model       string
	RDMA        string
	Image       string
}

type SSPAIRDNATItem struct {
	External string
	Internal string
	Protocol string
	Gateway  string
}

type SSPAIRVolumeItem struct {
	Type      string
	Name      string
	MountPath string
	Endpoint  string
}

type SSPAIRWorkerItem struct {
	Name        string
	Role        string
	Group       string
	Node        string
	Phase       string
	HostIP      string
	PodIP       string
	Restarts    int
	StartedAt   string
	LastStarted string
}

type SSPAIRJobItem struct {
	PodEvidence      *podevidence.Result
	Name             string
	UID              string
	State            string
	Workspace        string
	Queue            string
	QueueType        string
	Cluster          string
	VCluster         string
	Namespace        string
	HostNamespace    string
	Priority         string
	Creator          string
	Region           string
	Profile          string
	ReadyReplicas    int
	Replicas         int
	DesiredReplicas  int
	GroupSize        int
	TotalInstances   int
	CreatedAt        string
	UpdatedAt        string
	InternalIP       string
	Resource         SSPAIRResourceItem
	DNATRules        []SSPAIRDNATItem
	Volumes          []SSPAIRVolumeItem
	Workers          []SSPAIRWorkerItem
	AuxiliaryWorkers []SSPAIRWorkerItem
	WorkerTotal      int
	Raw              platform.SSPAIRJob
}

type SSPAIRGatewayItem struct {
	Name          string
	UID           string
	State         string
	Workspace     string
	Queue         string
	QueueType     string
	Cluster       string
	Namespace     string
	HostNamespace string
	Priority      string
	Creator       string
	Region        string
	Profile       string
	Replicas      int
	CreatedAt     string
	UpdatedAt     string
	Resource      SSPAIRResourceItem
	DNATRules     []SSPAIRDNATItem
	Raw           platform.SSPAIRGateway
}

type SSPAIRJobListResult struct {
	Items           []SSPAIRJobItem
	Warnings        []string
	QueryIncomplete bool `json:"query_incomplete,omitempty" public:"omitempty"`
}

func (r *SSPAIRJobListResult) IsQueryComplete() bool { return r == nil || !r.QueryIncomplete }

type SSPAIRGatewayListResult struct{ Items []SSPAIRGatewayItem }

type SSPAIRService struct {
	platform  *platform.VirtualClusterClient
	clientset kubernetes.Interface
}

func (s *SSPAIRService) SetKubeClient(client kubernetes.Interface) { s.clientset = client }

func NewSSPAIRService(platformClient *platform.VirtualClusterClient) *SSPAIRService {
	return &SSPAIRService{platform: platformClient}
}

func (s *SSPAIRService) ListJobs(ctx context.Context, region string, workspace string, state string) (*SSPAIRJobListResult, error) {
	workspaces, inventoryErr := s.resolveWorkspacesWithStatus(ctx, region, workspace)
	if inventoryErr != nil && len(workspaces) == 0 {
		return nil, singleLineQueryError{inventoryErr}
	}
	type load struct {
		workspace platform.SSPWorkspace
		items     []platform.SSPAIRJob
		err       error
	}
	loads := boundedMap(ctx, workspaces, 12, func(ctx context.Context, workspace platform.SSPWorkspace) load {
		items, queryErr := s.platform.ListSSPAIRJobs(ctx, workspace, "")
		return load{workspace: workspace, items: items, err: queryErr}
	})
	items := make([]SSPAIRJobItem, 0)
	var failures []error
	if inventoryErr != nil {
		failures = append(failures, inventoryErr)
	}
	succeeded := 0
	for _, result := range loads {
		if result.err != nil {
			failures = append(failures, &SSPWorkspaceQueryError{Workspace: result.workspace.Name, Region: result.workspace.Region, Cause: result.err})
			continue
		}
		succeeded++
		for _, item := range result.items {
			items = append(items, s.airJobItem(item))
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(failures, func(i, j int) bool { return failures[i].Error() < failures[j].Error() })
	if succeeded == 0 && len(failures) > 0 {
		return nil, singleLineQueryError{fmt.Errorf("全部 workspace 查询失败：%w", errors.Join(failures...))}
	}
	items = filterAIRJobsByState(items, state)
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	result := &SSPAIRJobListResult{Items: items, QueryIncomplete: len(failures) > 0}
	if len(failures) > 0 {
		result.Warnings = []string{partialSSPQueryWarning(errors.Join(failures...))}
	}
	return result, nil
}

func filterAIRJobsByState(items []SSPAIRJobItem, state string) []SSPAIRJobItem {
	state = strings.TrimSpace(state)
	if state == "" {
		return items
	}
	filtered := make([]SSPAIRJobItem, 0, len(items))
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.State), state) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (s *SSPAIRService) GetJobs(ctx context.Context, identifiers []string, region string, workspace string, includeDetails bool, workerLimit int) ([]*SSPAIRJobItem, []error) {
	workspaces, err := s.resolveWorkspaces(ctx, region, workspace)
	if err != nil {
		return nil, []error{err}
	}
	selected := make([]SSPAIRJobItem, len(identifiers))
	errs := make([]error, len(identifiers))
	type discovery struct {
		items []platform.SSPAIRJob
		err   error
	}
	for index, identifier := range identifiers {
		queries := boundedMap(ctx, workspaces, 12, func(ctx context.Context, candidate platform.SSPWorkspace) discovery {
			filter := identifier
			if airLooksLikeUUID(identifier) {
				filter = ""
			}
			items, queryErr := s.platform.ListSSPAIRJobs(ctx, candidate, filter)
			return discovery{items: items, err: queryErr}
		})
		matches := make([]SSPAIRJobItem, 0, 1)
		var firstErr error
		for _, query := range queries {
			if query.err != nil {
				if firstErr == nil {
					firstErr = query.err
				}
				continue
			}
			for _, candidate := range query.items {
				item := s.airJobItem(candidate)
				if len(matchAIRJobs(identifier, []SSPAIRJobItem{item})) > 0 {
					matches = append(matches, item)
				}
			}
		}
		switch len(matches) {
		case 0:
			if firstErr != nil {
				errs[index] = fmt.Errorf("AIR job %q: %w", identifier, firstErr)
			} else {
				errs[index] = fmt.Errorf("AIR job %q not found", identifier)
			}
		case 1:
			selected[index] = matches[0]
		default:
			errs[index] = fmt.Errorf("AIR job %q matched multiple workspaces; use --workspace", identifier)
		}
	}
	type detailResult struct {
		item *SSPAIRJobItem
		err  error
	}
	indices := make([]int, 0, len(selected))
	for index := range selected {
		if errs[index] == nil {
			indices = append(indices, index)
		}
	}
	details := boundedMap(ctx, indices, 6, func(ctx context.Context, index int) detailResult {
		detail, queryErr := s.platform.GetSSPAIRJob(ctx, selected[index].Raw)
		if queryErr != nil {
			return detailResult{err: fmt.Errorf("AIR job %q: %w", identifiers[index], queryErr)}
		}
		item := s.airJobItem(*detail)
		vcNameCh := make(chan string, 1)
		go func(snapshot SSPAIRJobItem) {
			vcNameCh <- s.resolveAIRJobVCluster(ctx, snapshot)
		}(item)
		pods, podErr := s.findAIRPods(ctx, item.UID)
		setAIRNamespaces(&item.Namespace, &item.HostNamespace, pods)
		workers, total, workersErr := s.platform.ListSSPAIRWorkers(ctx, *detail, workerLimit)
		var workerQueryErr error
		if workersErr != nil {
			workerQueryErr = fmt.Errorf("AIR job %q workers: %w", identifiers[index], workersErr)
		} else {
			item.WorkerTotal = total
			setAIRWorkers(&item, workers, pods)
		}
		if includeDetails {
			if s.clientset != nil {
				if podErr != nil {
					item.PodEvidence = &podevidence.Result{Warnings: []string{"AIR Pod discovery: " + podErr.Error()}}
				} else {
					item.PodEvidence = collectWorkloadEvidence(ctx, s.clientset, s.platform, pods, item.Name, "air", item.Workspace)
					if len(pods) == 0 {
						item.PodEvidence.Warnings = append(item.PodEvidence.Warnings, "No matching Pods in current kubeconfig; verify HC environment")
					}
				}
			} else {
				item.PodEvidence = &podevidence.Result{Warnings: []string{"Kubernetes client unavailable; platform worker data retained"}}
			}
		}
		item.VCluster = <-vcNameCh
		return detailResult{item: &item, err: workerQueryErr}
	})
	result := make([]*SSPAIRJobItem, len(identifiers))
	for offset, detail := range details {
		index := indices[offset]
		if detail.err != nil {
			errs[index] = detail.err
		}
		result[index] = detail.item
	}
	return result, compactErrors(errs)
}

// The workers endpoint also returns auxiliary Pods such as metrics collectors.
// LWS labels identify compute instances without including those auxiliary Pods.
func setAIRWorkers(item *SSPAIRJobItem, workers []platform.SSPAIRWorker, pods []corev1.Pod) {
	podsByName := make(map[string]*corev1.Pod, len(pods))
	for index := range pods {
		pod := &pods[index]
		name := firstNonEmpty(pod.Annotations["vcluster.loft.sh/object-name"], pod.Name)
		podsByName[name] = pod
	}
	for _, worker := range workers {
		role, group := airWorkerRole(item.Name, worker)
		instance := SSPAIRWorkerItem{
			Name: worker.Name, Role: role, Group: group, Node: worker.NodeInfo.Name,
			Phase: worker.Phase, HostIP: worker.HostIP, PodIP: worker.IP,
			Restarts: worker.RestartCount, StartedAt: formatSSPTime(worker.StartTime), LastStarted: formatSSPTime(worker.LastStartedTime),
		}
		if pod := podsByName[worker.Name]; pod != nil {
			instance.Node = firstNonEmpty(instance.Node, pod.Spec.NodeName)
			instance.HostIP = firstNonEmpty(instance.HostIP, pod.Status.HostIP)
			instance.PodIP = firstNonEmpty(instance.PodIP, pod.Status.PodIP)
			instance.Phase = firstNonEmpty(instance.Phase, string(pod.Status.Phase))
			instance.Restarts = 0
			for _, status := range pod.Status.InitContainerStatuses {
				instance.Restarts += int(status.RestartCount)
			}
			for _, status := range pod.Status.ContainerStatuses {
				instance.Restarts += int(status.RestartCount)
			}
		}
		if role == "AUXILIARY" {
			item.AuxiliaryWorkers = append(item.AuxiliaryWorkers, instance)
		} else {
			item.Workers = append(item.Workers, instance)
		}
	}
}

func airWorkerRole(jobName string, worker platform.SSPAIRWorker) (string, string) {
	const prefix = "leaderworkerset.sigs.k8s.io/"
	labels := worker.Metadata.Labels
	if name := labels[prefix+"name"]; name != "" && name != jobName {
		return "AUXILIARY", ""
	}
	group, workerIndex := labels[prefix+"group-index"], labels[prefix+"worker-index"]
	// Older responses may omit metadata; LWS names encode the same indices.
	if group == "" || workerIndex == "" {
		if !strings.HasPrefix(worker.Name, jobName+"-") {
			return "AUXILIARY", ""
		}
		parts := strings.Split(strings.TrimPrefix(worker.Name, jobName+"-"), "-")
		switch len(parts) {
		case 1:
			group, workerIndex = parts[0], "0"
		case 2:
			group, workerIndex = parts[0], parts[1]
		default:
			return "AUXILIARY", ""
		}
	}
	groupNumber, groupErr := strconv.Atoi(group)
	index, indexErr := strconv.Atoi(workerIndex)
	if groupErr != nil || indexErr != nil || groupNumber < 0 || index < 0 {
		return "AUXILIARY", ""
	}
	if index == 0 {
		return "LEADER", strconv.Itoa(groupNumber)
	}
	return "WORKER", strconv.Itoa(groupNumber)
}

func (s *SSPAIRService) ListGateways(ctx context.Context, region string, workspace string) (*SSPAIRGatewayListResult, error) {
	workspaces, err := s.resolveWorkspaces(ctx, region, workspace)
	if err != nil {
		return nil, err
	}
	type load struct {
		items []platform.SSPAIRGateway
		err   error
	}
	loads := boundedMap(ctx, workspaces, 12, func(ctx context.Context, workspace platform.SSPWorkspace) load {
		items, queryErr := s.platform.ListSSPAIRGateways(ctx, workspace, "")
		return load{items: items, err: queryErr}
	})
	items := make([]SSPAIRGatewayItem, 0)
	var firstErr error
	for _, result := range loads {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		for _, item := range result.items {
			items = append(items, airGatewayItem(item))
		}
	}
	if len(items) == 0 && firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	return &SSPAIRGatewayListResult{Items: items}, nil
}

func (s *SSPAIRService) GetGateways(ctx context.Context, identifiers []string, region string, workspace string) ([]*SSPAIRGatewayItem, []error) {
	workspaces, err := s.resolveWorkspaces(ctx, region, workspace)
	if err != nil {
		return nil, []error{err}
	}
	result := make([]*SSPAIRGatewayItem, len(identifiers))
	errs := make([]error, len(identifiers))
	for index, identifier := range identifiers {
		type discovery struct {
			items []platform.SSPAIRGateway
			err   error
		}
		queries := boundedMap(ctx, workspaces, 12, func(ctx context.Context, candidate platform.SSPWorkspace) discovery {
			filter := identifier
			if airLooksLikeUUID(identifier) {
				filter = ""
			}
			items, queryErr := s.platform.ListSSPAIRGateways(ctx, candidate, filter)
			return discovery{items: items, err: queryErr}
		})
		matches := make([]SSPAIRGatewayItem, 0, 1)
		var firstErr error
		for _, query := range queries {
			if query.err != nil {
				if firstErr == nil {
					firstErr = query.err
				}
				continue
			}
			for _, candidate := range query.items {
				item := airGatewayItem(candidate)
				if len(matchAIRGateways(identifier, []SSPAIRGatewayItem{item})) > 0 {
					matches = append(matches, item)
				}
			}
		}
		switch len(matches) {
		case 0:
			if firstErr != nil {
				errs[index] = fmt.Errorf("AIR gateway %q: %w", identifier, firstErr)
			} else {
				errs[index] = fmt.Errorf("AIR gateway %q not found", identifier)
			}
		case 1:
			item := matches[0]
			pods, _ := s.findAIRPods(ctx, item.UID)
			setAIRNamespaces(&item.Namespace, &item.HostNamespace, pods)
			result[index] = &item
		default:
			errs[index] = fmt.Errorf("AIR gateway %q matched multiple workspaces; use --workspace", identifier)
		}
	}
	return result, compactErrors(errs)
}

func (s *SSPAIRService) findAIRPods(ctx context.Context, workloadUID string) ([]corev1.Pod, error) {
	if s == nil || s.clientset == nil || strings.TrimSpace(workloadUID) == "" {
		return nil, nil
	}
	podCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	pods, err := s.clientset.CoreV1().Pods(metav1.NamespaceAll).List(podCtx, metav1.ListOptions{
		LabelSelector:   labels.Set(map[string]string{sspWorkloadUIDLabel: workloadUID}).AsSelector().String(),
		ResourceVersion: "0",
	})
	if err != nil {
		return nil, err
	}
	return pods.Items, nil
}

func setAIRNamespaces(namespace *string, hostNamespace *string, pods []corev1.Pod) {
	if len(pods) == 0 {
		return
	}
	*hostNamespace = strings.TrimSpace(pods[0].Namespace)
	*namespace = aidWorkspaceNamespace(pods, *hostNamespace)
}

func airLooksLikeUUID(value string) bool {
	value = strings.TrimSpace(value)
	return len(value) == 36 && strings.Count(value, "-") == 4
}

func (s *SSPAIRService) resolveWorkspaces(ctx context.Context, region string, identifier string) ([]platform.SSPWorkspace, error) {
	items, err := s.resolveWorkspacesWithStatus(ctx, region, identifier)
	if len(items) > 0 {
		return items, nil
	}
	return items, err
}

func (s *SSPAIRService) resolveWorkspacesWithStatus(ctx context.Context, region string, identifier string) ([]platform.SSPWorkspace, error) {
	if s.platform == nil {
		return nil, fmt.Errorf("platform client is required")
	}
	regions := []string{strings.TrimSpace(region)}
	if regions[0] == "" {
		regions = s.platform.ConfiguredSSPRegions()
	}
	type load struct {
		region string
		items  []platform.SSPWorkspace
		err    error
	}
	loads := boundedMap(ctx, regions, 3, func(ctx context.Context, region string) load {
		items, queryErr := s.platform.ListSSPWorkspaces(ctx, region)
		return load{region: region, items: items, err: queryErr}
	})
	items := make([]platform.SSPWorkspace, 0)
	var failures []error
	for _, result := range loads {
		if result.err != nil {
			failures = append(failures, &SSPWorkspaceQueryError{Workspace: "workspace 清单", Region: result.region, Cause: result.err})
			continue
		}
		for _, workspace := range result.items {
			if identifier == "" || strings.EqualFold(strings.TrimSpace(workspace.Name), strings.TrimSpace(identifier)) || strings.EqualFold(strings.TrimSpace(workspace.UID), strings.TrimSpace(identifier)) {
				items = append(items, workspace)
			}
		}
	}
	if len(items) == 0 {
		if len(failures) > 0 {
			return nil, errors.Join(failures...)
		}
		if identifier != "" {
			return nil, fmt.Errorf("workspace %q not found", identifier)
		}
	}
	return items, errors.Join(failures...)
}

func (s *SSPAIRService) airJobItem(job platform.SSPAIRJob) SSPAIRJobItem {
	resource, image := airTemplateResource(job.Spec.LWS.LeaderWorkerTemplate.Leader)
	replicas := job.Status.Replicas
	if replicas == 0 {
		replicas = job.Spec.LWS.Replicas
	}
	desiredReplicas := job.Spec.LWS.Replicas
	groupSize := job.Spec.LWS.LeaderWorkerTemplate.Size
	totalInstances := job.TotalInstances
	if desiredReplicas > 0 && groupSize > 0 {
		totalInstances = desiredReplicas * groupSize
	}
	item := SSPAIRJobItem{
		Name: job.Name, UID: job.UID, State: job.Status.State, Workspace: job.WorkspaceName,
		Queue: job.Spec.Queue.Name, QueueType: job.Spec.Queue.Type, Cluster: resourceNameFromRID(job.Spec.Queue.ID, "clusters"),
		Priority: job.Spec.Priority, Creator: job.Ownership.CreatorName, Region: job.Region, Profile: job.ProfileName,
		ReadyReplicas: job.Status.ReadyReplicas, Replicas: replicas, CreatedAt: formatSSPTime(job.Status.CreateTime),
		DesiredReplicas: desiredReplicas, GroupSize: groupSize, TotalInstances: totalInstances,
		UpdatedAt: formatSSPTime(job.Status.UpdateTime), InternalIP: job.Status.LeaderServiceClusterIP,
		Resource: resource, Raw: job,
	}
	item.Resource.Image = image
	item.DNATRules = airDNATItems(job.Spec.DNATRules, job.Status.LeaderServiceClusterIP)
	for _, volume := range job.Spec.LWS.LeaderWorkerTemplate.VolumeMounts {
		item.Volumes = append(item.Volumes, SSPAIRVolumeItem{
			Type: volume.Type, Name: volume.Name, MountPath: volume.MountPath, Endpoint: airVolumeEndpoint(volume),
		})
	}
	return item
}

func (s *SSPAIRService) resolveAIRJobVCluster(ctx context.Context, item SSPAIRJobItem) string {
	if s == nil || s.platform == nil || strings.TrimSpace(item.Queue) == "" {
		return ""
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	queue, err := s.platform.GetSSPQueueResource(lookupCtx, item.Profile, item.Queue)
	if err != nil || queue == nil {
		return ""
	}
	return strings.TrimSpace(queue.VClusterName)
}

func airVolumeEndpoint(volume platform.SSPAIRVolumeMount) string {
	if endpoint := strings.TrimSpace(volume.Endpoint); endpoint != "" {
		return endpoint
	}
	volumeType := strings.ToLower(strings.TrimSpace(volume.Type))
	if (strings.Contains(volumeType, "afs") || strings.Contains(volumeType, "oceanstor")) && strings.TrimSpace(volume.ID) != "" {
		return "csi://" + strings.TrimPrefix(strings.TrimSpace(volume.ID), "csi://")
	}
	return ""
}

func airGatewayItem(gateway platform.SSPAIRGateway) SSPAIRGatewayItem {
	resource := airResourceItem(gateway.Spec.Resource)
	return SSPAIRGatewayItem{
		Name: gateway.Name, UID: gateway.UID, State: gateway.Status.State, Workspace: gateway.WorkspaceName,
		Queue: gateway.Spec.Queue.Name, QueueType: gateway.Spec.Queue.Type, Cluster: resourceNameFromRID(gateway.Spec.Queue.ID, "clusters"),
		Priority: gateway.Spec.Priority, Creator: gateway.Ownership.CreatorName, Region: gateway.Region, Profile: gateway.ProfileName,
		Replicas: gateway.Spec.Replicas, CreatedAt: formatSSPTime(gateway.Status.CreateTime), UpdatedAt: formatSSPTime(gateway.Status.UpdateTime),
		Resource: resource, DNATRules: airDNATItems(gateway.Spec.DNATRules, ""), Raw: gateway,
	}
}

func airTemplateResource(template platform.SSPAIRPodTemplate) (SSPAIRResourceItem, string) {
	if len(template.Containers) == 0 {
		return SSPAIRResourceItem{}, ""
	}
	return airResourceItem(template.Containers[0].Resource), template.Containers[0].Image.Path
}

func airResourceItem(resource platform.SSPAIRResource) SSPAIRResourceItem {
	accelerator := compactSSPValue(resource.AccelerateDeviceCount)
	if model := strings.TrimSpace(resource.AccelerateDeviceModel); model != "" {
		accelerator = strings.TrimSpace(accelerator + " " + model)
	}
	return SSPAIRResourceItem{
		MachineType: strings.Join(resource.MachineTypes, ","), CPU: compactSSPValue(resource.CPUCount),
		Memory: valueWithUnit(resource.MemoryGiB, "GiB"), Accelerator: accelerator,
		Model: strings.TrimSpace(resource.AccelerateDeviceModel), RDMA: resource.RDMAName,
	}
}

func airDNATItems(rules []platform.SSPAIRDNATRule, internalIP string) []SSPAIRDNATItem {
	items := make([]SSPAIRDNATItem, 0, len(rules))
	for _, rule := range rules {
		items = append(items, SSPAIRDNATItem{
			External: joinHostPort(rule.ExternalIP, rule.ExternalPort),
			Internal: joinHostPort(internalIP, compactSSPValue(rule.InternalPort)),
			Protocol: rule.Protocol, Gateway: rule.NATGatewayName,
		})
	}
	return items
}

func matchAIRJobs(identifier string, items []SSPAIRJobItem) []SSPAIRJobItem {
	result := make([]SSPAIRJobItem, 0, 1)
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(identifier), strings.TrimSpace(item.Name)) || strings.EqualFold(strings.TrimSpace(identifier), strings.TrimSpace(item.UID)) {
			result = append(result, item)
		}
	}
	return result
}

func matchAIRGateways(identifier string, items []SSPAIRGatewayItem) []SSPAIRGatewayItem {
	result := make([]SSPAIRGatewayItem, 0, 1)
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(identifier), strings.TrimSpace(item.Name)) || strings.EqualFold(strings.TrimSpace(identifier), strings.TrimSpace(item.UID)) {
			result = append(result, item)
		}
	}
	return result
}

func compactErrors(values []error) []error {
	result := make([]error, 0)
	for _, value := range values {
		if value != nil {
			result = append(result, value)
		}
	}
	return result
}

func resourceNameFromRID(rid string, kind string) string {
	parts := strings.Split(strings.Trim(rid, "/"), "/")
	for index := 0; index+1 < len(parts); index++ {
		if parts[index] == kind {
			return parts[index+1]
		}
	}
	return ""
}

func valueWithUnit(value any, unit string) string {
	text := compactSSPValue(value)
	if text == "" || strings.HasSuffix(strings.ToLower(text), strings.ToLower(unit)) {
		return text
	}
	return text + unit
}

func joinHostPort(host string, port string) string {
	host, port = strings.TrimSpace(host), strings.TrimSpace(port)
	if host == "" {
		return ""
	}
	if port == "" {
		return host
	}
	return host + ":" + port
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
