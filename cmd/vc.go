package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newVCCmd() *cobra.Command {
	vcCmd := &cobra.Command{
		Use:   "vc",
		Short: "查询平台 VC 信息",
	}

	vcCmd.AddCommand(newVCGetCmd())
	return vcCmd
}

func newVCGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [vc-name-or-uid]",
		Short: "列出 VC 或查询单个 VC 详情",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}

			vcService := service.NewVCService(vcClient)
			if len(args) == 0 {
				result, err := vcService.List(context.Background())
				if err != nil {
					return err
				}
				output.PrintVCList(result)
				return nil
			}

			result, err := vcService.Get(context.Background(), args[0])
			if err != nil {
				return err
			}
			output.PrintVCDetail(result)
			return nil
		},
	}
}
