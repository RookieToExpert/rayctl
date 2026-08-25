package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"rayctl/internal/kube"
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
	vcCmd.AddCommand(newVCNodeCmd())
	vcCmd.AddCommand(newClusterSetCmd())
	return vcCmd
}

func newVCGetCmd() *cobra.Command {
	var platformOnly bool
	cmd := &cobra.Command{
		Use:   "get [vc-name-or-uid...]",
		Short: "列出 VC，或并行查询一个或多个 VC",
		Long: "不带参数时列出平台 VC；指定一个 VC 时展示平台信息和 HC namespace 映射；" +
			"指定多个 VC 时并行查询并仅展示每个 VC 的概要表。",
		Example: strings.Join([]string{
			"  rayctl vc get",
			"  rayctl vc get vc-a3-llmit",
			"  rayctl vc get vc-a3-llmit vc-a3-deeplink vc-c550-jiaofu",
			"  rayctl vc get vc-a3-llmit --platform-only",
		}, "\n"),
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				_, vcService, err := newVCPlatformService()
				if err != nil {
					return err
				}
				result, err := vcService.List(cmd.Context())
				if err != nil {
					return err
				}
				output.PrintVCList(result)
				return nil
			}

			if len(args) == 1 {
				return runVCGet(cmd, args[0], platformOnly)
			}
			return runParallelVCGet(cmd.Context(), args, platformOnly)
		},
	}
	cmd.Flags().BoolVar(&platformOnly, "platform-only", false, "只显示平台 VC 信息，不查询 HC namespace 映射")
	return cmd
}

func runVCGet(cmd *cobra.Command, identifier string, platformOnly bool) error {
	vcClient, vcService, err := newVCPlatformService()
	if err != nil {
		return err
	}
	detail, err := vcService.Get(cmd.Context(), identifier)
	if err != nil {
		return err
	}
	if platformOnly {
		output.PrintVCDetail(detail)
		return nil
	}

	clientset, err := kube.NewClientset(kubeconfig)
	if err != nil {
		return err
	}
	mapping, err := service.NewClusterService(clientset, vcClient).GetResolved(cmd.Context(), detail.Name, detail.UID)
	if err != nil {
		return err
	}
	output.PrintVCOverview(detail, mapping)
	return nil
}

func newVCPlatformService() (*platform.VirtualClusterClient, *service.VCService, error) {
	vcClient, ok := platform.NewVirtualClusterClientFromEnv()
	if !ok {
		return nil, nil, fmt.Errorf("platform client is unavailable, please configure platform.json first")
	}
	return vcClient, service.NewVCService(vcClient), nil
}

func newVCNodeCmd() *cobra.Command {
	nodeCmd := &cobra.Command{
		Use:   "node",
		Short: "查询或修改 VC 的 ACN 节点成员关系",
	}
	nodeCmd.AddCommand(newVCNodeListCmd())
	nodeCmd.AddCommand(newVCNodeUsageCmd())
	nodeCmd.AddCommand(newVCNodeRemoveCmd())
	return nodeCmd
}

func newVCNodeListCmd() *cobra.Command {
	var longOutput bool
	cmd := &cobra.Command{
		Use:   "list <vc-name-or-uid> [vc-name-or-uid...]",
		Short: "并行列出一个或多个 VC 当前包含的 ACN 节点",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}
			vcService := service.NewVCService(vcClient)
			if clientset, err := kube.NewClientset(kubeconfig); err == nil {
				vcService = service.NewVCServiceWithKubeClient(vcClient, clientset)
			}
			type queryResult struct {
				identifier string
				result     *service.VCNodeListResult
				err        error
			}
			results := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				result, err := vcService.ListNodes(ctx, identifier)
				return queryResult{identifier: identifier, result: result, err: err}
			})
			valid := make([]*service.VCNodeListResult, 0, len(results))
			queryErrors := make([]error, 0)
			for _, result := range results {
				if result.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("vc %q: %w", result.identifier, result.err))
					continue
				}
				valid = append(valid, result.result)
			}
			if len(args) == 1 && len(valid) == 1 {
				output.PrintVCNodeList(valid[0], longOutput)
			} else {
				output.PrintVCNodeListMany(valid, longOutput)
			}
			return errors.Join(queryErrors...)
		},
	}
	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "仅显示 hostname、model、ACN 名称和 ACN UID")
	return cmd
}

