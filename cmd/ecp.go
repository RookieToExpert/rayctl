package cmd

import "github.com/spf13/cobra"

func newECPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ecp",
		Short: "兼容旧 ECP VCJob 和 AIS 工作负载",
	}
	cmd.AddCommand(newECPJobCmd())
	return cmd
}

func newECPJobCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "job",
		Short: "查询或创建旧 ECP VCJob",
	}
	cmd.AddCommand(newECPJobListCmd())
	cmd.AddCommand(newECPJobGetCmd())
	cmd.AddCommand(newJobCreateCmd())
	return cmd
}
