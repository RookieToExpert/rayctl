package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"rayctl/internal/kube"
	"rayctl/internal/platform"
)

type VCWorkloadResult struct {
	ClusterName string
	ClusterUID  string
	ProfileName string
	Items       []SSPQueueWorkloadItem
	Warnings    []string
}

// ListWorkloads combines SSP queue workloads and native ECP Volcano Jobs in a
// resolved VC. Kubernetes Deployments and other controller kinds are outside
// the workload APIs used by this command.
func (s *VCService) ListWorkloads(ctx context.Context, identifier string, query SSPQueueWorkloadQuery) (*VCWorkloadResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required")
	}
	if query.Limit < 0 {
		return nil, fmt.Errorf("workload limit must not be negative")
	}
	cluster, err := s.Get(ctx, identifier)
	if err != nil {
		return nil, err
	}
	result := &VCWorkloadResult{ClusterName: cluster.Name, ClusterUID: cluster.UID, ProfileName: cluster.Tenant, Items: make([]SSPQueueWorkloadItem, 0)}
	if strings.TrimSpace(cluster.Tenant) == "" || cluster.Tenant == "-" {
		return result, fmt.Errorf("cannot determine platform profile for vc %q", cluster.Name)
	}
	// A generic job filter includes both training jobs and native Volcano Jobs.
	sspQuery := query
	sspQuery.Limit = 0
	sspQuery.Type = normalizeScopedSSPWorkloadType(query.Type)
	dcloud := s.vcClient.ProfileEnvironment(cluster.Tenant) == platform.ClusterDCloud
	if dcloud && !includesECPWorkloadType(query.Type) {
		err := fmt.Errorf("SSP workload type %q is not available in DCloud", strings.TrimSpace(query.Type))
		result.Warnings = []string{err.Error()}
		return result, err
	}
	var sspResult <-chan asyncResult[*VCWorkloadResult]
	if !dcloud && !isECPOnlyWorkloadType(query.Type) {
		sspResult = asyncCall(ctx, func(ctx context.Context) (*VCWorkloadResult, error) {
			return s.listSSPVCWorkloads(ctx, *cluster, sspQuery)
		})
	}
	var ecpResult <-chan asyncResult[[]SSPQueueWorkloadItem]
	if includesECPWorkloadType(query.Type) {
		ecpResult = asyncCall(ctx, func(ctx context.Context) ([]SSPQueueWorkloadItem, error) {
			return s.listECPVCWorkloads(ctx, *cluster, query)
		})
	}
	errs := make([]error, 0)
	if sspResult != nil {
		load := <-sspResult
		if load.Value != nil {
			result.Items = append(result.Items, load.Value.Items...)
			result.Warnings = append(result.Warnings, load.Value.Warnings...)
		}
		if load.Err != nil {
			errs = append(errs, load.Err)
		}
	}
	if ecpResult != nil {
		load := <-ecpResult
		result.Items = append(result.Items, load.Value...)
		if load.Err != nil {
			err := fmt.Errorf("list ECP Volcano Jobs for vc %s: %w", cluster.Name, load.Err)
			result.Warnings = append(result.Warnings, err.Error())
			errs = append(errs, err)
		}
	}
	result.Items = sortScopedWorkloads(result.Items, query.Limit)
	if err := ctx.Err(); err != nil && len(errs) == 0 {
		result.Warnings = append(result.Warnings, err.Error())
		errs = append(errs, err)
	}
	return result, errors.Join(errs...)
}

func (s *VCService) listSSPVCWorkloads(ctx context.Context, cluster VCDetailResult, query SSPQueueWorkloadQuery) (*VCWorkloadResult, error) {
	result := &VCWorkloadResult{Items: make([]SSPQueueWorkloadItem, 0)}
	region := strings.TrimSpace(cluster.Region)
	if region == "-" {
		region = ""
	}
	clusters, err := s.vcClient.ListSSPClustersForProfile(ctx, cluster.Tenant, region)
	if err != nil {
		err = fmt.Errorf("list SSP clusters for vc %s: %w", cluster.Name, err)
		result.Warnings = []string{err.Error()}
		return result, err
	}
	matched := make([]platform.SSPCluster, 0)
	for _, candidate := range clusters {
		if sspClusterBelongsToVC(candidate, cluster) {
			matched = append(matched, candidate)
		}
	}
	type clusterQueues struct {
		items []SSPQueueItem
		err   error
	}
	loads := boundedMap(ctx, matched, 6, func(ctx context.Context, sspCluster platform.SSPCluster) clusterQueues {
		queues, err := s.vcClient.ListSSPClusterQueues(ctx, sspCluster)
		if err != nil {
			return clusterQueues{err: fmt.Errorf("list queues for SSP cluster %s: %w", sspCluster.Name, err)}
		}
		return clusterQueues{items: sspClusterQueueItems(sspClusterItem(sspCluster), queues)}
	})
	queues := make([]SSPQueueItem, 0)
	errs := make([]error, 0)
	for _, load := range loads {
		queues = append(queues, load.items...)
		if load.err != nil {
			result.Warnings = append(result.Warnings, load.err.Error())
			errs = append(errs, load.err)
		}
	}
	resources := NewSSPResourceService(nil, s.vcClient)
	var warnings []string
	result.Items, warnings, err = resources.collectScopedQueueWorkloads(ctx, queues, nil, query)
	result.Warnings = append(result.Warnings, warnings...)
	if err != nil {
		errs = append(errs, err)
	}
	return result, errors.Join(errs...)
}

