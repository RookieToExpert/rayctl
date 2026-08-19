package cmd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"rayctl/internal/platform"
	"rayctl/internal/service"
	authsession "rayctl/internal/session"
	"rayctl/pkg/output"
)

func newAuthCmd() *cobra.Command {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "查询和更新平台资源授权信息",
	}

	authCmd.AddCommand(newAuthCheckCmd())
	authCmd.AddCommand(newAuthGrantCmd())
	authCmd.AddCommand(newAuthRemoveCmd())
	authCmd.AddCommand(newAuthRolesCmd())
	authCmd.AddCommand(newAuthSSPCmd())
	authCmd.AddCommand(newAuthLoginCmd())
	authCmd.AddCommand(newAuthLogoutCmd())
	authCmd.AddCommand(newAuthTokenCmd())

	legacyAFS := newAuthCheckAFSCmd()
	legacyAFS.Deprecated = "请使用 rayctl auth check afs <afs-name>"
	legacyUser := newAuthCheckUserCmd()
	legacyUser.Deprecated = "请使用 rayctl auth check user <username-or-userid>"
	legacyGroups := newAuthCheckGroupsCmd()
	legacyGroups.Deprecated = "请使用 rayctl auth check groups <group-name-or-id>"
	authCmd.AddCommand(legacyAFS, legacyUser, legacyGroups)

	return authCmd
}

func newAuthCheckCmd() *cobra.Command {
	checkCmd := &cobra.Command{
		Use:   "check",
		Short: "查看用户、用户组、资源的授权信息",
	}
	checkCmd.AddCommand(newAuthCheckAFSCmd())
	checkCmd.AddCommand(newAuthCheckResourceCmd("vc", "查看 VC 归属的用户/用户组授权"))
	checkCmd.AddCommand(newAuthCheckResourceCmd("ccr", "查看 CCR namespace 归属的用户/用户组授权"))
	checkCmd.AddCommand(newAuthCheckResourceCmd("subnet", "查看 Subnet 归属的用户/用户组授权"))
	checkCmd.AddCommand(newAuthCheckResourceCmd("ais", "查看开发机 AI Space 归属的用户/用户组授权"))
	checkCmd.AddCommand(newAuthCheckResourceCmd("vpc", "查看 VPC 归属的用户/用户组授权"))
	checkCmd.AddCommand(newAuthCheckResourceCmd("eip", "查看 EIP 归属的用户/用户组授权"))
	checkCmd.AddCommand(newAuthCheckResourceCmd("natgateway", "查看 NAT Gateway 归属的用户/用户组授权"))
	checkCmd.AddCommand(newAuthCheckGroupsCmd())
	checkCmd.AddCommand(newAuthCheckUserCmd())
	return checkCmd
}

func newAuthGrantCmd() *cobra.Command {
	grantCmd := &cobra.Command{
		Use:   "grant",
		Short: "给资源新增授权",
	}
	grantCmd.AddCommand(newAuthGrantAFSCmd())
	grantCmd.AddCommand(newAuthGrantResourceCmd("vc", "vc <vc-name>", "给 VC 授权用户或用户组", "VC 角色: user/admin 或完整 role_name"))
	grantCmd.AddCommand(newAuthGrantResourceCmd("ccr", "ccr <namespace-name>", "给 CCR namespace 授权用户或用户组", "CCR 角色: user/imageUser/owner 或完整 role_name"))
	grantCmd.AddCommand(newAuthGrantResourceCmd("subnet", "subnet <subnet-name>", "给 Subnet 授权用户或用户组", "Subnet 角色: reader/editor 或完整 role_name"))
	grantCmd.AddCommand(newAuthGrantResourceCmd("ais", "ais <ais-name>", "给开发机 AI Space 授权用户或用户组", "AIS 角色: owner 或完整 role_name"))
	grantCmd.AddCommand(newAuthGrantResourceCmd("vpc", "vpc <vpc-name>", "给 VPC 授权用户或用户组", "VPC 角色: reader/editor 或完整 role_name"))
	grantCmd.AddCommand(newAuthGrantResourceCmd("eip", "eip <eip-name>", "给 EIP 授权用户或用户组", "EIP 角色: reader/editor 或完整 role_name"))
	grantCmd.AddCommand(newAuthGrantResourceCmd("natgateway", "natgateway <natgateway-name>", "给 NAT Gateway 授权用户或用户组", "NAT Gateway 角色: operator 或完整 role id/name"))
	return grantCmd
}

