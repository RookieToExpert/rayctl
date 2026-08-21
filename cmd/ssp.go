package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"rayctl/internal/kube"
	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newSSPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "ssp",
		Short:      "兼容旧版 SSP 命令",
		Deprecated: "SSP 已成为默认入口，请直接使用 rayctl job 或 rayctl aid",
	}
	cmd.AddCommand(newSSPJobCmd())
	cmd.AddCommand(newSSPAIDCmd())
	return cmd
}

func newSSPAIDCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aid",
		Short: "查询 SSP AID 开发机",
	}
	cmd.AddCommand(newSSPAIDGetCmd())
	return cmd
}

func newAIDCmd() *cobra.Command {
	return newSSPAIDCmd()
}

func newSSPAIDGetCmd() *cobra.Command {
	var workspace string
	var longOutput bool
	var debugTiming bool
	cmd := &cobra.Command{
		Use:   "get <aid-name-or-uid> [aid-name-or-uid...]",
		Short: "并行查询一个或多个 SSP AID 开发机并诊断 Pod 状态",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			region, err := selectedSSPRegionForLookup()
			if err != nil {
				return err
			}
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}
			platformClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform configuration is unavailable; configure ~/.rayctl/platform.json first")
			}
			aidService := service.NewSSPAIDService(clientset, platformClient)
			type queryResult struct {
				identifier string
				result     *service.SSPAIDGetResult
				err        error
			}
			results := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				result, err := aidService.GetAIDInRegion(ctx, identifier, workspace, region, longOutput)
				return queryResult{identifier, result, err}
			})
			queryErrors := make([]error, 0)
			printed := false
			for index, result := range results {
				if result.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("aid %q: %w", result.identifier, result.err))
					continue
				}
				if printed {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				if len(args) > 1 {
					fmt.Fprintf(cmd.OutOrStdout(), "===== AID [%d/%d]: %s =====\n\n", index+1, len(args), result.identifier)
				}
				output.PrintSSPAIDDetail(result.result, longOutput, debugTiming)
				printed = true
			}
			return errors.Join(queryErrors...)
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "指定 workspace 名称，可避免已停止开发机跨 workspace 查询")
	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "显示首个 Pod 的最新日志")
	cmd.Flags().BoolVar(&debugTiming, "debug-timing", false, "打印 AID 查询各阶段耗时")
	return cmd
}

func newSSPJobCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "job",
		Short: "查询 SSP TrainingJob",
	}
	cmd.AddCommand(newSSPJobGetCmd())
	return cmd
}

func newSSPJobGetCmd() *cobra.Command {
	var workspace string
	var longOutput bool
	var queryTimeout time.Duration
	cmd := &cobra.Command{
		Use:   "get <job-name-or-uid> [job-name-or-uid...]",
		Short: "并行查询一个或多个 SSP TrainingJob 并诊断 Pending 原因",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			region, err := selectedSSPRegionForLookup()
			if err != nil {
				return err
			}
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}
			platformClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform configuration is unavailable; configure ~/.rayctl/platform.json first")
			}

			jobService := service.NewSSPJobService(clientset, platformClient)
			type queryResult struct {
				identifier string
				result     *service.SSPJobGetResult
				err        error
			}
			results := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				queryCtx := ctx
				cancel := func() {}
				if queryTimeout > 0 {
					queryCtx, cancel = context.WithTimeout(ctx, queryTimeout)
				}
				defer cancel()
				queryIdentifier := normalizeJobGetIdentifier(identifier)
				detection, detectErr := jobService.DetectWorkload(queryCtx, queryIdentifier)
				var result *service.SSPJobGetResult
				var err error
				switch {
				case detectErr == nil && detection != nil && detection.Type == service.SSPWorkloadTypeTrainingJob:
					result, err = jobService.GetJobWithDetectionInRegion(queryCtx, queryIdentifier, workspace, region, longOutput, detection)
				case detectErr == nil && detection != nil && detection.Type == service.SSPWorkloadTypeAID:
					err = fmt.Errorf("%q 是 SSP AID 开发机，请使用 rayctl aid get %s", identifier, queryIdentifier)
				default:
					result, err = jobService.GetJobInRegion(queryCtx, queryIdentifier, workspace, region, longOutput)
				}
				if err != nil {
					err = formatJobGetError(queryCtx, identifier, queryTimeout, err)
				}
				return queryResult{identifier, result, err}
			})
			queryErrors := make([]error, 0)
			printed := false
			for index, result := range results {
				if result.err != nil {
					queryErrors = append(queryErrors, result.err)
					continue
				}
				if printed {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				if len(args) > 1 {
					fmt.Fprintf(cmd.OutOrStdout(), "===== SSP JOB [%d/%d]: %s =====\n\n", index+1, len(args), result.identifier)
				}
				output.PrintSSPJobDetail(result.result, longOutput)
				printed = true
			}
			return errors.Join(queryErrors...)
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "指定 workspace 名称，可避免历史任务跨 workspace 查询")
	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "显示首个 Pod 的最新日志")
	cmd.Flags().DurationVar(&queryTimeout, "timeout", defaultJobGetTimeout, "单个任务的查询超时，例如 5s、30s；设为 0 表示不限制")
	return cmd
}
