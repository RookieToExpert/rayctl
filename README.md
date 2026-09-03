# rayctl
强大的 k8s 工具 for 商汤 dcluster
=======

`rayctl` is a custom Kubernetes CLI written in Go for AI infrastructure and cluster management workflows.

## Command Examples

```bash
rayctl node list
rayctl node list ecp
rayctl node list "accelerator=huawei-ascend"
rayctl node cordon worker-01
rayctl node uncordon worker-01
rayctl node cordon 10.140.214.222 10.140.214.223
rayctl node uncordon 10.140.214.222 10.140.214.223
rayctl node describe worker-01
rayctl node check worker-01
rayctl node check 10.140.214.222 10.140.214.223

# 多资源查询默认紧凑输出，最多 4 路并发并保持输入顺序
rayctl afs get afs-a afs-b
rayctl afs get afs-a -l
rayctl vpc get vpc-a vpc-b
rayctl subnet get subnet-a subnet-b
rayctl natgw get nat-a nat-b
rayctl vc node list vc-a vc-b
rayctl user get user-a user-b
rayctl auth check user user-a user-b
# 只读查询指定环境的同租户用户/用户组权限，不修改 current_profile
rayctl auth check user ug-owner -e pt
rayctl auth check groups ug-a2-jcpt-yunwei -e d
rayctl auth check groups ug-a2-jcpt-yunwei -e dcloud
rayctl rbac get vc-a vc-b
rayctl policy get disallow-privileged-containers vc-a vc-b
rayctl --kubeconfig=/path/to/config node list ecp

# SSP 是默认任务入口；多任务查询并发执行且保持输入顺序
rayctl job list
rayctl job list -s Running -n 100
rayctl job get job-a job-b job-c
# 旧 ECP VCJob 使用明确的兼容入口
rayctl ecp job list
rayctl ecp job list -s pending
rayctl ecp job get old-job
rayctl ecp job list cluster vc-a3-intern-delivery
rayctl ecs get ais-a ecs-b ais-c

# 列出当前 profile 下的资源，或按名称/UID 查看单个资源
rayctl vpc list
rayctl vpc get vpc-muxi-ailab
rayctl subnet list
rayctl subnet get subnet-muxi-ailab
rayctl natgw list
rayctl natgw get nat-muxi-ailab
rayctl afs list
rayctl afs get afs-tangrui

# 查看 VC 节点，或按节点名、IP、ACN UID 从 VC 移除节点
rayctl vc list
rayctl vc get vc-a3-llmit
rayctl vc get vc-a3-llmit vc-a3-deeplink vc-c550-jiaofu
rayctl vc get vc-a3-llmit --platform-only
rayctl vc set d
rayctl vc node list vc-c550-ai4s-sys
rayctl vc node usage vc-a3-deeplink
rayctl vc node usage vc-a3-deeplink vc-a3-241ceshi
rayctl vc node remove vc-c550-ai4s-sys 10.12.138.140 --dry-run
rayctl vc node remove vc-c550-ai4s-sys 10.12.138.140 -y

# 默认通过平台 API 获取 AIT 训练任务；识别到 PT Pod 时自动使用 PT region
export KUBECONFIG=~/kubeconfig
rayctl job list
rayctl job get <job-name-or-uid>
rayctl job get <job-name-or-uid> --workspace ws-t-llm-frontier

# 查看 AID 开发机、资源规格、挂载卷、SSH DNAT 和 Pod 状态
rayctl aid get <aid-name-or-uid>
rayctl aid get <aid-name-or-uid> --workspace ws-t-wamcritic
rayctl aid list
rayctl aid list -w ws-d-a3-ai4s
rayctl aid list -q queue-d-reserved-a3-ai4s --state Running
rayctl aid list -l
rayctl aid list -A
rayctl ait get <training-job-name-or-uid>
rayctl ait list
rayctl ait list -w ws-d-a3-ai4s --limit 100
rayctl ait list -q queue-d-reserved-a3-ai4s
rayctl ait list -A

# 查看 SSP cluster、workspace、queue 及 queue 绑定节点的实时资源水位
rayctl cluster list
rayctl cluster get cluster-a3
rayctl cluster get cluster-a3 cluster-muxi
rayctl cluster list -e pt
rayctl workspace list
rayctl ws get ws-d-a3-ai4s
rayctl queue list
rayctl queue get queue-d-reserved-a3-ai4s
rayctl queue workload list queue-d-reserved-a3-ai4s
rayctl queue wl list queue-d-reserved-a3-ai4s -t job
rayctl queue workload list queue-d-reserved-a3-ai4s --state Running --priority NORMAL
rayctl queue node list queue-d-reserved-a3-ai4s
rayctl queue node usage queue-d-reserved-a3-ai4s
rayctl queue node usage queue-d-reserved-a3-ai4s --free

# AIR 推理任务和推理网关命令已预留；gateway 可缩写为 gw
rayctl air job get <job-name-or-uid>
rayctl air gateway get <gateway-name-or-uid>
rayctl air gw get <gateway-name-or-uid>

# SSP 工作空间成员权限跟随当前 profile，并使用 auth login 缓存的 Bearer session
rayctl auth ssp check ws-p-jcpt
rayctl auth ssp grant ws-p-jcpt -u autoolchain -r aid-creator,ait-operator --priority HIGH --dry-run
rayctl auth ssp grant ws-p-jcpt -g ug-a2-jcpt-yunwei -r workspace-owner --priority HIGHEST
```

## Project Initialization

```bash
go mod init rayctl
go get github.com/spf13/cobra@latest
go get k8s.io/client-go@latest
go get k8s.io/api@latest
go get k8s.io/apimachinery@latest
go mod tidy
```
