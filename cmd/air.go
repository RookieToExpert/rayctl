package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newAIRCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "air",
		Short: "查询 SSP AIR 推理任务和推理网关",
	}
	cmd.AddCommand(newAIRPlaceholderCmd("job", nil, "AIR 推理任务"))
	cmd.AddCommand(newAIRPlaceholderCmd("gateway", []string{"gw"}, "AIR 推理网关"))
	return cmd
}

func newAIRPlaceholderCmd(use string, aliases []string, resource string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     use,
		Aliases: aliases,
		Short:   "查询 " + resource + "（接口待接入）",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "get <name-or-uid> [name-or-uid...]",
		Short: "查询一个或多个" + resource + "（接口待接入）",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("%s API 尚未接入 rayctl", resource)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "列出" + resource + "（接口待接入）",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("%s API 尚未接入 rayctl", resource)
		},
	})
	return cmd
}
