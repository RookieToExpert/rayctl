package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"rayctl/internal/kube"
	metricsquery "rayctl/internal/metrics"
	"rayctl/internal/platform"
	"rayctl/pkg/output"

	"github.com/spf13/cobra"
)

type metricsFlags struct {
	workloadType string
	workspace    string
	since        string
	start        string
	end          string
	step         string
	pods         []string
	by           string
	output       string
}

func newMetricsCmd() *cobra.Command {
	metricsCmd := &cobra.Command{
		Use:   "metrics",
		Short: "查询工作负载的原始监控指标与时间线",
		Long:  "查询 VictoriaMetrics 中的工作负载指标。mem/gpu/restart 默认读取原始采样点，只有显式指定 --step 时才使用 query_range 固定步长查询。",
	}
	metricsCmd.AddCommand(newMetricsMemoryCmd())
	metricsCmd.AddCommand(newMetricsGPUCmd())
	metricsCmd.AddCommand(newMetricsRestartCmd())
	metricsCmd.AddCommand(newMetricsRawCmd())
	return metricsCmd
}

func newMetricsMemoryCmd() *cobra.Command {
	var flags metricsFlags
	command := &cobra.Command{
		Use:   "mem <workload>",
		Short: "查看容器内存曲线与 limit 占比",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options, err := flags.queryOptions(cmd, args[0])
			if err != nil {
				return err
			}
			client, environment, platformClient, closeClient, err := newMetricsClient(cmd.Context())
			if err != nil {
				return err
			}
			defer closeClient()
			options.Environment = environment
			svc := metricsquery.NewService(client, environment)
			svc.SetPodResolver(newPlatformMetricsPodResolver(platformClient))
			result, err := svc.Memory(cmd.Context(), options)
			if err != nil {
				return err
			}
			return output.PrintMetrics(result, flags.output)
		},
	}
	bindMetricsWorkloadFlags(command, &flags)
	return command
}

func newMetricsGPUCmd() *cobra.Command {
	var flags metricsFlags
	command := &cobra.Command{
		Use:   "gpu <workload>",
		Short: "查看 GPU/NPU 利用率和显存指标",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options, err := flags.queryOptions(cmd, args[0])
			if err != nil {
				return err
			}
			client, environment, platformClient, closeClient, err := newMetricsClient(cmd.Context())
			if err != nil {
				return err
			}
			defer closeClient()
			options.Environment = environment
			svc := metricsquery.NewService(client, environment)
			svc.SetPodResolver(newPlatformMetricsPodResolver(platformClient))
			result, err := svc.GPU(cmd.Context(), options)
			if err != nil {
				return err
			}
			return output.PrintMetrics(result, flags.output)
		},
	}
	bindMetricsWorkloadFlags(command, &flags)
	return command
}

func newMetricsRestartCmd() *cobra.Command {
	var flags metricsFlags
	command := &cobra.Command{
		Use:   "restart <workload>",
		Short: "查看容器重启计数和终止原因时间线",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options, err := flags.queryOptions(cmd, args[0])
			if err != nil {
				return err
			}
			client, environment, platformClient, closeClient, err := newMetricsClient(cmd.Context())
			if err != nil {
				return err
			}
			defer closeClient()
			options.Environment = environment
			svc := metricsquery.NewService(client, environment)
			svc.SetPodResolver(newPlatformMetricsPodResolver(platformClient))
			result, err := svc.Restart(cmd.Context(), options)
			if err != nil {
				return err
			}
			return output.PrintMetrics(result, flags.output)
		},
	}
	bindMetricsWorkloadFlags(command, &flags)
	return command
}

