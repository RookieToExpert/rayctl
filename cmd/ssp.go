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

func newAIDCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aid",
		Short: "查询 AID 开发机",
	}
	cmd.AddCommand(newAIDListCmd())
	cmd.AddCommand(newSSPAIDGetCmd())
	return cmd
}

func newAIDListCmd() *cobra.Command {
	var workspace string
	var queue string
	var state string
	var limit int
	var all bool
	var longOutput bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出当前环境中的 AID 开发机",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			region, err := selectedSSPRegion()
			if err != nil {
				return err
			}
			catalog, err := newSSPCatalogQueryService()
			if err != nil {
				return err
			}
			result, err := catalog.ListAID(cmd.Context(), service.SSPCatalogListOptions{
				Region: region, Workspace: workspace, Queue: queue, State: state, Limit: limit, All: all,
			})
			if err != nil {
				return err
			}
			output.PrintSSPAIDList(result, longOutput)
			return nil
		},
	}
	addSSPCatalogListFlags(cmd, &workspace, &queue, &state, &limit, &all)
	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "显示资源规格和创建时间")
	return cmd
}

func newSSPAIDGetCmd() *cobra.Command {
	var workspace string
	var longOutput bool
	var debugTiming bool
	cmd := &cobra.Command{
		Use:   "get <aid-name-or-uid> [aid-name-or-uid...]",
		Short: "并行查询一个或多个 AID 开发机并诊断 Pod 状态",
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

func newAITCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ait",
		Short: "查询 AIT 训练任务",
	}
	cmd.AddCommand(newAITListCmd())
	cmd.AddCommand(newSSPJobGetCmd())
	return cmd
}

func newAITListCmd() *cobra.Command {
	var workspace string
	var queue string
	var state string
	var limit int
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出当前环境中的 AIT 训练任务",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			region, err := selectedSSPRegion()
			if err != nil {
				return err
			}
			catalog, err := newSSPCatalogQueryService()
			if err != nil {
				return err
			}
			result, err := catalog.ListAIT(cmd.Context(), service.SSPCatalogListOptions{
				Region: region, Workspace: workspace, Queue: queue, State: state, Limit: limit, All: all,
			})
			if err != nil {
				return err
			}
			output.PrintSSPAITList(result)
			return nil
		},
	}
	addSSPCatalogListFlags(cmd, &workspace, &queue, &state, &limit, &all)
	return cmd
}

func addSSPCatalogListFlags(cmd *cobra.Command, workspace *string, queue *string, state *string, limit *int, all *bool) {
	cmd.Flags().StringVarP(workspace, "workspace", "w", "", "只查询指定 workspace")
	cmd.Flags().StringVarP(queue, "queue", "q", "", "只查询指定 queue；会自动定位所属 workspace")
	cmd.Flags().StringVarP(state, "state", "s", "", "按状态筛选，例如 Running、Pending")
	cmd.Flags().IntVarP(limit, "limit", "n", 50, "最多显示的最新记录数，范围 1-1000")
	cmd.Flags().BoolVarP(all, "all", "A", false, "显示全部记录，忽略 --limit")
}

func newSSPCatalogQueryService() (*service.SSPCatalogService, error) {
	platformClient, ok := platform.NewVirtualClusterClientFromEnv()
	if !ok {
		return nil, fmt.Errorf("platform configuration is unavailable; configure ~/.rayctl/platform.json first")
	}
	return service.NewSSPCatalogService(platformClient), nil
}

func newSSPJobGetCmd() *cobra.Command {
	var workspace string
	var longOutput bool
	var queryTimeout time.Duration
	cmd := &cobra.Command{
		Use:   "get <job-name-or-uid> [job-name-or-uid...]",
		Short: "并行查询一个或多个 AIT 训练任务并诊断 Pending 原因",
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
					err = fmt.Errorf("%q 是 AID 开发机，请使用 rayctl aid get %s", identifier, queryIdentifier)
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
					fmt.Fprintf(cmd.OutOrStdout(), "===== AIT JOB [%d/%d]: %s =====\n\n", index+1, len(args), result.identifier)
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
