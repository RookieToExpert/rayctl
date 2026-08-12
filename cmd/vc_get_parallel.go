package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"rayctl/internal/kube"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

const defaultVCGetParallelism = 4

type vcGetQueryResult struct {
	identifier string
	detail     *service.VCDetailResult
	mapping    *service.ClusterGetResult
	err        error
}

func runParallelVCGet(ctx context.Context, identifiers []string, platformOnly bool) error {
	vcClient, vcService, err := newVCPlatformService()
	if err != nil {
		return err
	}

	var clusterService *service.ClusterService
	if !platformOnly {
		clientset, err := kube.NewClientset(kubeconfig)
		if err != nil {
			return err
		}
		clusterService = service.NewClusterService(clientset, vcClient)
	}

	results := runBoundedQueries(ctx, identifiers, defaultVCGetParallelism, func(queryCtx context.Context, identifier string) vcGetQueryResult {
		result := vcGetQueryResult{identifier: identifier}
		if err := queryCtx.Err(); err != nil {
			result.err = err
			return result
		}
		result.detail, result.err = vcService.Get(queryCtx, identifier)
		if result.err != nil || platformOnly {
			return result
		}
		result.mapping, result.err = clusterService.GetResolved(queryCtx, result.detail.Name, result.detail.UID)
		return result
	})

	printed := false
	queryErrors := make([]error, 0)
	for _, result := range results {
		if result.err != nil {
			queryErrors = append(queryErrors, fmt.Errorf("vc %q: %w", result.identifier, result.err))
			continue
		}
		if printed {
			fmt.Fprintln(os.Stdout)
		}
		output.PrintVCOverviewSummary(result.detail, result.mapping)
		printed = true
	}
	return errors.Join(queryErrors...)
}
