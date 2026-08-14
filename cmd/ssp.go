package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"rayctl/internal/kube"
	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newSSPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ssp",
		Short: "查询 SSP TrainingJob 和开发机资源",
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

func newSSPAIDGetCmd() *cobra.Command {
	var workspace string
	var region string
	var longOutput bool
	cmd := &cobra.Command{
		Use:   "get <aid-name-or-uid> [aid-name-or-uid...]",
		Short: "并行查询一个或多个 SSP AID 开发机并诊断 Pod 状态",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
				output.PrintSSPAIDDetail(result.result, longOutput)
				printed = true
			}
			return errors.Join(queryErrors...)
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "指定 workspace 名称，可避免已停止开发机跨 workspace 查询")
	cmd.Flags().StringVar(&region, "region", "", "指定 SSP region，例如 cn-pj-01 或 cn-pj-03；默认自动识别")
	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "显示首个 Pod 的最新日志")
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
	cmd := &cobra.Command{
		Use:   "get <job-name-or-uid> [job-name-or-uid...]",
		Short: "并行查询一个或多个 SSP TrainingJob 并诊断 Pending 原因",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
				result, err := jobService.GetJob(ctx, identifier, workspace, longOutput)
				return queryResult{identifier, result, err}
			})
			queryErrors := make([]error, 0)
			printed := false
			for index, result := range results {
				if result.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("ssp job %q: %w", result.identifier, result.err))
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
	return cmd
}
