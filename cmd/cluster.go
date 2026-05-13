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
		Use:   "cluster",
		Short: "管理 rayctl 当前使用的集群环境",
	}
	cmd.AddCommand(newClusterSetCmd())
	return cmd
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
