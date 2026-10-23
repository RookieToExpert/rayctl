package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"rayctl/internal/kube"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newAIDCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aid",
		Short: "查询 AID 开发机",
	}
	cmd.AddCommand(newAIDListCmd())
	cmd.AddCommand(newSSPAIDGetCmd())
	return cmd
}

func newAIDListCmd() *cobra.Command {
	var workspace string
	var queue string
	var state string
	var limit int
	var all bool
	var since string
	var longOutput bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出当前环境中的 AID 开发机",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			createdAfter, windowed, err := resolveListCreatedAfter(all, cmd.Flags().Changed("since"), since)
			if err != nil {
				return err
			}
			region, err := selectedSSPRegion()
			if err != nil {
				return err
			}
			catalog, err := newSSPCatalogQueryService()
			if err != nil {
				return err
			}
			if clientset, clientErr := kube.NewClientset(kubeconfig); clientErr == nil {
				catalog.SetKubeClient(clientset)
			}
			result, err := catalog.ListAID(cmd.Context(), service.SSPCatalogListOptions{
				Region: region, Workspace: workspace, Queue: queue, State: state, Limit: limit, LimitChanged: cmd.Flags().Changed("limit"), All: windowed, CreatedAfter: createdAfter, IncludeNodes: longOutput,
			})
			if err != nil {
				return err
			}
			output.PrintSSPAIDList(result, longOutput)
			return nil
		},
	}
	addSSPCatalogListFlags(cmd, &workspace, &queue, &state, &limit, &all)
	cmd.Flags().StringVar(&since, "since", "7d", "配合 -A 限制创建时间窗口，例如 24h、7d、30d；单独使用也会启用窗口扫描；未传 -n 时最多 5000 条")
	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "显示资源规格和创建时间")
	return cmd
}

func newSSPAIDGetCmd() *cobra.Command {
	var noImage bool
	var workspace string
	var longOutput bool
	var debugTiming bool
	cmd := &cobra.Command{
		Use:   "get <aid-name-or-uid> [aid-name-or-uid...]",
		Short: "并行查询一个或多个 AID 开发机并诊断 Pod 状态",
		Long:  "查询 AID；-e all 依次查询 D/PT，并使用各环境的 HC 配置收集 Pod 证据。可用 RAYCTL_HC_KUBECONFIG_D/PT 指定配置路径。",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := selectedSSPRegionForLookup()
			if err != nil {
				return err
			}
			targets, err := hcTargets(cmd)
			if err != nil {
				return err
			}
			platformClient, platformErr := requirePlatformClient()
			if platformErr != nil {
				return platformErr
			}
			type queryResult struct {
				identifier string
				result     *service.SSPAIDGetResult
				err        error
			}
			results := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				result, err := service.QueryAIDEnvironments(ctx, targets, identifier, func(ctx context.Context, target service.HCEnvironment) (*service.SSPAIDGetResult, error) {
					return service.NewSSPAIDService(target.Client, platformClient).GetAIDInRegion(service.WithPodEvidenceOptions(ctx, noImage, target.Kubeconfig), identifier, workspace, target.Region, longOutput)
				})
				return queryResult{identifier, result, err}
			})
			queryErrors := make([]error, 0)
			printed := false
			for index, result := range results {
				if result.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("aid %q: %w", result.identifier, result.err))
				}
				if result.result == nil {
					continue
				}
				if printed {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				if len(args) > 1 {
					fmt.Fprintf(cmd.OutOrStdout(), "===== AID [%d/%d]: %s =====\n\n", index+1, len(args), result.identifier)
				}
				output.PrintSSPAIDDetail(result.result, longOutput, debugTiming)
				printed = true
			}
			return errors.Join(queryErrors...)
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "指定 workspace 名称，可避免已停止开发机跨 workspace 查询")
	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "仅显示身份锚点及新增的 UID、容器状态、日志、架构和挂载证据")
	cmd.Flags().BoolVar(&debugTiming, "debug-timing", false, "打印 AID 查询各阶段耗时")
	cmd.Flags().BoolVar(&noImage, "no-image", false, "-l 时跳过镜像架构查询")
	return cmd
}

func newAITCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ait",
		Short: "查询 AIT 训练任务",
	}
	cmd.AddCommand(newAITListCmd())
	cmd.AddCommand(newSSPJobGetCmd())
	return cmd
}

func newAITListCmd() *cobra.Command {
	var workspace string
	var queue string
	var state string
	var limit int
	var all bool
	var since string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出当前环境中的 AIT 训练任务",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			createdAfter, windowed, err := resolveListCreatedAfter(all, cmd.Flags().Changed("since"), since)
			if err != nil {
				return err
			}
			region, err := selectedSSPRegion()
			if err != nil {
				return err
			}
			catalog, err := newSSPCatalogQueryService()
			if err != nil {
				return err
			}
			result, err := catalog.ListAIT(cmd.Context(), service.SSPCatalogListOptions{
				Region: region, Workspace: workspace, Queue: queue, State: state, Limit: limit, LimitChanged: cmd.Flags().Changed("limit"), All: windowed, CreatedAfter: createdAfter,
			})
			if err != nil {
				return err
			}
			output.PrintSSPAITList(result)
			return nil
		},
	}
	addSSPCatalogListFlags(cmd, &workspace, &queue, &state, &limit, &all)
	cmd.Flags().StringVar(&since, "since", "7d", "配合 -A 限制时间窗口；FAILED 按失败时间，其他状态按创建时间，例如 24h、7d、30d；单独使用也会启用窗口扫描，未传 -n 时最多 5000 条")
	return cmd
}

