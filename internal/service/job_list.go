package service

import (
	"context"
	"errors"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"net/http"
	"os"
	"path/filepath"
	"rayctl/internal/kube"
	"rayctl/internal/platform"
	"sort"
	"strings"
	"sync"
	"time"
)

func (s *JobService) GetClusterJobs(ctx context.Context, identifier string, includeInactive bool, statusFilter string) (*JobClusterListResult, error) {
	if s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for cluster job listing")
	}

	targetName, vcRef, profileName, _, err := s.resolveClusterTarget(ctx, identifier)
	if err != nil {
		return nil, err
	}
	showInactive := includeInactive || strings.EqualFold(strings.TrimSpace(statusFilter), "all")
	if !showInactive && strings.TrimSpace(profileName) != "" {
		return s.getActiveClusterJobs(ctx, targetName, vcRef, profileName, statusFilter)
	}

	var list []unstructured.Unstructured
	var allPods []corev1.Pod
	var jobsErr, podsErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if strings.TrimSpace(profileName) != "" {
			list, jobsErr = s.vcClient.ListVolcanoJobsForProfile(ctx, profileName, vcRef, "default")
			return
		}
		list, jobsErr = s.vcClient.ListVolcanoJobs(ctx, vcRef, "default")
	}()
	go func() {
		defer wg.Done()
		if strings.TrimSpace(profileName) != "" {
			allPods, podsErr = s.vcClient.ListPodsForProfile(ctx, profileName, vcRef, "default")
			return
		}
		allPods, podsErr = s.vcClient.ListPods(ctx, vcRef, "default")
	}()
	wg.Wait()
	if jobsErr != nil {
		return nil, fmt.Errorf("list volcano jobs for cluster %q: %w", targetName, jobsErr)
	}
	if podsErr != nil {
		return nil, fmt.Errorf("list pods for cluster %q: %w", targetName, podsErr)
	}
	podsByJob := make(map[string][]corev1.Pod)
	for _, pod := range allPods {
		jobName := firstNonEmpty(strings.TrimSpace(pod.Labels["volcano.sh/job-name"]), deriveJobName(pod))
		if strings.TrimSpace(jobName) == "" {
			continue
		}
		podsByJob[jobName] = append(podsByJob[jobName], pod)
	}

	items := make([]JobClusterItem, 0, len(list))
	activeJobCount := 0
	activePodCount := 0
	totalJobCount := 0
	totalPodCount := 0
	for i := range list {
		job := &list[i]
		status := extractVolcanoJobPhase(job)
		jobName := firstNonEmpty(
			getNestedString(job.Object, "metadata", "annotations", "vcluster.loft.sh/object-name"),
			job.GetName(),
		)
		pods := podsByJob[jobName]
		_, _, activeCount := summarizeJobPods(pods)
		totalJobCount++
		totalPodCount += len(pods)
		if isActiveVolcanoJobPhase(status) {
			activeJobCount++
			activePodCount += activeCount
		}
		if !showInactive && !isActiveVolcanoJobPhase(status) {
			continue
		}
		items = append(items, JobClusterItem{
			ClusterName:    targetName,
			JobName:        jobName,
			Submitter:      firstNonEmpty(getNestedString(job.Object, "metadata", "labels", "lepton.sensetime.com/submitter"), getNestedString(job.Object, "metadata", "annotations", "lepton.sensetime.com/submitter"), "-"),
			Status:         status,
			CreatedAt:      job.GetCreationTimestamp().Time.In(utcPlus8).Format("2006-01-02 15:04:05"),
			CreatedAtShort: job.GetCreationTimestamp().Time.In(utcPlus8).Format("01-02 15:04"),
			CreatedAtTime:  job.GetCreationTimestamp().Time,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAtTime.Equal(items[j].CreatedAtTime) {
			return items[i].CreatedAtTime.After(items[j].CreatedAtTime)
		}
		return items[i].JobName < items[j].JobName
	})
	items = filterClusterItemsByStatus(items, statusFilter)

	return &JobClusterListResult{
		ClusterName:    targetName,
		Items:          items,
		ActiveJobCount: activeJobCount,
		ActivePodCount: activePodCount,
		TotalJobCount:  totalJobCount,
		TotalPodCount:  totalPodCount,
		StatusFilter:   normalizeClusterStatusFilter(statusFilter),
	}, nil
}

