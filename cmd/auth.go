package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newAuthCmd() *cobra.Command {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "查询平台资源授权信息",
	}

	authCmd.AddCommand(newAuthAFSCmd())
	authCmd.AddCommand(newAuthUserCmd())
	return authCmd
}

func newAuthAFSCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "afs <afs-name>",
		Short: "查看 AFS 归属的用户/用户组授权",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}

			authService := service.NewAuthService(vcClient)
			result, err := authService.GetAFS(context.Background(), args[0])
			if err != nil {
				return err
			}
			output.PrintAuthAFSResult(result)
			return nil
		},
	}
}

func newAuthUserCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "user <username-or-userid>",
		Short: "查看用户所属组和权限",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}

			authService := service.NewAuthService(vcClient)
			results, err := authService.GetUser(context.Background(), args[0])
			if err != nil {
				return err
			}
			for i, result := range results {
				if i > 0 {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				output.PrintAuthUserResult(result)
			}
			return nil
		},
	}
}
