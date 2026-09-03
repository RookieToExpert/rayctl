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

func newPVCmd() *cobra.Command {
	pvCmd := &cobra.Command{
		Use:   "pv",
		Short: "查询 host PV 与 AFS/host PVC 的映射关系",
	}

	pvCmd.AddCommand(newPVGetCmd())
	pvCmd.AddCommand(newPVCheckCmd())
	return pvCmd
}

func newPVGetCmd() *cobra.Command {
	return newPVQueryCmd("get <host-pv-name-or-uid> [host-pv-name-or-uid...]", "根据 host PV 名称或 UID 反查对应的 AFS 和 host PVC")
}

func newPVCheckCmd() *cobra.Command {
	cmd := newPVQueryCmd("check <host-pv-name-or-uid> [host-pv-name-or-uid...]", "兼容旧版：根据 host PV 名称或 UID 查询")
	cmd.Hidden = true
	cmd.Deprecated = "请使用 rayctl pv get"
	return cmd
}

func newPVQueryCmd(use string, short string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			storageService := service.NewStorageService(clientset, vcClient)
			type queryResult struct {
				identifier string
				result     *service.PVCheckResult
				err        error
			}
			results := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				result, err := storageService.CheckPV(ctx, identifier)
				return queryResult{identifier: identifier, result: result, err: err}
			})
			queryErrors := make([]error, 0)
			printed := false
			for _, query := range results {
				if query.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("pv %q: %w", query.identifier, query.err))
					continue
				}
				if printed {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				output.PrintPVCheckDetail(query.result)
				printed = true
			}
			return errors.Join(queryErrors...)
		},
	}

	return cmd
}