func (s *JobService) getActiveClusterJobs(ctx context.Context, targetName string, vcRef string, profileName string, statusFilter string) (*JobClusterListResult, error) {
	return s.getActiveClusterJobsWithTotals(ctx, targetName, vcRef, profileName, statusFilter, true)
}

func (s *JobService) getActiveClusterJobsWithTotals(ctx context.Context, targetName string, vcRef string, profileName string, statusFilter string, includeJobTotal bool) (*JobClusterListResult, error) {
	totalJobCount := -1
	var totalPodCount int
	var allPods []corev1.Pod
	var jobsErr, podsErr error
	var wg sync.WaitGroup
	if includeJobTotal {
		wg.Add(1)
		go func() {
			defer wg.Done()
			totalJobCount, jobsErr = s.vcClient.CountVolcanoJobsForProfile(ctx, profileName, vcRef, "default")
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		allPods, podsErr = s.vcClient.ListActivePodsForProfile(ctx, profileName, vcRef, "default")
	}()
	wg.Wait()
	if jobsErr != nil {
		return nil, fmt.Errorf("count volcano jobs for cluster %q: %w", targetName, jobsErr)
	}
	if podsErr != nil {
		return nil, fmt.Errorf("list pods for cluster %q: %w", targetName, podsErr)
	}

	podsByJob := make(map[string][]corev1.Pod)
	for _, pod := range allPods {
		jobName := firstNonEmpty(strings.TrimSpace(pod.Labels["volcano.sh/job-name"]), deriveJobName(pod))
		if strings.TrimSpace(jobName) == "" {
			continue
		}
		podsByJob[jobName] = append(podsByJob[jobName], pod)
	}
	// The active path intentionally avoids downloading historical Pods. A
	// negative total tells the renderer that the all-time Pod count was skipped.
	totalPodCount = -1

	activeNames := make([]string, 0)
	for jobName, pods := range podsByJob {
		_, _, activeCount := summarizeJobPods(pods)
		if activeCount > 0 {
			activeNames = append(activeNames, jobName)
		}
	}
	sort.Strings(activeNames)

	items := make([]JobClusterItem, 0, len(activeNames))
	activeJobCount := 0
	activePodCount := 0
	for _, jobName := range activeNames {
		pods := podsByJob[jobName]
		_, pendingPods, activePods := summarizeJobPods(pods)
		status := "Running"
		if pendingPods > 0 {
			status = "Pending"
		}
		submitter := "-"
		createdAt := time.Time{}
		for _, pod := range pods {
			submitter = firstNonEmpty(strings.TrimSpace(pod.Labels["lepton.sensetime.com/submitter"]), strings.TrimSpace(pod.Annotations["lepton.sensetime.com/submitter"]), submitter)
			if createdAt.IsZero() || pod.CreationTimestamp.Time.Before(createdAt) {
				createdAt = pod.CreationTimestamp.Time
			}
		}
		activeJobCount++
		activePodCount += activePods
		items = append(items, JobClusterItem{
			ClusterName:    targetName,
			JobName:        jobName,
			Submitter:      submitter,
			Status:         status,
			CreatedAt:      createdAt.In(utcPlus8).Format("2006-01-02 15:04:05"),
			CreatedAtShort: createdAt.In(utcPlus8).Format("01-02 15:04"),
			CreatedAtTime:  createdAt,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAtTime.Equal(items[j].CreatedAtTime) {
			return items[i].CreatedAtTime.After(items[j].CreatedAtTime)
		}
		return items[i].JobName < items[j].JobName
	})
	items = filterClusterItemsByStatus(items, statusFilter)

	return &JobClusterListResult{
		ClusterName:    targetName,
		Items:          items,
		ActiveJobCount: activeJobCount,
		ActivePodCount: activePodCount,
		TotalJobCount:  totalJobCount,
		TotalPodCount:  totalPodCount,
		StatusFilter:   normalizeClusterStatusFilter(statusFilter),
	}, nil
}

func (s *JobService) GetCurrentTenantClusterJobs(ctx context.Context, includeInactive bool, statusFilter string) (*JobClusterListResult, error) {
	if s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for cluster job listing")
	}

	vclusters, err := s.vcClient.ListCurrentProfileVirtualClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list current tenant virtual clusters: %w", err)
	}
	showInactive := includeInactive || strings.EqualFold(strings.TrimSpace(statusFilter), "all")
	if !showInactive {
		return s.getCurrentTenantActiveClusterJobs(ctx, vclusters, statusFilter)
	}

	items := make([]JobClusterItem, 0)
	activeJobCount := 0
	activePodCount := 0
	totalJobCount := 0
	totalPodCount := 0

	for _, vc := range vclusters {
		clusterName := firstNonEmpty(strings.TrimSpace(vc.Name), strings.TrimSpace(vc.DisplayName), "vc-"+strings.TrimSpace(vc.UID))
		vcRef := firstNonEmpty("vc-"+strings.TrimSpace(vc.UID), strings.TrimSpace(vc.Name), strings.TrimSpace(vc.UID))

		list, err := s.vcClient.ListVolcanoJobsForProfile(ctx, vc.ProfileName, vcRef, "default")
		if err != nil {
			return nil, fmt.Errorf("list volcano jobs for cluster %q: %w", clusterName, err)
		}
		allPods, err := s.vcClient.ListPodsForProfile(ctx, vc.ProfileName, vcRef, "default")
		if err != nil {
			return nil, fmt.Errorf("list pods for cluster %q: %w", clusterName, err)
		}
		podsByJob := make(map[string][]corev1.Pod)
		for _, pod := range allPods {
			jobName := firstNonEmpty(strings.TrimSpace(pod.Labels["volcano.sh/job-name"]), deriveJobName(pod))
			if strings.TrimSpace(jobName) == "" {
				continue
			}
			podsByJob[jobName] = append(podsByJob[jobName], pod)
		}

		for i := range list {
			job := &list[i]
			status := extractVolcanoJobPhase(job)
			jobName := firstNonEmpty(
				getNestedString(job.Object, "metadata", "annotations", "vcluster.loft.sh/object-name"),
				job.GetName(),
			)
			pods := podsByJob[jobName]
			_, _, activeCount := summarizeJobPods(pods)
			totalJobCount++
			totalPodCount += len(pods)
			if isActiveVolcanoJobPhase(status) {
				activeJobCount++
				activePodCount += activeCount
			}
			if !showInactive && !isActiveVolcanoJobPhase(status) {
				continue
			}
			items = append(items, JobClusterItem{
				ClusterName:    clusterName,
				JobName:        jobName,
				Submitter:      firstNonEmpty(getNestedString(job.Object, "metadata", "labels", "lepton.sensetime.com/submitter"), getNestedString(job.Object, "metadata", "annotations", "lepton.sensetime.com/submitter"), "-"),
				Status:         status,
				CreatedAt:      job.GetCreationTimestamp().Time.In(utcPlus8).Format("2006-01-02 15:04:05"),
				CreatedAtShort: job.GetCreationTimestamp().Time.In(utcPlus8).Format("01-02 15:04"),
				CreatedAtTime:  job.GetCreationTimestamp().Time,
			})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAtTime.Equal(items[j].CreatedAtTime) {
			return items[i].CreatedAtTime.After(items[j].CreatedAtTime)
		}
		if items[i].ClusterName != items[j].ClusterName {
			return items[i].ClusterName < items[j].ClusterName
		}
		return items[i].JobName < items[j].JobName
	})
	items = filterClusterItemsByStatus(items, statusFilter)

	return &JobClusterListResult{
		ClusterName:     "当前租户全部分区",
		Items:           items,
		ActiveJobCount:  activeJobCount,
		ActivePodCount:  activePodCount,
		TotalJobCount:   totalJobCount,
		TotalPodCount:   totalPodCount,
		ShowClusterName: true,
		StatusFilter:    normalizeClusterStatusFilter(statusFilter),
	}, nil
}

