package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"rayctl/internal/kube"
	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newJobCmd() *cobra.Command {
	jobCmd := &cobra.Command{
		Use:   "job",
		Short: "查询 Volcano Job 和 PodGroup",
	}

	jobCmd.AddCommand(newJobGetCmd())
	jobCmd.AddCommand(newJobCheckCmd())
	jobCmd.AddCommand(newJobCreateCmd())
	return jobCmd
}

func newJobGetCmd() *cobra.Command {
	getCmd := &cobra.Command{
		Use:   "get",
		Short: "查询 job 或 podgroup",
	}

	getCmd.AddCommand(newJobGetJobCmd())
	getCmd.AddCommand(newJobGetPodGroupCmd())
	return getCmd
}

func newJobGetJobCmd() *cobra.Command {
	var debugTiming bool

	cmd := &cobra.Command{
		Use:   "job <job-name-or-pod-name-or-uid> [job-name-or-pod-name-or-uid...]",
		Short: "根据任务名、Pod 名或 UID 查询 Job",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, dynamicClient, err := newJobClients()
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			jobService := service.NewJobService(clientset, dynamicClient, vcClient)
			for i, identifier := range args {
				result, err := jobService.GetJob(context.Background(), identifier)
				if err != nil {
					return fmt.Errorf("job %q: %w", identifier, err)
				}
				if vcClient != nil {
					if vcUID := virtualClusterUIDFromName(result.VClusterName); vcUID != "" {
						displayNames, err := vcClient.ResolveDisplayNames(context.Background(), []string{vcUID})
						if err == nil {
							if displayName, ok := displayNames[vcUID]; ok {
								result.VClusterName = displayName
							}
						}
					}
				}
				if i > 0 {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				output.PrintJobDetail(result, debugTiming)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&debugTiming, "debug-timing", false, "Print timing diagnostics for job get")
	return cmd
}

func virtualClusterUIDFromName(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "vc-") {
		candidate := strings.TrimPrefix(value, "vc-")
		if looksLikeUUID(candidate) {
			return candidate
		}
	}
	if looksLikeUUID(value) {
		return value
	}
	return ""
}

func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, ch := range value {
		switch i {
		case 8, 13, 18, 23:
			if ch != '-' {
				return false
			}
		default:
			if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') && (ch < 'A' || ch > 'F') {
				return false
			}
		}
	}
	return true
}

func newJobGetPodGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "pg <podgroup-name-or-uid> [podgroup-name-or-uid...]",
		Aliases: []string{"podgroup"},
		Short:   "根据名称或 UID 查询 PodGroup",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, dynamicClient, err := newJobClients()
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			jobService := service.NewJobService(clientset, dynamicClient, vcClient)
			for i, identifier := range args {
				result, err := jobService.GetPodGroup(context.Background(), identifier)
				if err != nil {
					return fmt.Errorf("podgroup %q: %w", identifier, err)
				}
				if i > 0 {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				output.PrintPodGroupDetail(result)
			}
			return nil
		},
	}

	return cmd
}

func newJobClients() (kubernetes.Interface, dynamic.Interface, error) {
	clientset, err := kube.NewClientset(kubeconfig)
	if err != nil {
		return nil, nil, err
	}

	dynamicClient, err := kube.NewDynamicClient(kubeconfig)
	if err != nil {
		return nil, nil, err
	}

	return clientset, dynamicClient, nil
}

func newJobCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check <job-name> [job-name...]",
		Short: "检查 Job 为什么起不来",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, dynamicClient, err := newJobClients()
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			jobService := service.NewJobService(clientset, dynamicClient, vcClient)
			for i, identifier := range args {
				result, err := jobService.CheckJob(context.Background(), identifier)
				if err != nil {
					return fmt.Errorf("job %q: %w", identifier, err)
				}
				if i > 0 {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				output.PrintJobCheckDetail(result)
			}
			return nil
		},
	}

	return cmd
}

func newJobCreateCmd() *cobra.Command {
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "创建 Volcano Job",
	}
	createCmd.AddCommand(newJobCreate910CSingleCmd())
	return createCmd
}

