package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newVPCCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "vpc", Short: "查询 VPC 资源"}
	cmd.AddCommand(newVPCGetCmd())
	return cmd
}

func newVPCGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [vpc-name-or-uid]",
		Short: "列出 VPC 或查询单个 VPC 详情",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			networkService, err := newNetworkResourceService()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				result, err := networkService.ListVPCs(cmd.Context())
				if err != nil {
					return err
				}
				output.PrintVPCList(result)
				return nil
			}
			result, err := networkService.GetVPC(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			output.PrintVPCDetail(result)
			return nil
		},
	}
}

func newSubnetCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "subnet", Short: "查询 Subnet 资源"}
	cmd.AddCommand(newSubnetGetCmd())
	return cmd
}

func newSubnetGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [subnet-name-or-uid]",
		Short: "列出 Subnet 或查询单个 Subnet 详情",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			networkService, err := newNetworkResourceService()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				result, err := networkService.ListSubnets(cmd.Context())
				if err != nil {
					return err
				}
				output.PrintSubnetList(result)
				return nil
			}
			result, err := networkService.GetSubnet(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			output.PrintSubnetDetail(result)
			return nil
		},
	}
}

func newNATGatewayCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "natgw", Short: "查询 NAT Gateway 资源"}
	cmd.AddCommand(newNATGatewayGetCmd())
	return cmd
}

func newNATGatewayGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [natgw-name-or-uid]",
		Short: "列出 NAT Gateway 或查询单个 NAT Gateway 详情",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			networkService, err := newNetworkResourceService()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				result, err := networkService.ListNATGateways(cmd.Context())
				if err != nil {
					return err
				}
				output.PrintNATGatewayList(result)
				return nil
			}
			result, err := networkService.GetNATGateway(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			output.PrintNATGatewayDetail(result)
			return nil
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
