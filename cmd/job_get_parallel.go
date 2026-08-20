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
	longOutput  bool
	debugTiming bool
	timeout     time.Duration
}

type jobGetQueryResult struct {
	identifier string
	ecpJobs    []*service.JobGetResult
	err        error
}

func runParallelECPJobGet(
	ctx context.Context,
	identifiers []string,
	jobService *service.JobService,
	vcClient *platform.VirtualClusterClient,
	options jobGetOptions,
) error {
	results := runBoundedQueries(ctx, identifiers, defaultJobGetParallelism, func(parent context.Context, identifier string) jobGetQueryResult {
		queryCtx := parent
		cancel := func() {}
		if options.timeout > 0 {
			queryCtx, cancel = context.WithTimeout(parent, options.timeout)
		}
		queryIdentifier := normalizeJobGetIdentifier(identifier)
		jobs, err := jobService.GetJobsWithLogs(queryCtx, queryIdentifier, options.longOutput)
		if err == nil {
			err = resolveECPJobVCNames(queryCtx, jobs, vcClient)
		}
		if err != nil {
			err = formatJobGetError(queryCtx, identifier, options.timeout, err)
		}
		cancel()
		return jobGetQueryResult{identifier: identifier, ecpJobs: jobs, err: err}
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
			fmt.Fprintf(os.Stdout, "===== ECP 任务查询 [%d/%d]: %s =====\n\n", index+1, len(results), result.identifier)
		}
		for jobIndex, job := range result.ecpJobs {
			if jobIndex > 0 {
				fmt.Fprintln(os.Stdout)
			}
			output.PrintJobDetail(job, options.longOutput, options.debugTiming)
		}
		printed = true
	}

	return errors.Join(queryErrors...)
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
