package cmd

import (
	"context"
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
	var longOutput bool
	cmd := &cobra.Command{
		Use:   "get <aid-name-or-uid>",
		Short: "查询 SSP AID 开发机并诊断 Pod 状态",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}
			platformClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform configuration is unavailable; configure ~/.rayctl/platform.json first")
			}
			result, err := service.NewSSPAIDService(clientset, platformClient).GetAID(context.Background(), args[0], workspace, longOutput)
			if err != nil {
				return err
			}
			output.PrintSSPAIDDetail(result, longOutput)
			return nil
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "指定 workspace 名称，可避免已停止开发机跨 workspace 查询")
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
		Use:   "get <job-name-or-uid>",
		Short: "查询 SSP TrainingJob 并诊断 Pending 原因",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}
			platformClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform configuration is unavailable; configure ~/.rayctl/platform.json first")
			}

			result, err := service.NewSSPJobService(clientset, platformClient).GetJob(context.Background(), args[0], workspace, longOutput)
			if err != nil {
				return err
			}
			output.PrintSSPJobDetail(result, longOutput)
			return nil
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "指定 workspace 名称，可避免历史任务跨 workspace 查询")
	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "显示首个 Pod 的最新日志")
	return cmd
}
