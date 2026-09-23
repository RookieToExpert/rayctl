package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	var includeAID bool
	var includeAIT bool
	var includeATE bool
	var includeAIR bool
	var includeAll bool

	cmd := &cobra.Command{
		Use:   "get <username-or-userid> [username-or-userid...]",
		Short: "并行查询一个或多个用户的信息和所属用户组",
		Example: strings.Join([]string{
			"  rayctl user get zhangsan --jobs",
			"  rayctl user get zhangsan --aid",
			"  rayctl user get zhangsan --ait --ate",
			"  rayctl user get zhangsan --all",
		}, "\n"),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vcClient, ok := platform.NewVirtualClusterClientFromEnv()
			if !ok {
				return fmt.Errorf("platform client is unavailable, please configure platform.json first")
			}

			userService := service.NewUserService(vcClient)
			workloadOptions := service.UserWorkloadOptions{
				AID:                includeAID || includeAll,
				AIT:                includeAIT || includeAll,
				ATE:                includeATE || includeAll || includeJobs,
				AIR:                includeAIR || includeAll,
				IncludeInactiveATE: includeATE || includeAll,
			}
			useWorkloadView := includeAID || includeAIT || includeATE || includeAIR || includeAll
			type queryResult struct {
				identifier string
				results    []*service.UserGetResult
				err        error
			}
			queries := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				var results []*service.UserGetResult
				var err error
				if useWorkloadView {
					results, err = userService.GetWithWorkloads(ctx, identifier, workloadOptions)
				} else {
					results, err = userService.Get(ctx, identifier, includeJobs)
				}
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
				output.PrintUserSummary(valid, includeJobs || useWorkloadView)
			}
			return errors.Join(queryErrors...)
		},
	}

	cmd.Flags().BoolVar(&includeJobs, "jobs", false, "同时查询该用户在当前租户下提交的活跃 ATE（兼容旧用法）")
	cmd.Flags().BoolVar(&includeAID, "aid", false, "同时查询该用户提交的 AID 开发机")
	cmd.Flags().BoolVar(&includeAIT, "ait", false, "同时查询该用户提交的 AIT 训练任务")
	cmd.Flags().BoolVar(&includeATE, "ate", false, "同时查询该用户提交的 ATE（包含当前可查询的全部状态）")
	cmd.Flags().BoolVar(&includeAIR, "air", false, "同时查询该用户提交的 AIR 推理任务")
	cmd.Flags().BoolVar(&includeAll, "all", false, "同时查询该用户提交的 AID、AIT、ATE 和 AIR")
	return cmd
}
