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

func newAFSCmd() *cobra.Command {
	afsCmd := &cobra.Command{
		Use:   "afs",
		Short: "查询 AFS 与 PVC/PV 的映射关系",
	}

	afsCmd.AddCommand(newAFSCheckCmd())
	return afsCmd
}

func newAFSCheckCmd() *cobra.Command {
	var longOutput bool
	cmd := &cobra.Command{
		Use:   "check <afs-name-or-uid> [afs-name-or-uid...]",
		Short: "根据 AFS 前端名称或 UID 查询 host pv/pvc 和关联的 virtual pvc",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			storageService := service.NewStorageService(clientset, vcClient)
			for i, identifier := range args {
				result, err := storageService.CheckAFS(context.Background(), identifier)
				if err != nil {
					return fmt.Errorf("afs %q: %w", identifier, err)
				}
				if i > 0 {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				output.PrintAFSCheckDetail(result, longOutput)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "Show additional detail rows such as tenant")
	return cmd
}
