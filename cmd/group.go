package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newGroupCmd() *cobra.Command {
	groupCmd := &cobra.Command{Use: "group", Short: "查询用户组成员和协管员"}
	groupCmd.AddCommand(newGroupGetCmd())
	groupCmd.AddCommand(newGroupListCmd())
	return groupCmd
}

func newGroupListCmd() *cobra.Command {
	var limit int
	var long bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出当前 profile 的平台用户组",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if limit < 0 {
				return fmt.Errorf("--limit must be 0 or greater")
			}
			client, err := requirePlatformClient()
			if err != nil {
				return err
			}
			profileName, err := client.ResolveProfileName("")
			if err != nil {
				return err
			}
			items, err := service.NewDirectoryService(client).ListGroups(cmd.Context(), profileName, limit)
			if err != nil {
				return err
			}
			output.PrintGroupList(items, long)
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "最多显示的用户组数；0 表示全部")
	cmd.Flags().BoolVarP(&long, "long", "l", false, "显示组 ID、POSIX、租户 ID 和创建时间")
	return cmd
}

func newGroupGetCmd() *cobra.Command {
	var long bool
	cmd := &cobra.Command{
		Use:   "get <group-name-or-id> [group-name-or-id...]",
		Short: "查看用户组成员和协管员",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, err := requirePlatformClient()
			if err != nil {
				return err
			}
			profileName, err := vcClient.ResolveProfileName("")
			if err != nil {
				return err
			}
			authService := service.NewAuthService(vcClient)
			type queryResult struct {
				identifier string
				results    []*service.GroupGetResult
				err        error
			}
			queries := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				results, err := authService.GetGroupMembersForProfile(ctx, identifier, profileName)
				return queryResult{identifier, results, err}
			})
			valid := make([]*service.GroupGetResult, 0)
			var queryErrors []error
			for _, query := range queries {
				if query.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("group %q: %w", query.identifier, query.err))
					continue
				}
				valid = append(valid, query.results...)
			}
			if long && len(valid) == 1 {
				output.PrintGroupGetResult(valid[0], true)
			} else {
				output.PrintGroupGetSummary(valid, long)
			}
			return errors.Join(queryErrors...)
		},
	}
	cmd.Flags().BoolVarP(&long, "long", "l", false, "仅显示身份锚点及新增 POSIX/租户 ID、成员 ID 和组内关系")
	return cmd
}