func newAuthRemoveCmd() *cobra.Command {
	removeCmd := newAuthRemoveResourceCmd("afs", "remove [afs-name]", "移除资源授权", "AFS 角色: editor/reader/owner 或完整 role_name")
	removeCmd.AddCommand(newAuthRemoveResourceCmd("afs", "afs <afs-name>", "移除 AFS 授权", "AFS 角色: editor/reader/owner 或完整 role_name"))
	removeCmd.AddCommand(newAuthRemoveResourceCmd("vc", "vc <vc-name>", "移除 VC 授权", "VC 角色: user/admin 或完整 role_name"))
	removeCmd.AddCommand(newAuthRemoveResourceCmd("ccr", "ccr <namespace-name>", "移除 CCR namespace 授权", "CCR 角色: user/imageUser/owner 或完整 role_name"))
	removeCmd.AddCommand(newAuthRemoveResourceCmd("subnet", "subnet <subnet-name>", "移除 Subnet 授权", "Subnet 角色: reader/editor 或完整 role_name"))
	removeCmd.AddCommand(newAuthRemoveResourceCmd("ais", "ais <ais-name>", "移除开发机 AI Space 授权", "AIS 角色: owner 或完整 role_name"))
	removeCmd.AddCommand(newAuthRemoveResourceCmd("vpc", "vpc <vpc-name>", "移除 VPC 授权", "VPC 角色: reader/editor 或完整 role_name"))
	removeCmd.AddCommand(newAuthRemoveResourceCmd("eip", "eip <eip-name>", "移除 EIP 授权", "EIP 角色: reader/editor 或完整 role_name"))
	removeCmd.AddCommand(newAuthRemoveResourceCmd("natgateway", "natgateway <natgateway-name>", "移除 NAT Gateway 授权", "NAT Gateway 角色: operator 或完整 role id/name"))
	return removeCmd
}

func newAuthRolesCmd() *cobra.Command {
	rolesCmd := &cobra.Command{
		Use:   "roles",
		Short: "查看资源可授权角色",
	}
	rolesCmd.AddCommand(newAuthRolesResourceCmd("afs", "afs", "查看 AFS 可授权角色"))
	rolesCmd.AddCommand(newAuthRolesResourceCmd("vc", "vc", "查看 VC 可授权角色"))
	rolesCmd.AddCommand(newAuthRolesResourceCmd("ccr", "ccr", "查看 CCR namespace 可授权角色"))
	rolesCmd.AddCommand(newAuthRolesResourceCmd("subnet", "subnet", "查看 Subnet 可授权角色"))
	rolesCmd.AddCommand(newAuthRolesResourceCmd("ais", "ais", "查看开发机 AI Space 可授权角色"))
	rolesCmd.AddCommand(newAuthRolesResourceCmd("vpc", "vpc", "查看 VPC 可授权角色"))
	rolesCmd.AddCommand(newAuthRolesResourceCmd("eip", "eip", "查看 EIP 可授权角色"))
	rolesCmd.AddCommand(newAuthRolesResourceCmd("natgateway", "natgateway", "查看 NAT Gateway 可授权角色"))
	return rolesCmd
}

func newAuthCheckAFSCmd() *cobra.Command {
	return newAuthCheckResourceCmd("afs", "查看 AFS 归属的用户/用户组授权")
}

func newAuthCheckResourceCmd(resourceType string, short string) *cobra.Command {
	var bearerToken string
	var long bool
	cmd := &cobra.Command{
		Use:   resourceType + " <resource-name> [resource-name...]",
		Short: short,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthCheckResources(cmd, resourceType, args, bearerToken, long)
		},
	}
	cmd.Flags().StringVarP(&bearerToken, "bearer-token", "t", "", "控制台 Bearer token；也可使用当前 auth login session")
	cmd.Flags().BoolVarP(&long, "long", "l", false, "显示完整授权成员、角色名和创建时间")
	return cmd
}

func newAuthCheckGroupsCmd() *cobra.Command {
	var long bool
	var environment string
	cmd := &cobra.Command{
		Use:     "groups <group-name-or-id> [group-name-or-id...]",
		Aliases: []string{"group"},
		Short:   "查看用户组信息和权限",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthCheckGroups(cmd, args, long, environment)
		},
	}
	cmd.Flags().BoolVarP(&long, "long", "l", false, "显示 source/service/role names/create time 等详细权限信息")
	cmd.Flags().StringVarP(&environment, "environment", "v", "", "指定平台环境: d、pt/p、dcloud；默认使用 current_profile")
	return cmd
}

func newAuthCheckUserCmd() *cobra.Command {
	var long bool
	var environment string
	cmd := &cobra.Command{
		Use:   "user <username-or-userid> [username-or-userid...]",
		Short: "查看用户所属组和权限",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthCheckUser(cmd, args, long, environment)
		},
	}
	cmd.Flags().BoolVarP(&long, "long", "l", false, "显示 member/source/service/role names/create time 等详细权限信息")
	cmd.Flags().StringVarP(&environment, "environment", "v", "", "指定平台环境: d、pt/p、dcloud；默认使用 current_profile")
	return cmd
}

func newAuthSSPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ssp",
		Short: "查看和授予 SSP 工作空间成员权限",
	}
	cmd.AddCommand(newAuthSSPCheckCmd())
	cmd.AddCommand(newAuthSSPGrantCmd())
	return cmd
}