func addSSPCatalogListFlags(cmd *cobra.Command, workspace *string, queue *string, state *string, limit *int, all *bool) {
	cmd.Flags().StringVarP(workspace, "workspace", "w", "", "只查询指定 workspace")
	cmd.Flags().StringVarP(queue, "queue", "q", "", "只查询指定 queue；会自动定位所属 workspace")
	cmd.Flags().StringVarP(state, "state", "s", "", "按状态筛选，例如 Running、Pending")
	cmd.Flags().IntVarP(limit, "limit", "n", 50, "最多显示的最新记录数，范围 1-1000；窗口扫描时显式 -n 仍生效，上限 5000")
	cmd.Flags().BoolVarP(all, "all", "A", false, "显示最近 --since 时间窗内的记录，默认 7d，最多 5000 条")
}

func resolveListCreatedAfter(all bool, sinceChanged bool, since string) (time.Time, bool, error) {
	if !all && !sinceChanged {
		return time.Time{}, false, nil
	}
	duration, err := parseListDuration(since)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("invalid --since %q, examples: 24h, 7d, 30d", since)
	}
	return time.Now().Add(-duration), true, nil
}

func parseListDuration(value string) (time.Duration, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil || days < 1 {
			return 0, fmt.Errorf("invalid day duration")
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("invalid duration")
	}
	return duration, nil
}

func newSSPCatalogQueryService() (*service.SSPCatalogService, error) {
	platformClient, platformErr := requirePlatformClient()
	if platformErr != nil {
		return nil, platformErr
	}
	return service.NewSSPCatalogService(platformClient), nil
}

func newSSPJobGetCmd() *cobra.Command {
	var noImage bool
	var workspace string
	var longOutput bool
	var queryTimeout time.Duration
	cmd := &cobra.Command{
		Use:   "get <job-name-or-uid> [job-name-or-uid...]",
		Short: "并行查询一个或多个 AIT 训练任务并诊断 Pending 原因",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			region, err := selectedSSPRegionForLookup()
			if err != nil {
				return err
			}
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}
			platformClient, platformErr := requirePlatformClient()
			if platformErr != nil {
				return platformErr
			}

			jobService := service.NewSSPJobService(clientset, platformClient)
			type queryResult struct {
				identifier string
				result     *service.SSPJobGetResult
				err        error
			}
			results := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				ctx = service.WithPodEvidenceOptions(ctx, noImage, kubeconfig)
				queryCtx := ctx
				cancel := func() {}
				if queryTimeout > 0 {
					queryCtx, cancel = context.WithTimeout(ctx, queryTimeout)
				}
				defer cancel()
				queryIdentifier := normalizeJobGetIdentifier(identifier)
				detection, detectErr := jobService.DetectWorkload(queryCtx, queryIdentifier)
				var result *service.SSPJobGetResult
				var err error
				switch {
				case detectErr == nil && detection != nil && detection.Type == service.SSPWorkloadTypeTrainingJob:
					result, err = jobService.GetJobWithDetectionInRegion(queryCtx, queryIdentifier, workspace, region, longOutput, detection)
				case detectErr == nil && detection != nil && detection.Type == service.SSPWorkloadTypeAID:
					err = fmt.Errorf("%q 是 AID 开发机，请使用 rayctl aid get %s", identifier, queryIdentifier)
				default:
					result, err = jobService.GetJobInRegion(queryCtx, queryIdentifier, workspace, region, longOutput)
				}
				if err != nil {
					err = formatJobGetError(queryCtx, identifier, queryTimeout, err)
				}
				return queryResult{identifier, result, err}
			})
			queryErrors := make([]error, 0)
			printed := false
			for index, result := range results {
				if result.err != nil {
					queryErrors = append(queryErrors, result.err)
					continue
				}
				if printed {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				if len(args) > 1 {
					fmt.Fprintf(cmd.OutOrStdout(), "===== AIT JOB [%d/%d]: %s =====\n\n", index+1, len(args), result.identifier)
				}
				output.PrintSSPJobDetail(result.result, longOutput)
				printed = true
			}
			return errors.Join(queryErrors...)
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "指定 workspace 名称，可避免历史任务跨 workspace 查询")
	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "仅显示身份锚点及前 3 个可疑 Pod 的新增诊断、日志与挂载证据")
	cmd.Flags().DurationVar(&queryTimeout, "timeout", defaultJobGetTimeout, "单个任务的查询超时，例如 5s、30s；设为 0 表示不限制")
	cmd.Flags().BoolVar(&noImage, "no-image", false, "-l 时跳过镜像架构查询")
	return cmd
}
