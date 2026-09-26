package cmd

import (
	"context"
	"errors"
	"fmt"
	"github.com/spf13/cobra"
	"rayctl/internal/service"
	"strings"
	"time"
)

func newECPJobGetCmd() *cobra.Command {
	var debugTiming bool
	var longOutput bool
	var queryTimeout time.Duration

	getCmd := &cobra.Command{
		Use:   "get <job-name-or-pod-name-or-uid> [job-name-or-pod-name-or-uid...]",
		Short: "并行查询一个或多个 ATE VCJob",
		Long:  "根据任务名、Pod 名或 UID 并行查询一个或多个 ATE VCJob 详情。",
		Example: strings.Join([]string{
			"  rayctl ate get example-job",
			"  rayctl ate get job-a job-b job-c",
		}, "\n"),
		Args: cobra.MinimumNArgs(1),
		RunE: func(getCmd *cobra.Command, args []string) error {
			clientset, dynamicClient, err := newJobClients()
			if err != nil {
				return err
			}

			vcClient, platformErr := optionalPlatformClient()
			if platformErr != nil {
				return platformErr
			}
			jobService := service.NewJobService(clientset, dynamicClient, vcClient)
			return runParallelECPJobGet(
				getCmd.Context(), args, jobService, vcClient,
				jobGetOptions{
					longOutput:  longOutput,
					debugTiming: debugTiming,
					timeout:     queryTimeout,
				},
			)
		},
	}

	getCmd.Flags().BoolVar(&debugTiming, "debug-timing", false, "Print timing diagnostics for job get")
	getCmd.Flags().BoolVarP(&longOutput, "long", "l", false, "显示 master Pod 的最新日志")
	getCmd.Flags().DurationVar(&queryTimeout, "timeout", defaultJobGetTimeout, "单个任务的查询超时，例如 5s、30s；设为 0 表示不限制")
	return getCmd
}

func formatJobGetError(ctx context.Context, identifier string, timeout time.Duration, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("job %q 查询超过 %s，已自动停止；可能是平台 API、HC API 或 Pending 诊断请求响应较慢，请检查 kubeconfig 与 platform profile，也可通过 --timeout 临时增加查询时间", identifier, timeout)
	}
	return fmt.Errorf("job %q: %w", identifier, err)
}

func normalizeJobGetIdentifier(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func virtualClusterUIDFromName(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "vc-") {
		candidate := strings.TrimPrefix(value, "vc-")
		if looksLikeUUID(candidate) {
			return candidate
		}
	}
	if looksLikeUUID(value) {
		return value
	}
	return ""
}

func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, ch := range value {
		switch i {
		case 8, 13, 18, 23:
			if ch != '-' {
				return false
			}
		default:
			if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') && (ch < 'A' || ch > 'F') {
				return false
			}
		}
	}
	return true
}