func newAuthSSPCheckCmd() *cobra.Command {
	var bearerToken string
	cmd := &cobra.Command{
		Use:   "check <workspace>",
		Short: "查看 SSP 工作空间成员和角色",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}
			profileName, err := vcClient.ResolveProfileName("")
			if err != nil {
				return err
			}
			token, _, err := bearerTokenForAuthGrantCommand(cmd.Context(), cmd, vcClient, bearerToken)
			if err != nil {
				return err
			}
			result, err := service.NewAuthService(vcClient).GetSSPWorkspaceAuth(cmd.Context(), profileName, profileName, args[0], token)
			if err != nil {
				return err
			}
			output.PrintAuthSSPResult(result)
			return nil
		},
	}
	cmd.Flags().StringVarP(&bearerToken, "bearer-token", "t", "", "控制台 Bearer token；默认使用当前 auth login session")
	return cmd
}

func newAuthSSPGrantCmd() *cobra.Command {
	var user string
	var group string
	var roles []string
	var priority string
	var bearerToken string
	var dryRun bool
	var yes bool
	cmd := &cobra.Command{
		Use:   "grant <workspace>",
		Short: "给 SSP 工作空间授权用户或用户组",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			memberType, memberIdentifier, err := authMemberFlagValues(user, group)
			if err != nil {
				return err
			}
			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}
			profileName, err := vcClient.ResolveProfileName("")
			if err != nil {
				return err
			}
			token, _, err := bearerTokenForAuthGrantCommand(cmd.Context(), cmd, vcClient, bearerToken)
			if err != nil {
				return err
			}
			req := service.AuthSSPGrantRequest{
				Workspace:        args[0],
				ProfileName:      profileName,
				Environment:      profileName,
				MemberType:       memberType,
				MemberIdentifier: memberIdentifier,
				Roles:            roles,
				Priority:         priority,
				BearerToken:      token,
				DryRun:           true,
			}
			authService := service.NewAuthService(vcClient)
			result, err := authService.GrantSSPWorkspace(cmd.Context(), req)
			if err != nil {
				return err
			}
			if dryRun {
				output.PrintAuthSSPGrantResult(result)
				return nil
			}
			if !yes {
				fmt.Fprintf(cmd.OutOrStdout(), "将给 SSP 工作空间 %s 授权 %s %s，角色 %s，优先级 %s，是否继续? (y/N): ", result.Workspace, result.MemberType, result.MemberName, result.Roles, result.Priority)
				line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if err != nil {
					return err
				}
				if !isYes(line) {
					fmt.Fprintln(cmd.OutOrStdout(), "已取消授权。")
					return nil
				}
			}
			req.DryRun = false
			result, err = authService.GrantSSPWorkspace(cmd.Context(), req)
			if err != nil {
				return err
			}
			output.PrintAuthSSPGrantResult(result)
			return nil
		},
	}
	cmd.Flags().StringVarP(&user, "user", "u", "", "授权给用户 username 或 user id")
	cmd.Flags().StringVarP(&group, "group", "g", "", "授权给用户组 name 或 group id")
	cmd.Flags().StringSliceVarP(&roles, "role", "r", nil, "角色 alias/name，可重复或逗号分隔，例如 aid-creator,ait-operator")
	cmd.Flags().StringVarP(&priority, "priority", "p", "NORMAL", "工作空间优先级上限: NORMAL、HIGH、HIGHEST")
	cmd.Flags().StringVarP(&bearerToken, "bearer-token", "t", "", "控制台 Bearer token；默认使用当前 auth login session")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "只展示将要提交的授权 payload，不真正写入")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "跳过确认直接写入")
	return cmd
}

func authMemberFlagValues(user string, group string) (string, string, error) {
	user = strings.TrimSpace(user)
	group = strings.TrimSpace(group)
	if user != "" && group != "" {
		return "", "", fmt.Errorf("--user 和 --group 只能二选一")
	}
	if user != "" {
		return "USER", user, nil
	}
	if group != "" {
		return "GROUP", group, nil
	}
	return "", "", fmt.Errorf("请使用 --user 或 --group 指定授权对象")
}

func normalizedAuthEnvironment(environment string, profileName string) string {
	value := strings.ToLower(strings.TrimSpace(environment))
	if value == "p" {
		return "pt"
	}
	if value != "" {
		return value
	}
	return profileName
}

func newAuthGrantAFSCmd() *cobra.Command {
	return newAuthGrantResourceCmd("afs", "afs <afs-name>", "给 AFS 授权用户或用户组", "AFS 角色: editor/reader/owner 或完整 role_name")
}

