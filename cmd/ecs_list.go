package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newECSListCmd() *cobra.Command {
	var options service.ECSListOptions
	var long bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出当前环境各租户的 ECS/AIS",
		Long:  "分页读取当前环境已配置租户的 ECS/AIS 清单；缺失规格时按 UID 定向查询 HC VM/VMI 的 CPU 和内存。按类型、名称排序，默认最多显示 100 条；-n 0 显示全部。",
		Example: "  rayctl ecs list\n" +
			"  rayctl ecs list -t ecs --state Running\n" +
			"  rayctl ecs list --name yunwei -l\n" +
			"  rayctl ecs list -n 0 -o json",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := options.Validate(); err != nil {
				return err
			}
			client, err := requirePlatformClient()
			if err != nil {
				return err
			}
			result, err := service.NewECSListService(client).WithMachineSpecs(kubeconfig).List(cmd.Context(), options)
			if result != nil {
				output.PrintECSList(result, long)
				for _, warning := range result.Warnings {
					fmt.Fprintln(cmd.ErrOrStderr(), "warning:", warning)
				}
			}
			return err
		},
	}
	cmd.Flags().StringVarP(&options.ResourceType, "type", "t", "all", "资源类型: all、ecs、ais")
	cmd.Flags().StringVar(&options.Name, "name", "", "按名称、显示名称或 UID 筛选，忽略大小写")
	cmd.Flags().StringVarP(&options.State, "state", "s", "", "按平台状态筛选，忽略大小写")
	cmd.Flags().IntVarP(&options.Limit, "limit", "n", 100, "最多显示的资源数；0 表示全部，筛选后生效")
	cmd.Flags().BoolVarP(&long, "long", "l", false, "显示租户、UID、机器规格和 VPC")
	return cmd
}
