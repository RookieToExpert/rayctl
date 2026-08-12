package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"rayctl/internal/kube"
	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newECSCmd() *cobra.Command {
	ecsCmd := &cobra.Command{
		Use:   "ecs",
		Short: "查询 ECS/AIS 在 HC 中对应的 VM/VMI 信息",
	}

	ecsCmd.AddCommand(newECSCheckCmd())
	ecsCmd.AddCommand(newECSLoginCmd())
	return ecsCmd
}

func newECSCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "check <ais-name-or-ecs-name-or-uid> [ais-name-or-ecs-name-or-uid...]",
		Short:   "并行查询一个或多个 AIS/ECS 的 VM、namespace、node、创建人和内网 IP",
		Example: "  rayctl ecs check ais-example ecs-example another-uid",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dynamicClient, err := kube.NewDynamicClient(kubeconfig)
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			ecsService := service.NewECSService(dynamicClient, vcClient)
			results := ecsService.CheckMany(cmd.Context(), args, 4)
			printed := false
			queryErrors := make([]error, 0)
			for _, result := range results {
				if result.Err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("ecs %q: %w", result.Identifier, result.Err))
					continue
				}
				if printed {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				output.PrintECSCheckDetail(result.Result)
				printed = true
			}
			return errors.Join(queryErrors...)
		},
	}

	return cmd
}

func newECSLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login <ais-name-or-ecs-name-or-uid>",
		Short: "通过 virtctl console 直接登录 ECS/AIS 对应的 VM",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dynamicClient, err := kube.NewDynamicClient(kubeconfig)
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			ecsService := service.NewECSService(dynamicClient, vcClient)
			item, err := ecsService.ResolveSingle(context.Background(), args[0])
			if err != nil {
				return err
			}
			if item == nil || item.VMName == "-" || item.Namespace == "-" {
				return fmt.Errorf("unable to determine VM/namespace for %q", args[0])
			}

			virtctlPath, lookErr := exec.LookPath("virtctl")
			if lookErr != nil {
				return fmt.Errorf("缺少 virtctl 命令，请先安装 virtctl 后再执行")
			}

			consoleCmd := exec.CommandContext(context.Background(), virtctlPath, "console", item.VMName, "-n", item.Namespace)
			consoleCmd.Stdin = os.Stdin
			consoleCmd.Stdout = os.Stdout
			consoleCmd.Stderr = os.Stderr
			return consoleCmd.Run()
		},
	}

	return cmd
}
