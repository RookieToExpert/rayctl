package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"rayctl/internal/kube"
	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

const defaultJobGetTimeout = 10 * time.Second

func newJobCmd() *cobra.Command {
	jobCmd := &cobra.Command{
		Use:   "job",
		Short: "查询 SSP TrainingJob",
	}

	getCmd := newSSPJobGetCmd()
	legacyCluster := newJobGetClusterCmd()
	legacyCluster.Deprecated = "请使用 rayctl ecp job get cluster"
	getCmd.AddCommand(legacyCluster)
	jobCmd.AddCommand(getCmd)

	legacyCreate := newJobCreateCmd()
	legacyCreate.Deprecated = "这是 ECP VCJob 创建入口，请使用 rayctl ecp job create"
	jobCmd.AddCommand(legacyCreate)
	return jobCmd
}

func newECPJobGetCmd() *cobra.Command {
	var debugTiming bool
	var longOutput bool
	var queryTimeout time.Duration

	getCmd := &cobra.Command{
		Use:   "get <job-name-or-pod-name-or-uid> [job-name-or-pod-name-or-uid...]",
		Short: "并行查询一个或多个旧 ECP VCJob，或按 VC 分区列出任务",
		Long: "根据任务名、Pod 名或 UID 并行查询一个或多个旧 ECP VCJob 详情。\n" +
			"也可以使用 cluster 子命令查看指定 VC 分区或当前租户全部 VC 的任务列表；默认只显示 Running 和 Pending 任务。",
		Example: strings.Join([]string{
			"  rayctl ecp job get example-job",
			"  rayctl ecp job get job-a job-b job-c",
			"  rayctl ecp job get cluster vc-a3-intern-delivery",
			"  rayctl ecp job get cluster vc-a3-intern-delivery pending",
			"  rayctl ecp job get cluster vc-a3-intern-delivery --all-status",
			"  rayctl ecp job get cluster -a",
			"  rayctl ecp job get cluster -a pending",
		}, "\n"),
		Args: cobra.MinimumNArgs(1),
		RunE: func(getCmd *cobra.Command, args []string) error {
			clientset, dynamicClient, err := newJobClients()
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			jobService := service.NewJobService(clientset, dynamicClient, vcClient)
			return runParallelECPJobGet(
				getCmd.Context(), args, jobService, vcClient,
				jobGetOptions{
					longOutput:  longOutput,
					debugTiming: debugTiming,
					timeout:     queryTimeout,
				},
			)
		},
	}

	getCmd.Flags().BoolVar(&debugTiming, "debug-timing", false, "Print timing diagnostics for job get")
	getCmd.Flags().BoolVarP(&longOutput, "long", "l", false, "显示 master Pod 的最新日志")
	getCmd.Flags().DurationVar(&queryTimeout, "timeout", defaultJobGetTimeout, "单个任务的查询超时，例如 5s、30s；设为 0 表示不限制")
	getCmd.AddCommand(newJobGetClusterCmd())
	return getCmd
}

func formatJobGetError(ctx context.Context, identifier string, timeout time.Duration, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("job %q 查询超过 %s，已自动停止；可能是平台 API、HC API 或 Pending 诊断请求响应较慢，请检查 kubeconfig 与 platform profile，也可通过 --timeout 临时增加查询时间", identifier, timeout)
	}
	return fmt.Errorf("job %q: %w", identifier, err)
}

