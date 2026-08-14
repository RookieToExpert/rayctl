# rayctl
强大的 k8s 工具 for 商汤 dcluster
=======

`rayctl` is a custom Kubernetes CLI written in Go for AI infrastructure and cluster management workflows.

## Command Examples

```bash
rayctl node get
rayctl node get ecp
rayctl node get "accelerator=huawei-ascend"
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
rayctl auth check user ug-owner -v pt
rayctl auth check groups ug-a2-jcpt-yunwei -v d
rayctl auth check groups ug-a2-jcpt-yunwei -v dcloud
rayctl rbac get vc-a vc-b
rayctl policy get disallow-privileged-containers vc-a vc-b
rayctl --kubeconfig=/path/to/config node get ecp

# 并行查询多个 ECP/SSP 任务，输出仍按输入顺序展示
rayctl job get job-a job-b job-c
rayctl ecs check ais-a ecs-b ais-c

# 列出当前 profile 下的资源，或按名称/UID 查看单个资源
rayctl vpc get
rayctl vpc get vpc-muxi-ailab
rayctl subnet get
rayctl subnet get subnet-muxi-ailab
rayctl natgw get
rayctl natgw get nat-muxi-ailab
rayctl afs get
rayctl afs get afs-tangrui

# 查看 VC 节点，或按节点名、IP、ACN UID 从 VC 移除节点
rayctl vc get
rayctl vc get vc-a3-llmit
rayctl vc get vc-a3-llmit vc-a3-deeplink vc-c550-jiaofu
rayctl vc get vc-a3-llmit --platform-only
rayctl vc set d
rayctl vc node list vc-c550-ai4s-sys
rayctl vc node usage vc-a3-deeplink
rayctl vc node usage vc-a3-deeplink vc-a3-241ceshi
rayctl vc node remove vc-c550-ai4s-sys 10.12.138.140 --dry-run
rayctl vc node remove vc-c550-ai4s-sys 10.12.138.140 -y

# 通过 PT 平台 API 获取 SSP TrainingJob，并用 PT HC Pod 诊断 Pending 原因
export KUBECONFIG=~/kubeconfigpt
rayctl ssp job get <job-name-or-uid>
rayctl ssp job get <job-name-or-uid> --workspace ws-t-llm-frontier

# 查看 SSP AID 开发机、资源规格、挂载卷、SSH DNAT 和 Pod 状态
rayctl ssp aid get <aid-name-or-uid>
rayctl ssp aid get <aid-name-or-uid> --workspace ws-t-wamcritic

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
