package cmd

import (
	"context"
	"time"

	"github.com/spf13/cobra"
	"rayctl/internal/kube"
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
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := kube.NewClientset(kubeconfig)
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
			result, err := service.NewPodLocateService(client, platformClient).Get(ctx, args[0], namespace, long)
			if err != nil {
				return err
			}
			if platformErr != nil {
				result.Warnings = append(result.Warnings, "platform enrichment unavailable: "+platformErr.Error())
			}
			output.PrintPodLocateResult(result)
			return nil
		},
	}
	get.Flags().StringVarP(&namespace, "namespace", "n", "", "HC namespace")
	get.Flags().BoolVarP(&long, "long", "l", false, "显示 PVC 到 AFS 的映射")
	get.Flags().BoolVar(&noPlatform, "no-platform", false, "仅查询 Kubernetes，不调用平台 API")
	parent.AddCommand(get)
	return parent
}
