package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"rayctl/internal/service"
)

func newVCRenewCmd() *cobra.Command {
	var dry, skip bool
	var directory string
	command := &cobra.Command{Use: "renew", Short: "补齐 VC kubeconfig，保留已有文件，确认后清理已证实删除的 VC", SilenceUsage: true, SilenceErrors: true, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		client, _, err := newVCPlatformService()
		if err != nil {
			return err
		}
		root := directory
		if root == "" {
			root, err = os.UserHomeDir()
			if err != nil {
				return err
			}
		}
		environment := effectiveEnvironmentSelection()
		if environment == "" || environment == "auto" {
			environment = client.ProfileEnvironment(client.CurrentProfileName())
		}
		return service.RenewVCKubeconfigs(cmd.Context(), client, service.VCRenewOptions{Root: root, Environment: environment, DryRun: dry, SkipConnectivity: skip, Out: cmd.OutOrStdout(), Confirm: func(paths []string) bool {
			fmt.Fprintln(cmd.OutOrStdout(), "以下 VC 的 kubeconfig 文件将被永久移除：")
			for _, path := range paths {
				fmt.Fprintln(cmd.OutOrStdout(), "  "+path)
			}
			fmt.Fprint(cmd.OutOrStdout(), "是否确认？(y/N): ")
			answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			return strings.EqualFold(strings.TrimSpace(answer), "y")
		}})
	}}
	command.Flags().BoolVar(&dry, "dry-run", false, "只显示同步计划，不写入或删除文件")
	command.Flags().BoolVar(&skip, "skip-connectivity-check", false, "跳过公网连通性检查，仍校验证书")
	command.Flags().StringVar(&directory, "directory", "", "输出根目录，默认 $HOME，下分 D/PT/Dcloud；目录必须已存在")
	return command
}
