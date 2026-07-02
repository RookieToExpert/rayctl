package cmd

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"rayctl/internal/kube"
	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newPolicyCmd() *cobra.Command {
	policyCmd := &cobra.Command{
		Use:   "policy",
		Short: "更新集群策略白名单",
	}

	policyCmd.AddCommand(newPolicyGetCmd())
	policyCmd.AddCommand(newPolicyUpdateCmd())
	return policyCmd
}

func newPolicyGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <policy-name> [vc-name-or-uid]",
		Short: "查看集群策略白名单",
		Long: "当前仅支持查看 HC 上的 disallow-privileged-containers。\n" +
			"不传 VC 时列出所有已加入白名单的 vcluster；传 VC 时检查该 vcluster 是否已加入白名单。",
		Example: strings.Join([]string{
			"  rayctl policy get disallow-privileged-containers",
			"  rayctl policy get disallow-privileged-containers vc-c550-h3c-test",
		}, "\n"),
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}

			dynamicClient, err := kube.NewDynamicClient(kubeconfig)
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			clusterService := service.NewClusterService(clientset, vcClient)
			policyService := service.NewPolicyService(dynamicClient, clusterService)

			target := ""
			if len(args) > 1 {
				target = args[1]
			}
			result, err := policyService.GetClusterPolicy(context.Background(), args[0], target)
			if err != nil {
				return err
			}

			output.PrintPolicyGetResult(result)
			return nil
		},
	}
}

func newPolicyUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update <policy-name> <vc-name-or-uid>",
		Short: "为指定 vcluster 更新 HC 上的 clusterpolicy 白名单",
		Long: "当前仅支持更新 HC 上的 disallow-privileged-containers。\n" +
			"命令会自动为目标 vcluster 追加 namespaceSelector 豁免规则。",
		Example: strings.Join([]string{
			"  rayctl policy update disallow-privileged-containers vc-c550-h3c-test",
		}, "\n"),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}

			dynamicClient, err := kube.NewDynamicClient(kubeconfig)
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			clusterService := service.NewClusterService(clientset, vcClient)
			policyService := service.NewPolicyService(dynamicClient, clusterService)

			result, err := policyService.UpdateClusterPolicy(context.Background(), args[0], args[1])
			if err != nil {
				return err
			}

			output.PrintPolicyUpdateResult(result)
			return nil
		},
	}
}
