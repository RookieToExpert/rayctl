package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newWorkspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workspace",
		Aliases: []string{"ws"},
		Short:   "查询 SSP workspace 及其队列",
	}
	cmd.AddCommand(newWorkspaceListCmd())
	cmd.AddCommand(newWorkspaceGetCmd())
	cmd.AddCommand(newWorkspaceNodeCmd())
	cmd.AddCommand(newWorkspaceWorkloadCmd())
	return cmd
}

func newWorkspaceNodeCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "node", Short: "查询 SSP workspace 的节点资源水位"}
	cmd.AddCommand(newWorkspaceNodeUsageCmd())
	return cmd
}

func newWorkspaceNodeUsageCmd() *cobra.Command {
	var freeOnly bool
	cmd := &cobra.Command{
		Use:     "usage <workspace-name-or-uid> [workspace-name-or-uid...]",
		Short:   "并行查看一个或多个 SSP workspace 的逐节点资源水位",
		Long:    "聚合 workspace 下所有 queue 的节点并按 IP/hostname 去重；ALLOC/TOTAL 表示用户工作负载已申请资源/节点可分配资源。",
		Example: "  rayctl ws node usage ws-d-a3-ai4s\n  rayctl ws node usage ws-d-a3-ai4s --free",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			region, err := selectedSSPRegionForLookup()
			if err != nil {
				return err
			}
			resourceService, err := newSSPResourceQueryService()
			if err != nil {
				return err
			}
			type queryResult struct {
				identifier string
				result     *service.SSPWorkspaceNodeUsageResult
				err        error
			}
			results := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				result, queryErr := resourceService.GetWorkspaceNodeUsage(ctx, identifier, region)
				return queryResult{identifier: identifier, result: result, err: queryErr}
			})
			queryErrors := make([]error, 0)
			valid := make([]*service.SSPWorkspaceNodeUsageResult, 0, len(results))
			for _, result := range results {
				if result.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("workspace %q: %w", result.identifier, result.err))
					continue
				}
				if freeOnly {
					result.result.FilterFreeNodes()
				}
				for _, warning := range result.result.Warnings {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", warning)
				}
				valid = append(valid, result.result)
			}
			output.PrintSSPWorkspaceNodeUsage(valid)
			return errors.Join(queryErrors...)
		},
	}
	cmd.Flags().BoolVarP(&freeOnly, "free", "f", false, "只显示 CPU、内存均有余量且加速卡未满的节点；CPU-only 节点检查 CPU/内存")
	return cmd
}

func newWorkspaceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出 SSP workspace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			region, err := selectedSSPRegion()
			if err != nil {
				return err
			}
			resourceService, err := newSSPResourceQueryService()
			if err != nil {
				return err
			}
			result, err := resourceService.ListWorkspaces(cmd.Context(), region)
			if err != nil {
				return err
			}
			output.PrintSSPWorkspaceList(result)
			return nil
		},
	}
}

func newWorkspaceGetCmd() *cobra.Command {
	var longOutput bool
	cmd := &cobra.Command{
		Use:   "get <workspace-name-or-uid> [workspace-name-or-uid...]",
		Short: "查询一个或多个 SSP workspace 详情",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resourceService, err := newSSPResourceQueryService()
			if err != nil {
				return err
			}
			region, err := selectedSSPRegionForLookup()
			if err != nil {
				return err
			}
			type queryResult struct {
				identifier string
				result     *service.SSPWorkspaceItem
				err        error
			}
			results := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				result, err := resourceService.GetWorkspace(ctx, identifier, region)
				return queryResult{identifier: identifier, result: result, err: err}
			})
			queryErrors := make([]error, 0)
			for _, result := range results {
				if result.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("workspace %q: %w", result.identifier, result.err))
					continue
				}
				output.PrintSSPWorkspaceDetail(result.result, longOutput)
			}
			return errors.Join(queryErrors...)
		},
	}
	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "仅显示身份锚点及新增 Queue UID")
	return cmd
}
