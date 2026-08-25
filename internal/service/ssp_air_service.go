package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"rayctl/internal/platform"
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
	Phase       string
	HostIP      string
	PodIP       string
	Restarts    int
	StartedAt   string
	LastStarted string
}

type SSPAIRJobItem struct {
	Name          string
	UID           string
	State         string
	Workspace     string
	Queue         string
	QueueType     string
	Cluster       string
	Priority      string
	Creator       string
	Region        string
	Profile       string
	ReadyReplicas int
	Replicas      int
	CreatedAt     string
	UpdatedAt     string
	InternalIP    string
	Resource      SSPAIRResourceItem
	DNATRules     []SSPAIRDNATItem
	Volumes       []SSPAIRVolumeItem
	Workers       []SSPAIRWorkerItem
	WorkerTotal   int
	Raw           platform.SSPAIRJob
}

type SSPAIRGatewayItem struct {
	Name      string
	UID       string
	State     string
	Workspace string
	Queue     string
	QueueType string
	Cluster   string
	Priority  string
	Creator   string
	Region    string
	Profile   string
	Replicas  int
	CreatedAt string
	UpdatedAt string
	Resource  SSPAIRResourceItem
	DNATRules []SSPAIRDNATItem
	Raw       platform.SSPAIRGateway
}

type SSPAIRJobListResult struct{ Items []SSPAIRJobItem }
type SSPAIRGatewayListResult struct{ Items []SSPAIRGatewayItem }

type SSPAIRService struct {
	platform *platform.VirtualClusterClient
}

func NewSSPAIRService(platformClient *platform.VirtualClusterClient) *SSPAIRService {
	return &SSPAIRService{platform: platformClient}
}

func (s *SSPAIRService) ListJobs(ctx context.Context, region string, workspace string) (*SSPAIRJobListResult, error) {
	workspaces, err := s.resolveWorkspaces(ctx, region, workspace)
	if err != nil {
		return nil, err
	}
	type load struct {
		items []platform.SSPAIRJob
		err   error
	}
	loads := boundedMap(ctx, workspaces, 12, func(ctx context.Context, workspace platform.SSPWorkspace) load {
		items, queryErr := s.platform.ListSSPAIRJobs(ctx, workspace, "")
		return load{items: items, err: queryErr}
	})
	items := make([]SSPAIRJobItem, 0)
	var firstErr error
	for _, result := range loads {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		for _, item := range result.items {
			items = append(items, s.airJobItem(item))
		}
	}
	if len(items) == 0 && firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	return &SSPAIRJobListResult{Items: items}, nil
}

func (s *SSPAIRService) GetJobs(ctx context.Context, identifiers []string, region string, workspace string, includeWorkers bool, workerLimit int) ([]*SSPAIRJobItem, []error) {
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
		if includeWorkers {
			workers, total, workersErr := s.platform.ListSSPAIRWorkers(ctx, *detail, workerLimit)
			if workersErr != nil {
				return detailResult{err: fmt.Errorf("AIR job %q workers: %w", identifiers[index], workersErr)}
			}
			item.WorkerTotal = total
			for _, worker := range workers {
				item.Workers = append(item.Workers, SSPAIRWorkerItem{
					Name: worker.Name, Phase: worker.Phase, HostIP: worker.HostIP, PodIP: worker.IP,
					Restarts: worker.RestartCount, StartedAt: formatSSPTime(worker.StartTime), LastStarted: formatSSPTime(worker.LastStartedTime),
				})
			}
		}
		return detailResult{item: &item}
	})
	result := make([]*SSPAIRJobItem, len(identifiers))
	for offset, detail := range details {
		index := indices[offset]
		if detail.err != nil {
			errs[index] = detail.err
			continue
		}
		result[index] = detail.item
	}
	return result, compactErrors(errs)
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
			result[index] = &item
		default:
			errs[index] = fmt.Errorf("AIR gateway %q matched multiple workspaces; use --workspace", identifier)
		}
	}
	return result, compactErrors(errs)
}

func airLooksLikeUUID(value string) bool {
	value = strings.TrimSpace(value)
	return len(value) == 36 && strings.Count(value, "-") == 4
}

func (s *SSPAIRService) resolveWorkspaces(ctx context.Context, region string, identifier string) ([]platform.SSPWorkspace, error) {
	if s.platform == nil {
		return nil, fmt.Errorf("platform client is required")
	}
	regions := []string{strings.TrimSpace(region)}
	if regions[0] == "" {
		regions = s.platform.ConfiguredSSPRegions()
	}
	type load struct {
		items []platform.SSPWorkspace
		err   error
	}
	loads := boundedMap(ctx, regions, 3, func(ctx context.Context, region string) load {
		items, queryErr := s.platform.ListSSPWorkspaces(ctx, region)
		return load{items: items, err: queryErr}
	})
	items := make([]platform.SSPWorkspace, 0)
	var firstErr error
	for _, result := range loads {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		for _, workspace := range result.items {
			if identifier == "" || strings.EqualFold(strings.TrimSpace(workspace.Name), strings.TrimSpace(identifier)) || strings.EqualFold(strings.TrimSpace(workspace.UID), strings.TrimSpace(identifier)) {
				items = append(items, workspace)
			}
		}
	}
	if len(items) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		if identifier != "" {
			return nil, fmt.Errorf("workspace %q not found", identifier)
		}
	}
	return items, nil
}

func (s *SSPAIRService) airJobItem(job platform.SSPAIRJob) SSPAIRJobItem {
	resource, image := airTemplateResource(job.Spec.LWS.LeaderWorkerTemplate.Leader)
	replicas := job.Status.Replicas
	if replicas == 0 {
		replicas = firstPositive(job.TotalInstances, job.Spec.LWS.Replicas)
	}
	item := SSPAIRJobItem{
		Name: job.Name, UID: job.UID, State: job.Status.State, Workspace: job.WorkspaceName,
		Queue: job.Spec.Queue.Name, QueueType: job.Spec.Queue.Type, Cluster: resourceNameFromRID(job.Spec.Queue.ID, "clusters"),
		Priority: job.Spec.Priority, Creator: job.Ownership.CreatorName, Region: job.Region, Profile: job.ProfileName,
		ReadyReplicas: job.Status.ReadyReplicas, Replicas: replicas, CreatedAt: formatSSPTime(job.Status.CreateTime),
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