func normalizeJobGetIdentifier(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func newJobGetClusterCmd() *cobra.Command {
	var includeInactive bool
	var allVC bool

	cmd := &cobra.Command{
		Use:   "cluster [cluster-name-or-uid] [pending|running|active|all]",
		Short: "查看某个分区下的任务列表",
		Long: "查看指定 VC 分区下的任务列表，默认只显示 Running 和 Pending 任务。\n" +
			"可用 pending、running 或 active 过滤状态；使用 --all-status 显示包括已结束任务在内的全部状态；使用 -a 查询当前租户全部 VC。",
		Example: strings.Join([]string{
			"  rayctl job get cluster vc-a3-intern-delivery",
			"  rayctl job get cluster vc-a3-intern-delivery pending",
			"  rayctl job get cluster vc-a3-intern-delivery --all-status",
			"  rayctl job get cluster -a",
			"  rayctl job get cluster -a running",
		}, "\n"),
		Args: func(cmd *cobra.Command, args []string) error {
			if allVC {
				if len(args) > 1 {
					return fmt.Errorf("at most one status filter is allowed when using -a")
				}
				if len(args) == 1 && !isValidClusterStatusFilter(args[0]) {
					return fmt.Errorf("unsupported status filter %q", args[0])
				}
				return nil
			}
			if len(args) == 0 || len(args) > 2 {
				return fmt.Errorf("expected cluster name with optional status filter")
			}
			if len(args) == 2 && !isValidClusterStatusFilter(args[1]) {
				return fmt.Errorf("unsupported status filter %q", args[1])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			clientset, dynamicClient, err := newJobClients()
			if err != nil {
				return err
			}

			vcClient, _ := platform.NewVirtualClusterClientFromEnv()
			jobService := service.NewJobService(clientset, dynamicClient, vcClient)
			var result *service.JobClusterListResult
			statusFilter := ""
			if allVC {
				if len(args) == 1 {
					statusFilter = args[0]
				}
				result, err = jobService.GetCurrentTenantClusterJobs(context.Background(), includeInactive, statusFilter)
			} else {
				if len(args) == 2 {
					statusFilter = args[1]
				}
				result, err = jobService.GetClusterJobs(context.Background(), args[0], includeInactive, statusFilter)
			}
			if err != nil {
				return err
			}
			output.PrintJobClusterList(result)
			return nil
		},
	}

	cmd.Flags().BoolVar(&includeInactive, "all-status", false, "显示包含非 Running/Pending 在内的全部任务")
	cmd.Flags().BoolVarP(&allVC, "all-vc", "a", false, "查看当前租户下全部分区的任务")
	return cmd
}

func isValidClusterStatusFilter(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pending", "running", "active", "all":
		return true
	default:
		return false
	}
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

func newJobCreateCmd() *cobra.Command {
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "创建 Volcano Job",
	}
	createCmd.AddCommand(newJobCreate910CSingleCmd())
	createCmd.AddCommand(newJobCreate910CMultiCmd())
	createCmd.AddCommand(newJobCreate910BSingleCmd())
	createCmd.AddCommand(newJobCreate910BMultiCmd())
	createCmd.AddCommand(newJobCreateC550DefaultSingleCmd())
	createCmd.AddCommand(newJobCreateC550DefaultMultiCmd())
	createCmd.AddCommand(newJobCreateC550H3CSingleCmd())
	createCmd.AddCommand(newJobCreateC550H3CMultiCmd())
	createCmd.AddCommand(newJobCreateC550SuperpodSingleCmd())
	createCmd.AddCommand(newJobCreateC550SuperpodMultiCmd())
	return createCmd
}

type jobCreateTemplateConfig struct {
	CommandName            string
	ShortDescription       string
	FixedSPBlock           bool
	AcceleratorResource    string
	ExtraResourceName      string
	ExtraResourceValue     string
	MinAccelerators        int
	MaxAccelerators        int
	AcceleratorsEvenOnly   bool
	MaxCPU                 int
	MaxMemoryGi            int
	MachineType            string
	HostArch               string
	AcceleratorType        string
	UseDefaultNodeSelector bool
	UsePCILinkVolume       bool
	DefaultQueue           string
	DefaultPriorityClass   string
}

func newJobCreate910CSingleCmd() *cobra.Command {
	return newSingleTemplateJobCreateCmd(jobCreateTemplateConfig{
		CommandName:            "910c-single",
		ShortDescription:       "通过交互方式创建一个 910C 单机 Volcano Job",
		FixedSPBlock:           false,
		AcceleratorResource:    "huawei.com/Ascend910",
		UseDefaultNodeSelector: true,
		MinAccelerators:        2,
		MaxAccelerators:        16,
		AcceleratorsEvenOnly:   true,
		MaxCPU:                 256,
		MaxMemoryGi:            1920,
		MachineType:            "h2ls.ru.k10",
		HostArch:               "huawei-arm",
		AcceleratorType:        "module-910c-8",
		DefaultQueue:           "default",
		DefaultPriorityClass:   "normal",
	})
}

func newJobCreate910BSingleCmd() *cobra.Command {
	return newSingleTemplateJobCreateCmd(jobCreateTemplateConfig{
		CommandName:            "910b-single",
		ShortDescription:       "通过交互方式创建一个 910B 单机 Volcano Job",
		FixedSPBlock:           true,
		AcceleratorResource:    "huawei.com/Ascend910",
		UseDefaultNodeSelector: true,
		MinAccelerators:        1,
		MaxAccelerators:        8,
		AcceleratorsEvenOnly:   false,
		MaxCPU:                 144,
		MaxMemoryGi:            1920,
		MachineType:            "h1ls.rp.k60a",
		HostArch:               "huawei-arm",
		AcceleratorType:        "module-910b-8",
		DefaultQueue:           "default",
		DefaultPriorityClass:   "normal",
	})
}

func newJobCreate910CMultiCmd() *cobra.Command {
	var (
		whatIf                bool
		flagName              string
		flagNamespace         string
		flagFramework         string
		flagImage             string
		flagCommand           string
		flagImagePullSecret   string
		flagNodes             string
		flagLogicalSupernodes string
		flagDataPVC           string
		flagAOSSPVC           string
		flagSHMSize           string
		flagPriorityClass     string
	)

	cmd := &cobra.Command{
		Use:          "910c-multi",
		Short:        "通过交互方式创建一个 910C 多机 Volcano Job",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeIdentity, err := kube.ResolveKubeconfigIdentity(kubeconfig)
			if err != nil {
				return err
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			jobName, namespace, frameworkType, image, command, imagePullSecret, totalNodes, logicalSupernodes, dataPVCName, aossPVCName, shmSize, priorityClass, interactive, err := resolve910CMultiJobCreateInputs(
				reader,
				cmd,
				flagName,
				flagNamespace,
				flagFramework,
				flagImage,
				flagCommand,
				flagImagePullSecret,
				flagNodes,
				flagLogicalSupernodes,
				flagDataPVC,
				flagAOSSPVC,
				flagSHMSize,
				flagPriorityClass,
			)
			if err != nil {
				return err
			}

			totalCards := totalNodes * 16
			spBlockValue := strconv.Itoa(totalCards / logicalSupernodes)

			fmt.Fprintln(cmd.OutOrStdout())
			fixedValueParts := []string{
				fmt.Sprintf("framework=%s", frameworkType),
				"master=1",
				fmt.Sprintf("worker=%d", totalNodes-1),
				fmt.Sprintf("minAvailable=%d", totalNodes),
				fmt.Sprintf("sp-block=%s", spBlockValue),
				fmt.Sprintf("逻辑超节点个数=%d", logicalSupernodes),
				fmt.Sprintf("逻辑超节点芯片数=%d", totalCards/logicalSupernodes),
				"cpu=256",
				"memory=1920Gi",
				"accelerators=16",
				"accelerator resource=huawei.com/Ascend910",
				"machine-type=h2ls.ru.k10",
				"host-arch=huawei-arm",
				"accelerator-type=module-910c-8",
				"queue=default",
				"ring-controller.atlas=ascend-910b",
			}
			if frameworkType == "MPI" {
				fixedValueParts = append(fixedValueParts, "mpi port=22")
			} else {
				fixedValueParts = append(fixedValueParts, "master port=23456")
			}
			output.PrintJobCreatePreview([][]string{
				{"当前 kubeconfig", kubeIdentity.Path},
				{"当前 context", kubeIdentity.CurrentContext},
				{"当前 cluster", kubeIdentity.Cluster},
				{"submitter", kubeIdentity.User},
				{"job", strings.TrimSpace(jobName)},
				{"namespace", strings.TrimSpace(namespace)},
				{"framework", frameworkType},
				{"机器数", strconv.Itoa(totalNodes)},
				{"逻辑超节点个数", strconv.Itoa(logicalSupernodes)},
				{"sp-block", spBlockValue},
				{"image", strings.TrimSpace(image)},
				{"priorityClass", strings.TrimSpace(priorityClass)},
				{"固定值", strings.Join(fixedValueParts, " | ")},
			})
			fmt.Fprintln(cmd.OutOrStdout(), "提醒: 逻辑超节点芯片数必须是 16 的倍数，因此【逻辑超节点个数】必须整除总机器数。")
			fmt.Fprintln(cmd.OutOrStdout(), "提醒: job create 会直接在当前 kubeconfig 指向的 VC/集群里创建 Volcano Job。")

			writeYAMLOnly := whatIf
			if interactive {
				yamlOnlyAnswer, err := promptPVCValue(reader, cmd, "是否只生成 YAML 文件到本地而不创建任务? (y/n): ", "")
				if err != nil {
					return err
				}
				if isYes(yamlOnlyAnswer) {
					writeYAMLOnly = true
				} else {
					confirmed, err := promptPVCValue(reader, cmd, "是否确认在当前集群创建? (y/n): ", "")
					if err != nil {
						return err
					}
					if !isYes(confirmed) {
						fmt.Fprintln(cmd.OutOrStdout(), "已取消创建。")
						return nil
					}
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
				Name:                   strings.TrimSpace(jobName),
				Namespace:              strings.TrimSpace(namespace),
				Submitter:              kubeIdentity.User,
				SPBlock:                spBlockValue,
				FrameworkType:          frameworkType,
				MasterPort:             "23456",
				Replicas:               1,
				MinAvailable:           int64(totalNodes),
				MasterReplicas:         1,
				WorkerReplicas:         int64(totalNodes - 1),
				Image:                  strings.TrimSpace(image),
				Command:                command,
				ImagePullSecret:        strings.TrimSpace(imagePullSecret),
				CPU:                    "256",
				Memory:                 "1920Gi",
				AcceleratorResource:    "huawei.com/Ascend910",
				AcceleratorCount:       "16",
				DataPVCName:            strings.TrimSpace(dataPVCName),
				AOSSPVCName:            strings.TrimSpace(aossPVCName),
				SHMSize:                strings.TrimSpace(shmSize),
				MachineType:            "h2ls.ru.k10",
				HostArch:               "huawei-arm",
				AcceleratorType:        "module-910c-8",
				UseDefaultNodeSelector: true,
				RequireIPCLock:         true,
				PriorityClass:          strings.TrimSpace(priorityClass),
				Queue:                  "default",
			}
			if writeYAMLOnly {
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
				if whatIf {
					fmt.Fprintf(cmd.OutOrStdout(), "what-if 模式：已生成 YAML，未提交到集群: %s\n", outputPath)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "已生成 YAML，未提交到集群: %s\n", outputPath)
				}
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
	cmd.Flags().StringVar(&flagFramework, "framework", "PyTorch", "直接指定 framework，支持 PyTorch 或 MPI")
	cmd.Flags().StringVar(&flagImage, "image", "", "直接指定完整镜像地址")
	cmd.Flags().StringVar(&flagCommand, "command", "", "直接指定启动命令")
	cmd.Flags().StringVar(&flagImagePullSecret, "secret", "", "直接指定 imagePullSecret；官方镜像可留空")
	cmd.Flags().StringVar(&flagNodes, "nodes", "", "直接指定多机总数量，至少 2")
	cmd.Flags().StringVar(&flagLogicalSupernodes, "logical-supernodes", "1", "直接指定逻辑超节点个数，必须整除总机器数")
	cmd.Flags().StringVar(&flagDataPVC, "data-pvc", "", "直接指定文件存储对应的 PVC 名称")
	cmd.Flags().StringVar(&flagAOSSPVC, "aoss-pvc", "", "直接指定对象存储对应的 PVC 名称")
	cmd.Flags().StringVar(&flagSHMSize, "shm-size", "64Gi", "直接指定 shm 大小")
	cmd.Flags().StringVar(&flagPriorityClass, "priority-class", "normal", "直接指定 priorityClass")
	return cmd
}

func newJobCreate910BMultiCmd() *cobra.Command {
	return newMultiTemplateJobCreateCmd(jobCreateTemplateConfig{
		CommandName:            "910b-multi",
		ShortDescription:       "通过交互方式创建一个 910B 多机 Volcano Job",
		FixedSPBlock:           true,
		AcceleratorResource:    "huawei.com/Ascend910",
		UseDefaultNodeSelector: true,
		MinAccelerators:        1,
		MaxAccelerators:        8,
		AcceleratorsEvenOnly:   false,
		MaxCPU:                 144,
		MaxMemoryGi:            1920,
		MachineType:            "h1ls.rp.k60a",
		HostArch:               "huawei-arm",
		AcceleratorType:        "module-910b-8",
		DefaultQueue:           "default",
		DefaultPriorityClass:   "normal",
	})
}

func newJobCreateC550DefaultSingleCmd() *cobra.Command {
	return newSingleTemplateJobCreateCmd(jobCreateTemplateConfig{
		CommandName:          "c550-default-single",
		ShortDescription:     "通过交互方式创建一个 C550 风冷单机 Volcano Job",
		FixedSPBlock:         true,
		AcceleratorResource:  "metax-tech.com/gpu",
		ExtraResourceName:    "rdma-training/roce",
		ExtraResourceValue:   "1",
		MinAccelerators:      1,
		MaxAccelerators:      8,
		AcceleratorsEvenOnly: false,
		MaxCPU:               224,
		MaxMemoryGi:          1440,
		MachineType:          "x2ls.ri.i80",
		HostArch:             "huawei-arm",
		AcceleratorType:      "module-910c-8",
		UsePCILinkVolume:     true,
		DefaultQueue:         "default",
		DefaultPriorityClass: "normal",
	})
}

func newJobCreateC550DefaultMultiCmd() *cobra.Command {
	return newMultiTemplateJobCreateCmd(jobCreateTemplateConfig{
		CommandName:          "c550-default-multi",
		ShortDescription:     "通过交互方式创建一个 C550 风冷多机 Volcano Job",
		FixedSPBlock:         true,
		AcceleratorResource:  "metax-tech.com/gpu",
		ExtraResourceName:    "rdma-training/roce",
		ExtraResourceValue:   "1",
		MinAccelerators:      1,
		MaxAccelerators:      8,
		AcceleratorsEvenOnly: false,
		MaxCPU:               224,
		MaxMemoryGi:          1440,
		MachineType:          "x2ls.ri.i80",
		HostArch:             "huawei-arm",
		AcceleratorType:      "module-910c-8",
		UsePCILinkVolume:     true,
		DefaultQueue:         "default",
		DefaultPriorityClass: "normal",
	})
}

func newJobCreateC550H3CSingleCmd() *cobra.Command {
	return newSingleTemplateJobCreateCmd(jobCreateTemplateConfig{
		CommandName:          "c550-h3c-single",
		ShortDescription:     "通过交互方式创建一个 C550 液冷单机 Volcano Job",
		FixedSPBlock:         true,
		AcceleratorResource:  "metax-tech.com/gpu",
		ExtraResourceName:    "rdma-training/roce",
		ExtraResourceValue:   "1",
		MinAccelerators:      1,
		MaxAccelerators:      8,
		AcceleratorsEvenOnly: false,
		MaxCPU:               224,
		MaxMemoryGi:          640,
		MachineType:          "x2ls.ri.i70",
		HostArch:             "huawei-arm",
		AcceleratorType:      "module-910c-8",
		UsePCILinkVolume:     false,
		DefaultQueue:         "default",
		DefaultPriorityClass: "normal",
	})
}

func newJobCreateC550H3CMultiCmd() *cobra.Command {
	return newMultiTemplateJobCreateCmd(jobCreateTemplateConfig{
		CommandName:          "c550-h3c-multi",
		ShortDescription:     "通过交互方式创建一个 C550 液冷多机 Volcano Job",
		FixedSPBlock:         true,
		AcceleratorResource:  "metax-tech.com/gpu",
		ExtraResourceName:    "rdma-training/roce",
		ExtraResourceValue:   "1",
		MinAccelerators:      1,
		MaxAccelerators:      8,
		AcceleratorsEvenOnly: false,
		MaxCPU:               224,
		MaxMemoryGi:          640,
		MachineType:          "x2ls.ri.i70",
		HostArch:             "huawei-arm",
		AcceleratorType:      "module-910c-8",
		UsePCILinkVolume:     false,
		DefaultQueue:         "default",
		DefaultPriorityClass: "normal",
	})
}

func newJobCreateC550SuperpodSingleCmd() *cobra.Command {
	return newSingleTemplateJobCreateCmd(jobCreateTemplateConfig{
		CommandName:          "c550-superpod-single",
		ShortDescription:     "通过交互方式创建一个 C550 超节点单机 Volcano Job",
		FixedSPBlock:         true,
		AcceleratorResource:  "metax-tech.com/gpu",
		ExtraResourceName:    "rdma-training/roce",
		ExtraResourceValue:   "1",
		MinAccelerators:      1,
		MaxAccelerators:      8,
		AcceleratorsEvenOnly: false,
		MaxCPU:               120,
		MaxMemoryGi:          1440,
		MachineType:          "x3ls.ri.i80",
		HostArch:             "huawei-arm",
		AcceleratorType:      "module-910c-8",
		UsePCILinkVolume:     false,
		DefaultQueue:         "default",
		DefaultPriorityClass: "normal",
	})
}

func newJobCreateC550SuperpodMultiCmd() *cobra.Command {
	return newMultiTemplateJobCreateCmd(jobCreateTemplateConfig{
		CommandName:          "c550-superpod-multi",
		ShortDescription:     "通过交互方式创建一个 C550 超节点多机 Volcano Job",
		FixedSPBlock:         true,
		AcceleratorResource:  "metax-tech.com/gpu",
		ExtraResourceName:    "rdma-training/roce",
		ExtraResourceValue:   "1",
		MinAccelerators:      1,
		MaxAccelerators:      8,
		AcceleratorsEvenOnly: false,
		MaxCPU:               120,
		MaxMemoryGi:          1440,
		MachineType:          "x3ls.ri.i80",
		HostArch:             "huawei-arm",
		AcceleratorType:      "module-910c-8",
		UsePCILinkVolume:     false,
		DefaultQueue:         "default",
		DefaultPriorityClass: "normal",
	})
}

func newSingleTemplateJobCreateCmd(cfg jobCreateTemplateConfig) *cobra.Command {
	var (
		whatIf              bool
		flagName            string
		flagNamespace       string
		flagFramework       string
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
		Use:          cfg.CommandName,
		Short:        cfg.ShortDescription,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeIdentity, err := kube.ResolveKubeconfigIdentity(kubeconfig)
			if err != nil {
				return err
			}

			reader := bufio.NewReader(cmd.InOrStdin())

			jobName, namespace, frameworkType, image, command, imagePullSecret, cpuValue, memoryValue, acceleratorCountValue, dataPVCName, aossPVCName, shmSize, priorityClass, interactive, err := resolveSingleTemplateJobCreateInputs(
				reader,
				cmd,
				cfg,
				flagName,
				flagNamespace,
				flagFramework,
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

			spBlockValue := ""
			if !cfg.FixedSPBlock {
				spBlockValue = strconv.Itoa(acceleratorCountValue)
			}

			fmt.Fprintln(cmd.OutOrStdout())
			fixedValueParts := []string{
				fmt.Sprintf("framework=%s", frameworkType),
				"master port=23456",
				"replicas=1",
				fmt.Sprintf("accelerator resource=%s", cfg.AcceleratorResource),
				fmt.Sprintf("machine-type=%s", cfg.MachineType),
				fmt.Sprintf("host-arch=%s", cfg.HostArch),
				fmt.Sprintf("accelerator-type=%s", cfg.AcceleratorType),
				fmt.Sprintf("queue=%s", cfg.DefaultQueue),
				"ring-controller.atlas=ascend-910b",
			}
			if spBlockValue != "" {
				fixedValueParts = append([]string{fmt.Sprintf("sp-block=%s", spBlockValue)}, fixedValueParts...)
			} else {
				fixedValueParts = append([]string{"sp-block=不设置"}, fixedValueParts...)
			}
			output.PrintJobCreatePreview([][]string{
				{"当前 kubeconfig", kubeIdentity.Path},
				{"当前 context", kubeIdentity.CurrentContext},
				{"当前 cluster", kubeIdentity.Cluster},
				{"submitter", kubeIdentity.User},
				{"job", strings.TrimSpace(jobName)},
				{"namespace", strings.TrimSpace(namespace)},
				{"framework", frameworkType},
				{"image", strings.TrimSpace(image)},
				{"CPU", strconv.Itoa(cpuValue)},
				{"MEMORY", fmt.Sprintf("%dGi", memoryValue)},
				{"加速卡数量", strconv.Itoa(acceleratorCountValue)},
				{"priorityClass", strings.TrimSpace(priorityClass)},
				{"固定值", strings.Join(fixedValueParts, " | ")},
			})
			fmt.Fprintln(cmd.OutOrStdout(), "提醒: job create 会直接在当前 kubeconfig 指向的 VC/集群里创建 Volcano Job。")
			writeYAMLOnly := whatIf
			if interactive {
				yamlOnlyAnswer, err := promptPVCValue(reader, cmd, "是否只生成 YAML 文件到本地而不创建任务? (y/n): ", "")
				if err != nil {
					return err
				}
				if isYes(yamlOnlyAnswer) {
					writeYAMLOnly = true
				} else {
					confirmed, err := promptPVCValue(reader, cmd, "是否确认在当前集群创建? (y/n): ", "")
					if err != nil {
						return err
					}
					if !isYes(confirmed) {
						fmt.Fprintln(cmd.OutOrStdout(), "已取消创建。")
						return nil
					}
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
				Name:                   strings.TrimSpace(jobName),
				Namespace:              strings.TrimSpace(namespace),
				Submitter:              kubeIdentity.User,
				SPBlock:                spBlockValue,
				FrameworkType:          frameworkType,
				MasterPort:             "23456",
				Replicas:               1,
				Image:                  strings.TrimSpace(image),
				Command:                command,
				ImagePullSecret:        strings.TrimSpace(imagePullSecret),
				CPU:                    strconv.Itoa(cpuValue),
				Memory:                 fmt.Sprintf("%dGi", memoryValue),
				AcceleratorResource:    cfg.AcceleratorResource,
				AcceleratorCount:       strconv.Itoa(acceleratorCountValue),
				ExtraResourceName:      cfg.ExtraResourceName,
				ExtraResourceValue:     cfg.ExtraResourceValue,
				DataPVCName:            strings.TrimSpace(dataPVCName),
				AOSSPVCName:            strings.TrimSpace(aossPVCName),
				SHMSize:                strings.TrimSpace(shmSize),
				MachineType:            cfg.MachineType,
				HostArch:               cfg.HostArch,
				AcceleratorType:        cfg.AcceleratorType,
				UseDefaultNodeSelector: cfg.UseDefaultNodeSelector,
				UsePCILinkVolume:       cfg.UsePCILinkVolume,
				PriorityClass:          strings.TrimSpace(priorityClass),
				Queue:                  cfg.DefaultQueue,
			}
			if writeYAMLOnly {
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
				if whatIf {
					fmt.Fprintf(cmd.OutOrStdout(), "what-if 模式：已生成 YAML，未提交到集群: %s\n", outputPath)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "已生成 YAML，未提交到集群: %s\n", outputPath)
				}
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
	cmd.Flags().StringVar(&flagFramework, "framework", "PyTorch", "直接指定 framework，支持 PyTorch 或 MPI")
	cmd.Flags().StringVar(&flagImage, "image", "", "直接指定完整镜像地址")
	cmd.Flags().StringVar(&flagCommand, "command", "", "直接指定启动命令")
	cmd.Flags().StringVar(&flagImagePullSecret, "secret", "", "直接指定 imagePullSecret；官方镜像可留空")
	cmd.Flags().StringVar(&flagCPU, "cpu", "", fmt.Sprintf("直接指定 CPU，范围 1 到 %d", cfg.MaxCPU))
	cmd.Flags().StringVar(&flagMemory, "memory", "", fmt.Sprintf("直接指定内存数字，默认单位 Gi，范围 1 到 %d", cfg.MaxMemoryGi))
	if cfg.AcceleratorsEvenOnly {
		cmd.Flags().StringVar(&flagAccelerators, "accelerators", "", fmt.Sprintf("直接指定加速卡数量，偶数，范围 %d 到 %d", cfg.MinAccelerators, cfg.MaxAccelerators))
	} else {
		cmd.Flags().StringVar(&flagAccelerators, "accelerators", "", fmt.Sprintf("直接指定加速卡数量，范围 %d 到 %d", cfg.MinAccelerators, cfg.MaxAccelerators))
	}
	cmd.Flags().StringVar(&flagDataPVC, "data-pvc", "", "直接指定文件存储对应的 PVC 名称")
	cmd.Flags().StringVar(&flagAOSSPVC, "aoss-pvc", "", "直接指定对象存储对应的 PVC 名称")
	cmd.Flags().StringVar(&flagSHMSize, "shm-size", "64Gi", "直接指定 shm 大小")
	cmd.Flags().StringVar(&flagPriorityClass, "priority-class", cfg.DefaultPriorityClass, "直接指定 priorityClass")
	return cmd
}

func newMultiTemplateJobCreateCmd(cfg jobCreateTemplateConfig) *cobra.Command {
	var (
		whatIf              bool
		flagName            string
		flagNamespace       string
		flagFramework       string
		flagImage           string
		flagCommand         string
		flagImagePullSecret string
		flagNodes           string
		flagDataPVC         string
		flagAOSSPVC         string
		flagSHMSize         string
		flagPriorityClass   string
	)

	cmd := &cobra.Command{
		Use:          cfg.CommandName,
		Short:        cfg.ShortDescription,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeIdentity, err := kube.ResolveKubeconfigIdentity(kubeconfig)
			if err != nil {
				return err
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			jobName, namespace, frameworkType, image, command, imagePullSecret, totalNodes, dataPVCName, aossPVCName, shmSize, priorityClass, interactive, err := resolveMultiTemplateJobCreateInputs(
				reader,
				cmd,
				cfg,
				flagName,
				flagNamespace,
				flagFramework,
				flagImage,
				flagCommand,
				flagImagePullSecret,
				flagNodes,
				flagDataPVC,
				flagAOSSPVC,
				flagSHMSize,
				flagPriorityClass,
			)
			if err != nil {
				return err
			}

			spBlockValue := ""
			if !cfg.FixedSPBlock {
				spBlockValue = strconv.Itoa(cfg.MaxAccelerators)
			}
			fmt.Fprintln(cmd.OutOrStdout())
			fixedValueParts := []string{
				fmt.Sprintf("framework=%s", frameworkType),
				"master=1",
				fmt.Sprintf("worker=%d", totalNodes-1),
				fmt.Sprintf("minAvailable=%d", totalNodes),
				fmt.Sprintf("cpu=%d", cfg.MaxCPU),
				fmt.Sprintf("memory=%dGi", cfg.MaxMemoryGi),
				fmt.Sprintf("accelerators=%d", cfg.MaxAccelerators),
				fmt.Sprintf("accelerator resource=%s", cfg.AcceleratorResource),
				fmt.Sprintf("machine-type=%s", cfg.MachineType),
				fmt.Sprintf("host-arch=%s", cfg.HostArch),
				fmt.Sprintf("accelerator-type=%s", cfg.AcceleratorType),
				fmt.Sprintf("queue=%s", cfg.DefaultQueue),
				"ring-controller.atlas=ascend-910b",
			}
			if frameworkType == "MPI" {
				fixedValueParts = append(fixedValueParts, "mpi port=22")
			} else {
				fixedValueParts = append(fixedValueParts, "master port=23456")
			}
			if spBlockValue != "" {
				fixedValueParts = append([]string{fmt.Sprintf("sp-block=%s", spBlockValue)}, fixedValueParts...)
			} else {
				fixedValueParts = append([]string{"sp-block=不设置"}, fixedValueParts...)
			}
			output.PrintJobCreatePreview([][]string{
				{"当前 kubeconfig", kubeIdentity.Path},
				{"当前 context", kubeIdentity.CurrentContext},
				{"当前 cluster", kubeIdentity.Cluster},
				{"submitter", kubeIdentity.User},
				{"job", strings.TrimSpace(jobName)},
				{"namespace", strings.TrimSpace(namespace)},
				{"framework", frameworkType},
				{"机器数", strconv.Itoa(totalNodes)},
				{"image", strings.TrimSpace(image)},
				{"priorityClass", strings.TrimSpace(priorityClass)},
				{"固定值", strings.Join(fixedValueParts, " | ")},
			})
			fmt.Fprintln(cmd.OutOrStdout(), "提醒: job create 会直接在当前 kubeconfig 指向的 VC/集群里创建 Volcano Job。")
			writeYAMLOnly := whatIf
			if interactive {
				yamlOnlyAnswer, err := promptPVCValue(reader, cmd, "是否只生成 YAML 文件到本地而不创建任务? (y/n): ", "")
				if err != nil {
					return err
				}
				if isYes(yamlOnlyAnswer) {
					writeYAMLOnly = true
				} else {
					confirmed, err := promptPVCValue(reader, cmd, "是否确认在当前集群创建? (y/n): ", "")
					if err != nil {
						return err
					}
					if !isYes(confirmed) {
						fmt.Fprintln(cmd.OutOrStdout(), "已取消创建。")
						return nil
					}
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
				Name:                   strings.TrimSpace(jobName),
				Namespace:              strings.TrimSpace(namespace),
				Submitter:              kubeIdentity.User,
				SPBlock:                spBlockValue,
				FrameworkType:          frameworkType,
				MasterPort:             "23456",
				Replicas:               1,
				MinAvailable:           int64(totalNodes),
				MasterReplicas:         1,
				WorkerReplicas:         int64(totalNodes - 1),
				Image:                  strings.TrimSpace(image),
				Command:                command,
				ImagePullSecret:        strings.TrimSpace(imagePullSecret),
				CPU:                    strconv.Itoa(cfg.MaxCPU),
				Memory:                 fmt.Sprintf("%dGi", cfg.MaxMemoryGi),
				AcceleratorResource:    cfg.AcceleratorResource,
				AcceleratorCount:       strconv.Itoa(cfg.MaxAccelerators),
				ExtraResourceName:      cfg.ExtraResourceName,
				ExtraResourceValue:     cfg.ExtraResourceValue,
				DataPVCName:            strings.TrimSpace(dataPVCName),
				AOSSPVCName:            strings.TrimSpace(aossPVCName),
				SHMSize:                strings.TrimSpace(shmSize),
				MachineType:            cfg.MachineType,
				HostArch:               cfg.HostArch,
				AcceleratorType:        cfg.AcceleratorType,
				UseDefaultNodeSelector: cfg.UseDefaultNodeSelector,
				UsePCILinkVolume:       cfg.UsePCILinkVolume,
				RequireIPCLock:         true,
				PriorityClass:          strings.TrimSpace(priorityClass),
				Queue:                  cfg.DefaultQueue,
			}
			if writeYAMLOnly {
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
				if whatIf {
					fmt.Fprintf(cmd.OutOrStdout(), "what-if 模式：已生成 YAML，未提交到集群: %s\n", outputPath)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "已生成 YAML，未提交到集群: %s\n", outputPath)
				}
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
	cmd.Flags().StringVar(&flagFramework, "framework", "PyTorch", "直接指定 framework，支持 PyTorch 或 MPI")
	cmd.Flags().StringVar(&flagImage, "image", "", "直接指定完整镜像地址")
	cmd.Flags().StringVar(&flagCommand, "command", "", "直接指定启动命令")
	cmd.Flags().StringVar(&flagImagePullSecret, "secret", "", "直接指定 imagePullSecret；官方镜像可留空")
	cmd.Flags().StringVar(&flagNodes, "nodes", "", "直接指定多机总数量，至少 2")
	cmd.Flags().StringVar(&flagDataPVC, "data-pvc", "", "直接指定文件存储对应的 PVC 名称")
	cmd.Flags().StringVar(&flagAOSSPVC, "aoss-pvc", "", "直接指定对象存储对应的 PVC 名称")
	cmd.Flags().StringVar(&flagSHMSize, "shm-size", "64Gi", "直接指定 shm 大小")
	cmd.Flags().StringVar(&flagPriorityClass, "priority-class", cfg.DefaultPriorityClass, "直接指定 priorityClass")
	return cmd
}

func resolveSingleTemplateJobCreateInputs(reader *bufio.Reader, cmd *cobra.Command, cfg jobCreateTemplateConfig, flagName string, flagNamespace string, flagFramework string, flagImage string, flagCommand string, flagImagePullSecret string, flagCPU string, flagMemory string, flagAccelerators string, flagDataPVC string, flagAOSSPVC string, flagSHMSize string, flagPriorityClass string) (string, string, string, string, string, string, int, int, int, string, string, string, string, bool, error) {
	if strings.TrimSpace(flagName) == "" && strings.TrimSpace(flagImage) == "" && strings.TrimSpace(flagCommand) == "" && strings.TrimSpace(flagCPU) == "" && strings.TrimSpace(flagMemory) == "" && strings.TrimSpace(flagAccelerators) == "" {
		fmt.Fprintln(cmd.OutOrStdout(), "提示: 输入 :b 可返回上一步。")
		var (
			jobName               string
			namespace             = "default"
			frameworkType         = "PyTorch"
			image                 string
			command               string
			imagePullSecret       string
			cpuValue              int
			memoryValue           int
			acceleratorCountValue int
			dataPVCName           string
			aossPVCName           string
			shmSize               = "64Gi"
			priorityClass         = cfg.DefaultPriorityClass
		)
		step := 1
		for {
			switch step {
			case 1:
				value, back, err := promptCreateValue(reader, cmd, "1. 请输入 job 名字: ", jobName)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
				}
				if back {
					fmt.Fprintln(cmd.OutOrStdout(), "已经是第一步了。")
					continue
				}
				jobName = strings.TrimSpace(value)
				step++
			case 2:
				value, back, err := promptCreateValue(reader, cmd, "2. 请输入 namespace(默认为 default): ", namespace)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				namespace = strings.TrimSpace(value)
				step++
			case 3:
				value, back, err := promptFrameworkValueWithBack(reader, cmd, "3. 请输入 framework(默认 PyTorch，可选 PyTorch/MPI): ", frameworkType)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				frameworkType = value
				step++
			case 4:
				value, back, err := promptCreateValue(reader, cmd, "4. 请输入完整镜像地址(例如 registry2.d.pjlab.org.cn/lepton-trainingjob/a2-cann:8.3.rc2-910b-ubuntu22.04-py3.11): ", image)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				image = strings.TrimSpace(value)
				step++
			case 5:
				value, back, err := promptCreateValue(reader, cmd, "5. 请输入启动命令 command: ", command)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				command = value
				step++
			case 6:
				defaultSecret := "-"
				if strings.TrimSpace(imagePullSecret) != "" {
					defaultSecret = imagePullSecret
				}
				value, back, err := promptCreateValue(reader, cmd, "6. 请输入 imagePullSecret(如果是非官方镜像需要填写，否则直接回车留空): ", defaultSecret)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				if strings.TrimSpace(value) == "-" {
					imagePullSecret = ""
				} else {
					imagePullSecret = strings.TrimSpace(value)
				}
				step++
			case 7:
				value, back, err := promptIntInRangeWithBack(reader, cmd, fmt.Sprintf("7. 请输入 CPU(不高于 %d): ", cfg.MaxCPU), 1, cfg.MaxCPU, cpuValue)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				cpuValue = value
				step++
			case 8:
				value, back, err := promptIntInRangeWithBack(reader, cmd, fmt.Sprintf("8. 请输入内存 MEMORY，默认单位 Gi(不高于 %d): ", cfg.MaxMemoryGi), 1, cfg.MaxMemoryGi, memoryValue)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				memoryValue = value
				step++
			case 9:
				acceleratorPrompt := fmt.Sprintf("9. 请输入加速卡数量(范围 %d 到 %d): ", cfg.MinAccelerators, cfg.MaxAccelerators)
				if cfg.AcceleratorsEvenOnly {
					acceleratorPrompt = fmt.Sprintf("9. 请输入加速卡数量(仅允许偶数，范围 %d 到 %d): ", cfg.MinAccelerators, cfg.MaxAccelerators)
				}
				value, back, err := promptAcceleratorCountWithBack(reader, cmd, acceleratorPrompt, cfg.MinAccelerators, cfg.MaxAccelerators, cfg.AcceleratorsEvenOnly, acceleratorCountValue)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				acceleratorCountValue = value
				step++
			case 10:
				defaultPVC := "-"
				if strings.TrimSpace(dataPVCName) != "" {
					defaultPVC = dataPVCName
				}
				value, back, err := promptCreateValue(reader, cmd, "10. 请输入文件存储对应的 PVC 名称(可留空，直接回车): ", defaultPVC)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				if strings.TrimSpace(value) == "-" {
					dataPVCName = ""
				} else {
					dataPVCName = strings.TrimSpace(value)
				}
				step++
			case 11:
				defaultPVC := "-"
				if strings.TrimSpace(aossPVCName) != "" {
					defaultPVC = aossPVCName
				}
				value, back, err := promptCreateValue(reader, cmd, "11. 请输入对象存储对应的 PVC 名称(可留空，直接回车): ", defaultPVC)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				if strings.TrimSpace(value) == "-" {
					aossPVCName = ""
				} else {
					aossPVCName = strings.TrimSpace(value)
				}
				step++
			case 12:
				value, back, err := promptCreateValue(reader, cmd, "12. 请输入 shm 大小(默认 64Gi): ", shmSize)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				shmSize = strings.TrimSpace(value)
				step++
			case 13:
				value, back, err := promptCreateValue(reader, cmd, fmt.Sprintf("13. 请输入 priorityClass(默认 %s): ", cfg.DefaultPriorityClass), priorityClass)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				priorityClass = strings.TrimSpace(value)
				return jobName, namespace, frameworkType, image, command, imagePullSecret, cpuValue, memoryValue, acceleratorCountValue, dataPVCName, aossPVCName, shmSize, priorityClass, true, nil
			}
		}
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
		return "", "", "", "", "", "", 0, 0, 0, "", "", "", "", false, fmt.Errorf("检测到你在用参数模式，请同时补齐这些必填参数: %s", strings.Join(missing, ", "))
	}

	frameworkType, err := parseFrameworkValue(flagFramework)
	if err != nil {
		return "", "", "", "", "", "", 0, 0, 0, "", "", "", "", false, fmt.Errorf("参数 --framework 非法: %w", err)
	}
	cpuValue, err := parseIntInRange(flagCPU, 1, cfg.MaxCPU)
	if err != nil {
		return "", "", "", "", "", "", 0, 0, 0, "", "", "", "", false, fmt.Errorf("参数 --cpu 非法: %w", err)
	}
	memoryValue, err := parseIntInRange(flagMemory, 1, cfg.MaxMemoryGi)
	if err != nil {
		return "", "", "", "", "", "", 0, 0, 0, "", "", "", "", false, fmt.Errorf("参数 --memory 非法: %w", err)
	}
	acceleratorCountValue, err := parseAcceleratorCount(flagAccelerators, cfg.MinAccelerators, cfg.MaxAccelerators, cfg.AcceleratorsEvenOnly)
	if err != nil {
		return "", "", "", "", "", "", 0, 0, 0, "", "", "", "", false, fmt.Errorf("参数 --accelerators 非法: %w", err)
	}

	return strings.TrimSpace(flagName), strings.TrimSpace(flagNamespace), frameworkType, strings.TrimSpace(flagImage), strings.TrimSpace(flagCommand), strings.TrimSpace(flagImagePullSecret), cpuValue, memoryValue, acceleratorCountValue, strings.TrimSpace(flagDataPVC), strings.TrimSpace(flagAOSSPVC), strings.TrimSpace(flagSHMSize), strings.TrimSpace(flagPriorityClass), false, nil
}

func resolveMultiTemplateJobCreateInputs(reader *bufio.Reader, cmd *cobra.Command, cfg jobCreateTemplateConfig, flagName string, flagNamespace string, flagFramework string, flagImage string, flagCommand string, flagImagePullSecret string, flagNodes string, flagDataPVC string, flagAOSSPVC string, flagSHMSize string, flagPriorityClass string) (string, string, string, string, string, string, int, string, string, string, string, bool, error) {
	if strings.TrimSpace(flagName) == "" && strings.TrimSpace(flagImage) == "" && strings.TrimSpace(flagCommand) == "" && strings.TrimSpace(flagNodes) == "" {
		fmt.Fprintln(cmd.OutOrStdout(), "提示: 输入 :b 可返回上一步。")
		var (
			jobName         string
			namespace       = "default"
			frameworkType   = "PyTorch"
			totalNodes      int
			image           string
			command         string
			imagePullSecret string
			dataPVCName     string
			aossPVCName     string
			shmSize         = "64Gi"
			priorityClass   = cfg.DefaultPriorityClass
		)
		step := 1
		for {
			switch step {
			case 1:
				value, back, err := promptCreateValue(reader, cmd, "1. 请输入 job 名字: ", jobName)
				if err != nil {
					return "", "", "", "", "", "", 0, "", "", "", "", true, err
				}
				if back {
					fmt.Fprintln(cmd.OutOrStdout(), "已经是第一步了。")
					continue
				}
				jobName = strings.TrimSpace(value)
				step++
			case 2:
				value, back, err := promptCreateValue(reader, cmd, "2. 请输入 namespace(默认为 default): ", namespace)
				if err != nil {
					return "", "", "", "", "", "", 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				namespace = strings.TrimSpace(value)
				step++
			case 3:
				value, back, err := promptFrameworkValueWithBack(reader, cmd, "3. 请输入 framework(默认 PyTorch，可选 PyTorch/MPI): ", frameworkType)
				if err != nil {
					return "", "", "", "", "", "", 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				frameworkType = value
				step++
			case 4:
				value, back, err := promptIntInRangeWithBack(reader, cmd, "4. 请输入总机器数(至少 2): ", 2, 64, totalNodes)
				if err != nil {
					return "", "", "", "", "", "", 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				totalNodes = value
				step++
			case 5:
				value, back, err := promptCreateValue(reader, cmd, "5. 请输入完整镜像地址: ", image)
				if err != nil {
					return "", "", "", "", "", "", 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				image = strings.TrimSpace(value)
				step++
			case 6:
				value, back, err := promptCreateValue(reader, cmd, "6. 请输入启动命令 command: ", command)
				if err != nil {
					return "", "", "", "", "", "", 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				command = value
				step++
			case 7:
				defaultSecret := "-"
				if strings.TrimSpace(imagePullSecret) != "" {
					defaultSecret = imagePullSecret
				}
				value, back, err := promptCreateValue(reader, cmd, "7. 请输入 imagePullSecret(如果是非官方镜像需要填写，否则直接回车留空): ", defaultSecret)
				if err != nil {
					return "", "", "", "", "", "", 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				if strings.TrimSpace(value) == "-" {
					imagePullSecret = ""
				} else {
					imagePullSecret = strings.TrimSpace(value)
				}
				step++
			case 8:
				defaultPVC := "-"
				if strings.TrimSpace(dataPVCName) != "" {
					defaultPVC = dataPVCName
				}
				value, back, err := promptCreateValue(reader, cmd, "8. 请输入文件存储对应的 PVC 名称(可留空，直接回车): ", defaultPVC)
				if err != nil {
					return "", "", "", "", "", "", 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				if strings.TrimSpace(value) == "-" {
					dataPVCName = ""
				} else {
					dataPVCName = strings.TrimSpace(value)
				}
				step++
			case 9:
				defaultPVC := "-"
				if strings.TrimSpace(aossPVCName) != "" {
					defaultPVC = aossPVCName
				}
				value, back, err := promptCreateValue(reader, cmd, "9. 请输入对象存储对应的 PVC 名称(可留空，直接回车): ", defaultPVC)
				if err != nil {
					return "", "", "", "", "", "", 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				if strings.TrimSpace(value) == "-" {
					aossPVCName = ""
				} else {
					aossPVCName = strings.TrimSpace(value)
				}
				step++
			case 10:
				value, back, err := promptCreateValue(reader, cmd, "10. 请输入 shm 大小(默认 64Gi): ", shmSize)
				if err != nil {
					return "", "", "", "", "", "", 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				shmSize = strings.TrimSpace(value)
				step++
			case 11:
				value, back, err := promptCreateValue(reader, cmd, fmt.Sprintf("11. 请输入 priorityClass(默认 %s): ", cfg.DefaultPriorityClass), priorityClass)
				if err != nil {
					return "", "", "", "", "", "", 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				priorityClass = strings.TrimSpace(value)
				return jobName, namespace, frameworkType, image, command, imagePullSecret, totalNodes, dataPVCName, aossPVCName, shmSize, priorityClass, true, nil
			}
		}
	}

	missing := make([]string, 0, 4)
	if strings.TrimSpace(flagName) == "" {
		missing = append(missing, "--name")
	}
	if strings.TrimSpace(flagImage) == "" {
		missing = append(missing, "--image")
	}
	if strings.TrimSpace(flagCommand) == "" {
		missing = append(missing, "--command")
	}
	if strings.TrimSpace(flagNodes) == "" {
		missing = append(missing, "--nodes")
	}
	if len(missing) > 0 {
		return "", "", "", "", "", "", 0, "", "", "", "", false, fmt.Errorf("检测到你在用参数模式，请同时补齐这些必填参数: %s", strings.Join(missing, ", "))
	}
	frameworkType, err := parseFrameworkValue(flagFramework)
	if err != nil {
		return "", "", "", "", "", "", 0, "", "", "", "", false, fmt.Errorf("参数 --framework 非法: %w", err)
	}
	totalNodes, err := parseIntInRange(flagNodes, 2, 64)
	if err != nil {
		return "", "", "", "", "", "", 0, "", "", "", "", false, fmt.Errorf("参数 --nodes 非法: %w", err)
	}
	return strings.TrimSpace(flagName), strings.TrimSpace(flagNamespace), frameworkType, strings.TrimSpace(flagImage), strings.TrimSpace(flagCommand), strings.TrimSpace(flagImagePullSecret), totalNodes, strings.TrimSpace(flagDataPVC), strings.TrimSpace(flagAOSSPVC), strings.TrimSpace(flagSHMSize), strings.TrimSpace(flagPriorityClass), false, nil
}

func resolve910CMultiJobCreateInputs(reader *bufio.Reader, cmd *cobra.Command, flagName string, flagNamespace string, flagFramework string, flagImage string, flagCommand string, flagImagePullSecret string, flagNodes string, flagLogicalSupernodes string, flagDataPVC string, flagAOSSPVC string, flagSHMSize string, flagPriorityClass string) (string, string, string, string, string, string, int, int, string, string, string, string, bool, error) {
	if strings.TrimSpace(flagName) == "" && strings.TrimSpace(flagImage) == "" && strings.TrimSpace(flagCommand) == "" && strings.TrimSpace(flagNodes) == "" {
		fmt.Fprintln(cmd.OutOrStdout(), "提示: 输入 :b 可返回上一步。")
		var (
			jobName           string
			namespace         = "default"
			frameworkType     = "PyTorch"
			totalNodes        int
			logicalSupernodes = 1
			image             string
			command           string
			imagePullSecret   string
			dataPVCName       string
			aossPVCName       string
			shmSize           = "64Gi"
			priorityClass     = "normal"
		)
		step := 1
		for {
			switch step {
			case 1:
				value, back, err := promptCreateValue(reader, cmd, "1. 请输入 job 名字: ", jobName)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, "", "", "", "", true, err
				}
				if back {
					fmt.Fprintln(cmd.OutOrStdout(), "已经是第一步了。")
					continue
				}
				jobName = strings.TrimSpace(value)
				step++
			case 2:
				value, back, err := promptCreateValue(reader, cmd, "2. 请输入 namespace(默认为 default): ", namespace)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				namespace = strings.TrimSpace(value)
				step++
			case 3:
				value, back, err := promptFrameworkValueWithBack(reader, cmd, "3. 请输入 framework(默认 PyTorch，可选 PyTorch/MPI): ", frameworkType)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				frameworkType = value
				step++
			case 4:
				value, back, err := promptIntInRangeWithBack(reader, cmd, "4. 请输入总机器数(至少 2): ", 2, 64, totalNodes)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				totalNodes = value
				if logicalSupernodes > totalNodes {
					logicalSupernodes = 1
				}
				step++
			case 5:
				value, back, err := prompt910CMultiLogicalSupernodesWithBack(reader, cmd, totalNodes, "5. 请输入逻辑超节点个数(默认 1，必须整除总机器数): ", logicalSupernodes)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				logicalSupernodes = value
				step++
			case 6:
				value, back, err := promptCreateValue(reader, cmd, "6. 请输入完整镜像地址: ", image)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				image = strings.TrimSpace(value)
				step++
			case 7:
				value, back, err := promptCreateValue(reader, cmd, "7. 请输入启动命令 command: ", command)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				command = value
				step++
			case 8:
				defaultSecret := "-"
				if strings.TrimSpace(imagePullSecret) != "" {
					defaultSecret = imagePullSecret
				}
				value, back, err := promptCreateValue(reader, cmd, "8. 请输入 imagePullSecret(如果是非官方镜像需要填写，否则直接回车留空): ", defaultSecret)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				if strings.TrimSpace(value) == "-" {
					imagePullSecret = ""
				} else {
					imagePullSecret = strings.TrimSpace(value)
				}
				step++
			case 9:
				defaultPVC := "-"
				if strings.TrimSpace(dataPVCName) != "" {
					defaultPVC = dataPVCName
				}
				value, back, err := promptCreateValue(reader, cmd, "9. 请输入文件存储对应的 PVC 名称(可留空，直接回车): ", defaultPVC)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				if strings.TrimSpace(value) == "-" {
					dataPVCName = ""
				} else {
					dataPVCName = strings.TrimSpace(value)
				}
				step++
			case 10:
				defaultPVC := "-"
				if strings.TrimSpace(aossPVCName) != "" {
					defaultPVC = aossPVCName
				}
				value, back, err := promptCreateValue(reader, cmd, "10. 请输入对象存储对应的 PVC 名称(可留空，直接回车): ", defaultPVC)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				if strings.TrimSpace(value) == "-" {
					aossPVCName = ""
				} else {
					aossPVCName = strings.TrimSpace(value)
				}
				step++
			case 11:
				value, back, err := promptCreateValue(reader, cmd, "11. 请输入 shm 大小(默认 64Gi): ", shmSize)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				shmSize = strings.TrimSpace(value)
				step++
			case 12:
				value, back, err := promptCreateValue(reader, cmd, "12. 请输入 priorityClass(默认 normal): ", priorityClass)
				if err != nil {
					return "", "", "", "", "", "", 0, 0, "", "", "", "", true, err
				}
				if back {
					step--
					continue
				}
				priorityClass = strings.TrimSpace(value)
				return jobName, namespace, frameworkType, image, command, imagePullSecret, totalNodes, logicalSupernodes, dataPVCName, aossPVCName, shmSize, priorityClass, true, nil
			}
		}
	}

	missing := make([]string, 0, 5)
	if strings.TrimSpace(flagName) == "" {
		missing = append(missing, "--name")
	}
	if strings.TrimSpace(flagImage) == "" {
		missing = append(missing, "--image")
	}
	if strings.TrimSpace(flagCommand) == "" {
		missing = append(missing, "--command")
	}
	if strings.TrimSpace(flagNodes) == "" {
		missing = append(missing, "--nodes")
	}
	if strings.TrimSpace(flagLogicalSupernodes) == "" {
		missing = append(missing, "--logical-supernodes")
	}
	if len(missing) > 0 {
		return "", "", "", "", "", "", 0, 0, "", "", "", "", false, fmt.Errorf("检测到你在用参数模式，请同时补齐这些必填参数: %s", strings.Join(missing, ", "))
	}

	frameworkType, err := parseFrameworkValue(flagFramework)
	if err != nil {
		return "", "", "", "", "", "", 0, 0, "", "", "", "", false, fmt.Errorf("参数 --framework 非法: %w", err)
	}
	totalNodes, err := parseIntInRange(flagNodes, 2, 64)
	if err != nil {
		return "", "", "", "", "", "", 0, 0, "", "", "", "", false, fmt.Errorf("参数 --nodes 非法: %w", err)
	}
	logicalSupernodes, err := parse910CMultiLogicalSupernodes(flagLogicalSupernodes, totalNodes)
	if err != nil {
		return "", "", "", "", "", "", 0, 0, "", "", "", "", false, fmt.Errorf("参数 --logical-supernodes 非法: %w", err)
	}
	return strings.TrimSpace(flagName), strings.TrimSpace(flagNamespace), frameworkType, strings.TrimSpace(flagImage), strings.TrimSpace(flagCommand), strings.TrimSpace(flagImagePullSecret), totalNodes, logicalSupernodes, strings.TrimSpace(flagDataPVC), strings.TrimSpace(flagAOSSPVC), strings.TrimSpace(flagSHMSize), strings.TrimSpace(flagPriorityClass), false, nil
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

func promptIntInRangeWithBack(reader *bufio.Reader, cmd *cobra.Command, label string, min int, max int, defaultValue int) (int, bool, error) {
	defaultText := ""
	if defaultValue > 0 {
		defaultText = strconv.Itoa(defaultValue)
	}
	for {
		value, back, err := promptCreateValue(reader, cmd, label, defaultText)
		if err != nil {
			return 0, false, err
		}
		if back {
			return 0, true, nil
		}
		number, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil && number >= min && number <= max {
			return number, false, nil
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

func promptAcceleratorCount(reader *bufio.Reader, cmd *cobra.Command, label string, min int, max int, evenOnly bool) (int, error) {
	if evenOnly {
		return promptEvenIntInRange(reader, cmd, label, min, max)
	}
	return promptIntInRange(reader, cmd, label, min, max)
}

func promptAcceleratorCountWithBack(reader *bufio.Reader, cmd *cobra.Command, label string, min int, max int, evenOnly bool, defaultValue int) (int, bool, error) {
	for {
		number, back, err := promptIntInRangeWithBack(reader, cmd, label, min, max, defaultValue)
		if err != nil {
			return 0, false, err
		}
		if back {
			return 0, true, nil
		}
		if !evenOnly || number%2 == 0 {
			return number, false, nil
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

func parseAcceleratorCount(value string, min int, max int, evenOnly bool) (int, error) {
	if evenOnly {
		return parseEvenIntInRange(value, min, max)
	}
	return parseIntInRange(value, min, max)
}

func prompt910CMultiLogicalSupernodes(reader *bufio.Reader, cmd *cobra.Command, totalNodes int, label string, defaultValue int) (int, error) {
	defaultText := strconv.Itoa(defaultValue)
	for {
		value, err := promptPVCValue(reader, cmd, label, defaultText)
		if err != nil {
			return 0, err
		}
		number, err := parse910CMultiLogicalSupernodes(value, totalNodes)
		if err == nil {
			return number, nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "逻辑超节点个数必须是正整数，并且能整除总机器数 %d，这样逻辑超节点芯片数才会是 16 的倍数。\n", totalNodes)
	}
}

func prompt910CMultiLogicalSupernodesWithBack(reader *bufio.Reader, cmd *cobra.Command, totalNodes int, label string, defaultValue int) (int, bool, error) {
	defaultText := strconv.Itoa(defaultValue)
	for {
		value, back, err := promptCreateValue(reader, cmd, label, defaultText)
		if err != nil {
			return 0, false, err
		}
		if back {
			return 0, true, nil
		}
		number, err := parse910CMultiLogicalSupernodes(value, totalNodes)
		if err == nil {
			return number, false, nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "逻辑超节点个数必须是正整数，并且能整除总机器数 %d，这样逻辑超节点芯片数才会是 16 的倍数。\n", totalNodes)
	}
}

func parse910CMultiLogicalSupernodes(value string, totalNodes int) (int, error) {
	number, err := parseIntInRange(value, 1, totalNodes)
	if err != nil {
		return 0, err
	}
	if totalNodes%number != 0 {
		return 0, fmt.Errorf("必须整除总机器数 %d", totalNodes)
	}
	return number, nil
}

func promptFrameworkValue(reader *bufio.Reader, cmd *cobra.Command, label string, defaultValue string) (string, error) {
	for {
		value, err := promptPVCValue(reader, cmd, label, defaultValue)
		if err != nil {
			return "", err
		}
		normalized, err := parseFrameworkValue(value)
		if err == nil {
			return normalized, nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), "framework 仅支持 PyTorch 或 MPI。")
	}
}

func promptFrameworkValueWithBack(reader *bufio.Reader, cmd *cobra.Command, label string, defaultValue string) (string, bool, error) {
	for {
		value, back, err := promptCreateValue(reader, cmd, label, defaultValue)
		if err != nil {
			return "", false, err
		}
		if back {
			return "", true, nil
		}
		normalized, err := parseFrameworkValue(value)
		if err == nil {
			return normalized, false, nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), "framework 仅支持 PyTorch 或 MPI。")
	}
}

func parseFrameworkValue(value string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "PYTORCH":
		return "PyTorch", nil
	case "MPI":
		return "MPI", nil
	default:
		return "", fmt.Errorf("仅支持 PyTorch 或 MPI")
	}
}
