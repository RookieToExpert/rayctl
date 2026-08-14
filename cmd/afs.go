package cmd

import (
	"errors"
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
		Short: "查询 AFS 资源及其 PVC/PV 映射关系",
	}

	afsCmd.AddCommand(newAFSGetCmd())
	afsCmd.AddCommand(newAFSCheckCmd())
	return afsCmd
}

func newAFSGetCmd() *cobra.Command {
	var longOutput bool
	cmd := &cobra.Command{
		Use:   "get [afs-name-or-uid...]",
		Short: "列出 AFS，或并行查询一个或多个 AFS 与 host PV/PVC 映射",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				networkService, err := newNetworkResourceService()
				if err != nil {
					return err
				}
				result, err := networkService.ListAFS(cmd.Context())
				if err != nil {
					return err
				}
				output.PrintAFSList(result)
				return nil
			}
			return runAFSQueries(cmd, args, longOutput)
		},
	}
	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "查询并显示 host PV/PVC、租户及最多 15 个关联 Virtual PVC")
	return cmd
}

func runAFSQueries(cmd *cobra.Command, args []string, longOutput bool) error {
	clientset, err := kube.NewClientset(kubeconfig)
	if err != nil {
		return err
	}
	vcClient, _ := platform.NewVirtualClusterClientFromEnv()
	results := service.NewStorageService(clientset, vcClient).CheckAFSMany(cmd.Context(), args, longOutput, 4)
	valid := make([]*service.AFSCheckResult, 0, len(results))
	queryErrors := make([]error, 0)
	for _, result := range results {
		if result.Err != nil {
			queryErrors = append(queryErrors, fmt.Errorf("afs %q: %w", result.Identifier, result.Err))
			continue
		}
		valid = append(valid, result.Result)
	}
	if len(args) == 1 && len(valid) == 1 {
		output.PrintAFSCheckDetail(valid[0], longOutput)
	} else if len(valid) > 0 {
		output.PrintAFSCheckSummary(valid)
	}
	return errors.Join(queryErrors...)
}

func newAFSCheckCmd() *cobra.Command {
	var longOutput bool
	cmd := &cobra.Command{
		Use:        "check <afs-name-or-uid> [afs-name-or-uid...]",
		Short:      "兼容入口：查询 AFS 与 host PV/PVC 映射",
		Deprecated: "请使用 rayctl afs get <afs-name-or-uid...>",
		Args:       cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAFSQueries(cmd, args, longOutput)
		},
	}

	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "查询并显示 host PV/PVC、租户及最多 15 个关联 Virtual PVC")
	return cmd
}
