package cmd

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"rayctl/internal/kube"
	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newPVCCmd() *cobra.Command {
	pvcCmd := &cobra.Command{
		Use:   "pvc",
		Short: "查询 PVC 与 AFS 的映射关系",
	}

	pvcCmd.AddCommand(newPVCCheckCmd())
	pvcCmd.AddCommand(newPVCCreateCmd())
	return pvcCmd
}

func newPVCCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check <pvc-name> [pvc-name...]",
		Short: "根据 PVC 名称查询对应的 AFS 前端名称",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			storageService := service.NewStorageService(clientset, vcClient)
			for i, identifier := range args {
				result, err := storageService.CheckPVC(context.Background(), identifier)
				if err != nil {
					return fmt.Errorf("pvc %q: %w", identifier, err)
				}
				if i > 0 {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				output.PrintPVCCheckDetail(result)
			}
			return nil
		},
	}

	return cmd
}

func newPVCCreateCmd() *cobra.Command {
	var (
		flagName       string
		flagAFSUUID    string
		flagSecretName string
		flagNamespace  string
		flagSize       string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "通过交互方式创建一个挂载 AFS 的 PVC",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := kube.ResolvedKubeconfigPath(kubeconfig)
			req, interactive, err := resolvePVCCreateRequest(cmd, flagName, flagAFSUUID, flagSecretName, flagNamespace, flagSize)
			if err != nil {
				return err
			}
			if interactive {
				fmt.Fprintf(cmd.OutOrStdout(), "6. 当前 kubeconfig: %s\n", configPath)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "当前 kubeconfig: %s\n", configPath)
			}

			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}
			storageService := service.NewStorageService(clientset, nil)
			created, err := storageService.CreatePVC(context.Background(), req)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "PVC 创建成功: %s/%s\n", created.Namespace, created.Name)
			fmt.Fprintln(cmd.OutOrStdout(), "模板说明: 本命令额外要求填写 afs secretName，因为这是 PVC 真正可用所必须的字段。")
			return nil
		},
	}

	cmd.Flags().StringVar(&flagName, "name", "", "直接指定 pvc 名字；传入该参数后会优先走非交互创建")
	cmd.Flags().StringVar(&flagAFSUUID, "uid", "", "直接指定 afs uuid")
	cmd.Flags().StringVar(&flagSecretName, "secret", "", "直接指定 afs secret 名字")
	cmd.Flags().StringVar(&flagNamespace, "ns", "default", "直接指定创建 namespace")
	cmd.Flags().StringVar(&flagSize, "size", "1000Mi", "直接指定 pvc 大小")
	return cmd
}

func resolvePVCCreateRequest(cmd *cobra.Command, flagName string, flagAFSUUID string, flagSecretName string, flagNamespace string, flagSize string) (service.PVCCreateRequest, bool, error) {
	if strings.TrimSpace(flagName) == "" && strings.TrimSpace(flagAFSUUID) == "" && strings.TrimSpace(flagSecretName) == "" {
		reader := bufio.NewReader(cmd.InOrStdin())

		pvcName, err := promptPVCValue(reader, cmd, "1. 请输入 pvc 名字: ", "")
		if err != nil {
			return service.PVCCreateRequest{}, true, err
		}
		afsUUID, err := promptPVCValue(reader, cmd, "2. 请输入你 afs 的 uuid: ", "")
		if err != nil {
			return service.PVCCreateRequest{}, true, err
		}
		secretName, err := promptPVCValue(reader, cmd, "3. 请输入你 afs 对应的 secret 名字: ", "")
		if err != nil {
			return service.PVCCreateRequest{}, true, err
		}
		namespace, err := promptPVCValue(reader, cmd, "4. 请输入你需要创建到的命名空间(默认为 default): ", "default")
		if err != nil {
			return service.PVCCreateRequest{}, true, err
		}
		size, err := promptPVCValue(reader, cmd, "5. 请输入你需要的大小(默认 1000Mi): ", "1000Mi")
		if err != nil {
			return service.PVCCreateRequest{}, true, err
		}
		confirmed, err := promptPVCValue(reader, cmd, "是否确认创建到当前集群? (y/n): ", "")
		if err != nil {
			return service.PVCCreateRequest{}, true, err
		}
		if !isYes(confirmed) {
			return service.PVCCreateRequest{}, true, fmt.Errorf("已取消创建，请确认 kubeconfig 后重试")
		}
		return normalizedPVCCreateRequest(pvcName, afsUUID, secretName, namespace, size), true, nil
	}

	missing := make([]string, 0, 3)
	if strings.TrimSpace(flagName) == "" {
		missing = append(missing, "--name")
	}
	if strings.TrimSpace(flagAFSUUID) == "" {
		missing = append(missing, "--uid")
	}
	if strings.TrimSpace(flagSecretName) == "" {
		missing = append(missing, "--secret")
	}
	if len(missing) > 0 {
		return service.PVCCreateRequest{}, false, fmt.Errorf("检测到你在用参数模式，请同时补齐这些必填参数: %s", strings.Join(missing, ", "))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "检测到参数模式，已跳过交互确认，可直接用于脚本。\n")
	return normalizedPVCCreateRequest(flagName, flagAFSUUID, flagSecretName, flagNamespace, flagSize), false, nil
}

func normalizedPVCCreateRequest(name string, afsUUID string, secretName string, namespace string, size string) service.PVCCreateRequest {
	name = strings.TrimSpace(name)
	if !strings.HasPrefix(name, "pvc-") {
		name = "pvc-" + name
	}
	return service.PVCCreateRequest{
		Name:       name,
		Namespace:  strings.TrimSpace(namespace),
		AFSUUID:    strings.TrimSpace(afsUUID),
		SecretName: strings.TrimSpace(secretName),
		Size:       strings.TrimSpace(size),
	}
}

func promptPVCValue(reader *bufio.Reader, cmd *cobra.Command, label string, defaultValue string) (string, error) {
	for {
		fmt.Fprint(cmd.OutOrStdout(), label)
		value, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		value = strings.TrimSpace(value)
		if value == "" && defaultValue != "" {
			return defaultValue, nil
		}
		if value != "" {
			return value, nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), "该项不能为空，请重新输入。")
	}
}

func isYes(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
