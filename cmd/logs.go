package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"

	"github.com/spf13/cobra"
)

func newLogsCmd() *cobra.Command {
	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "查询平台监控日志",
	}
	logsCmd.AddCommand(newLogsECPCmd())
	return logsCmd
}

func newLogsECPCmd() *cobra.Command {
	ecpCmd := &cobra.Command{
		Use:   "ecp",
		Short: "查询 ECP 相关日志",
	}
	ecpCmd.AddCommand(newLogsECPWorkloadCmd())
	ecpCmd.AddCommand(&cobra.Command{
		Use:   "audit <vc-name>",
		Short: "查询 ECP 审计日志",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("rayctl logs ecp audit 暂未实现；当前先支持 workload 日志")
		},
	})
	return ecpCmd
}

func newLogsECPWorkloadCmd() *cobra.Command {
	var since string
	var workloadType string
	var workloadName string
	var pods []string
	var level string
	var keyword string
	var namespace string
	var container string
	var limit int

	workloadCmd := &cobra.Command{
		Use:   "workload <vc-name>",
		Short: "查询 ECP workload 日志",
		Example: strings.Join([]string{
			"rayctl logs ecp workload vc-a3-intern-delivery -n job-demo -l ERROR -K error",
			"rayctl logs ecp workload vc-a3-intern-delivery -t deployment -n nginx --since 2h",
			"rayctl logs ecp workload vc-a3-intern-delivery -n job-demo -p job-demo-master-0 -p job-demo-worker-0",
		}, "\n"),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform config not found; please configure ~/.rayctl/platform.json")
			}

			duration, err := time.ParseDuration(strings.TrimSpace(since))
			if err != nil || duration <= 0 {
				return fmt.Errorf("invalid --since %q, examples: 30m, 2h, 24h", since)
			}
			if limit <= 0 {
				return fmt.Errorf("--limit must be greater than 0")
			}

			svc := service.NewLogsService(vcClient)
			result, err := svc.GetECPWorkloadLogs(context.Background(), service.ECPWorkloadLogOptions{
				VCluster:     args[0],
				Since:        duration,
				WorkloadType: workloadType,
				WorkloadName: workloadName,
				Pods:         pods,
				Level:        level,
				Keyword:      keyword,
				Namespace:    namespace,
				Container:    container,
				Limit:        limit,
			})
			if err != nil {
				return err
			}
			output.PrintECPWorkloadLogs(result)
			return nil
		},
	}

	workloadCmd.Flags().StringVar(&since, "since", "24h", "查询最近一段时间的日志，例如 30m、2h、24h")
	workloadCmd.Flags().StringVarP(&workloadType, "type", "t", "vcjob", "workload 类型，例如 vcjob、deployment")
	workloadCmd.Flags().StringVarP(&workloadName, "name", "n", "", "workload/任务名称")
	workloadCmd.Flags().StringArrayVarP(&pods, "pod", "p", nil, "Pod 名称，可重复传入")
	workloadCmd.Flags().StringVarP(&level, "level", "l", "", "日志级别，例如 INFO、ERROR、WARN、DEBUG")
	workloadCmd.Flags().StringVarP(&keyword, "keyword", "K", "", "日志关键词，使用 matchPhrase 过滤")
	workloadCmd.Flags().StringVar(&namespace, "namespace", "", "Kubernetes namespace，可选")
	workloadCmd.Flags().StringVar(&container, "container", "", "容器名称，可选")
	workloadCmd.Flags().IntVar(&limit, "limit", 40, "返回日志条数")
	return workloadCmd
}
