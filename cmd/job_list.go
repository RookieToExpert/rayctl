package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"rayctl/internal/service"
	"rayctl/pkg/output"
	"strings"
)

func newECPJobListCmd() *cobra.Command {
	var state string
	var limit int
	var all bool
	var includeInactive bool
	var since string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出当前环境中的 ATE VCJob",
		Long: "默认汇总当前 profile 全部 VC 中的 Running/Pending 任务。\n" +
			"使用 --all-status 查询已结束的历史任务；使用 cluster 子命令仅查询指定 VC。",
		Example: strings.Join([]string{
			"  rayctl ate list",
			"  rayctl ate list -s pending",
			"  rayctl ate list -n 100",
			"  rayctl ate list --all-status -A",
			"  rayctl ate list cluster vc-a3-intern-delivery",
		}, "\n"),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			createdAfter, windowed, err := resolveListCreatedAfter(all, cmd.Flags().Changed("since"), since)
			if err != nil {
				return err
			}
			if !isValidClusterStatusFilter(state) && strings.TrimSpace(state) != "" {
				return fmt.Errorf("unsupported status filter %q", state)
			}
			if !all && (limit < 1 || limit > 1000) {
				return fmt.Errorf("--limit must be between 1 and 1000")
			}
			if strings.EqualFold(strings.TrimSpace(state), "all") {
				includeInactive = true
			}

			vcClient, platformErr := requirePlatformClient()
			if platformErr != nil {
				return platformErr
			}
			jobService := service.NewJobService(nil, nil, vcClient)
			queryLimit := limit
			if all {
				queryLimit = 0
			}
			if windowed {
				queryLimit = 5000
			}
			result, err := jobService.ListCurrentTenantVolcanoJobsSince(cmd.Context(), includeInactive, state, queryLimit, createdAfter)
			if err != nil {
				return err
			}
			if !all && len(result.Items) > limit {
				result.Items = result.Items[:limit]
			}
			output.PrintECPJobList(result)
			return nil
		},
	}
	cmd.Flags().StringVarP(&state, "state", "s", "", "按状态筛选: pending、running、active、failed 或 all")
	cmd.Flags().IntVarP(&limit, "limit", "n", 50, "最多显示的最新任务数，范围 1-1000")
	cmd.Flags().BoolVarP(&all, "all", "A", false, "显示最近 --since 时间窗内的任务，默认 7d，最多 5000 条")
	cmd.Flags().StringVar(&since, "since", "7d", "配合 -A 限制创建时间窗口，例如 24h、7d、30d；单独使用也会启用窗口扫描")
	cmd.Flags().BoolVar(&includeInactive, "all-status", false, "查询包括已结束任务在内的历史任务")
	cmd.AddCommand(newJobListClusterCmd())
	return cmd
}

func newJobListClusterCmd() *cobra.Command {
	var includeInactive bool
	var allVC bool

	cmd := &cobra.Command{
		Use:   "cluster [cluster-name-or-uid] [pending|running|active|all]",
		Short: "查看某个分区下的任务列表",
		Long: "查看指定 VC 分区下的任务列表，默认只显示 Running 和 Pending 任务。\n" +
			"可用 pending、running 或 active 过滤状态；使用 --all-status 显示包括已结束任务在内的全部状态；使用 -a 查询当前租户全部 VC。",
		Example: strings.Join([]string{
			"  rayctl ate list cluster vc-a3-intern-delivery",
			"  rayctl ate list cluster vc-a3-intern-delivery pending",
			"  rayctl ate list cluster vc-a3-intern-delivery --all-status",
			"  rayctl ate list cluster -a",
			"  rayctl ate list cluster -a running",
		}, "\n"),
		Args: func(cmd *cobra.Command, args []string) error {
			if allVC {
				if len(args) > 1 {
					return fmt.Errorf("at most one status filter is allowed when using -a")
				}
				if len(args) == 1 && !isValidClusterStatusFilter(args[0]) {
					return fmt.Errorf("unsupported status filter %q", args[0])
				}
				return nil
			}
			if len(args) == 0 || len(args) > 2 {
				return fmt.Errorf("expected cluster name with optional status filter")
			}
			if len(args) == 2 && !isValidClusterStatusFilter(args[1]) {
				return fmt.Errorf("unsupported status filter %q", args[1])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, platformErr := requirePlatformClient()
			if platformErr != nil {
				return platformErr
			}
			jobService := service.NewJobService(nil, nil, vcClient)
			var result *service.JobClusterListResult
			var err error
			statusFilter := ""
			if allVC {
				if len(args) == 1 {
					statusFilter = args[0]
				}
				result, err = jobService.GetCurrentTenantClusterJobs(cmd.Context(), includeInactive, statusFilter)
			} else {
				if len(args) == 2 {
					statusFilter = args[1]
				}
				result, err = jobService.GetClusterJobs(cmd.Context(), args[0], includeInactive, statusFilter)
			}
			if err != nil {
				return err
			}
			output.PrintJobClusterList(result)
			return nil
		},
	}

	cmd.Flags().BoolVar(&includeInactive, "all-status", false, "显示包含非 Running/Pending 在内的全部任务")
	cmd.Flags().BoolVarP(&allVC, "all-vc", "a", false, "查看当前租户下全部分区的任务")
	return cmd
}

func isValidClusterStatusFilter(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pending", "running", "active", "failed", "all":
		return true
	default:
		return false
	}
}
