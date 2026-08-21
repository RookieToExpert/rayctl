package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newWorkspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workspace",
		Aliases: []string{"ws"},
		Short:   "查询 SSP workspace 及其队列",
	}
	cmd.AddCommand(newWorkspaceGetCmd())
	return cmd
}

func newWorkspaceGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get [workspace-name-or-uid...]",
		Short: "列出 SSP workspace，或查询一个或多个 workspace 详情",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			region, err := selectedSSPRegion()
			if err != nil {
				return err
			}
			resourceService, err := newSSPResourceQueryService()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				result, err := resourceService.ListWorkspaces(cmd.Context(), region)
				if err != nil {
					return err
				}
				output.PrintSSPWorkspaceList(result)
				return nil
			}
			region, err = selectedSSPRegionForLookup()
			if err != nil {
				return err
			}
			type queryResult struct {
				identifier string
				result     *service.SSPWorkspaceItem
				err        error
			}
			results := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) queryResult {
				result, err := resourceService.GetWorkspace(ctx, identifier, region)
				return queryResult{identifier: identifier, result: result, err: err}
			})
			queryErrors := make([]error, 0)
			for _, result := range results {
				if result.err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("workspace %q: %w", result.identifier, result.err))
					continue
				}
				output.PrintSSPWorkspaceDetail(result.result)
			}
			return errors.Join(queryErrors...)
		},
	}
	return cmd
}