func sspClusterBelongsToVC(candidate platform.SSPCluster, cluster VCDetailResult) bool {
	if !strings.EqualFold(strings.TrimSpace(candidate.ProfileName), strings.TrimSpace(cluster.Tenant)) ||
		(strings.TrimSpace(cluster.Subscription) != "" && !strings.EqualFold(strings.TrimSpace(candidate.Subscription), strings.TrimSpace(cluster.Subscription))) ||
		(cluster.Region != "" && cluster.Region != "-" && !strings.EqualFold(strings.TrimSpace(candidate.Region), strings.TrimSpace(cluster.Region))) {
		return false
	}
	sourceUID := strings.TrimSpace(candidate.Properties.Source.UID)
	if sourceUID == "" {
		resourceID := strings.TrimPrefix(lastResourceSegment(candidate.Properties.Source.ID), "vc-")
		if looksLikeUUID(resourceID) {
			sourceUID = resourceID
		}
	}
	if sourceUID != "" && strings.TrimSpace(cluster.UID) != "" {
		return strings.EqualFold(strings.TrimPrefix(sourceUID, "vc-"), strings.TrimPrefix(strings.TrimSpace(cluster.UID), "vc-"))
	}
	sourceName := firstNonEmpty(candidate.Properties.Source.Name, lastResourceSegment(candidate.Properties.Source.ID))
	return strings.EqualFold(strings.TrimSpace(sourceName), strings.TrimSpace(cluster.Name))
}

func (s *VCService) listECPVCWorkloads(ctx context.Context, cluster VCDetailResult, query SSPQueueWorkloadQuery) ([]SSPQueueWorkloadItem, error) {
	var jobs []unstructured.Unstructured
	var err error
	if s.vcClient.ProfileEnvironment(cluster.Tenant) == platform.ClusterDCloud {
		kubeconfig := localDCloudVCKubeconfig(platform.VirtualCluster{Name: cluster.Name})
		if kubeconfig == "" {
			return nil, fmt.Errorf("本地 VC kubeconfig 不存在")
		}
		jobs, err = listLocalVCWorkloadJobs(ctx, kubeconfig)
	} else {
		vcRef := cluster.Name
		if strings.TrimSpace(cluster.UID) != "" {
			vcRef = "vc-" + strings.TrimPrefix(strings.TrimSpace(cluster.UID), "vc-")
		}
		// This cached endpoint already supports the state filter. Priority is a
		// Kubernetes priorityClassName, so filter it after reading the pages.
		filter := ""
		if state := strings.TrimSpace(query.State); state != "" {
			filter = fmt.Sprintf("state=%q", state)
		}
		jobs, err = s.vcClient.ListVolcanoJobsPageForProfile(ctx, cluster.Tenant, vcRef, filter, 0)
	}
	if err != nil {
		return nil, err
	}
	items := make([]SSPQueueWorkloadItem, 0, len(jobs))
	for index := range jobs {
		job := &jobs[index]
		if isSSPManagedVCWorkloadType(job.GetLabels()[sspWorkloadTypeLabel]) {
			continue
		}
		item := ecpVCWorkloadItem(job)
		if state := strings.TrimSpace(query.State); state != "" && !strings.EqualFold(strings.TrimSpace(item.State), state) {
			continue
		}
		if priority := strings.TrimSpace(query.Priority); priority != "" && !strings.EqualFold(strings.TrimSpace(item.Priority), priority) {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func listLocalVCWorkloadJobs(ctx context.Context, kubeconfig string) ([]unstructured.Unstructured, error) {
	restConfig, err := kube.NewRestConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	restConfig.Timeout = 4 * time.Second
	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create VC client: %w", err)
	}
	list, err := client.Resource(volcanoJobGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func ecpVCWorkloadItem(job *unstructured.Unstructured) SSPQueueWorkloadItem {
	return SSPQueueWorkloadItem{
		Queue: getNestedString(job.Object, "spec", "queue"), Namespace: job.GetNamespace(), Type: "vcJob",
		Name: firstNonEmpty(job.GetAnnotations()["vcluster.loft.sh/object-name"], job.GetName()), UID: string(job.GetUID()),
		State: extractVolcanoJobPhase(job), Workspace: job.GetLabels()[sspWorkspaceNameLabel],
		Priority:  getNestedString(job.Object, "spec", "priorityClassName"),
		Creator:   firstNonEmpty(job.GetLabels()["lepton.sensetime.com/submitter"], job.GetAnnotations()["lepton.sensetime.com/submitter"], job.GetAnnotations()["identity.compute.sensecore.cn/user-name"]),
		CreatorID: job.GetLabels()["identity.compute.sensecore.cn/user-id"], CreatedAt: formatJobTime(job.GetCreationTimestamp().Time),
	}
}

func isECPOnlyWorkloadType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "vcjob", "vc-job", "ecp-vcjob", "volcano", "volcanojob", "volcano-job":
		return true
	default:
		return false
	}
}

func includesECPWorkloadType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "job", "trainingjob", "training-job":
		return true
	default:
		return isECPOnlyWorkloadType(value)
	}
}

func isSSPManagedVCWorkloadType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "trainingjob", "training-job", "training_job", "aid", "air", "infergateway", "infer-gateway", "gw", "gateway":
		return true
	default:
		return false
	}
}

func normalizeScopedSSPWorkloadType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "job", "trainingjob", "training-job":
		return "trainingJob"
	case "aid":
		return "aid"
	case "gw", "gateway", "infergateway", "infer-gateway":
		return "inferGateway"
	default:
		return strings.TrimSpace(value)
	}
}