func newJobCreate910CSingleCmd() *cobra.Command {
	var (
		whatIf              bool
		flagName            string
		flagNamespace       string
		flagImage           string
		flagCommand         string
		flagImagePullSecret string
		flagCPU             string
		flagMemory          string
		flagAccelerators    string
		flagDataPVC         string
		flagAOSSPVC         string
		flagSHMSize         string
		flagPriorityClass   string
	)

	cmd := &cobra.Command{
		Use:   "910c-single",
		Short: "通过交互方式创建一个 910C 单机 Volcano Job",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeIdentity, err := kube.ResolveKubeconfigIdentity(kubeconfig)
			if err != nil {
				return err
			}

			reader := bufio.NewReader(cmd.InOrStdin())

			jobName, namespace, image, command, imagePullSecret, cpuValue, memoryValue, acceleratorCountValue, dataPVCName, aossPVCName, shmSize, priorityClass, interactive, err := resolveJobCreate910CSingleInputs(
				reader,
				cmd,
				flagName,
				flagNamespace,
				flagImage,
				flagCommand,
				flagImagePullSecret,
				flagCPU,
				flagMemory,
				flagAccelerators,
				flagDataPVC,
				flagAOSSPVC,
				flagSHMSize,
				flagPriorityClass,
			)
			if err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout())
			output.PrintJobCreatePreview([][]string{
				{"当前 kubeconfig", kubeIdentity.Path},
				{"当前 context", kubeIdentity.CurrentContext},
				{"当前 cluster", kubeIdentity.Cluster},
				{"submitter", kubeIdentity.User},
				{"job", strings.TrimSpace(jobName)},
				{"namespace", strings.TrimSpace(namespace)},
				{"image", strings.TrimSpace(image)},
				{"CPU", strconv.Itoa(cpuValue)},
				{"MEMORY", fmt.Sprintf("%dGi", memoryValue)},
				{"加速卡数量", strconv.Itoa(acceleratorCountValue)},
				{"priorityClass", strings.TrimSpace(priorityClass)},
				{"固定值", fmt.Sprintf("sp-block=%d | master port=23456 | replicas=1 | accelerator resource=huawei.com/Ascend910 | machine-type=h2ls.ru.k10 | host-arch=huawei-arm | accelerator-type=module-910c-8 | queue=default | ring-controller.atlas=ascend-910b", acceleratorCountValue)},
			})
			fmt.Fprintln(cmd.OutOrStdout(), "提醒: job create 会直接在当前 kubeconfig 指向的 VC/集群里创建 Volcano Job。")
			if interactive {
				confirmed, err := promptPVCValue(reader, cmd, "是否确认在当前集群创建? (y/n): ", "")
				if err != nil {
					return err
				}
				if !isYes(confirmed) {
					fmt.Fprintln(cmd.OutOrStdout(), "已取消创建。")
					return nil
				}
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "检测到参数模式，已跳过交互确认，可直接用于脚本。")
			}

			clientset, dynamicClient, err := newJobClients()
			if err != nil {
				return err
			}
			jobService := service.NewJobService(clientset, dynamicClient, nil)
			request := service.JobCreateRequest{
				Name:                strings.TrimSpace(jobName),
				Namespace:           strings.TrimSpace(namespace),
				Submitter:           kubeIdentity.User,
				SPBlock:             strconv.Itoa(acceleratorCountValue),
				MasterPort:          "23456",
				Replicas:            1,
				Image:               strings.TrimSpace(image),
				Command:             command,
				ImagePullSecret:     strings.TrimSpace(imagePullSecret),
				CPU:                 strconv.Itoa(cpuValue),
				Memory:              fmt.Sprintf("%dGi", memoryValue),
				AcceleratorResource: "huawei.com/Ascend910",
				AcceleratorCount:    strconv.Itoa(acceleratorCountValue),
				DataPVCName:         strings.TrimSpace(dataPVCName),
				AOSSPVCName:         strings.TrimSpace(aossPVCName),
				SHMSize:             strings.TrimSpace(shmSize),
				MachineType:         "h2ls.ru.k10",
				HostArch:            "huawei-arm",
				AcceleratorType:     "module-910c-8",
				PriorityClass:       strings.TrimSpace(priorityClass),
				Queue:               "default",
			}
			if whatIf {
				manifest, err := jobService.BuildJobManifest(request)
				if err != nil {
					return err
				}
				content, err := yaml.Marshal(manifest.Object)
				if err != nil {
					return fmt.Errorf("marshal job yaml: %w", err)
				}
				outputPath := filepath.Join(".", fmt.Sprintf("%s.yaml", request.Name))
				if err := os.WriteFile(outputPath, content, 0644); err != nil {
					return fmt.Errorf("write job yaml %s: %w", outputPath, err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "what-if 模式：已生成 YAML，未提交到集群: %s\n", outputPath)
				return nil
			}
			created, err := jobService.CreateJob(context.Background(), request)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Job 创建成功: %s/%s\n", created.GetNamespace(), created.GetName())
			return nil
		},
	}

	cmd.Flags().BoolVar(&whatIf, "what-if", false, "只生成最终 YAML，不真正创建 Job")
	cmd.Flags().StringVar(&flagName, "name", "", "直接指定 job 名字")
	cmd.Flags().StringVar(&flagNamespace, "ns", "default", "直接指定 namespace")
	cmd.Flags().StringVar(&flagImage, "image", "", "直接指定完整镜像地址")
	cmd.Flags().StringVar(&flagCommand, "command", "", "直接指定启动命令")
	cmd.Flags().StringVar(&flagImagePullSecret, "secret", "", "直接指定 imagePullSecret；官方镜像可留空")
	cmd.Flags().StringVar(&flagCPU, "cpu", "", "直接指定 CPU，范围 1 到 256")
	cmd.Flags().StringVar(&flagMemory, "memory", "", "直接指定内存数字，默认单位 Gi，范围 1 到 1920")
	cmd.Flags().StringVar(&flagAccelerators, "accelerators", "", "直接指定加速卡数量，偶数，范围 2 到 16")
	cmd.Flags().StringVar(&flagDataPVC, "data-pvc", "", "直接指定文件存储对应的 PVC 名称")
	cmd.Flags().StringVar(&flagAOSSPVC, "aoss-pvc", "", "直接指定对象存储对应的 PVC 名称")
	cmd.Flags().StringVar(&flagSHMSize, "shm-size", "64Gi", "直接指定 shm 大小")
	cmd.Flags().StringVar(&flagPriorityClass, "priority-class", "normal", "直接指定 priorityClass")
	return cmd
}