func newAuthGrantResourceCmd(resourceType string, use string, short string, roleHelp string) *cobra.Command {
	var user string
	var group string
	var name string
	var role string
	var scope string
	var dryRun bool
	var yes bool
	var bearerToken string
	var debugAuth bool

	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resourceName := strings.TrimSpace(name)
			if len(args) > 0 {
				if resourceName != "" {
					return fmt.Errorf("资源名参数和 --name 只能二选一")
				}
				resourceName = strings.TrimSpace(args[0])
			}
			if resourceName == "" && strings.TrimSpace(scope) == "" {
				return fmt.Errorf("请指定资源名或 --scope")
			}

			memberType := ""
			memberIdentifier := ""
			if strings.TrimSpace(user) != "" {
				memberType = "USER"
				memberIdentifier = user
			}
			if strings.TrimSpace(group) != "" {
				if memberType != "" {
					return fmt.Errorf("--user 和 --group 只能二选一")
				}
				memberType = "GROUP"
				memberIdentifier = group
			}
			if memberType == "" {
				return fmt.Errorf("请使用 --user 或 --group 指定授权对象")
			}

			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}
			token := ""
			tokenSource := ""
			tokenDebugPrinted := false
			if authResourceScopeRequiresBearer(resourceType) && strings.TrimSpace(scope) == "" {
				var tokenErr error
				token, tokenSource, tokenErr = bearerTokenForAuthGrantCommand(context.Background(), cmd, vcClient, bearerToken)
				if tokenErr != nil {
					return tokenErr
				}
				if debugAuth {
					fmt.Fprintf(cmd.ErrOrStderr(), "auth grant debug: bearer source=%s %s\n", tokenSource, bearerDebugSummary(token))
					tokenDebugPrinted = true
				}
			}

			req := service.AuthGrantAFSRequest{
				ResourceType:     resourceType,
				ResourceName:     resourceName,
				Scope:            scope,
				MemberType:       memberType,
				MemberIdentifier: memberIdentifier,
				Role:             role,
				DryRun:           dryRun,
				BearerToken:      token,
			}

			authService := service.NewAuthService(vcClient)
			preflightReq := req
			preflightReq.DryRun = true
			result, err := authService.GrantAFS(context.Background(), preflightReq)
			if err != nil {
				return err
			}
			if result.Result == "already exists" || dryRun {
				output.PrintAuthGrantAFSResult(result)
				return nil
			}

			if !yes {
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"将给 %s %s 授权 %s %s，角色 %s，是否继续? (y/N): ",
					result.ResourceType,
					firstNonEmptyAuthValue(result.ResourceName, result.AFSName),
					result.MemberType,
					firstNonEmptyAuthValue(result.MemberIdentify, result.MemberName, result.MemberValue),
					result.RoleName,
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

			req.DryRun = false
			if token == "" {
				var tokenErr error
				token, tokenSource, tokenErr = bearerTokenForAuthGrantCommand(context.Background(), cmd, vcClient, bearerToken)
				if tokenErr != nil {
					return tokenErr
				}
			}
			req.BearerToken = token
			if debugAuth && !tokenDebugPrinted {
				fmt.Fprintf(cmd.ErrOrStderr(), "auth grant debug: bearer source=%s %s\n", tokenSource, bearerDebugSummary(token))
			}
			result, err = authService.GrantAFS(context.Background(), req)
			if err != nil {
				return err
			}
			output.PrintAuthGrantAFSResult(result)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "资源名称；也可以直接作为位置参数传入")
	cmd.Flags().StringVarP(&user, "user", "u", "", "授权给用户 username 或 user id")
	cmd.Flags().StringVarP(&group, "group", "g", "", "授权给用户组 name 或 group id")
	cmd.Flags().StringVarP(&role, "role", "r", "", roleHelp)
	cmd.Flags().StringVarP(&scope, "scope", "s", "", "手动指定资源 scope，默认根据资源名自动解析")
	cmd.Flags().StringVarP(&bearerToken, "bearer-token", "t", "", "控制台 Bearer token；也可用 RAYCTL_BEARER_TOKEN")
	cmd.Flags().BoolVar(&debugAuth, "debug-auth", false, "打印脱敏授权调试信息，例如 token 来源")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "只展示将要提交的授权 payload，不真正写入")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "跳过确认直接写入")
	return cmd
}

