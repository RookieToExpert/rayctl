package cmd

import (
	"bufio"
	"context"
	"errors"
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
	rbacCmd.AddCommand(newRBACGrantCmd())
	rbacCmd.AddCommand(newRBACRemoveCmd())
	return rbacCmd
}

func newRBACGrantCmd() *cobra.Command {
	var namespace string
	var role string
	var users []string
	var groups []string
	var bearerToken string
	var dryRun bool
	var yes bool
	var debugAuth bool

	cmd := &cobra.Command{
		Use:   "grant <vc-name-or-uid>",
		Short: "通过平台 API 给 VC 用户或用户组授予 Kubernetes RBAC 权限",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(namespace) == "" {
				return fmt.Errorf("请使用 --namespace 指定 namespace，集群级授权请填写 all")
			}
			if strings.TrimSpace(role) == "" {
				return fmt.Errorf("请使用 --role 指定 cluster-admin、admin、edit 或 view")
			}
			if len(users) == 0 && len(groups) == 0 {
				return fmt.Errorf("请至少使用一个 --user 或 --group 指定授权对象")
			}

			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}
			iamToken, computeToken, source, err := bearerTokensForRBACGrantCommand(context.Background(), cmd, vcClient, bearerToken)
			if err != nil {
				return err
			}
			if debugAuth {
				fmt.Fprintf(cmd.ErrOrStderr(), "rbac grant debug: source=%s iam=%s compute=%s\n", source, bearerDebugSummary(iamToken), bearerDebugSummary(computeToken))
			}

			rbacService := service.NewRBACService(vcClient)
			result, err := rbacService.PrepareGrant(context.Background(), service.RBACGrantRequest{
				ClusterIdentifier:  args[0],
				Namespace:          namespace,
				Role:               role,
				Users:              users,
				Groups:             groups,
				IAMBearerToken:     iamToken,
				ComputeBearerToken: computeToken,
				DryRun:             dryRun,
			})
			if err != nil {
				return err
			}
			output.PrintRBACGrantResult(result)
			if dryRun || result.Result == "already granted" {
				return nil
			}

			if !yes {
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"将在 VC %s 的 %s 范围创建 %s 并绑定角色 %s，是否继续? (y/N): ",
					result.ClusterName,
					result.Namespace,
					result.BindingKind,
					result.Role,
				)
				line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if err != nil {
					return err
				}
				if !isYes(line) {
					fmt.Fprintln(cmd.OutOrStdout(), "已取消授权。")
					return nil
				}
			}

			if err := rbacService.ApplyGrant(context.Background(), result, computeToken); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "授权完成: binding=%s result=%s\n", result.BindingName, result.Result)
			return nil
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "授权范围：all 表示整个 VC，否则填写具体 namespace")
	cmd.Flags().StringVarP(&role, "role", "r", "", "角色：cluster-admin、admin、edit、view")
	cmd.Flags().StringSliceVarP(&users, "user", "u", nil, "用户 username 或 user ID；支持逗号分隔或重复传入")
	cmd.Flags().StringSliceVarP(&groups, "group", "g", nil, "用户组 name 或 group ID；支持逗号分隔或重复传入")
	cmd.Flags().StringVarP(&bearerToken, "bearer-token", "t", "", "compute 代理 Bearer token；IAM token 仍优先读取登录 session")
	cmd.Flags().BoolVar(&debugAuth, "debug-auth", false, "打印脱敏授权调试信息，例如 token 来源")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "只展示授权计划和 payload，不真正创建 Binding")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "跳过确认直接创建 Binding")
	return cmd
}

func newRBACRemoveCmd() *cobra.Command {
	var namespace string
	var bindingName string
	var bearerToken string
	var dryRun bool
	var yes bool
	var debugAuth bool

	cmd := &cobra.Command{
		Use:   "remove <vc-name-or-uid>",
		Short: "通过平台 API 删除 VC 中指定的 Kubernetes RBAC Binding",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(namespace) == "" {
				return fmt.Errorf("请使用 --namespace 指定 namespace，集群级授权请填写 all")
			}
			if strings.TrimSpace(bindingName) == "" {
				return fmt.Errorf("请使用 --binding 精确指定要删除的 Binding 名")
			}

			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}
			computeToken, source, err := bearerTokenForRBACComputeCommand(context.Background(), cmd, vcClient, bearerToken)
			if err != nil {
				return err
			}
			if debugAuth {
				fmt.Fprintf(cmd.ErrOrStderr(), "rbac remove debug: source=%s compute=%s\n", source, bearerDebugSummary(computeToken))
			}

			rbacService := service.NewRBACService(vcClient)
			result, err := rbacService.PrepareRemove(context.Background(), service.RBACRemoveRequest{
				ClusterIdentifier:  args[0],
				Namespace:          namespace,
				BindingName:        bindingName,
				ComputeBearerToken: computeToken,
				DryRun:             dryRun,
			})
			if err != nil {
				return err
			}
			output.PrintRBACRemoveResult(result)
			if dryRun {
				return nil
			}

			if !yes {
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"将删除整个 %s %s，并移除其中 %d 个 Subject 的 %s 权限，是否继续? (y/N): ",
					result.BindingKind,
					result.BindingName,
					result.SubjectCount,
					result.Role,
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

			if err := rbacService.ApplyRemove(context.Background(), result, computeToken); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "移除完成: binding=%s result=%s\n", result.BindingName, result.Result)
			return nil
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "授权范围：all 表示整个 VC，否则填写具体 namespace")
	cmd.Flags().StringVarP(&bindingName, "binding", "b", "", "要删除的 RoleBinding 或 ClusterRoleBinding 名")
	cmd.Flags().StringVarP(&bearerToken, "bearer-token", "t", "", "compute 代理 Bearer token，默认读取登录 session 的 id_token")
	cmd.Flags().BoolVar(&debugAuth, "debug-auth", false, "打印脱敏授权调试信息，例如 token 来源")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "只展示删除影响，不真正删除 Binding")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "跳过确认直接删除 Binding")
	return cmd
}

func newRBACGetCmd() *cobra.Command {
	var selector string
	var long bool

	cmd := &cobra.Command{
		Use:   "get <vc-name-or-uid> [vc-name-or-uid...]",
		Short: "并行查看一个或多个 VC 中平台管理的 RBAC Binding",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}

			ctx := context.Background()
			bearerToken, err := bearerTokenForCommand(ctx, cmd, vcClient, "")
			if err != nil {
				return err
			}
			rbacService := service.NewRBACService(vcClient)
			type queryResult struct {
				identifier string
				result     *service.RBACGetResult
				err        error
			}
			queries := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				result, err := rbacService.Get(ctx, identifier, selector, bearerToken)
				return queryResult{identifier, result, err}
			})
			valid := make([]*service.RBACGetResult, 0)
			queryErrors := make([]error, 0)
			for _, query := range queries {
				if query.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("rbac %q: %w", query.identifier, query.err))
					continue
				}
				valid = append(valid, query.result)
			}
			if long && len(args) == 1 && len(valid) == 1 {
				output.PrintRBACGetResult(valid[0], true)
			} else {
				output.PrintRBACGetSummary(valid, len(args) > 1, long)
			}
			return errors.Join(queryErrors...)
		},
	}

	cmd.Flags().StringVarP(&selector, "selector", "s", "", "RoleBinding/ClusterRoleBinding labelSelector，默认 resource.compute.sensecore.cn/control")
	cmd.Flags().BoolVarP(&long, "long", "l", false, "显示 Binding 名和精确到秒的创建时间")
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
