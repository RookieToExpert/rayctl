package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newUserCmd() *cobra.Command {
	userCmd := &cobra.Command{
		Use:   "user",
		Short: "查询平台用户信息",
	}

	userCmd.AddCommand(newUserGetCmd())
	return userCmd
}

func newUserGetCmd() *cobra.Command {
	var includeJobs bool

	cmd := &cobra.Command{
		Use:   "get <username-or-userid> [username-or-userid...]",
		Short: "并行查询一个或多个用户的信息和所属用户组",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}

			userService := service.NewUserService(vcClient)
			type queryResult struct {
				identifier string
				results    []*service.UserGetResult
				err        error
			}
			queries := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				results, err := userService.Get(ctx, identifier, includeJobs)
				return queryResult{identifier, results, err}
			})
			valid := make([]*service.UserGetResult, 0)
			queryErrors := make([]error, 0)
			for _, query := range queries {
				if query.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("user %q: %w", query.identifier, query.err))
					continue
				}
				valid = append(valid, query.results...)
			}
			if len(args) == 1 && len(valid) == 1 {
				output.PrintUserDetail(valid[0])
			} else {
				output.PrintUserSummary(valid, includeJobs)
			}
			return errors.Join(queryErrors...)
		},
	}

	cmd.Flags().BoolVar(&includeJobs, "jobs", false, "同时查询该用户在当前租户下提交的活跃任务")
	return cmd
}