func newAuthRemoveResourceCmd(resourceType string, use string, short string, roleHelp string) *cobra.Command {
	var user string
	var group string
	var name string
	var role string
	var scope string
	var dryRun bool
	var yes bool
	var bearerToken string
	var debugAuth bool

	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resourceName := strings.TrimSpace(name)
			if len(args) > 0 {
				if resourceName != "" {
					return fmt.Errorf("资源名参数和 --name 只能二选一")
				}
				resourceName = strings.TrimSpace(args[0])
			}
			if resourceName == "" && strings.TrimSpace(scope) == "" {
				return fmt.Errorf("请指定资源名或 --scope")
			}

			memberType := ""
			memberIdentifier := ""
			if strings.TrimSpace(user) != "" {
				memberType = "USER"
				memberIdentifier = user
			}
			if strings.TrimSpace(group) != "" {
				if memberType != "" {
					return fmt.Errorf("--user 和 --group 只能二选一")
				}
				memberType = "GROUP"
				memberIdentifier = group
			}
			if memberType == "" {
				return fmt.Errorf("请使用 --user 或 --group 指定授权对象")
			}

			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}

			token, source, err := bearerTokenForAuthGrantCommand(context.Background(), cmd, vcClient, bearerToken)
			if err != nil {
				return err
			}
			if debugAuth {
				fmt.Fprintf(cmd.ErrOrStderr(), "auth remove debug: bearer source=%s %s\n", source, bearerDebugSummary(token))
			}

			req := service.AuthGrantAFSRequest{
				ResourceType:     resourceType,
				ResourceName:     resourceName,
				Scope:            scope,
				MemberType:       memberType,
				MemberIdentifier: memberIdentifier,
				Role:             role,
				DryRun:           true,
				BearerToken:      token,
			}

			authService := service.NewAuthService(vcClient)
			result, err := authService.RemoveAFS(context.Background(), req)
			if err != nil {
				return err
			}
			if result.Result == "not found" || dryRun {
				output.PrintAuthGrantAFSResult(result)
				return nil
			}

			if !yes {
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"将移除 %s %s 对 %s %s 的角色 %s 授权，policy %s，是否继续? (y/N): ",
					result.ResourceType,
					firstNonEmptyAuthValue(result.ResourceName, result.AFSName),
					result.MemberType,
					firstNonEmptyAuthValue(result.MemberIdentify, result.MemberName, result.MemberValue),
					result.RoleName,
					result.PolicyID,
				)
				line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if err != nil {
					return err
				}
				if !isYes(line) {
					fmt.Fprintln(cmd.OutOrStdout(), "已取消移除授权。")
					return nil
				}
			}

			req.DryRun = false
			result, err = authService.RemoveAFS(context.Background(), req)
			if err != nil {
				return err
			}
			output.PrintAuthGrantAFSResult(result)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "资源名称；也可以直接作为位置参数传入")
	cmd.Flags().StringVarP(&user, "user", "u", "", "移除用户 username 或 user id 的授权")
	cmd.Flags().StringVarP(&group, "group", "g", "", "移除用户组 name 或 group id 的授权")
	cmd.Flags().StringVarP(&role, "role", "r", "", roleHelp)
	cmd.Flags().StringVarP(&scope, "scope", "s", "", "手动指定资源 scope，默认根据资源名自动解析")
	cmd.Flags().StringVarP(&bearerToken, "bearer-token", "t", "", "控制台 Bearer token；也可用 RAYCTL_BEARER_TOKEN")
	cmd.Flags().BoolVar(&debugAuth, "debug-auth", false, "打印脱敏授权调试信息，例如 token 来源")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "只展示将要移除的授权，不真正写入")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "跳过确认直接移除")
	return cmd
}

func newAuthRolesResourceCmd(resourceType string, use string, short string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}

			authService := service.NewAuthService(vcClient)
			result, err := authService.GetResourceRoles(context.Background(), resourceType)
			if err != nil {
				return err
			}
			output.PrintAuthRolesResult(result)
			return nil
		},
	}
	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	var username string
	var tenantCode string
	var environment string
	var debugLogin bool
	var bearerToken string
	var passwordStdin bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "登录控制台并缓存 Bearer token",
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}
			if strings.TrimSpace(environment) != "" {
				if _, err := vcClient.SelectProfileForProcess(environment); err != nil {
					return err
				}
			}
			if strings.TrimSpace(bearerToken) != "" {
				item, err := saveBearerSessionForCommand(cmd, vcClient, username, tenantCode, bearerToken)
				if err != nil {
					return err
				}
				_, expires := authsession.TokenStatus(*item)
				fmt.Fprintf(cmd.OutOrStdout(), "登录成功: profile=%s username=%s expires=%s\n", sessionProfileName(vcClient), firstNonEmptyAuthValue(item.Username), expires)
				return nil
			}
			item, err := loginForCommand(context.Background(), cmd, vcClient, username, tenantCode, debugLogin, passwordStdin)
			if err != nil {
				return err
			}
			_, expires := authsession.TokenStatus(*item)
			fmt.Fprintf(cmd.OutOrStdout(), "登录成功: profile=%s username=%s expires=%s\n", sessionProfileName(vcClient), item.Username, expires)
			return nil
		},
	}
	cmd.Flags().StringVarP(&username, "username", "u", "", "登录账号，默认读取 PJLAB_USERNAME 或交互输入")
	cmd.Flags().StringVar(&tenantCode, "tenant-code", "", "登录租户代码，默认读取 PJLAB_TENANT_CODE 或使用 username")
	cmd.Flags().StringVarP(&environment, "environment", "v", "", "将登录 session 缓存到指定环境: d、pt/p、dcloud；不修改 current_profile")
	cmd.Flags().BoolVar(&debugLogin, "debug-login", false, "打印脱敏登录调试信息，不输出密码或 token")
	cmd.Flags().StringVarP(&bearerToken, "bearer-token", "t", "", "直接缓存控制台 Bearer token，不走账号密码登录")
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "从标准输入读取密码，适合包含特殊字符的密码")
	return cmd
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "删除当前 profile 的登录缓存",
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}
			store, err := authsession.Load("")
			if err != nil {
				return err
			}
			profile := sessionProfileName(vcClient)
			delete(store.Profiles, profile)
			if err := authsession.Save("", store); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "已退出登录: profile=%s\n", profile)
			return nil
		},
	}
}

func newAuthTokenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "token",
		Short: "查看当前 profile 的登录态状态",
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}
			store, err := authsession.Load("")
			if err != nil {
				return err
			}
			profile := sessionProfileName(vcClient)
			item := store.Profiles[profile]
			status, expires := authsession.TokenStatus(item)
			fmt.Fprintf(cmd.OutOrStdout(), "profile=%s username=%s status=%s expires=%s session=%s\n",
				profile,
				firstNonEmptyAuthValue(item.Username),
				status,
				expires,
				authsession.DefaultPath(),
			)
			return nil
		},
	}
}

func runAuthCheckAFS(cmd *cobra.Command, args []string) error {
	return runAuthCheckResources(cmd, "afs", args, "", false)
}

func runAuthCheckResource(cmd *cobra.Command, resourceType string, resourceName string, explicitToken string) error {
	return runAuthCheckResources(cmd, resourceType, []string{resourceName}, explicitToken, false)
}

func runAuthCheckResources(cmd *cobra.Command, resourceType string, resourceNames []string, explicitToken string, long bool) error {
	vcClient, ok := platform.NewVirtualClusterClientFromEnv()
	if !ok {
		return fmt.Errorf("platform client is unavailable, please configure platform.json first")
	}

	bearerToken := ""
	if authResourceScopeRequiresBearer(resourceType) {
		var err error
		bearerToken, _, err = bearerTokenForAuthGrantCommand(context.Background(), cmd, vcClient, explicitToken)
		if err != nil {
			return err
		}
	}

	authService := service.NewAuthService(vcClient)
	type queryResult struct {
		identifier string
		result     *service.AuthAFSResult
		err        error
	}
	queries := runBoundedQueries(cmd.Context(), resourceNames, 4, func(ctx context.Context, identifier string) queryResult {
		result, err := authService.GetResourceAuthWithBearer(ctx, resourceType, identifier, bearerToken)
		return queryResult{identifier, result, err}
	})
	valid := make([]*service.AuthAFSResult, 0, len(queries))
	queryErrors := make([]error, 0)
	for _, query := range queries {
		if query.err != nil {
			queryErrors = append(queryErrors, fmt.Errorf("%s %q: %w", resourceType, query.identifier, query.err))
			continue
		}
		valid = append(valid, query.result)
	}
	if long && len(resourceNames) == 1 && len(valid) == 1 {
		output.PrintAuthAFSResult(valid[0])
	} else {
		output.PrintAuthResourceSummary(valid, len(resourceNames) > 1)
	}
	return errors.Join(queryErrors...)
}

func authResourceScopeRequiresBearer(resourceType string) bool {
	switch strings.ToLower(strings.TrimSpace(resourceType)) {
	case "vpc", "eip", "nat", "dnat", "natgateway", "nat-gateway", "nat_gateway":
		return true
	default:
		return false
	}
}

func runAuthCheckGroups(cmd *cobra.Command, args []string, long bool, environment string) error {
	vcClient, ok := platform.NewVirtualClusterClientFromEnv()
	if !ok {
		return fmt.Errorf("platform client is unavailable, please configure platform.json first")
	}

	authService := service.NewAuthService(vcClient)
	profileName, err := vcClient.ResolveProfileName(environment)
	if err != nil {
		return err
	}
	type queryResult struct {
		identifier string
		results    []*service.AuthGroupResult
		err        error
	}
	queries := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
		results, err := authService.GetGroupsForProfile(ctx, identifier, profileName)
		return queryResult{identifier, results, err}
	})
	valid := make([]*service.AuthGroupResult, 0)
	queryErrors := make([]error, 0)
	for _, query := range queries {
		if query.err != nil {
			queryErrors = append(queryErrors, fmt.Errorf("group %q: %w", query.identifier, query.err))
			continue
		}
		valid = append(valid, query.results...)
	}
	if long && len(args) == 1 {
		for i, result := range valid {
			if i > 0 {
				fmt.Fprintln(cmd.OutOrStdout())
			}
			output.PrintAuthGroupResult(result, true)
		}
	} else {
		output.PrintAuthGroupSummary(valid)
	}
	return errors.Join(queryErrors...)
}

func runAuthCheckUser(cmd *cobra.Command, args []string, long bool, environment string) error {
	vcClient, ok := platform.NewVirtualClusterClientFromEnv()
	if !ok {
		return fmt.Errorf("platform client is unavailable, please configure platform.json first")
	}

	authService := service.NewAuthService(vcClient)
	profileName, err := vcClient.ResolveProfileName(environment)
	if err != nil {
		return err
	}
	type queryResult struct {
		identifier string
		results    []*service.AuthUserResult
		err        error
	}
	queries := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
		results, err := authService.GetUserForProfile(ctx, identifier, profileName)
		return queryResult{identifier, results, err}
	})
	valid := make([]*service.AuthUserResult, 0)
	queryErrors := make([]error, 0)
	for _, query := range queries {
		if query.err != nil {
			queryErrors = append(queryErrors, fmt.Errorf("user %q: %w", query.identifier, query.err))
			continue
		}
		valid = append(valid, query.results...)
	}
	if long && len(args) == 1 {
		for i, result := range valid {
			if i > 0 {
				fmt.Fprintln(cmd.OutOrStdout())
			}
			output.PrintAuthUserResult(result, true)
		}
	} else {
		output.PrintAuthUserSummary(valid)
	}
	return errors.Join(queryErrors...)
}