func resolveJobCreate910CSingleInputs(reader *bufio.Reader, cmd *cobra.Command, flagName string, flagNamespace string, flagImage string, flagCommand string, flagImagePullSecret string, flagCPU string, flagMemory string, flagAccelerators string, flagDataPVC string, flagAOSSPVC string, flagSHMSize string, flagPriorityClass string) (string, string, string, string, string, int, int, int, string, string, string, string, bool, error) {
	if strings.TrimSpace(flagName) == "" && strings.TrimSpace(flagImage) == "" && strings.TrimSpace(flagCommand) == "" && strings.TrimSpace(flagCPU) == "" && strings.TrimSpace(flagMemory) == "" && strings.TrimSpace(flagAccelerators) == "" {
		jobName, err := promptPVCValue(reader, cmd, "1. 请输入 job 名字: ", "")
		if err != nil {
			return "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
		}
		namespace, err := promptPVCValue(reader, cmd, "2. 请输入 namespace(默认为 default): ", "default")
		if err != nil {
			return "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
		}
		image, err := promptPVCValue(reader, cmd, "3. 请输入完整镜像地址(例如 registry2.d.pjlab.org.cn/lepton-trainingjob/a2-cann:8.3.rc2-910b-ubuntu22.04-py3.11): ", "")
		if err != nil {
			return "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
		}
		command, err := promptPVCValue(reader, cmd, "4. 请输入启动命令 command: ", "")
		if err != nil {
			return "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
		}
		imagePullSecret, err := promptPVCValue(reader, cmd, "5. 请输入 imagePullSecret(如果是非官方镜像需要填写，否则直接回车留空): ", "-")
		if err != nil {
			return "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
		}
		if strings.TrimSpace(imagePullSecret) == "-" {
			imagePullSecret = ""
		}
		cpuValue, err := promptIntInRange(reader, cmd, "6. 请输入 CPU(不高于 256): ", 1, 256)
		if err != nil {
			return "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
		}
		memoryValue, err := promptIntInRange(reader, cmd, "7. 请输入内存 MEMORY，默认单位 Gi(不高于 1920): ", 1, 1920)
		if err != nil {
			return "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
		}
		acceleratorCountValue, err := promptEvenIntInRange(reader, cmd, "8. 请输入加速卡数量(仅允许偶数，范围 2 到 16): ", 2, 16)
		if err != nil {
			return "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
		}
		dataPVCName, err := promptPVCValue(reader, cmd, "9. 请输入文件存储对应的 PVC 名称(可留空，直接回车): ", "-")
		if err != nil {
			return "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
		}
		if strings.TrimSpace(dataPVCName) == "-" {
			dataPVCName = ""
		}
		aossPVCName, err := promptPVCValue(reader, cmd, "10. 请输入对象存储对应的 PVC 名称(可留空，直接回车): ", "-")
		if err != nil {
			return "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
		}
		if strings.TrimSpace(aossPVCName) == "-" {
			aossPVCName = ""
		}
		shmSize, err := promptPVCValue(reader, cmd, "11. 请输入 shm 大小(默认 64Gi): ", "64Gi")
		if err != nil {
			return "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
		}
		priorityClass, err := promptPVCValue(reader, cmd, "12. 请输入 priorityClass(默认 normal): ", "normal")
		if err != nil {
			return "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
		}
		return strings.TrimSpace(jobName), strings.TrimSpace(namespace), strings.TrimSpace(image), command, strings.TrimSpace(imagePullSecret), cpuValue, memoryValue, acceleratorCountValue, strings.TrimSpace(dataPVCName), strings.TrimSpace(aossPVCName), strings.TrimSpace(shmSize), strings.TrimSpace(priorityClass), true, nil
	}

	missing := make([]string, 0, 6)
	if strings.TrimSpace(flagName) == "" {
		missing = append(missing, "--name")
	}
	if strings.TrimSpace(flagImage) == "" {
		missing = append(missing, "--image")
	}
	if strings.TrimSpace(flagCommand) == "" {
		missing = append(missing, "--command")
	}
	if strings.TrimSpace(flagCPU) == "" {
		missing = append(missing, "--cpu")
	}
	if strings.TrimSpace(flagMemory) == "" {
		missing = append(missing, "--memory")
	}
	if strings.TrimSpace(flagAccelerators) == "" {
		missing = append(missing, "--accelerators")
	}
	if len(missing) > 0 {
		return "", "", "", "", "", 0, 0, 0, "", "", "", "", false, fmt.Errorf("检测到你在用参数模式，请同时补齐这些必填参数: %s", strings.Join(missing, ", "))
	}

	cpuValue, err := parseIntInRange(flagCPU, 1, 256)
	if err != nil {
		return "", "", "", "", "", 0, 0, 0, "", "", "", "", false, fmt.Errorf("参数 --cpu 非法: %w", err)
	}
	memoryValue, err := parseIntInRange(flagMemory, 1, 1920)
	if err != nil {
		return "", "", "", "", "", 0, 0, 0, "", "", "", "", false, fmt.Errorf("参数 --memory 非法: %w", err)
	}
	acceleratorCountValue, err := parseEvenIntInRange(flagAccelerators, 2, 16)
	if err != nil {
		return "", "", "", "", "", 0, 0, 0, "", "", "", "", false, fmt.Errorf("参数 --accelerators 非法: %w", err)
	}

	return strings.TrimSpace(flagName), strings.TrimSpace(flagNamespace), strings.TrimSpace(flagImage), strings.TrimSpace(flagCommand), strings.TrimSpace(flagImagePullSecret), cpuValue, memoryValue, acceleratorCountValue, strings.TrimSpace(flagDataPVC), strings.TrimSpace(flagAOSSPVC), strings.TrimSpace(flagSHMSize), strings.TrimSpace(flagPriorityClass), false, nil
}

func promptIntInRange(reader *bufio.Reader, cmd *cobra.Command, label string, min int, max int) (int, error) {
	for {
		value, err := promptPVCValue(reader, cmd, label, "")
		if err != nil {
			return 0, err
		}
		number, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil && number >= min && number <= max {
			return number, nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "请输入 %d 到 %d 之间的整数。\n", min, max)
	}
}

func promptEvenIntInRange(reader *bufio.Reader, cmd *cobra.Command, label string, min int, max int) (int, error) {
	for {
		number, err := promptIntInRange(reader, cmd, label, min, max)
		if err != nil {
			return 0, err
		}
		if number%2 == 0 {
			return number, nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), "请输入偶数。")
	}
}

func parseIntInRange(value string, min int, max int) (int, error) {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || number < min || number > max {
		return 0, fmt.Errorf("请输入 %d 到 %d 之间的整数", min, max)
	}
	return number, nil
}

func parseEvenIntInRange(value string, min int, max int) (int, error) {
	number, err := parseIntInRange(value, min, max)
	if err != nil {
		return 0, err
	}
	if number%2 != 0 {
		return 0, fmt.Errorf("请输入偶数")
	}
	return number, nil
}