func newMetricsRawCmd() *cobra.Command {
	var flags metricsFlags
	command := &cobra.Command{
		Use:   "raw <promql>",
		Short: "直接执行 PromQL",
		Long:  "直接执行 PromQL。不指定 --step 时执行瞬时 query；指定 --step 后执行 query_range。瞬时查询可用 --end 指定查询时刻。",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			useRange := strings.TrimSpace(flags.step) != ""
			if !useRange && (cmd.Flags().Changed("since") || strings.TrimSpace(flags.start) != "") {
				return fmt.Errorf("metrics raw 的 --since/--start 需要同时指定 --step；瞬时查询请只使用 --end")
			}
			if useRange {
				if err := validateMetricsTimeFlags(cmd, flags.start, flags.end); err != nil {
					return err
				}
			}
			start, end, err := parseMetricsRange(flags.since, flags.start, flags.end)
			if err != nil {
				return err
			}
			if !useRange {
				start = end
			}
			var step time.Duration
			if useRange {
				step, err = time.ParseDuration(flags.step)
				if err != nil || step <= 0 {
					return fmt.Errorf("invalid --step %q, examples: 20s, 1m, 5m", flags.step)
				}
			}
			if err := validateMetricsOutput(flags.output); err != nil {
				return err
			}
			client, environment, _, closeClient, err := newMetricsClient(cmd.Context())
			if err != nil {
				return err
			}
			defer closeClient()
			result, err := metricsquery.NewService(client, environment).Raw(cmd.Context(), args[0], start, end, step, useRange)
			if err != nil {
				return err
			}
			return output.PrintMetrics(result, flags.output)
		},
	}
	command.Flags().StringVar(&flags.since, "since", "2h", "回溯窗口；raw 仅在同时指定 --step 时生效")
	command.Flags().StringVar(&flags.start, "start", "", "开始时间，支持 RFC3339 或 UTC+8 的 2006-01-02 15:04:05")
	command.Flags().StringVar(&flags.end, "end", "", "结束/瞬时查询时间，支持 RFC3339 或 UTC+8 的 2006-01-02 15:04:05")
	command.Flags().StringVar(&flags.step, "step", "", "查询步长，例如 20s、1m；指定后使用 query_range")
	command.Flags().StringVarP(&flags.output, "output", "o", "table", "输出格式: table、json、csv")
	return command
}

func bindMetricsWorkloadFlags(command *cobra.Command, flags *metricsFlags) {
	command.Flags().StringVarP(&flags.workloadType, "type", "t", "auto", "工作负载类型: auto、aid、ait、air")
	command.Flags().StringVarP(&flags.workspace, "workspace", "w", "", "指定 workspace，重名工作负载时必填")
	command.Flags().StringVar(&flags.since, "since", "2h", "回溯窗口，例如 30m、2h、24h")
	command.Flags().StringVar(&flags.start, "start", "", "开始时间，支持 RFC3339 或 UTC+8 的 2006-01-02 15:04:05")
	command.Flags().StringVar(&flags.end, "end", "", "结束时间，支持 RFC3339 或 UTC+8 的 2006-01-02 15:04:05")
	command.Flags().StringVar(&flags.step, "step", "", "查询步长；不指定时读取原始采样点")
	command.Flags().StringArrayVar(&flags.pods, "pod", nil, "只查看指定 Pod，可重复传入")
	command.Flags().StringVar(&flags.by, "by", "pod", "聚合维度: pod、container、gpu")
	command.Flags().StringVarP(&flags.output, "output", "o", "table", "输出格式: table、json、csv")
}

