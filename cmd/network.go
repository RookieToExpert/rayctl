package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newVPCCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "vpc", Short: "查询 VPC 资源"}
	cmd.AddCommand(newVPCListCmd())
	cmd.AddCommand(newVPCGetCmd())
	return cmd
}

func newVPCListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出 VPC 资源",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			networkService, err := newNetworkResourceService()
			if err != nil {
				return err
			}
			result, err := networkService.ListVPCs(cmd.Context())
			if err != nil {
				return err
			}
			output.PrintVPCList(result)
			return nil
		},
	}
}

func newVPCGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <vpc-name-or-uid> [vpc-name-or-uid...]",
		Short: "批量查询一个或多个 VPC",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			networkService, err := newNetworkResourceService()
			if err != nil {
				return err
			}
			results := networkService.GetVPCMany(cmd.Context(), args)
			items := make([]service.VPCListItem, 0, len(results))
			queryErrors := make([]error, 0)
			for _, result := range results {
				if result.Err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("vpc %q: %w", result.Identifier, result.Err))
					continue
				}
				items = append(items, *result.Item)
			}
			if len(args) == 1 && len(items) == 1 {
				output.PrintVPCDetail(&items[0])
			} else {
				output.PrintVPCList(&service.VPCListResult{Items: items})
			}
			return errors.Join(queryErrors...)
		},
	}
}

func newSubnetCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "subnet", Short: "查询 Subnet 资源"}
	cmd.AddCommand(newSubnetListCmd())
	cmd.AddCommand(newSubnetGetCmd())
	return cmd
}

func newSubnetListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出 Subnet 资源",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			networkService, err := newNetworkResourceService()
			if err != nil {
				return err
			}
			result, err := networkService.ListSubnets(cmd.Context())
			if err != nil {
				return err
			}
			output.PrintSubnetList(result)
			return nil
		},
	}
}

func newSubnetGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <subnet-name-or-uid> [subnet-name-or-uid...]",
		Short: "批量查询一个或多个 Subnet",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			networkService, err := newNetworkResourceService()
			if err != nil {
				return err
			}
			results := networkService.GetSubnetMany(cmd.Context(), args)
			items := make([]service.SubnetListItem, 0, len(results))
			queryErrors := make([]error, 0)
			for _, result := range results {
				if result.Err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("subnet %q: %w", result.Identifier, result.Err))
					continue
				}
				items = append(items, *result.Item)
			}
			if len(args) == 1 && len(items) == 1 {
				output.PrintSubnetDetail(&items[0])
			} else {
				output.PrintSubnetList(&service.SubnetListResult{Items: items})
			}
			return errors.Join(queryErrors...)
		},
	}
}

func newNATGatewayCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "natgw", Short: "查询 NAT Gateway 资源"}
	cmd.AddCommand(newNATGatewayListCmd())
	cmd.AddCommand(newNATGatewayGetCmd())
	return cmd
}

func newNATGatewayListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出 NAT Gateway 资源",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			networkService, err := newNetworkResourceService()
			if err != nil {
				return err
			}
			result, err := networkService.ListNATGateways(cmd.Context())
			if err != nil {
				return err
			}
			output.PrintNATGatewayList(result)
			return nil
		},
	}
}

func newNATGatewayGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <natgw-name-or-uid> [natgw-name-or-uid...]",
		Short: "批量查询一个或多个 NAT Gateway",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			networkService, err := newNetworkResourceService()
			if err != nil {
				return err
			}
			results := networkService.GetNATGatewayMany(cmd.Context(), args)
			items := make([]service.NATGatewayListItem, 0, len(results))
			queryErrors := make([]error, 0)
			for _, result := range results {
				if result.Err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("natgw %q: %w", result.Identifier, result.Err))
					continue
				}
				items = append(items, *result.Item)
			}
			if len(args) == 1 && len(items) == 1 {
				output.PrintNATGatewayDetail(&items[0])
			} else {
				output.PrintNATGatewayList(&service.NATGatewayListResult{Items: items})
			}
			return errors.Join(queryErrors...)
		},
	}
}

func newNetworkResourceService() (*service.NetworkResourceService, error) {
	client, ok := platform.NewVirtualClusterClientFromEnv()
	if !ok {
		return nil, fmt.Errorf("platform client is unavailable, please configure platform.json first")
	}
	return service.NewNetworkResourceService(client), nil
}
