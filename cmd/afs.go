package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"rayctl/internal/kube"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newAFSCmd() *cobra.Command {
	afsCmd := &cobra.Command{
		Use:   "afs",
		Short: "查询 AFS 资源及其 PVC/PV 映射关系",
	}

	afsCmd.AddCommand(newAFSListCmd())
	afsCmd.AddCommand(newAFSGetCmd())
	afsCmd.AddCommand(newAFSCreateCmd())
	afsCmd.AddCommand(newAFSCheckCmd())
	return afsCmd
}

func newAFSCreateCmd() *cobra.Command {
	var size string
	var storageClass string
	var zone string
	var displayName string
	var years int
	var autoRenew bool
	var dryRun bool
	var yes bool

	cmd := &cobra.Command{
		Use:   "create <afs-name>",
		Short: "创建个人 AFS 并完成订单支付",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, platformErr := requirePlatformClient()
			if platformErr != nil {
				return platformErr
			}
			createService := service.NewAFSCreateService(client)
			plan, err := createService.Prepare(cmd.Context(), service.AFSCreateOptions{
				Name: args[0], DisplayName: displayName, Size: size, StorageClass: storageClass,
				Zone: zone, Years: years, AutoRenew: autoRenew,
			})
			if err != nil {
				return err
			}
			output.PrintAFSCreatePlan(plan, dryRun)
			if dryRun {
				return nil
			}
			if !yes {
				fmt.Fprintf(cmd.OutOrStdout(), "将创建 AFS %s（%s，%s，%s，%d 年），是否继续? (y/N): ", plan.Name, plan.Size, plan.StorageClass, plan.Zone, plan.Years)
				line, readErr := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if readErr != nil {
					return readErr
				}
				if !isYes(strings.TrimSpace(line)) {
					fmt.Fprintln(cmd.OutOrStdout(), "已取消创建。")
					return nil
				}
			}
			result, createErr := createService.Apply(cmd.Context(), plan)
			if result != nil {
				output.PrintAFSCreateResult(result)
			}
			return createErr
		},
	}
	cmd.Flags().StringVar(&size, "size", "1TB", "AFS 容量，例如 500GB、1TB、2PB")
	cmd.Flags().StringVar(&storageClass, "class", "OCEANSTOR", "存储类型")
	cmd.Flags().StringVar(&zone, "zone", "", "可用区；默认使用当前 region 的 a 可用区")
	cmd.Flags().StringVar(&displayName, "display-name", "", "显示名称")
	cmd.Flags().IntVar(&years, "years", 10, "预付费年限")
	cmd.Flags().BoolVar(&autoRenew, "auto-renew", true, "是否自动续费")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "查询匹配的 SKU 并展示请求，不创建资源")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "跳过确认直接创建并支付订单")
	return cmd
}

func newAFSListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出 AFS 资源",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
		},
	}
}

func newAFSGetCmd() *cobra.Command {
	var longOutput bool
	cmd := &cobra.Command{
		Use:   "get <afs-name-or-uid> [afs-name-or-uid...]",
		Short: "并行查询一个或多个 AFS 与 host PV/PVC 映射",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
	vcClient, platformErr := optionalPlatformClient()
	if platformErr != nil {
		return platformErr
	}
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
		Hidden:     true,
		Args:       cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAFSQueries(cmd, args, longOutput)
		},
	}

	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "查询并显示 host PV/PVC、租户及最多 15 个关联 Virtual PVC")
	return cmd
}
