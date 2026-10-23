package cmd

import (
	"context"
	"errors"
	"time"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newPodCmd() *cobra.Command {
	parent := &cobra.Command{Use: "pod", Short: "从 HC Pod 名或 IP 反查负载"}
	var namespace string
	var long, noPlatform bool
	get := &cobra.Command{
		Use:   "get <pod-name|host-namespace/pod-name|pod-ip>",
		Short: "查询 Pod 身份、负载归属和运行状态",
		Long:  "查询 Pod；-e all 查询 D/PT 的 HC 并标明环境，同名跨环境需要 -e 消歧。auto 仅查当前环境。",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targets, err := hcTargets(cmd)
			if err != nil {
				return err
			}
			var platformClient *platform.VirtualClusterClient
			var platformErr error
			if !noPlatform {
				platformClient, platformErr = optionalPlatformClient()
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			result, queryErr := service.QueryHCEnvironments(ctx, targets, args[0], func(ctx context.Context, target service.HCEnvironment) (*service.PodLocateResult, error) {
				result, err := service.NewPodLocateService(target.Client, platformClient).Get(ctx, args[0], namespace, long)
				if result != nil {
					result.Environment = target.Name
				}
				return result, err
			}, func(err error) bool {
				var missing *service.PodNotFoundError
				return errors.As(err, &missing) || apierrors.IsNotFound(err)
			})
			if result == nil {
				return queryErr
			}
			if platformErr != nil {
				result.Warnings = append(result.Warnings, "platform enrichment unavailable: "+platformErr.Error())
			}
			output.PrintPodLocateResult(result)
			return queryErr
		},
	}
	get.Flags().StringVarP(&namespace, "namespace", "n", "", "HC namespace")
	get.Flags().BoolVarP(&long, "long", "l", false, "显示 PVC 到 AFS 的映射")
	get.Flags().BoolVar(&noPlatform, "no-platform", false, "仅查询 Kubernetes，不调用平台 API")
	parent.AddCommand(get)
	return parent
}
