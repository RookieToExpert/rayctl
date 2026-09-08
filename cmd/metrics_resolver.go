package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	metricsquery "rayctl/internal/metrics"
	"rayctl/internal/platform"
)

type metricsPodCandidate struct {
	workloadType string
	workspace    string
	pods         []string
}

func newPlatformMetricsPodResolver(client *platform.VirtualClusterClient) metricsquery.PodResolver {
	return func(ctx context.Context, workload string, workloadType string, workspaceName string) (metricsquery.PodResolution, error) {
		if client == nil {
			return metricsquery.PodResolution{}, fmt.Errorf("platform client is unavailable")
		}
		workspaces, err := metricsWorkspaces(ctx, client, workspaceName)
		if err != nil {
			return metricsquery.PodResolution{}, err
		}
		if len(workspaces) == 0 {
			return metricsquery.PodResolution{}, fmt.Errorf("workspace %q not found", workspaceName)
		}

		types := []string{"ait", "aid", "air"}
		if value := strings.ToLower(strings.TrimSpace(workloadType)); value != "" && value != "auto" {
			types = []string{value}
		}
		type result struct {
			candidates []metricsPodCandidate
			err        error
		}
		results := make(chan result, len(workspaces))
		semaphore := make(chan struct{}, 12)
		var wait sync.WaitGroup
		for _, workspace := range workspaces {
			workspace := workspace
			wait.Add(1)
			go func() {
				defer wait.Done()
				select {
				case semaphore <- struct{}{}:
				case <-ctx.Done():
					results <- result{err: ctx.Err()}
					return
				}
				defer func() { <-semaphore }()
				candidates, queryErr := metricsPodsInWorkspace(ctx, client, workspace, workload, types)
				results <- result{candidates: candidates, err: queryErr}
			}()
		}
		wait.Wait()
		close(results)

		candidates := make([]metricsPodCandidate, 0)
		var firstErr error
		for item := range results {
			candidates = append(candidates, item.candidates...)
			if firstErr == nil && item.err != nil {
				firstErr = item.err
			}
		}
		candidates = mergeMetricsPodCandidates(candidates)
		switch len(candidates) {
		case 0:
			if firstErr != nil {
				return metricsquery.PodResolution{}, firstErr
			}
			return metricsquery.PodResolution{}, fmt.Errorf("workload %q not found in SSP platform APIs", workload)
		case 1:
			return metricsquery.PodResolution{Type: candidates[0].workloadType, Workspace: candidates[0].workspace, Pods: candidates[0].pods}, nil
		default:
			labels := make([]string, 0, len(candidates))
			for _, candidate := range candidates {
				labels = append(labels, candidate.workspace+"/"+candidate.workloadType)
			}
			sort.Strings(labels)
			return metricsquery.PodResolution{}, fmt.Errorf("workload %q matches multiple platform resources (%s); use -w/--workspace and -t/--type", workload, strings.Join(labels, ", "))
		}
	}
}

func metricsWorkspaces(ctx context.Context, client *platform.VirtualClusterClient, requested string) ([]platform.SSPWorkspace, error) {
	regions := client.ConfiguredSSPRegions()
	items := make([]platform.SSPWorkspace, 0)
	var firstErr error
	for _, region := range regions {
		workspaces, err := client.ListSSPWorkspaces(ctx, region)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, workspace := range workspaces {
			if requested == "" || strings.EqualFold(strings.TrimSpace(workspace.Name), strings.TrimSpace(requested)) {
				items = append(items, workspace)
			}
		}
	}
	if len(items) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return items, nil
}

func metricsPodsInWorkspace(ctx context.Context, client *platform.VirtualClusterClient, workspace platform.SSPWorkspace, workload string, types []string) ([]metricsPodCandidate, error) {
	result := make([]metricsPodCandidate, 0)
	var firstErr error
	for _, workloadType := range types {
		switch workloadType {
		case "ait":
			jobs, err := client.FindSSPTrainingJobsForProfile(ctx, workspace.ProfileName, workspace.Subscription, workspace.Region, workspace.Name, workload)
			if err != nil {
				firstErr = firstNonNilError(firstErr, err)
				continue
			}
			for _, job := range jobs {
				if !metricsIdentifierMatches(workload, job.Name, job.DisplayName, job.UID, job.ID) {
					continue
				}
				workers, _, workerErr := client.ListSSPTrainingJobWorkers(ctx, job, 1000)
				if workerErr != nil {
					firstErr = firstNonNilError(firstErr, workerErr)
					continue
				}
				pods := make([]string, 0, len(workers))
				for _, worker := range workers {
					pods = append(pods, worker.Name)
				}
				result = append(result, metricsPodCandidate{workloadType: "ait", workspace: workspace.Name, pods: uniqueMetricsStrings(pods)})
			}
		case "aid":
			aids, err := client.FindSSPAIDsForProfile(ctx, workspace.ProfileName, workspace.Subscription, workspace.Region, workspace.Name, workload)
			if err != nil {
				firstErr = firstNonNilError(firstErr, err)
				continue
			}
			for _, aid := range aids {
				if !metricsIdentifierMatches(workload, aid.Name, aid.DisplayName, aid.UID, aid.ID) {
					continue
				}
				result = append(result, metricsPodCandidate{workloadType: "aid", workspace: workspace.Name, pods: []string{aid.Name + "-0"}})
			}
		case "air":
			jobs, err := client.ListSSPAIRJobs(ctx, workspace, workload)
			if err != nil {
				firstErr = firstNonNilError(firstErr, err)
				continue
			}
			for _, job := range jobs {
				if !metricsIdentifierMatches(workload, job.Name, job.UID, job.ID) {
					continue
				}
				workers, _, workerErr := client.ListSSPAIRWorkers(ctx, job, 1000)
				if workerErr != nil {
					firstErr = firstNonNilError(firstErr, workerErr)
					continue
				}
				pods := make([]string, 0, len(workers))
				for _, worker := range workers {
					pods = append(pods, worker.Name)
				}
				result = append(result, metricsPodCandidate{workloadType: "air", workspace: workspace.Name, pods: uniqueMetricsStrings(pods)})
			}
		}
	}
	return result, firstErr
}

func mergeMetricsPodCandidates(items []metricsPodCandidate) []metricsPodCandidate {
	merged := map[string]metricsPodCandidate{}
	for _, item := range items {
		key := strings.ToLower(item.workspace + "\x00" + item.workloadType)
		current := merged[key]
		current.workspace = item.workspace
		current.workloadType = item.workloadType
		current.pods = uniqueMetricsStrings(append(current.pods, item.pods...))
		merged[key] = current
	}
	result := make([]metricsPodCandidate, 0, len(merged))
	for _, item := range merged {
		if len(item.pods) > 0 {
			result = append(result, item)
		}
	}
	return result
}

func metricsIdentifierMatches(identifier string, values ...string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(identifier), strings.TrimSpace(value)) && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func uniqueMetricsStrings(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func firstNonNilError(current error, next error) error {
	if current != nil {
		return current
	}
	return next
}