// ListCurrentTenantVolcanoJobs queries the ECP cached Job endpoint for each VC.
// Unlike GetCurrentTenantClusterJobs, this path does not infer jobs from Pods.
func (s *JobService) ListCurrentTenantVolcanoJobs(ctx context.Context, includeInactive bool, statusFilter string, limit int) (*JobClusterListResult, error) {
	return s.ListCurrentTenantVolcanoJobsSince(ctx, includeInactive, statusFilter, limit, time.Time{})
}

func (s *JobService) ListCurrentTenantVolcanoJobsSince(ctx context.Context, includeInactive bool, statusFilter string, limit int, createdAfter time.Time) (*JobClusterListResult, error) {
	if s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for cluster job listing")
	}
	if limit < 0 {
		return nil, fmt.Errorf("job list limit must not be negative")
	}

	vclusters, err := s.vcClient.ListCurrentProfileVirtualClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list current tenant virtual clusters: %w", err)
	}
	filter := volcanoJobAPIFilter(includeInactive, statusFilter)

	type clusterResult struct {
		name string
		jobs []unstructured.Unstructured
		err  error
	}
	results := boundedMap(ctx, vclusters, 32, func(ctx context.Context, vc platform.VirtualCluster) clusterResult {
		clusterName := firstNonEmpty(strings.TrimSpace(vc.Name), strings.TrimSpace(vc.DisplayName), "vc-"+strings.TrimSpace(vc.UID))
		vcRef := firstNonEmpty("vc-"+strings.TrimSpace(vc.UID), strings.TrimSpace(vc.Name), strings.TrimSpace(vc.UID))
		if s.vcClient.ProfileEnvironment(vc.ProfileName) == platform.ClusterDCloud {
			kubeconfig := localDCloudVCKubeconfig(vc)
			if kubeconfig == "" {
				return clusterResult{name: clusterName, err: fmt.Errorf("本地 VC kubeconfig 不存在")}
			}
			jobs, queryErr := listLocalVolcanoJobs(ctx, kubeconfig, includeInactive, statusFilter, limit, createdAfter)
			return clusterResult{name: clusterName, jobs: jobs, err: queryErr}
		}
		jobs, queryErr := s.vcClient.ListVolcanoJobsPageForProfileSince(ctx, vc.ProfileName, vcRef, filter, limit, createdAfter)
		return clusterResult{name: clusterName, jobs: jobs, err: queryErr}
	})

	items := make([]JobClusterItem, 0)
	warnings := make([]string, 0)
	succeeded := 0
	only404 := len(results) > 0
	for _, result := range results {
		if result.err != nil {
			var status *platform.HTTPStatusError
			if !errors.As(result.err, &status) || status.StatusCode != http.StatusNotFound {
				only404 = false
			}
			warnings = append(warnings, fmt.Sprintf("跳过 VC %s：%s", result.name, conciseJobListError(result.err)))
			continue
		}
		only404 = false
		succeeded++
		for index := range result.jobs {
			job := &result.jobs[index]
			createdAt := job.GetCreationTimestamp().Time
			eventAt := createdAt
			if normalizeClusterStatusFilter(statusFilter) == "failed" {
				eventAt = volcanoJobTerminalTimestamp(job)
			}
			items = append(items, JobClusterItem{
				ClusterName: result.name,
				JobName: firstNonEmpty(
					getNestedString(job.Object, "metadata", "annotations", "vcluster.loft.sh/object-name"),
					job.GetName(),
				),
				Submitter: firstNonEmpty(
					getNestedString(job.Object, "metadata", "labels", "lepton.sensetime.com/submitter"),
					getNestedString(job.Object, "metadata", "annotations", "lepton.sensetime.com/submitter"),
					getNestedString(job.Object, "metadata", "annotations", "identity.compute.sensecore.cn/user-name"),
					getNestedString(job.Object, "metadata", "labels", "identity.compute.sensecore.cn/user-id"),
					"-",
				),
				Status:         extractVolcanoJobPhase(job),
				CreatedAt:      createdAt.In(utcPlus8).Format("2006-01-02 15:04:05"),
				CreatedAtShort: createdAt.In(utcPlus8).Format("01-02 15:04"),
				CreatedAtTime:  createdAt,
				eventAtTime:    eventAt,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].eventAtTime.Equal(items[j].eventAtTime) {
			return items[i].eventAtTime.After(items[j].eventAtTime)
		}
		if items[i].ClusterName != items[j].ClusterName {
			return items[i].ClusterName < items[j].ClusterName
		}
		return items[i].JobName < items[j].JobName
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if only404 && s.vcClient.ProfileEnvironment(s.vcClient.CurrentProfileName()) == "pt" {
		return nil, ErrPTATEUnavailable
	}
	if succeeded == 0 && len(vclusters) > 0 {
		return nil, fmt.Errorf("全部 %d 个 VC 的任务查询均失败；%s", len(vclusters), firstNonEmpty(strings.TrimPrefix(warnings[0], "跳过 VC "), "请检查平台或 VC kubeconfig"))
	}
	return &JobClusterListResult{
		ClusterName:     "当前租户全部分区",
		Items:           items,
		ShowClusterName: true,
		StatusFilter:    normalizeClusterStatusFilter(statusFilter),
		TotalJobCount:   -1,
		TotalPodCount:   -1,
		Warnings:        warnings,
	}, nil
}

func localDCloudVCKubeconfig(vc platform.VirtualCluster) string {
	name := strings.TrimSpace(vc.Name)
	if !strings.HasPrefix(name, "vc-") || filepath.Base(name) != name {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	path := filepath.Join(home, "Dcloud", name)
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return ""
	}
	return path
}

func listLocalVolcanoJobs(ctx context.Context, kubeconfig string, includeInactive bool, statusFilter string, limit int, createdAfter time.Time) ([]unstructured.Unstructured, error) {
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
	return filterLocalECPVolcanoJobs(list.Items, includeInactive, statusFilter, limit, createdAfter), nil
}

func filterLocalECPVolcanoJobs(jobs []unstructured.Unstructured, includeInactive bool, statusFilter string, limit int, createdAfter time.Time) []unstructured.Unstructured {
	filtered := make([]unstructured.Unstructured, 0, len(jobs))
	stateFilter := normalizeClusterStatusFilter(statusFilter)
	for index := range jobs {
		job := jobs[index]
		submitter := firstNonEmpty(
			getNestedString(job.Object, "metadata", "labels", "lepton.sensetime.com/submitter"),
			getNestedString(job.Object, "metadata", "annotations", "lepton.sensetime.com/submitter"),
		)
		if job.GetNamespace() != "default" && strings.TrimSpace(submitter) == "" {
			continue
		}
		eventAt := job.GetCreationTimestamp().Time
		if stateFilter == "failed" {
			eventAt = volcanoJobTerminalTimestamp(&job)
		}
		if !createdAfter.IsZero() && !eventAt.IsZero() && eventAt.Before(createdAfter) {
			continue
		}
		phase := extractVolcanoJobPhase(&job)
		if stateFilter == "failed" {
			if !strings.EqualFold(phase, stateFilter) {
				continue
			}
		} else if !includeInactive && !strings.EqualFold(strings.TrimSpace(statusFilter), "all") {
			switch stateFilter {
			case "pending", "running":
				if !strings.EqualFold(phase, stateFilter) {
					continue
				}
			default:
				if !isActiveVolcanoJobPhase(phase) {
					continue
				}
			}
		}
		filtered = append(filtered, job)
	}
	sort.Slice(filtered, func(i, j int) bool {
		left := filtered[i].GetCreationTimestamp().Time
		right := filtered[j].GetCreationTimestamp().Time
		if stateFilter == "failed" {
			left = volcanoJobTerminalTimestamp(&filtered[i])
			right = volcanoJobTerminalTimestamp(&filtered[j])
		}
		return left.After(right)
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

func volcanoJobTerminalTimestamp(job *unstructured.Unstructured) time.Time {
	if job == nil {
		return time.Time{}
	}
	latest := parseJobTimestamp(firstNonEmpty(
		getNestedText(job.Object, "status", "state", "lastTransitionTime"),
		getNestedText(job.Object, "status", "completionTime"),
		getNestedText(job.Object, "status", "finishedAt"),
	))
	conditions, _, _ := unstructured.NestedSlice(job.Object, "status", "conditions")
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		transition := parseJobTimestamp(firstNonEmpty(
			getNestedText(condition, "lastTransitionTime"),
			getNestedText(condition, "transitionTime"),
			getNestedText(condition, "lastUpdateTime"),
		))
		if transition.After(latest) {
			latest = transition
		}
	}
	if latest.IsZero() {
		return job.GetCreationTimestamp().Time
	}
	return latest
}

func conciseJobListError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "context deadline exceeded") {
		return "查询超时"
	}
	message := strings.TrimSpace(err.Error())
	if index := strings.Index(message, ": request "); index >= 0 {
		message = message[:index]
	}
	if message == "" {
		return "查询失败"
	}
	return message
}

func volcanoJobAPIStateFilter(includeInactive bool, statusFilter string) string {
	filter := normalizeClusterStatusFilter(statusFilter)
	if filter == "failed" {
		return fmt.Sprintf(`state=%q`, "Failed")
	}
	if includeInactive || strings.EqualFold(strings.TrimSpace(statusFilter), "all") {
		return ""
	}
	switch filter {
	case "pending":
		return `state="Pending"`
	case "running":
		return `state="Running"`
	default:
		return `(state="Running" OR state="Pending")`
	}
}

func volcanoJobAPIFilter(includeInactive bool, statusFilter string) string {
	stateFilter := volcanoJobAPIStateFilter(includeInactive, statusFilter)
	return joinVolcanoJobAPIFilter(`(namespace="default" OR submitter!="")`, stateFilter)
}

func joinVolcanoJobAPIFilter(left string, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	return left + " AND " + right
}

func (s *JobService) getCurrentTenantActiveClusterJobs(ctx context.Context, vclusters []platform.VirtualCluster, statusFilter string) (*JobClusterListResult, error) {
	type clusterResult struct {
		name   string
		result *JobClusterListResult
		err    error
	}
	results := boundedMap(ctx, vclusters, 24, func(ctx context.Context, vc platform.VirtualCluster) clusterResult {
		clusterName := firstNonEmpty(strings.TrimSpace(vc.Name), strings.TrimSpace(vc.DisplayName), "vc-"+strings.TrimSpace(vc.UID))
		vcRef := firstNonEmpty("vc-"+strings.TrimSpace(vc.UID), strings.TrimSpace(vc.Name), strings.TrimSpace(vc.UID))
		result, err := s.getActiveClusterJobsWithTotals(ctx, clusterName, vcRef, vc.ProfileName, statusFilter, false)
		return clusterResult{name: clusterName, result: result, err: err}
	})

	combined := &JobClusterListResult{
		ClusterName:     "当前租户全部分区",
		ShowClusterName: true,
		StatusFilter:    normalizeClusterStatusFilter(statusFilter),
		TotalJobCount:   -1,
		TotalPodCount:   -1,
	}
	for _, query := range results {
		if query.err != nil {
			return nil, fmt.Errorf("list active jobs for cluster %q: %w", query.name, query.err)
		}
		if query.result == nil {
			continue
		}
		combined.Items = append(combined.Items, query.result.Items...)
		combined.ActiveJobCount += query.result.ActiveJobCount
		combined.ActivePodCount += query.result.ActivePodCount
	}
	sort.Slice(combined.Items, func(i, j int) bool {
		if !combined.Items[i].CreatedAtTime.Equal(combined.Items[j].CreatedAtTime) {
			return combined.Items[i].CreatedAtTime.After(combined.Items[j].CreatedAtTime)
		}
		if combined.Items[i].ClusterName != combined.Items[j].ClusterName {
			return combined.Items[i].ClusterName < combined.Items[j].ClusterName
		}
		return combined.Items[i].JobName < combined.Items[j].JobName
	})
	return combined, nil
}

// GetCurrentTenantUserJobs returns active jobs submitted by the requested users.
// Unlike the cluster summary path, it does not fetch Pods because user output does
// not need Pod counts.
func (s *JobService) GetCurrentTenantUserJobs(ctx context.Context, submitters []string) ([]JobClusterItem, error) {
	return s.GetCurrentTenantUserJobsWithStatus(ctx, submitters, false)
}

// GetCurrentTenantUserJobsWithStatus returns jobs submitted by the requested
// users. The legacy user --jobs path passes false to preserve active-only output.
func (s *JobService) GetCurrentTenantUserJobsWithStatus(ctx context.Context, submitters []string, includeInactive bool) ([]JobClusterItem, error) {
	if s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for user job listing")
	}

	normalized := make([]string, 0, len(submitters))
	seen := make(map[string]struct{}, len(submitters))
	for _, submitter := range submitters {
		submitter = strings.TrimSpace(submitter)
		key := strings.ToLower(submitter)
		if submitter == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, submitter)
	}
	if len(normalized) == 0 {
		return []JobClusterItem{}, nil
	}

	vclusters, err := s.vcClient.ListCurrentProfileVirtualClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list current tenant virtual clusters: %w", err)
	}

	selector := "lepton.sensetime.com/submitter=" + normalized[0]
	if len(normalized) > 1 {
		selector = fmt.Sprintf("lepton.sensetime.com/submitter in (%s)", strings.Join(normalized, ","))
	}

	type clusterJobsResult struct {
		clusterName string
		jobs        []unstructured.Unstructured
		err         error
	}
	results := make(chan clusterJobsResult, len(vclusters))
	semaphore := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, vc := range vclusters {
		vc := vc
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				results <- clusterJobsResult{err: ctx.Err()}
				return
			}

			clusterName := firstNonEmpty(strings.TrimSpace(vc.Name), strings.TrimSpace(vc.DisplayName), "vc-"+strings.TrimSpace(vc.UID))
			vcRef := firstNonEmpty("vc-"+strings.TrimSpace(vc.UID), strings.TrimSpace(vc.Name), strings.TrimSpace(vc.UID))
			jobs, listErr := s.vcClient.ListVolcanoJobsByLabelSelector(ctx, vcRef, "default", selector)
			results <- clusterJobsResult{clusterName: clusterName, jobs: jobs, err: listErr}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	items := make([]JobClusterItem, 0)
	for result := range results {
		if result.err != nil {
			return nil, fmt.Errorf("list volcano jobs for cluster %q: %w", result.clusterName, result.err)
		}
		for i := range result.jobs {
			job := &result.jobs[i]
			status := extractVolcanoJobPhase(job)
			if !includeInactive && !isActiveVolcanoJobPhase(status) {
				continue
			}
			submitter := firstNonEmpty(
				getNestedString(job.Object, "metadata", "labels", "lepton.sensetime.com/submitter"),
				getNestedString(job.Object, "metadata", "annotations", "lepton.sensetime.com/submitter"),
				"-",
			)
			if _, ok := seen[strings.ToLower(strings.TrimSpace(submitter))]; !ok {
				continue
			}
			createdAt := job.GetCreationTimestamp().Time
			items = append(items, JobClusterItem{
				ClusterName:    result.clusterName,
				JobName:        firstNonEmpty(getNestedString(job.Object, "metadata", "annotations", "vcluster.loft.sh/object-name"), job.GetName()),
				Submitter:      submitter,
				Status:         status,
				CreatedAt:      createdAt.In(utcPlus8).Format("2006-01-02 15:04:05"),
				CreatedAtShort: createdAt.In(utcPlus8).Format("01-02 15:04"),
				CreatedAtTime:  createdAt,
			})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAtTime.Equal(items[j].CreatedAtTime) {
			return items[i].CreatedAtTime.After(items[j].CreatedAtTime)
		}
		if items[i].ClusterName != items[j].ClusterName {
			return items[i].ClusterName < items[j].ClusterName
		}
		return items[i].JobName < items[j].JobName
	})
	return items, nil
}

func normalizeClusterStatusFilter(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "all":
		return ""
	case "pending":
		return "pending"
	case "running":
		return "running"
	case "active":
		return "active"
	case "failed":
		return "failed"
	default:
		return ""
	}
}

func filterClusterItemsByStatus(items []JobClusterItem, statusFilter string) []JobClusterItem {
	filter := normalizeClusterStatusFilter(statusFilter)
	if filter == "" {
		return items
	}
	filtered := make([]JobClusterItem, 0, len(items))
	for _, item := range items {
		status := strings.ToLower(strings.TrimSpace(item.Status))
		switch filter {
		case "pending":
			if status == "pending" {
				filtered = append(filtered, item)
			}
		case "running":
			if status == "running" {
				filtered = append(filtered, item)
			}
		case "active":
			if status == "running" || status == "pending" {
				filtered = append(filtered, item)
			}
		case "failed":
			if status == "failed" {
				filtered = append(filtered, item)
			}
		}
	}
	return filtered
}
