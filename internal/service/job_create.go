package service

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"strings"
)

func (s *JobService) BuildJobManifest(req JobCreateRequest) (*unstructured.Unstructured, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("job name is required")
	}
	namespace := strings.TrimSpace(req.Namespace)
	if namespace == "" {
		namespace = "default"
	}
	submitter := strings.TrimSpace(req.Submitter)
	if submitter == "" {
		return nil, fmt.Errorf("submitter is required")
	}
	spBlock := strings.TrimSpace(req.SPBlock)
	frameworkType := normalizeJobFrameworkType(req.FrameworkType)
	masterPort := strings.TrimSpace(req.MasterPort)
	if masterPort == "" {
		return nil, fmt.Errorf("master port is required")
	}
	if req.Replicas <= 0 {
		return nil, fmt.Errorf("replicas must be greater than 0")
	}
	masterReplicas := req.MasterReplicas
	workerReplicas := req.WorkerReplicas
	minAvailable := req.MinAvailable
	isMultiNode := masterReplicas > 0 || workerReplicas > 0 || minAvailable > 1
	if isMultiNode {
		if masterReplicas <= 0 {
			masterReplicas = 1
		}
		if workerReplicas < 0 {
			return nil, fmt.Errorf("worker replicas must be greater than or equal to 0")
		}
		if minAvailable <= 0 {
			minAvailable = masterReplicas + workerReplicas
		}
		if minAvailable <= 0 {
			return nil, fmt.Errorf("minAvailable must be greater than 0")
		}
	} else {
		masterReplicas = 0
		workerReplicas = req.Replicas
		if minAvailable <= 0 {
			minAvailable = 1
		}
	}
	image := strings.TrimSpace(req.Image)
	if image == "" {
		return nil, fmt.Errorf("image is required")
	}
	command := strings.TrimSpace(req.Command)
	if command == "" {
		return nil, fmt.Errorf("command is required")
	}
	cpu := strings.TrimSpace(req.CPU)
	if cpu == "" {
		return nil, fmt.Errorf("cpu is required")
	}
	memory := strings.TrimSpace(req.Memory)
	if memory == "" {
		return nil, fmt.Errorf("memory is required")
	}
	acceleratorResource := strings.TrimSpace(req.AcceleratorResource)
	if acceleratorResource == "" {
		return nil, fmt.Errorf("accelerator resource is required")
	}
	acceleratorCount := strings.TrimSpace(req.AcceleratorCount)
	if acceleratorCount == "" {
		return nil, fmt.Errorf("accelerator count is required")
	}
	dataPVCName := strings.TrimSpace(req.DataPVCName)
	aossPVCName := strings.TrimSpace(req.AOSSPVCName)
	shmSize := strings.TrimSpace(req.SHMSize)
	if shmSize == "" {
		return nil, fmt.Errorf("shm size is required")
	}
	machineType := strings.TrimSpace(req.MachineType)
	if machineType == "" {
		return nil, fmt.Errorf("machine type is required")
	}
	hostArch := strings.TrimSpace(req.HostArch)
	if hostArch == "" {
		return nil, fmt.Errorf("host arch is required")
	}
	acceleratorType := strings.TrimSpace(req.AcceleratorType)
	if acceleratorType == "" {
		return nil, fmt.Errorf("accelerator type is required")
	}
	priorityClass := strings.TrimSpace(req.PriorityClass)
	if priorityClass == "" {
		priorityClass = "normal"
	}
	queue := strings.TrimSpace(req.Queue)
	if queue == "" {
		queue = "default"
	}

	volumeMounts := make([]interface{}, 0, 3)
	volumes := make([]interface{}, 0, 3)
	if dataPVCName != "" {
		volumeMounts = append(volumeMounts, map[string]interface{}{"name": "data-volume", "mountPath": "/data"})
		volumes = append(volumes, map[string]interface{}{
			"name": "data-volume",
			"persistentVolumeClaim": map[string]interface{}{
				"claimName": dataPVCName,
			},
		})
	}
	if aossPVCName != "" {
		volumeMounts = append(volumeMounts, map[string]interface{}{"name": "aoss-volume", "mountPath": "/mnt/test"})
		volumes = append(volumes, map[string]interface{}{
			"name": "aoss-volume",
			"persistentVolumeClaim": map[string]interface{}{
				"claimName": aossPVCName,
			},
		})
	}
	volumeMounts = append(volumeMounts, map[string]interface{}{"name": "shm-data", "mountPath": "/dev/shm"})
	volumes = append(volumes, map[string]interface{}{
		"name": "shm-data",
		"emptyDir": map[string]interface{}{
			"medium":    "Memory",
			"sizeLimit": shmSize,
		},
	})
	if req.UsePCILinkVolume {
		volumeMounts = append(volumeMounts, map[string]interface{}{"name": "pci-link", "mountPath": "/opt/pci_switch_link"})
		volumes = append(volumes, map[string]interface{}{
			"name": "pci-link",
			"hostPath": map[string]interface{}{
				"path": "/opt/pci_switch_link",
			},
		})
	}

	requests := map[string]interface{}{
		"cpu":               cpu,
		"memory":            memory,
		acceleratorResource: acceleratorCount,
	}
	limits := map[string]interface{}{
		"cpu":               cpu,
		"memory":            memory,
		acceleratorResource: acceleratorCount,
	}
	if extraName := strings.TrimSpace(req.ExtraResourceName); extraName != "" {
		extraValue := strings.TrimSpace(req.ExtraResourceValue)
		if extraValue == "" {
			extraValue = "1"
		}
		requests[extraName] = extraValue
		limits[extraName] = extraValue
	}

	buildTaskSpec := func(taskName string, replicas int64, includeCompletePolicy bool) map[string]interface{} {
		container := map[string]interface{}{
			"name":            taskName,
			"image":           image,
			"imagePullPolicy": "IfNotPresent",
			"command":         []interface{}{"bash", "-c"},
			"args":            []interface{}{command},
			"env":             []interface{}{},
			"volumeMounts":    volumeMounts,
			"resources": map[string]interface{}{
				"requests": requests,
				"limits":   limits,
			},
		}
		if req.RequireIPCLock {
			container["securityContext"] = map[string]interface{}{
				"capabilities": map[string]interface{}{
					"add": []interface{}{"IPC_LOCK"},
				},
			}
		}
		spec := map[string]interface{}{
			"containers":    []interface{}{container},
			"restartPolicy": "Never",
			"volumes":       volumes,
			"affinity": map[string]interface{}{
				"nodeAffinity": map[string]interface{}{
					"requiredDuringSchedulingIgnoredDuringExecution": map[string]interface{}{
						"nodeSelectorTerms": []interface{}{
							map[string]interface{}{
								"matchExpressions": []interface{}{
									map[string]interface{}{
										"key":      "resource.compute.sensecore.cn/machine-type",
										"operator": "In",
										"values":   []interface{}{machineType},
									},
								},
							},
						},
					},
				},
			},
		}
		if req.UseDefaultNodeSelector {
			spec["nodeSelector"] = map[string]interface{}{
				"host-arch":        hostArch,
				"accelerator-type": acceleratorType,
			}
		}
		if secretName := strings.TrimSpace(req.ImagePullSecret); secretName != "" {
			spec["imagePullSecrets"] = []interface{}{
				map[string]interface{}{"name": secretName},
			}
		} else {
			spec["imagePullSecrets"] = nil
		}
		policies := []interface{}{
			map[string]interface{}{
				"event":  "PodEvicted",
				"action": "RestartJob",
			},
		}
		if includeCompletePolicy {
			policies = append(policies, map[string]interface{}{
				"event":  "TaskCompleted",
				"action": "CompleteJob",
			})
		}
		return map[string]interface{}{
			"replicas": replicas,
			"name":     taskName,
			"policies": policies,
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"lepton.sensetime.com/submitter":      submitter,
						"lepton.sensetime.com/framework-type": frameworkType,
						"ring-controller.atlas":               "ascend-910b",
					},
				},
				"spec": spec,
			},
		}
	}

	topPolicies := []interface{}{
		map[string]interface{}{
			"event":  "PodEvicted",
			"action": "RestartJob",
		},
	}
	plugins := map[string]interface{}{
		"svc": []interface{}{},
	}
	tasks := make([]interface{}, 0, 2)
	if isMultiNode {
		tasks = append(tasks, buildTaskSpec("master", masterReplicas, frameworkType == "MPI"))
		if workerReplicas > 0 {
			tasks = append(tasks, buildTaskSpec("worker", workerReplicas, false))
		}
	} else {
		taskName := "worker"
		if frameworkType == "MPI" {
			taskName = "master"
		}
		tasks = append(tasks, buildTaskSpec(taskName, workerReplicas, frameworkType == "MPI"))
	}
	if frameworkType == "MPI" {
		plugins["ssh"] = []interface{}{}
		if isMultiNode {
			plugins["mpi"] = []interface{}{
				"--master=master",
				"--worker=worker",
				"--port=22",
			}
			plugins["hcclrank"] = []interface{}{}
		}
	} else {
		plugins["pytorch"] = []interface{}{
			"--master=master",
			"--worker=worker",
			fmt.Sprintf("--port=%s", masterPort),
		}
		plugins["hcclrank"] = []interface{}{}
	}

	metadataAnnotations := map[string]interface{}{}
	if spBlock != "" {
		metadataAnnotations["sp-block"] = spBlock
	}

	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "batch.volcano.sh/v1alpha1",
			"kind":       "Job",
			"metadata": map[string]interface{}{
				"name":         name,
				"generateName": "vcjob-",
				"namespace":    namespace,
				"labels": map[string]interface{}{
					"lepton.sensetime.com/submitter":      submitter,
					"lepton.sensetime.com/framework-type": frameworkType,
					"ring-controller.atlas":               "ascend-910b",
				},
				"annotations": metadataAnnotations,
			},
			"spec": map[string]interface{}{
				"minAvailable":      minAvailable,
				"plugins":           plugins,
				"maxRetry":          1,
				"tasks":             tasks,
				"priorityClassName": priorityClass,
				"queue":             queue,
				"schedulerName":     "volcano",
				"policies":          topPolicies,
			},
		},
	}

	return job, nil
}

func (s *JobService) CreateJob(ctx context.Context, req JobCreateRequest) (*unstructured.Unstructured, error) {
	job, err := s.BuildJobManifest(req)
	if err != nil {
		return nil, err
	}

	namespace := strings.TrimSpace(req.Namespace)
	if namespace == "" {
		namespace = "default"
	}
	created, err := s.dynamicClient.Resource(volcanoJobGVR).Namespace(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create volcano job %s/%s: %w", namespace, job.GetName(), err)
	}
	return created, nil
}

func normalizeJobFrameworkType(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "MPI":
		return "MPI"
	default:
		return "PyTorch"
	}
}
