package cmd

import (
	"context"
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
		Use:   "get <username-or-userid>",
		Short: "根据 username 或 userid 查询用户信息和所属用户组",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}

			userService := service.NewUserService(vcClient)
			results, err := userService.Get(context.Background(), args[0], includeJobs)
			if err != nil {
				return err
			}
			for i, result := range results {
				if i > 0 {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				output.PrintUserDetail(result)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&includeJobs, "jobs", false, "同时查询该用户在当前租户下提交的活跃任务")
	return cmd
}
