package cmd

import (
	"fmt"

	"rayctl/internal/platform"
)

func requirePlatformClient() (*platform.VirtualClusterClient, error) {
	client, err := platform.NewVirtualClusterClientFromEnvWithError()
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("platform configuration is unavailable; configure ~/.rayctl/platform.json first")
	}
	return client, nil
}

func optionalPlatformClient() (*platform.VirtualClusterClient, error) {
	return platform.NewVirtualClusterClientFromEnvWithError()
}