func bearerTokenForCommand(ctx context.Context, cmd *cobra.Command, vcClient *platform.VirtualClusterClient, explicitToken string) (string, error) {
	if token := firstNonEmptyAuthValueNoDash(explicitToken, rbacBearerToken()); token != "" {
		return strings.TrimPrefix(token, "Bearer "), nil
	}

	store, err := authsession.Load("")
	if err != nil {
		return "", err
	}
	profile := sessionProfileName(vcClient)
	if token, ok := authsession.ValidIDToken(store.Profiles[profile]); ok {
		return token, nil
	}

	item, err := loginForCommand(ctx, cmd, vcClient, "", "", false, false)
	if err != nil {
		return "", err
	}
	token, ok := authsession.ValidIDToken(*item)
	if !ok {
		return "", fmt.Errorf("login succeeded but id_token is not usable")
	}
	return token, nil
}

func bearerTokenForAuthGrantCommand(ctx context.Context, cmd *cobra.Command, vcClient *platform.VirtualClusterClient, explicitToken string) (string, string, error) {
	if token := strings.TrimSpace(explicitToken); token != "" {
		return strings.TrimPrefix(token, "Bearer "), "--bearer-token", nil
	}
	if token := strings.TrimSpace(os.Getenv("RAYCTL_BEARER_TOKEN")); token != "" {
		return strings.TrimPrefix(token, "Bearer "), "RAYCTL_BEARER_TOKEN", nil
	}

	store, err := authsession.Load("")
	if err != nil {
		return "", "", err
	}
	profile := sessionProfileName(vcClient)
	if token, ok := authsession.ValidAccessToken(store.Profiles[profile]); ok {
		return token, "session/access_token", nil
	}

	item, err := loginForCommand(ctx, cmd, vcClient, "", "", false, false)
	if err != nil {
		return "", "", err
	}
	token, ok := authsession.ValidAccessToken(*item)
	if !ok {
		return "", "", fmt.Errorf("login succeeded but token is not usable")
	}
	return token, "login/access_token", nil
}

func bearerTokensForRBACGrantCommand(ctx context.Context, cmd *cobra.Command, vcClient *platform.VirtualClusterClient, explicitComputeToken string) (string, string, string, error) {
	store, err := authsession.Load("")
	if err != nil {
		return "", "", "", err
	}
	profile := sessionProfileName(vcClient)
	item := store.Profiles[profile]
	iamToken, iamOK := authsession.ValidAccessToken(item)
	computeToken, computeOK := authsession.ValidIDToken(item)

	if token := strings.TrimSpace(explicitComputeToken); token != "" {
		computeToken = strings.TrimPrefix(token, "Bearer ")
		computeOK = true
		if iamOK {
			return iamToken, computeToken, "session/access_token + --bearer-token", nil
		}
	}
	if token := strings.TrimSpace(os.Getenv("RAYCTL_BEARER_TOKEN")); token != "" && !computeOK {
		computeToken = strings.TrimPrefix(token, "Bearer ")
		computeOK = true
		if iamOK {
			return iamToken, computeToken, "session/access_token + RAYCTL_BEARER_TOKEN", nil
		}
	}
	if iamOK && computeOK {
		return iamToken, computeToken, "session/access_token + session/id_token", nil
	}

	loggedIn, err := loginForCommand(ctx, cmd, vcClient, "", "", false, false)
	if err != nil {
		return "", "", "", err
	}
	iamToken, iamOK = authsession.ValidAccessToken(*loggedIn)
	computeToken, computeOK = authsession.ValidIDToken(*loggedIn)
	if !iamOK || !computeOK {
		return "", "", "", fmt.Errorf("login succeeded but access_token or id_token is missing")
	}
	return iamToken, computeToken, "login/access_token + login/id_token", nil
}

func bearerTokenForRBACComputeCommand(ctx context.Context, cmd *cobra.Command, vcClient *platform.VirtualClusterClient, explicitToken string) (string, string, error) {
	if token := strings.TrimSpace(explicitToken); token != "" {
		return strings.TrimPrefix(token, "Bearer "), "--bearer-token", nil
	}

	store, err := authsession.Load("")
	if err != nil {
		return "", "", err
	}
	profile := sessionProfileName(vcClient)
	if token, ok := authsession.ValidIDToken(store.Profiles[profile]); ok {
		return token, "session/id_token", nil
	}
	if token := strings.TrimSpace(os.Getenv("RAYCTL_BEARER_TOKEN")); token != "" {
		return strings.TrimPrefix(token, "Bearer "), "RAYCTL_BEARER_TOKEN", nil
	}

	loggedIn, err := loginForCommand(ctx, cmd, vcClient, "", "", false, false)
	if err != nil {
		return "", "", err
	}
	token, ok := authsession.ValidIDToken(*loggedIn)
	if !ok {
		return "", "", fmt.Errorf("login succeeded but id_token is missing")
	}
	return token, "login/id_token", nil
}