func newVCNodeUsageCmd() *cobra.Command {
	var freeOnly bool
	cmd := &cobra.Command{
		Use:   "usage <vc-name-or-uid> [vc-name-or-uid...]",
		Short: "快速查看一个或多个 VC 的逐节点资源水位",
		Long:  "直接通过平台节点资源批量 API 查看每台机器的状态及 CPU、内存、加速卡水位。ALLOC/TOTAL 表示已分配量/总量。",
		Example: strings.Join([]string{
			"  rayctl vc node usage vc-a3-deeplink",
			"  rayctl vc node usage vc-a3-deeplink --free",
			"  rayctl vc node usage vc-a3-deeplink vc-c550-jiaofu",
		}, "\n"),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}
			vcService := service.NewVCServiceWithKubeClient(vcClient, clientset)
			type queryResult struct {
				identifier string
				result     *service.VCResourceUsageResult
				err        error
			}
			queries := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				result, err := vcService.GetResourceUsage(ctx, identifier)
				return queryResult{identifier: identifier, result: result, err: err}
			})

			results := make([]*service.VCResourceUsageResult, 0, len(queries))
			queryErrors := make([]error, 0)
			for _, query := range queries {
				if query.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("vc %q: %w", query.identifier, query.err))
					continue
				}
				if freeOnly {
					query.result.FilterFreeAcceleratorNodes()
				}
				results = append(results, query.result)
			}
			if len(results) > 0 {
				output.PrintVCResourceUsage(results)
			}
			return errors.Join(queryErrors...)
		},
	}
	cmd.Flags().BoolVarP(&freeOnly, "free", "f", false, "只显示仍有可用加速卡（ACCEL FREE > 0）的节点")
	return cmd
}

func newVCNodeRemoveCmd() *cobra.Command {
	var dryRun bool
	var yes bool
	var longOutput bool

	cmd := &cobra.Command{
		Use:   "remove <vc-name-or-uid> <node-name-or-ip-or-uid>...",
		Short: "从指定 VC 移除一个或多个 ACN 节点",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}
			vcService := service.NewVCService(vcClient)
			result, err := vcService.PrepareNodeRemove(cmd.Context(), args[0], args[1:])
			if err != nil {
				return err
			}
			if dryRun {
				result.Result = "dry-run"
			}
			output.PrintVCNodeRemoveResult(result, longOutput, dryRun)
			if dryRun {
				return nil
			}

			if !yes {
				nodeLabels := make([]string, 0, len(result.Nodes))
				for _, node := range result.Nodes {
					nodeLabels = append(nodeLabels, firstNonEmptyString(node.HostName, node.HostIP, node.Name, node.UID))
				}
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"将从 VC %s 移除 %d 个节点（%s），是否继续? (y/N): ",
					result.ClusterName,
					len(result.Nodes),
					strings.Join(nodeLabels, ", "),
				)
				line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if err != nil {
					return err
				}
				if !isYes(line) {
					fmt.Fprintln(cmd.OutOrStdout(), "已取消移除。")
					return nil
				}
			}

			if err := vcService.ApplyNodeRemove(cmd.Context(), result); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "移除完成: vc=%s nodes=%d result=%s\n", result.ClusterName, len(result.Nodes), result.Result)
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "只展示目标节点和请求 payload，不真正移除")
	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "显示 ACN 名称和 ACN UID")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "跳过确认直接移除")
	return cmd
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "-"
}
