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

func newPVCCmd() *cobra.Command {
	pvcCmd := &cobra.Command{
		Use:   "pvc",
		Short: "查询 PVC 与 AFS 的映射关系",
	}

	pvcCmd.AddCommand(newPVCCheckCmd())
	return pvcCmd
}

func newPVCCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check <pvc-name> [pvc-name...]",
		Short: "根据 PVC 名称查询对应的 AFS 前端名称",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			storageService := service.NewStorageService(clientset, vcClient)
			for i, identifier := range args {
				result, err := storageService.CheckPVC(context.Background(), identifier)
				if err != nil {
					return fmt.Errorf("pvc %q: %w", identifier, err)
				}
				if i > 0 {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				output.PrintPVCCheckDetail(result)
			}
			return nil
		},
	}

	return cmd
}
