package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

const defaultJobGetParallelism = 4

type jobGetOptions struct {
	workspace   string
	longOutput  bool
	debugTiming bool
	timeout     time.Duration
}

type jobGetQueryResult struct {
	identifier string
	ecpJobs    []*service.JobGetResult
	sspJob     *service.SSPJobGetResult
	err        error
}

func runParallelJobGet(
	ctx context.Context,
	identifiers []string,
	jobService *service.JobService,
	sspJobService *service.SSPJobService,
	vcClient *platform.VirtualClusterClient,
	options jobGetOptions,
) error {
	results := runBoundedQueries(ctx, identifiers, defaultJobGetParallelism, func(parent context.Context, identifier string) jobGetQueryResult {
		queryCtx := parent
		cancel := func() {}
		if options.timeout > 0 {
			queryCtx, cancel = context.WithTimeout(parent, options.timeout)
		}
		result := querySingleJobGet(queryCtx, identifier, jobService, sspJobService, vcClient, options)
		if result.err != nil {
			result.err = formatJobGetError(queryCtx, identifier, options.timeout, result.err)
		}
		cancel()
		return result
	})

	printed := false
	queryErrors := make([]error, 0)
	for index, result := range results {
		if result.err != nil {
			queryErrors = append(queryErrors, result.err)
			continue
		}
		if printed {
			fmt.Fprintln(os.Stdout)
		}
		if len(results) > 1 {
			fmt.Fprintf(os.Stdout, "===== 任务查询 [%d/%d]: %s =====\n\n", index+1, len(results), result.identifier)
		}
		printJobGetQueryResult(result, options)
		printed = true
	}

	return errors.Join(queryErrors...)
}

func querySingleJobGet(
	ctx context.Context,
	identifier string,
	jobService *service.JobService,
	sspJobService *service.SSPJobService,
	vcClient *platform.VirtualClusterClient,
	options jobGetOptions,
) jobGetQueryResult {
	result := jobGetQueryResult{identifier: identifier}
	queryIdentifier := normalizeJobGetIdentifier(identifier)
	detectedType := ""
	if sspJobService != nil {
		detectCtx, stopDetection := context.WithTimeout(ctx, jobTypeDetectionTimeout)
		detection, detectErr := sspJobService.DetectWorkload(detectCtx, queryIdentifier)
		stopDetection()
		if detectErr == nil && detection != nil {
			detectedType = detection.Type
			switch detection.Type {
			case service.SSPWorkloadTypeTrainingJob:
				result.sspJob, result.err = sspJobService.GetJobWithDetection(ctx, queryIdentifier, options.workspace, options.longOutput, detection)
				return result
			case service.SSPWorkloadTypeAID:
				result.err = fmt.Errorf("is an SSP AID workload; use rayctl ssp aid get %s", queryIdentifier)
				return result
			}
		}
	}

	if sspJobService == nil || detectedType == service.WorkloadTypeECPVCJob {
		result.ecpJobs, result.err = jobService.GetJobsWithLogs(ctx, queryIdentifier, options.longOutput)
		if result.err == nil {
			result.err = resolveECPJobVCNames(ctx, result.ecpJobs, vcClient)
		}
		return result
	}

	type ecpQueryResult struct {
		results []*service.JobGetResult
		err     error
	}
	type sspQueryResult struct {
		result *service.SSPJobGetResult
		err    error
	}
	queryCtx, stopQueries := context.WithCancel(ctx)
	defer stopQueries()
	ecpResults := make(chan ecpQueryResult, 1)
	sspResults := make(chan sspQueryResult, 1)
	go func() {
		results, err := jobService.GetJobsWithLogs(queryCtx, queryIdentifier, options.longOutput)
		ecpResults <- ecpQueryResult{results: results, err: err}
	}()
	go func() {
		job, err := sspJobService.GetJob(queryCtx, queryIdentifier, options.workspace, options.longOutput)
		sspResults <- sspQueryResult{result: job, err: err}
	}()

	select {
	case <-ctx.Done():
		result.err = ctx.Err()
	case sspResult := <-sspResults:
		if sspResult.err == nil {
			stopQueries()
			result.sspJob = sspResult.result
			result.err = ctx.Err()
			return result
		}
		if isSSPKubeconfigMismatch(sspResult.err) {
			stopQueries()
			result.err = sspResult.err
			return result
		}
		select {
		case <-ctx.Done():
			result.err = ctx.Err()
		case ecpResult := <-ecpResults:
			stopQueries()
			if ecpResult.err != nil {
				result.err = ecpResult.err
			} else if jobResultsContainSSPTrainingJob(ecpResult.results) {
				result.err = fmt.Errorf("is an SSP TrainingJob but its AIT record could not be loaded: %w", sspResult.err)
			} else {
				result.ecpJobs = ecpResult.results
				result.err = resolveECPJobVCNames(ctx, result.ecpJobs, vcClient)
			}
		}
	case ecpResult := <-ecpResults:
		if ecpResult.err == nil && !jobResultsContainSSPTrainingJob(ecpResult.results) {
			stopQueries()
			result.ecpJobs = ecpResult.results
			result.err = resolveECPJobVCNames(ctx, result.ecpJobs, vcClient)
			return result
		}
		select {
		case <-ctx.Done():
			result.err = ctx.Err()
		case sspResult := <-sspResults:
			stopQueries()
			if sspResult.err == nil {
				result.sspJob = sspResult.result
				result.err = ctx.Err()
			} else if isSSPKubeconfigMismatch(sspResult.err) {
				result.err = sspResult.err
			} else if ecpResult.err != nil {
				result.err = ecpResult.err
			} else {
				result.err = fmt.Errorf("is an SSP TrainingJob but its AIT record could not be loaded: %w", sspResult.err)
			}
		}
	}
	return result
}

func resolveECPJobVCNames(ctx context.Context, results []*service.JobGetResult, vcClient *platform.VirtualClusterClient) error {
	if vcClient == nil {
		return ctx.Err()
	}
	for _, result := range results {
		if result == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		vcUID := virtualClusterUIDFromName(result.VClusterName)
		if vcUID == "" {
			continue
		}
		resource, err := vcClient.FindResourceByUID(ctx, vcUID, "virtualClusters")
		if err := ctx.Err(); err != nil {
			return err
		}
		if err == nil && strings.TrimSpace(resource.Name) != "" {
			result.VClusterName = strings.TrimSpace(resource.Name)
		}
	}
	return nil
}

func printJobGetQueryResult(result jobGetQueryResult, options jobGetOptions) {
	if result.sspJob != nil {
		output.PrintSSPJobDetail(result.sspJob, options.longOutput)
		return
	}
	for index, job := range result.ecpJobs {
		if index > 0 {
			fmt.Fprintln(os.Stdout)
		}
		output.PrintJobDetail(job, options.longOutput, options.debugTiming)
	}
}
