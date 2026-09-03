package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newClusterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "查询 SSP Cluster 或切换平台环境",
	}
	cmd.AddCommand(newClusterListCmd())
	cmd.AddCommand(newClusterGetCmd())
	cmd.AddCommand(newClusterSetCmd())
	return cmd
}

func newClusterListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出 SSP Cluster",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			region, err := selectedSSPRegion()
			if err != nil {
				return err
			}
			platformClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform configuration is unavailable; configure ~/.rayctl/platform.json first")
			}
			result, err := service.NewSSPResourceService(nil, platformClient).ListClusters(cmd.Context(), region)
			if err != nil {
				return err
			}
			output.PrintSSPClusterList(result)
			return nil
		},
	}
}

func newClusterGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <cluster-name-or-uid> [cluster-name-or-uid...]",
		Short: "查询一个或多个 SSP Cluster 详情",
		Long:  "指定名称或 UID，展示绑定 VC、资源水位及关联 Queue。",
		Example: strings.Join([]string{
			"  rayctl cluster list",
			"  rayctl cluster get cluster-a3",
			"  rayctl cluster get cluster-a3 cluster-muxi",
			"  rayctl cluster list -e pt",
		}, "\n"),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			platformClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform configuration is unavailable; configure ~/.rayctl/platform.json first")
			}
			resourceService := service.NewSSPResourceService(nil, platformClient)
			region, err := selectedSSPRegionForLookup()
			if err != nil {
				return err
			}

			type queryResult struct {
				identifier string
				result     *service.SSPClusterItem
				err        error
			}
			results := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				result, err := resourceService.GetCluster(ctx, identifier, region)
				return queryResult{identifier: identifier, result: result, err: err}
			})
			queryErrors := make([]error, 0)
			for _, result := range results {
				if result.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("cluster %q: %w", result.identifier, result.err))
					continue
				}
				output.PrintSSPClusterDetail(result.result)
			}
			return errors.Join(queryErrors...)
		},
	}
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
