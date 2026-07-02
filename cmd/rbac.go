package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newRBACCmd() *cobra.Command {
	rbacCmd := &cobra.Command{
		Use:   "rbac",
		Short: "查询平台 RBAC 绑定信息",
	}

	rbacCmd.AddCommand(newRBACGetCmd())
	return rbacCmd
}

func newRBACGetCmd() *cobra.Command {
	var selector string

	cmd := &cobra.Command{
		Use:   "get <vc-name-or-uid>",
		Short: "查看 VC 集群维度 ClusterRoleBinding",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}

			rbacService := service.NewRBACService(vcClient)
			result, err := rbacService.Get(context.Background(), args[0], selector, rbacBearerToken())
			if err != nil {
				return err
			}
			output.PrintRBACGetResult(result)
			return nil
		},
	}

	cmd.Flags().StringVarP(&selector, "selector", "l", "", "ClusterRoleBinding labelSelector，默认 resource.compute.sensecore.cn/control")
	return cmd
}

func rbacBearerToken() string {
	for _, key := range []string{"RAYCTL_BEARER_TOKEN", "BEARER_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return strings.TrimPrefix(value, "Bearer ")
		}
	}
	return ""
}
