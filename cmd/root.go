package cmd

import (
	"context"

	"github.com/spf13/cobra"
)

var kubeconfig string

var rootCmd = &cobra.Command{
	Use:           "rayctl",
	Short:         "rayctl is a lightweight Kubernetes CLI for high-frequency queries",
	Long:          "rayctl is a custom Kubernetes CLI built with Cobra and client-go to simplify daily cluster inspection tasks.",
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return prepareEnvironmentSelection()
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&globalOutput, "output", "o", "table", "输出格式: table、json；metrics 另支持 csv")
	rootCmd.PersistentFlags().StringVarP(&kubeconfig, "kubeconfig", "k", "", "Path to the kubeconfig file (defaults to KUBECONFIG or $HOME/kubeconfig)")
	rootCmd.PersistentFlags().StringVarP(&targetEnvironment, "environment", "e", "auto", "平台环境: auto、d、pt、dcloud 或 all；默认根据当前 kubeconfig 自动识别")
	rootCmd.AddCommand(newAFSCmd())
	rootCmd.AddCommand(newAIDCmd())
	rootCmd.AddCommand(newAITCmd())
	rootCmd.AddCommand(newATECmd())
	rootCmd.AddCommand(newAIRCmd())
	rootCmd.AddCommand(newAuthCmd())
	rootCmd.AddCommand(newClusterCmd())
	rootCmd.AddCommand(newECSCmd())
	rootCmd.AddCommand(newGroupCmd())
	rootCmd.AddCommand(newLogsCmd())
	rootCmd.AddCommand(newMetricsCmd())
	rootCmd.AddCommand(newNATGatewayCmd())
	rootCmd.AddCommand(newNodeCmd())
	rootCmd.AddCommand(newPodCmd())
	rootCmd.AddCommand(newPolicyCmd())
	rootCmd.AddCommand(newQueueCmd())
	rootCmd.AddCommand(newPVCmd())
	rootCmd.AddCommand(newRBACCmd())
	rootCmd.AddCommand(newPVCCmd())
	rootCmd.AddCommand(newUserCmd())
	rootCmd.AddCommand(newVCCmd())
	rootCmd.AddCommand(newVPCCmd())
	rootCmd.AddCommand(newWorkspaceCmd())
	rootCmd.AddCommand(newSubnetCmd())
	installJSONCommands(rootCmd)
	installListSummaries(rootCmd)
}

func Execute() error {
	return ExecuteContext(context.Background())
}

func ExecuteContext(ctx context.Context) error {
	resetRequestedProcessExitCode()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		return err
	}
	return ctx.Err()
}
