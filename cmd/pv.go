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

func newPVCmd() *cobra.Command {
	pvCmd := &cobra.Command{
		Use:   "pv",
		Short: "查询 host PV 与 AFS/host PVC 的映射关系",
	}

	pvCmd.AddCommand(newPVCheckCmd())
	return pvCmd
}

func newPVCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check <host-pv-name-or-uid> [host-pv-name-or-uid...]",
		Short: "根据 host PV 名称或 UID 反查对应的 AFS 和 host PVC",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			storageService := service.NewStorageService(clientset, vcClient)
			for i, identifier := range args {
				result, err := storageService.CheckPV(context.Background(), identifier)
				if err != nil {
					return fmt.Errorf("pv %q: %w", identifier, err)
				}
				if i > 0 {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				output.PrintPVCheckDetail(result)
			}
			return nil
		},
	}

	return cmd
}
