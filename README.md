# rayctl

`get -l` 使用增量详情：先显示名称、状态、HC namespace、节点的身份锚点，再显示默认视图没有的字段与诊断；多对象分别显示锚点。`list -l` 继续增加列。`-o json` 保留完整的已采集对象，不应用表格去重或隐藏卷的规则。标识字段完整保留；AIR list 默认显示 NAME/STATE/READY/WORKSPACE，`READY 1/1 ×2` 表示就绪组/总组 × 每组实例数，`-n` 默认 50，`-l` 增加队列和创建信息，CREATED 保留完整年月日和时分秒。

带 `-n/--limit` 且不接收位置参数的列表，JSON `summary.requested` 表示实际请求的条数上限：例如 `ait list -n 3`、`air job list -n 3` 为 3，默认普通列表为 50；AIT 的时间窗口扫描未显式指定 `-n` 时为 5000。按多个位置参数查询对象时仍表示目标数量。
强大的 k8s 工具 for 商汤 dcluster
=======

`rayctl` is a custom Kubernetes CLI written in Go for AI infrastructure and cluster management workflows.

## Command Examples

```bash
rayctl node list
rayctl node list ecp
rayctl node list ssp
rayctl node list ssp -l
# USE 按当前用途显示 ECP/ECS/SSP；SSP 依据 Cluster 与源 VC 的关联识别，空闲节点也可识别。
# -l 的 PROD 保留宿主节点原始标签；直接传 label selector 仍按原始标签筛选。
rayctl node list "accelerator=huawei-ascend"
rayctl node cordon worker-01
rayctl node uncordon worker-01
rayctl node cordon 10.140.214.222 10.140.214.223
rayctl node uncordon 10.140.214.222 10.140.214.223
rayctl node get worker-01
rayctl node get 10.140.214.222 10.140.214.223
rayctl node get 10.12.145.100 --workloads
rayctl node get 10.12.145.100 --workloads -l
rayctl node get worker-01 --workloads --no-platform -o json
# --workloads 按 Pod 展示全部可见归属，STATUS 包含容器 Waiting 原因（如 ContainerCreating）。
# ECS 节点默认同时显示 Pod 名与 ECS 业务名；--workloads 也保留 ECS 类型及业务名。
# -l 额外显示 VC、WORKSPACE、CREATOR、ACCEL REQ、SOURCE；ACCEL REQ 是资源请求量而非实时利用率。
# 平台补全整批共享 2 秒预算；失败保留 k8s 行与 warnings，未知类型为 OTHER。

# 从 HC Pod 名或 Pod IP 反查负载；同名时必须指定 host namespace
rayctl pod get dev-ww-new-0
rayctl pod get vcluster-b55aba4e-3c427f5a/dev-ww-new-0
rayctl pod get 10.119.211.204 -l -o json
rayctl pod get dev-ww-new-0 -n host-namespace --no-platform
# 默认不查存储；-l 查询 PVC -> AFS。平台补全最多 2 秒，失败保留 Pod 信息并提示。
# STATUS 结合删除标记和容器状态显示 Terminating、CrashLoopBackOff 等；PHASE、READY 保留 Kubernetes 原始值。

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
# 查询队列的直接 IAM 授权，支持名称片段、完整 UID、批量、-l 和 JSON
rayctl auth check queue queue-d-elastic-a2-caicopilot
rayctl auth check queue elastic-a2-caicopilot -e d -l
rayctl auth check queue queue-a queue-b -o json
rayctl rbac get vc-a vc-b
rayctl policy get disallow-privileged-containers vc-a vc-b
rayctl --kubeconfig=/path/to/config node list ecp

# SSP 是默认任务入口；多任务查询并发执行且保持输入顺序
rayctl ait list
rayctl ait list -s Running -n 100
rayctl ait get job-a job-b job-c
# 旧 ECP VCJob 使用明确的兼容入口
rayctl ate list
rayctl ate list -s pending
rayctl ate get old-job
rayctl ate list cluster vc-a3-intern-delivery
rayctl ecs list
rayctl ecs list -t ecs --state Running
rayctl ecs list --name yunwei -l
rayctl ecs list -n 0 -o json
# ecs list 分页汇总当前环境中已配置且有权限的各租户 ECS/AIS，隔离其他平台环境。
# 平台机器规格为空时，仅对筛选和截取后展示的资源按 UID 查询 HC VM/VMI，显示实际 vCPU / 内存；无 HC 配置仍可列出平台数据。
# 默认按类型、名称排序并最多显示 100 条；--name/--state 筛选后再应用 -n，-n 0 显示全部。
# -l 增加 TENANT、UID、机器规格、VPC；JSON 同时保留 tenant_id。创建人按所属租户补全，失败保留 ID；清单部分失败保留成功结果并返回错误。
rayctl ecs get ais-a ecs-b ais-c
# ecs get 同时展示所属租户及 DNAT 外部地址:端口 -> 内部地址:端口、协议、状态和 NAT 网关。
# 资源接口缺失 DNAT 时按所属租户、VPC 和内网 IP 查询；查询失败保留机器详情并提示告警。

# 汇总当前环境各已配置租户的资源，或按名称/UID 查看单个资源；list -l 显示 TENANT
rayctl vpc list
rayctl vpc get vpc-muxi-ailab
rayctl subnet list
rayctl subnet get subnet-muxi-ailab
rayctl natgw list
rayctl natgw get nat-muxi-ailab
rayctl afs list
rayctl afs get afs-tangrui
rayctl afs create afs-example --dry-run
rayctl afs create afs-example          # 默认 1TB、OCEANSTOR
rayctl afs create afs-example --size 2TB --zone cn-pj-01a -y

# 查看 VC 节点，或按节点名、IP、ACN 名称、ACN UID 添加/移除节点
rayctl vc list
rayctl vc get vc-a3-llmit
rayctl vc get vc-a3-llmit vc-a3-deeplink vc-c550-jiaofu
rayctl vc get vc-a3-llmit --platform-only
# 控制面规格：区分平台期望、HC 下发标签和实际 Deployment 资源/Ready
rayctl vc config get vc-a2-discovery-prod -e d
rayctl vc config get vc-p-cluster -e pt -o json
rayctl vc config update vc-a2-discovery-prod --flavor large --dry-run -e d
# 去掉 --dry-run 后交互确认；--yes 可跳过确认，默认等待收敛，--timeout 默认 2m。
# 规格从平台读取；minimum/small 为 1 副本，medium/large 为 3 副本，不能只凭 Pod 数反推。
# minimum/small 节点上限分别为 15/30；unlimit 无资源 limits，不建议使用，须 --allow-unlimit。
# 通过 HC Service proxy 调用持久 API，不连接数据库。--platform-only 跳过 HC/规格查询。
rayctl vc set d
rayctl vc node list vc-c550-ai4s-sys
rayctl vc node usage vc-a3-deeplink
rayctl vc node usage vc-a3-deeplink vc-a3-241ceshi
rayctl vc node add vc-c550-ai4s-sys 10.12.138.140 --dry-run
rayctl vc node add vc-c550-ai4s-sys host-10-12-138-140 -y
rayctl vc node remove vc-c550-ai4s-sys 10.12.138.140 --dry-run
rayctl vc node remove vc-c550-ai4s-sys 10.12.138.140 -y

# 默认通过平台 API 获取 AIT 训练任务；识别到 PT Pod 时自动使用 PT region
export KUBECONFIG=~/kubeconfig
rayctl ait list
rayctl ait get <job-name-or-uid>
rayctl ait get <job-name-or-uid> --workspace ws-t-llm-frontier

# 查看 AID 开发机；运行中开发机的结论含近 24h 重启次数、最近重启时间和原因
# Pod 重建后仍查询同名 Pod 的监控历史；监控时间是采样观测时间
rayctl aid get <aid-name-or-uid>
rayctl aid get <aid-name-or-uid> --workspace ws-t-wamcritic
rayctl aid get dev-yifan-gpu2 -e all -l --no-image
# -e all 查询 D/PT 的对应 HC；命中时退出 0，其他范围无权限压成一行 workspace 警告。
# JSON 用 query_incomplete 标记未完成范围；未命中且存在无权限范围时仍报查询不完整。
# AID 默认 workspace 范围与 AIR 一致，均来自对应 region 的平台 RMH 清单；HC 标签只用于定位 Pod。
# 平台可见但 HC 没有对应 namespace 的 workspace 也会查询；同名不同 subscription 分别保留。
# AID 显式 --workspace 仍可直接查询未列入清单的名称；AID get 与 AIR job list 会标记部分 profile/分页失败。
# RAYCTL_HC_KUBECONFIG_D/PT 可以覆盖默认的 ~/kubeconfig 与 ~/kubeconfigpt。
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
rayctl workspace workload list ws-d-a3-ai4s
rayctl ws wl list ws-d-a3-ai4s -t aid --state Running
rayctl ws workload list ws-d-a3-ai4s ws-d-a3-frontier -o json
rayctl queue list
rayctl queue get queue-d-reserved-a3-ai4s
rayctl queue workload list queue-d-reserved-a3-ai4s
rayctl queue wl list queue-d-reserved-a3-ai4s -t job
rayctl queue workload list queue-d-reserved-a3-ai4s --state Running --priority NORMAL
rayctl vc workload list vc-a3-llmit
rayctl vc wl list vc-a3-llmit vc-a3-deeplink -t job --state Running
rayctl vc workload list vc-a3-llmit -t vcJob -o json
# workspace 聚合其所有 queue；VC 包含关联 SSP queue 工作负载和 ECP Volcano VCJob。
# VC 的 -t job 包含 SSP 训练任务和 ECP VCJob，-t vcJob/ecp-vcjob 仅查询 ECP。
# 三种 workload list 均支持 --type、--state、--priority；默认读取全部分页。
# 部分来源失败仍保留成功记录，同时返回错误；JSON 的 summary.complete 为 false。
rayctl queue node list queue-d-reserved-a3-ai4s
rayctl queue node usage queue-d-reserved-a3-ai4s
rayctl queue node usage queue-d-reserved-a3-ai4s --free

# AIR 推理任务和推理网关；gateway 可缩写为 gw
rayctl air job list -n 50
# 部分 workspace 失败时保留可用结果、退出 0 并汇总警告；全部失败仍退出非零。
# JSON 的 summary.complete=false 保留不完整范围；UID 去重后再筛选和限条数，metadata 可含 duplicates_removed。
rayctl air job get <job-name-or-uid>
rayctl air job get <job-name-or-uid> -l
# 默认显示实例组、每组实例数、总实例数以及每个计算实例的节点、Host IP、Pod IP。
# 辅助 Pod 单独展示；LEADER SERVICE IP 是集群内服务地址，未分配时显示说明。
rayctl air gateway get <gateway-name-or-uid>
rayctl air gw get <gateway-name-or-uid>

# GPU 默认摘要；--detail 展开采样点。coverage 按序列计算，缺失不填 0。
rayctl metrics gpu dev-5dc5d665 -w ws-t-vcg -k ~/kubeconfigpt --since 24h
rayctl metrics gpu dev-5dc5d665 -w ws-t-vcg -k ~/kubeconfigpt --since 72h --by gpu -o json
# 覆盖率不足 90% 或缺口超过 3 个采样间隔时警告，并列出 UTC+8 缺口。
# 相同断档合并显示；相同覆盖率与断档合并为一行，JSON coverage 仍逐卡保留。
# --health 只按验证过的物理 UUID 关联；缺失显示未知。SBE 为信息项，DBE/隔离页参与异常判定。

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
