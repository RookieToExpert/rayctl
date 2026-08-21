package cmd

import (
	"github.com/spf13/cobra"
)

var kubeconfig string

var rootCmd = &cobra.Command{
	Use:               "rayctl",
	Short:             "rayctl is a lightweight Kubernetes CLI for high-frequency queries",
	Long:              "rayctl is a custom Kubernetes CLI built with Cobra and client-go to simplify daily cluster inspection tasks.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return prepareEnvironmentSelection() },
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&kubeconfig, "kubeconfig", "k", "", "Path to the kubeconfig file (defaults to KUBECONFIG or $HOME/kubeconfig)")
	rootCmd.PersistentFlags().StringVarP(&targetEnvironment, "environment", "e", "auto", "平台环境: auto、d、pt、dcloud 或 all；默认根据当前 kubeconfig 自动识别")
	rootCmd.AddCommand(newAFSCmd())
	rootCmd.AddCommand(newAIDCmd())
	rootCmd.AddCommand(newAIRCmd())
	rootCmd.AddCommand(newAuthCmd())
	rootCmd.AddCommand(newClusterCmd())
	rootCmd.AddCommand(newECSCmd())
	rootCmd.AddCommand(newECPCmd())
	rootCmd.AddCommand(newLogsCmd())
	rootCmd.AddCommand(newNATGatewayCmd())
	rootCmd.AddCommand(newNodeCmd())
	rootCmd.AddCommand(newPolicyCmd())
	rootCmd.AddCommand(newQueueCmd())
	rootCmd.AddCommand(newPVCmd())
	rootCmd.AddCommand(newRBACCmd())
	rootCmd.AddCommand(newJobCmd())
	rootCmd.AddCommand(newPVCCmd())
	rootCmd.AddCommand(newUserCmd())
	rootCmd.AddCommand(newVCCmd())
	rootCmd.AddCommand(newVPCCmd())
	rootCmd.AddCommand(newWorkspaceCmd())
	rootCmd.AddCommand(newSubnetCmd())
	rootCmd.AddCommand(newSSPCmd())
}

func Execute() error {
	return rootCmd.Execute()
}