func (flags metricsFlags) queryOptions(cmd *cobra.Command, workload string) (metricsquery.QueryOptions, error) {
	workload = strings.TrimSpace(workload)
	if workload == "" {
		return metricsquery.QueryOptions{}, fmt.Errorf("workload is required")
	}
	workloadType := strings.ToLower(strings.TrimSpace(flags.workloadType))
	switch workloadType {
	case "", "auto", "aid", "ait", "air":
	default:
		return metricsquery.QueryOptions{}, fmt.Errorf("unsupported --type %q; expected auto, aid, ait, or air", flags.workloadType)
	}
	by := strings.ToLower(strings.TrimSpace(flags.by))
	switch by {
	case "pod", "container", "gpu":
	default:
		return metricsquery.QueryOptions{}, fmt.Errorf("unsupported --by %q; expected pod, container, or gpu", flags.by)
	}
	if err := validateMetricsOutput(flags.output); err != nil {
		return metricsquery.QueryOptions{}, err
	}
	if err := validateMetricsTimeFlags(cmd, flags.start, flags.end); err != nil {
		return metricsquery.QueryOptions{}, err
	}
	start, end, err := parseMetricsRange(flags.since, flags.start, flags.end)
	if err != nil {
		return metricsquery.QueryOptions{}, err
	}
	var step time.Duration
	if strings.TrimSpace(flags.step) != "" {
		step, err = time.ParseDuration(strings.TrimSpace(flags.step))
		if err != nil || step <= 0 {
			return metricsquery.QueryOptions{}, fmt.Errorf("invalid --step %q, examples: 20s, 1m, 5m", flags.step)
		}
	}
	return metricsquery.QueryOptions{
		Workload: workload, Type: workloadType, Workspace: strings.TrimSpace(flags.workspace), Pods: flags.pods, By: by,
		Start: start, End: end, Step: step, UseStep: step > 0,
	}, nil
}

func validateMetricsTimeFlags(cmd *cobra.Command, start string, end string) error {
	startSet := strings.TrimSpace(start) != ""
	endSet := strings.TrimSpace(end) != ""
	if startSet != endSet {
		return fmt.Errorf("--start 和 --end 必须同时指定")
	}
	if startSet && cmd != nil && cmd.Flags().Changed("since") {
		return fmt.Errorf("--since 与 --start/--end 不能同时使用")
	}
	return nil
}

func parseMetricsRange(since string, startText string, endText string) (time.Time, time.Time, error) {
	end := time.Now()
	var err error
	if strings.TrimSpace(endText) != "" {
		end, err = parseMetricsTime(endText)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --end: %w", err)
		}
	}
	start := time.Time{}
	if strings.TrimSpace(startText) != "" {
		start, err = parseMetricsTime(startText)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --start: %w", err)
		}
	} else {
		duration, parseErr := time.ParseDuration(strings.TrimSpace(since))
		if parseErr != nil || duration <= 0 {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --since %q, examples: 30m, 2h, 24h", since)
		}
		start = end.Add(-duration)
	}
	if !start.Before(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("--start must be earlier than --end")
	}
	return start.UTC(), end.UTC(), nil
}

func parseMetricsTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if parsed, err := time.ParseInLocation(layout, value, metricsquery.BeijingLocation); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("expected RFC3339 or UTC+8 time like 2006-01-02 15:04:05")
}

func validateMetricsOutput(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "table", "json", "csv":
		return nil
	default:
		return fmt.Errorf("unsupported --output %q; expected table, json, or csv", value)
	}
}

func newMetricsClient(ctx context.Context) (*metricsquery.Client, string, *platform.VirtualClusterClient, func(), error) {
	platformClient, ok := platform.NewVirtualClusterClientFromEnv()
	if !ok {
		return nil, "", nil, nil, fmt.Errorf("platform config not found; please configure ~/.rayctl/platform.json")
	}
	config, err := platformClient.CurrentMetricsConfig()
	if err != nil {
		return nil, "", nil, nil, err
	}
	if strings.TrimSpace(config.Username) == "" || strings.TrimSpace(config.Password) == "" {
		return nil, "", nil, nil, fmt.Errorf("metrics Basic Auth 未配置；请在当前 profile 添加 metrics_username 和 metrics_password")
	}
	client, err := metricsquery.NewClient(ctx, metricsquery.EndpointConfig{
		BaseURL: config.BaseURL, Username: config.Username, Password: config.Password,
		Kubeconfig: kube.ResolvedKubeconfigPath(kubeconfig), RequestTimeout: 30 * time.Second,
	})
	if err != nil {
		return nil, "", nil, nil, err
	}
	return client, config.Environment, platformClient, client.Close, nil
}
