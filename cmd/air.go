package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newAIRCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "air", Short: "查询 SSP AIR 推理任务和推理网关"}
	cmd.AddCommand(newAIRJobCmd())
	cmd.AddCommand(newAIRGatewayCmd())
	return cmd
}

func newAIRJobCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "job", Short: "查询 AIR 推理任务"}
	cmd.AddCommand(newAIRJobListCmd())
	cmd.AddCommand(newAIRJobGetCmd())
	return cmd
}

func newAIRJobListCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出 AIR 推理任务",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			region, err := selectedSSPRegion()
			if err != nil {
				return err
			}
			airService, err := newAIRQueryService()
			if err != nil {
				return err
			}
			result, err := airService.ListJobs(cmd.Context(), region, workspace)
			if err != nil {
				return err
			}
			output.PrintSSPAIRJobList(result)
			return nil
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "只查询指定 workspace")
	return cmd
}

func newAIRJobGetCmd() *cobra.Command {
	var workspace string
	var longOutput bool
	var workerLimit int
	cmd := &cobra.Command{
		Use:   "get <name-or-uid> [name-or-uid...]",
		Short: "查询一个或多个 AIR 推理任务",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			region, err := selectedSSPRegionForLookup()
			if err != nil {
				return err
			}
			airService, err := newAIRQueryService()
			if err != nil {
				return err
			}
			results, queryErrors := airService.GetJobs(cmd.Context(), args, region, workspace, longOutput, workerLimit)
			printed := false
			for index, result := range results {
				if result == nil {
					continue
				}
				if printed {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				if len(args) > 1 {
					fmt.Fprintf(cmd.OutOrStdout(), "===== AIR JOB [%d/%d]: %s =====\n\n", index+1, len(args), args[index])
				}
				output.PrintSSPAIRJobDetail(result, longOutput)
				printed = true
			}
			return errors.Join(queryErrors...)
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "指定 workspace，可显著加快精确查询")
	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "显示 worker、卷、镜像和详细资源")
	cmd.Flags().IntVarP(&workerLimit, "worker-limit", "c", 20, "-l 时最多展示的 worker 数量")
	return cmd
}

func newAIRGatewayCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "gateway", Aliases: []string{"gw"}, Short: "查询 AIR 推理网关"}
	cmd.AddCommand(newAIRGatewayListCmd())
	cmd.AddCommand(newAIRGatewayGetCmd())
	return cmd
}

func newAIRGatewayListCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出 AIR 推理网关",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			region, err := selectedSSPRegion()
			if err != nil {
				return err
			}
			airService, err := newAIRQueryService()
			if err != nil {
				return err
			}
			result, err := airService.ListGateways(cmd.Context(), region, workspace)
			if err != nil {
				return err
			}
			output.PrintSSPAIRGatewayList(result)
			return nil
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "只查询指定 workspace")
	return cmd
}

func newAIRGatewayGetCmd() *cobra.Command {
	var workspace string
	var longOutput bool
	cmd := &cobra.Command{
		Use:   "get <name-or-uid> [name-or-uid...]",
		Short: "查询一个或多个 AIR 推理网关",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			region, err := selectedSSPRegionForLookup()
			if err != nil {
				return err
			}
			airService, err := newAIRQueryService()
			if err != nil {
				return err
			}
			results, queryErrors := airService.GetGateways(cmd.Context(), args, region, workspace)
			printed := false
			for index, result := range results {
				if result == nil {
					continue
				}
				if printed {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				if len(args) > 1 {
					fmt.Fprintf(cmd.OutOrStdout(), "===== AIR GATEWAY [%d/%d]: %s =====\n\n", index+1, len(args), args[index])
				}
				output.PrintSSPAIRGatewayDetail(result, longOutput)
				printed = true
			}
			return errors.Join(queryErrors...)
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "指定 workspace，可显著加快精确查询")
	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "显示 DNAT 和详细资源")
	return cmd
}

func newAIRQueryService() (*service.SSPAIRService, error) {
	platformClient, ok := platform.NewVirtualClusterClientFromEnv()
	if !ok {
		return nil, fmt.Errorf("platform configuration is unavailable; configure ~/.rayctl/platform.json first")
	}
	return service.NewSSPAIRService(platformClient), nil
}
