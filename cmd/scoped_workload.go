package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"rayctl/internal/service"
	"rayctl/pkg/output"
)

type scopedWorkloadFlags struct {
	workloadType string
	state        string
	priority     string
}

func (flags *scopedWorkloadFlags) bind(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&flags.workloadType, "type", "t", "", "工作负载类型: job/aid/air/gw，或平台原始类型")
	cmd.Flags().StringVar(&flags.state, "state", "", "按状态筛选，例如 Running、Pending")
	cmd.Flags().StringVar(&flags.priority, "priority", "", "按优先级筛选，例如 NORMAL")
}

func (flags scopedWorkloadFlags) query() service.SSPQueueWorkloadQuery {
	return service.SSPQueueWorkloadQuery{
		Type: normalizeQueueWorkloadType(flags.workloadType), State: flags.state, Priority: flags.priority,
	}
}

func newWorkspaceWorkloadCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "workload", Aliases: []string{"wl"}, Short: "查询 SSP workspace 中的工作负载"}
	cmd.AddCommand(newWorkspaceWorkloadListCmd())
	return cmd
}

func newWorkspaceWorkloadListCmd() *cobra.Command {
	var flags scopedWorkloadFlags
	cmd := &cobra.Command{
		Use:     "list <workspace-name-or-uid> [workspace-name-or-uid...]",
		Short:   "并行列出一个或多个 SSP workspace 中的工作负载",
		Long:    "聚合 workspace 下所有 queue 的平台工作负载，显示工作负载所属的 workspace 和 queue。",
		Example: "  rayctl ws workload list ws-d-a3-ai4s\n  rayctl ws wl list ws-d-a3-ai4s -t aid --state Running",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			region, err := selectedSSPRegionForLookup()
			if err != nil {
				return err
			}
			platformClient, err := requirePlatformClient()
			if err != nil {
				return err
			}
			resourceService := service.NewSSPResourceService(nil, platformClient)
			type queryResult struct {
				identifier string
				result     *service.SSPWorkspaceWorkloadResult
				err        error
			}
			query := flags.query()
			results := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				result, queryErr := resourceService.ListWorkspaceWorkloads(ctx, identifier, region, query)
				return queryResult{identifier: identifier, result: result, err: queryErr}
			})
			valid := make([]*service.SSPWorkspaceWorkloadResult, 0, len(results))
			queryErrors := make([]error, 0)
			for _, result := range results {
				if result.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("workspace %q: %w", result.identifier, result.err))
				}
				if result.result == nil {
					continue
				}
				printWorkloadWarnings(cmd, result.result.Warnings)
				valid = append(valid, result.result)
			}
			output.PrintSSPWorkspaceWorkloads(valid)
			return errors.Join(queryErrors...)
		},
	}
	flags.bind(cmd)
	return cmd
}

func newVCWorkloadCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "workload", Aliases: []string{"wl"}, Short: "查询 VC 中的 SSP 工作负载和 ECP VCJob"}
	cmd.AddCommand(newVCWorkloadListCmd())
	return cmd
}

func newVCWorkloadListCmd() *cobra.Command {
	var flags scopedWorkloadFlags
	cmd := &cobra.Command{
		Use:   "list <vc-name-or-uid> [vc-name-or-uid...]",
		Short: "并行列出一个或多个 VC 中的工作负载",
		Long: "查询 VC 关联的 SSP queue 工作负载和 ECP Volcano VCJob，显示工作负载所属的 workspace 和 queue。\n" +
			"-t job 同时包含 SSP 训练任务与 ECP VCJob；-t vcJob 或 -t ecp-vcjob 只查 ECP VCJob。",
		Example: "  rayctl vc workload list vc-a3-llmit\n  rayctl vc wl list vc-a3-llmit vc-a3-deeplink -t job --state Running",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, vcService, err := newVCPlatformService()
			if err != nil {
				return err
			}
			type queryResult struct {
				identifier string
				result     *service.VCWorkloadResult
				err        error
			}
			query := flags.query()
			results := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				result, queryErr := vcService.ListWorkloads(ctx, identifier, query)
				return queryResult{identifier: identifier, result: result, err: queryErr}
			})
			valid := make([]*service.VCWorkloadResult, 0, len(results))
			queryErrors := make([]error, 0)
			for _, result := range results {
				if result.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("vc %q: %w", result.identifier, result.err))
				}
				if result.result == nil {
					continue
				}
				printWorkloadWarnings(cmd, result.result.Warnings)
				valid = append(valid, result.result)
			}
			output.PrintVCWorkloads(valid)
			return errors.Join(queryErrors...)
		},
	}
	flags.bind(cmd)
	cmd.Flags().Lookup("type").Usage = "工作负载类型: job/aid/air/gw/vcJob；job 包含 SSP 训练任务和 ECP VCJob"
	return cmd
}

func printWorkloadWarnings(cmd *cobra.Command, warnings []string) {
	for _, warning := range warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", warning)
	}
}
