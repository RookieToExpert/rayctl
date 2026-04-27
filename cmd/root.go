package cmd

import (
	"github.com/spf13/cobra"
)

var kubeconfig string

var rootCmd = &cobra.Command{
	Use:   "rayctl",
	Short: "rayctl is a lightweight Kubernetes CLI for high-frequency queries",
	Long:  "rayctl is a custom Kubernetes CLI built with Cobra and client-go to simplify daily cluster inspection tasks.",
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&kubeconfig, "kubeconfig", "k", "", "Path to the kubeconfig file (defaults to KUBECONFIG or the hard-coded path in internal/kube/client.go)")
	rootCmd.AddCommand(newNodeCmd())
	rootCmd.AddCommand(newJobCmd())
}

func Execute() error {
	return rootCmd.Execute()
}