func bearerDebugSummary(token string) string {
	token = strings.TrimPrefix(strings.TrimSpace(token), "Bearer ")
	if token == "" {
		return "sha256=- jwt=empty ext=-"
	}
	sum := sha256.Sum256([]byte(token))
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return fmt.Sprintf("sha256=%x jwt=invalid ext=-", sum[:6])
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Sprintf("sha256=%x jwt=payload-decode-failed ext=-", sum[:6])
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return fmt.Sprintf("sha256=%x jwt=payload-json-failed ext=-", sum[:6])
	}
	extValue, ok := claims["ext"]
	if !ok {
		return fmt.Sprintf("sha256=%x jwt=ok ext=missing", sum[:6])
	}
	switch extValue.(type) {
	case map[string]interface{}:
		return fmt.Sprintf("sha256=%x jwt=ok ext=object", sum[:6])
	case string:
		return fmt.Sprintf("sha256=%x jwt=ok ext=string", sum[:6])
	default:
		return fmt.Sprintf("sha256=%x jwt=ok ext=%T", sum[:6], extValue)
	}
}

func saveBearerSessionForCommand(cmd *cobra.Command, vcClient *platform.VirtualClusterClient, username string, tenantCode string, bearerToken string) (*authsession.ProfileSession, error) {
	username = firstNonEmptyAuthValueNoDash(username, os.Getenv("PJLAB_USERNAME"))
	tenantCode = firstNonEmptyAuthValueNoDash(tenantCode, os.Getenv("PJLAB_TENANT_CODE"))
	if tenantCode == "" {
		tenantCode = username
	}
	item := authsession.NewBearerSession(vcClient.CurrentSigninURL(), username, tenantCode, bearerToken)
	if _, ok := authsession.ValidToken(item); !ok {
		return nil, fmt.Errorf("bearer token is empty or expired")
	}
	store, err := authsession.Load("")
	if err != nil {
		return nil, err
	}
	profile := sessionProfileName(vcClient)
	store.Profiles[profile] = item
	if err := authsession.Save("", store); err != nil {
		return nil, err
	}
	return &item, nil
}

func loginForCommand(ctx context.Context, cmd *cobra.Command, vcClient *platform.VirtualClusterClient, username string, tenantCode string, debugLogin bool, passwordStdin bool) (*authsession.ProfileSession, error) {
	username = firstNonEmptyAuthValueNoDash(username, os.Getenv("PJLAB_USERNAME"))
	tenantCode = firstNonEmptyAuthValueNoDash(tenantCode, os.Getenv("PJLAB_TENANT_CODE"))
	password := strings.TrimSpace(os.Getenv("PJLAB_PASSWORD"))
	reader := bufio.NewReader(cmd.InOrStdin())
	if username == "" {
		fmt.Fprint(cmd.OutOrStdout(), "Username: ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		username = strings.TrimSpace(line)
	}
	if password == "" {
		if passwordStdin {
			bytes, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return nil, err
			}
			password = strings.TrimRight(string(bytes), "\r\n")
		} else if !term.IsTerminal(int(os.Stdin.Fd())) {
			return nil, fmt.Errorf("password is required; set PJLAB_PASSWORD or run in an interactive terminal")
		} else {
			fmt.Fprint(cmd.OutOrStdout(), "Password: ")
			bytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(cmd.OutOrStdout())
			if err != nil {
				return nil, err
			}
			password = string(bytes)
		}
	}
	if tenantCode == "" {
		tenantCode = username
	}

	signinURL := vcClient.CurrentSigninURL()
	loginOptions := make([]authsession.LoginOption, 0, 1)
	if debugLogin || isTruthyEnv("RAYCTL_AUTH_DEBUG") {
		loginOptions = append(loginOptions, authsession.WithDebug(cmd.ErrOrStderr()))
	}
	// Account/password login uses the tenant/root-account page by default. That
	// frontend payload intentionally omits tenant_code; tenantCode is kept only
	// as session metadata and for bearer-token cache mode.
	item, err := authsession.Login(ctx, signinURL, vcClient.CurrentIAMBaseURL(), username, password, "", loginOptions...)
	if err != nil {
		return nil, err
	}
	item.TenantCode = tenantCode
	store, err := authsession.Load("")
	if err != nil {
		return nil, err
	}
	profile := sessionProfileName(vcClient)
	store.Profiles[profile] = *item
	if err := authsession.Save("", store); err != nil {
		return nil, err
	}
	return item, nil
}

func isTruthyEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func sessionProfileName(vcClient *platform.VirtualClusterClient) string {
	if vcClient == nil {
		return "default"
	}
	if name := strings.TrimSpace(vcClient.CurrentProfileName()); name != "" {
		return name
	}
	return "default"
}

func firstNonEmptyAuthValue(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return "-"
}

func firstNonEmptyAuthValueNoDash(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
