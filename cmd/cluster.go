package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"rayctl/internal/platform"
)

func newClusterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "cluster",
		Short:      "兼容旧版 VC 与环境切换命令",
		Hidden:     true,
		Deprecated: "请改用 rayctl vc",
	}
	cmd.AddCommand(newClusterGetCmd())
	cmd.AddCommand(newClusterSetCmd())
	return cmd
}

func newClusterGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <vc-name-or-uid>",
		Short: "查看 vcluster 在 HC 中的控制面和资源 namespace 映射",
		Long: "查看 vcluster 在 HC 中的 namespace 映射关系。\n" +
			"会展示控制面 namespace，以及当前 vcluster 下每个逻辑 namespace 对应的 host 资源 namespace。",
		Example: strings.Join([]string{
			"  rayctl cluster get vc-a3-llmit",
			"  rayctl cluster get vc-019d28e0-9610-74ef-a722-9242dede9e37",
		}, "\n"),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVCGet(cmd, args[0], false)
		},
	}
}

func newClusterSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <d|dcloud>",
		Short: "切换 rayctl 使用的集群环境",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cluster := strings.ToLower(strings.TrimSpace(args[0]))
			switch cluster {
			case platform.ClusterD, platform.ClusterDCloud:
			default:
				return fmt.Errorf("unsupported cluster %q, expected d or dcloud", args[0])
			}

			configPath := platform.DefaultConfigPath()
			cfg, err := platform.LoadConfigSnapshot(configPath)
			if err != nil {
				if !os.IsNotExist(err) {
					return fmt.Errorf("load platform config: %w", err)
				}
				cfg = &platform.ConfigSnapshot{}
			}

			cfg.Cluster = cluster
			cfg.Subscription = ""
			cfg.BaseURL = ""
			cfg.KubernetesBaseURL = ""

			if err := platform.SaveConfigSnapshot(configPath, cfg); err != nil {
				return fmt.Errorf("save platform config: %w", err)
			}

			label := "D"
			if cluster == platform.ClusterDCloud {
				label = "Dcloud"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "当前 rayctl 集群已切换为: %s\n", label)
			return nil
		},
	}
}
