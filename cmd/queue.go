package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"

	"rayctl/internal/kube"
	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newQueueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "查询 SSP 队列及其节点资源",
	}
	cmd.AddCommand(newQueueListCmd())
	cmd.AddCommand(newQueueGetCmd())
	cmd.AddCommand(newQueueNodeCmd())
	cmd.AddCommand(newQueueWorkloadCmd())
	return cmd
}

func newQueueWorkloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workload",
		Aliases: []string{"wl"},
		Short:   "查询 SSP 队列中的工作负载",
	}
	cmd.AddCommand(newQueueWorkloadListCmd())
	return cmd
}

func newQueueWorkloadListCmd() *cobra.Command {
	var workloadType string
	var state string
	var priority string
	cmd := &cobra.Command{
		Use:   "list <queue-name-or-uid> [queue-name-or-uid...]",
		Short: "并行列出一个或多个 SSP 队列中的工作负载",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			region, err := selectedSSPRegionForLookup()
			if err != nil {
				return err
			}
			resourceService, err := newSSPResourceQueryService()
			if err != nil {
				return err
			}
			query := service.SSPQueueWorkloadQuery{
				Type: normalizeQueueWorkloadType(workloadType), State: state, Priority: priority,
			}
			type queryResult struct {
				identifier string
				result     *service.SSPQueueWorkloadResult
				err        error
			}
			results := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				result, queryErr := resourceService.ListQueueWorkloads(ctx, identifier, region, query)
				return queryResult{identifier: identifier, result: result, err: queryErr}
			})
			valid := make([]*service.SSPQueueWorkloadResult, 0, len(results))
			queryErrors := make([]error, 0)
			for _, result := range results {
				if result.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("queue %q: %w", result.identifier, result.err))
					continue
				}
				valid = append(valid, result.result)
			}
			output.PrintSSPQueueWorkloads(valid)
			return errors.Join(queryErrors...)
		},
	}
	cmd.Flags().StringVarP(&workloadType, "type", "t", "", "工作负载类型: job/aid/air/gw，或平台原始类型")
	cmd.Flags().StringVar(&state, "state", "", "按状态筛选，例如 Running、Pending")
	cmd.Flags().StringVar(&priority, "priority", "", "按优先级筛选，例如 NORMAL")
	return cmd
}

func normalizeQueueWorkloadType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "job", "trainingjob", "training-job":
		return "trainingJob"
	case "aid":
		return "aid"
	case "gw", "gateway", "infergateway", "infer-gateway":
		return "inferGateway"
	default:
		return strings.TrimSpace(value)
	}
}

func newQueueListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出 SSP 队列",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			region, err := selectedSSPRegion()
			if err != nil {
				return err
			}
			resourceService, err := newSSPResourceQueryService()
			if err != nil {
				return err
			}
			result, err := resourceService.ListQueues(cmd.Context(), region)
			if err != nil {
				return err
			}
			output.PrintSSPQueueList(result)
			return nil
		},
	}
}

func newQueueGetCmd() *cobra.Command {
	var debugTiming bool
	var longOutput bool
	cmd := &cobra.Command{
		Use:   "get <queue-name-or-uid> [queue-name-or-uid...]",
		Short: "查询一个或多个 SSP 队列详情",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resourceService, err := newSSPResourceQueryService()
			if err != nil {
				return err
			}
			region, err := selectedSSPRegionForLookup()
			if err != nil {
				return err
			}
			type queryResult struct {
				identifier string
				result     *service.SSPQueueItem
				err        error
			}
			results := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				result, err := resourceService.GetQueue(ctx, identifier, region, longOutput)
				return queryResult{identifier: identifier, result: result, err: err}
			})
			queryErrors := make([]error, 0)
			for _, result := range results {
				if result.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("queue %q: %w", result.identifier, result.err))
					continue
				}
				output.PrintSSPQueueDetail(result.result, longOutput)
				if debugTiming {
					fmt.Fprintf(cmd.ErrOrStderr(), "queue get timing: resource=%s detail=%s reason=%s fallback=%s total=%s\n",
						result.result.Timings.ResourceLookup.Round(time.Millisecond),
						result.result.Timings.DetailLookup.Round(time.Millisecond),
						result.result.Timings.DetailReason,
						result.result.Timings.FallbackLookup.Round(time.Millisecond),
						result.result.Timings.Total.Round(time.Millisecond),
					)
				}
			}
			return errors.Join(queryErrors...)
		},
	}
	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "显示 workspace UID 和节点数等完整详情")
	cmd.Flags().BoolVar(&debugTiming, "debug-timing", false, "打印 queue get 各阶段耗时")
	return cmd
}

func newQueueNodeCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "node", Short: "查询 SSP 队列绑定的节点"}
	cmd.AddCommand(newQueueNodeListCmd())
	cmd.AddCommand(newQueueNodeUsageCmd())
	return cmd
}

func newQueueNodeListCmd() *cobra.Command {
	var longOutput bool
	cmd := &cobra.Command{
		Use:   "list <queue-name-or-uid> [queue-name-or-uid...]",
		Short: "并行列出一个或多个 SSP 队列绑定的节点",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			region, err := selectedSSPRegionForLookup()
			if err != nil {
				return err
			}
			resourceService, err := newSSPResourceQueryService()
			if err != nil {
				return err
			}
			type queryResult struct {
				identifier string
				result     *service.SSPQueueNodeListResult
				err        error
			}
			results := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				result, err := resourceService.ListQueueNodes(ctx, identifier, region)
				return queryResult{identifier: identifier, result: result, err: err}
			})
			queryErrors := make([]error, 0)
			for _, result := range results {
				if result.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("queue %q: %w", result.identifier, result.err))
					continue
				}
				output.PrintSSPQueueNodeList(result.result, longOutput)
			}
			return errors.Join(queryErrors...)
		},
	}
	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "显示 ACN UID、machine type 和 model")
	return cmd
}

func newQueueNodeUsageCmd() *cobra.Command {
	var freeOnly bool
	cmd := &cobra.Command{
		Use:   "usage <queue-name-or-uid> [queue-name-or-uid...]",
		Short: "并行查看一个或多个 SSP 队列的逐节点资源水位",
		Long:  "ALLOC/TOTAL 表示用户工作负载已申请资源/节点可分配资源。",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			region, err := selectedSSPRegionForLookup()
			if err != nil {
				return err
			}
			resourceService, err := newSSPResourceQueryService()
			if err != nil {
				return err
			}
			type queryResult struct {
				identifier string
				result     *service.SSPQueueNodeUsageResult
				err        error
			}
			results := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				result, err := resourceService.GetQueueNodeUsage(ctx, identifier, region)
				return queryResult{identifier: identifier, result: result, err: err}
			})
			queryErrors := make([]error, 0)
			valid := make([]*service.SSPQueueNodeUsageResult, 0, len(results))
			for _, result := range results {
				if result.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("queue %q: %w", result.identifier, result.err))
					continue
				}
				if freeOnly {
					result.result.FilterFreeNodes()
				}
				valid = append(valid, result.result)
			}
			output.PrintSSPQueueNodeUsage(valid)
			return errors.Join(queryErrors...)
		},
	}
	cmd.Flags().BoolVarP(&freeOnly, "free", "f", false, "只显示仍有可用资源的节点；加速节点按卡数，CPU-only 节点按 CPU/内存")
	return cmd
}

func newSSPResourceQueryService() (*service.SSPResourceService, error) {
	clientset, err := kube.NewClientset(kubeconfig)
	if err != nil {
		return nil, err
	}
	platformClient, ok := platform.NewVirtualClusterClientFromEnv()
	if !ok {
		return nil, fmt.Errorf("platform configuration is unavailable; configure ~/.rayctl/platform.json first")
	}
	resourceService := service.NewSSPResourceService(clientset, platformClient)
	resourceService.SetQueueNodeClientResolver(localQueueVClusterClient)
	return resourceService, nil
}

func localQueueVClusterClient(queue service.SSPQueueItem) (kubernetes.Interface, error) {
	vcName := strings.TrimSpace(queue.VCluster)
	if vcName == "" || vcName == "-" {
		return nil, fmt.Errorf("queue has no vcluster name")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	directories := []string{"D", "PT"}
	if strings.EqualFold(strings.TrimSpace(queue.Region), "cn-pj-03") {
		directories = []string{"PT", "D"}
	}
	for _, directory := range directories {
		path := filepath.Join(home, directory, vcName)
		if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
			return kube.NewClientset(path)
		}
	}
	return nil, fmt.Errorf("local vcluster kubeconfig for %s not found", vcName)
}
