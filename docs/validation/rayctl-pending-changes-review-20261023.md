# 118 个遗留文件审核记录（2026-10-23）

审核起点为 `b4a0464f453966cd7f73b1c1363b89b2ad7283e0`。
原有 118 个未提交文件由 56 个已跟踪文件修改、62 个新增文件组成，均为 Go 源码/测试或 Markdown 文档。
这些是此前积累的功能扩展，范围见下表和附录。未发现混入二进制、临时配置或新增的真实密钥字面量；敏感信息检查是针对本次差异的模式扫描。

## 按功能拆分

| 组 | 原有文件数 | 内容 | 提交 |
|---|---:|---|---|
| VC 配置更新 | 6 | 完整分页、预览需补齐及已不存在的本地配置，确认后清理，保留已变化文件；dry-run 不写文件。 | `bd42a92` |
| 列表条数 | 7 | AIT/AID/ATE 的窗口查询遵守显式 -n；未指定 -n 时窗口查询最多 5000 条。 | `871664d` |
| 授权查询 | 16 | 增加 auth check queue，使用 RMH 的真实 RID；VC 授权只读回退、用户定向查询、绑定批量查询与去重、批量 JSON 上下文。 | `98047c6` |
| ECS/AIS 与网络清单 | 32 | 增加 ecs list；同平台和区域的已配置租户清单汇总、租户列、部分结果、实际 VM/VMI 规格及 DNAT 回退查询。 | `309f8c8` |
| 节点和 Pod 诊断 | 26 | 按 SSP Cluster 与源 VC 识别当前用途；ECS 业务名；node get --workloads；pod get 按名称/IP 定位，显示生命周期状态。 | `c8401c0` |
| workspace/VC 负载与 AIR | 21 | 新增 workspace/VC workload list；范围过滤与部分错误；AIR 组数、计算实例、辅助 Pod、节点/IP 展示；AIR 输出移至独立领域文件。 | `18caa86` |
| AID 与存储证据 | 8 | AID 查询同名 Pod 近 24h 重启历史，区分监控观测与精确终止时间；PVC 未绑定时识别 AFS，避免 AOSS 误判。 | `8b9dd50` |
| 使用和部署文档 | 2 | README 命令及输出说明；AGENTS 指定本机安装位置和 AIS Linux ARM64 部署位置。 | `本记录所在提交` |

原有文件合计 118 个；本次另新增这份审核记录。审核过程中仅修改了原有 6 个文件（下列三处实现及对应测试），其余 112 个文件与开工快照的内容哈希一致。

## 审核发现并修复的问题

1. **VC 清单分页重叠被误判完整。** 两页包含重复 UID 时，原来可能在后续去重后缺少资源，影响本地配置清理判断。现在立即返回清单不完整，禁止据此清理。
   回归：`TestVCKubeconfigInventoryRejectsOverlappingPages`，先验证旧实现失败，再确认修复后通过。
2. **取消操作后仍可能删除本地配置。** 开始前或确认回调期间取消时，原来仍可能执行清理。现在在清理和落盘前检查上下文，返回 `context.Canceled` 并保留文件。
   回归：`TestRenewCancellationPreservesRemovalCandidates`，覆盖开始前取消、确认时取消，先失败后通过。
3. **正常 init sidecar 被当作未完成初始化。** `restartPolicy: Always` 且已经启动的 init sidecar 处于 Running 时，原来显示 `Init:0/1`。现在跳过已经启动的 sidecar，同时保留其未启动时的 `Init:CrashLoopBackOff`。
   回归：`TestNodeWorkloadStatusAllowsStartedInitSidecar`，先失败后通过。

## 自动验证

- `GOCACHE=/private/tmp/rayctl-resource-audit-go-cache go test -race ./... -count=1`：通过。
- `GOCACHE=/private/tmp/rayctl-resource-audit-go-cache go vet ./...`：通过。
- 每组导出暂存区到独立目录后运行 `go test ./... -count=1`，校验提交边界及依赖。ECS 参数校验测试和 AFS 租户表格已归入 ECS/网络组。
- `git diff --check`、`gofmt -l cmd internal pkg`：通过。
- macOS ARM64 和 Linux ARM64 构建通过；安装路径分别为 `~/.local/libexec/rayctl`、AIS `/usr/local/bin/rayctl`。

## AIS 只读实测

连接用户指定的 AIS，使用本次源码构建的 Linux ARM64 版本查询。未执行授权变更、平台资源修改或真实配置清理。下列返回数量为当次观测，不是固定容量或对所有租户的保证。

| 验证 | 结果 |
|---|---|
| `ecs list -e d/pt -n 2 -o json` | D/PT 各返回 2 条，`complete=true`，耗时 2.38s / 0.64s |
| D 的 VPC / Subnet / NAT / AFS list | 分别返回 11 / 18 / 11 / 313 条，均 `complete=true` |
| D 的 AIT/AID/ATE `--since 24h -n 1` | 均恰好返回 1 条，`complete=true` |
| `auth check queue elastic-a2-caicopilot -e d -l` | 返回 1 条绑定及 RMH 真实队列 scope；另一个样本 UID/名称查询的 scope 一致 |
| `workspace workload list` | 非空样本返回 16 条，`complete=true`；空 workspace 正常返回 0 条 |
| `vc workload list` | 样本返回 1 条，`complete=true` |
| `node list ssp/ecs` | SSP 样本的 USE 均为 SSP；6 个 ECS 节点的 USE 均为 ECS |
| `node get --workloads` | 普通节点样本 45 行，ECS 样本 36 行，包含 ECS 和 OTHER |
| `pod get <namespace/name> --no-platform` | 样本成功定位，STATUS/PHASE 都为 Running |
| AIR 详情 `-l --no-image` | 1 组、每组 8 个实例；8 个计算实例和 1 个辅助 Pod 分开展示 |
| 运行中 AID `get -l --no-image` | 成功读取 1 个 Pod，近 24h 重启数为 0 |
| `vc renew --dry-run -e d --skip-connectivity-check` | `added=0 kept=27 removed=0 failed=0`；执行前后检查的 53 个配置文件时间戳/内容哈希一致 |

VC renew 使用其支持的表格模式；不支持 JSON 的拒绝结果未计作功能实测成功。Pod 明细 JSON 对未收集/不适用字段会输出 warnings，不能将 warnings 数量当作平台请求失败次数。
分页重叠、取消确认、init sidecar、跨 Pod 替换的重启历史等边界条件由自动测试验证，未在真实集群制造故障。旧的 504/429、20 秒超时、RDMA 容量 0 验证范围不变。

## 原有 118 个文件明细

### VC 配置更新（6）

- `cmd/vc_renew.go`
- `docs/vc-renew.md`
- `internal/platform/vc_kubeconfig.go`
- `internal/platform/vc_kubeconfig_test.go`
- `internal/service/vc_renew.go`
- `internal/service/vc_renew_test.go`

### 列表条数（7）

- `cmd/job_list.go`
- `cmd/ssp.go`
- `cmd/list_limit_test.go`
- `internal/service/list_limit.go`
- `internal/service/list_limit_test.go`
- `internal/service/ssp_catalog_service.go`
- `internal/service/ssp_catalog_service_test.go`

### 授权查询（16）

- `cmd/auth.go`
- `cmd/auth_queue.go`
- `cmd/auth_queue_test.go`
- `cmd/auth_vc_test.go`
- `internal/platform/iam_client.go`
- `internal/platform/iam_user_lookup_client.go`
- `internal/platform/iam_policy_performance_test.go`
- `internal/platform/iam_user_lookup_performance_test.go`
- `internal/platform/ssp_queue_auth_client.go`
- `internal/platform/ssp_queue_auth_client_test.go`
- `internal/service/auth_service.go`
- `internal/service/auth_resource_lookup.go`
- `internal/service/auth_resource_lookup_test.go`
- `internal/service/auth_binding_policy_test.go`
- `pkg/output/auth_table.go`
- `pkg/output/json.go`

### ECS/AIS 与网络清单（32）

- `cmd/ecs.go`
- `cmd/ecs_list.go`
- `cmd/ecs_list_test.go`
- `cmd/network.go`
- `cmd/afs.go`
- `internal/platform/ecs_client.go`
- `internal/platform/ecs_list_client_test.go`
- `internal/platform/network_client.go`
- `internal/platform/network_dnat_client.go`
- `internal/platform/network_dnat_client_test.go`
- `internal/platform/profile_inventory.go`
- `internal/platform/profiles.go`
- `internal/platform/resource_inventory_test.go`
- `internal/platform/vc_client.go`
- `internal/platform/ssp_client.go`
- `internal/platform/ssp_aid_client.go`
- `internal/service/ecs_service.go`
- `internal/service/ecs_service_test.go`
- `internal/service/ecs_dnat.go`
- `internal/service/ecs_dnat_test.go`
- `internal/service/ecs_list_service.go`
- `internal/service/ecs_list_service_test.go`
- `internal/service/ecs_machine_spec.go`
- `internal/service/ecs_machine_spec_test.go`
- `internal/service/network_service.go`
- `internal/service/platform_ports.go`
- `pkg/output/ecs_table.go`
- `pkg/output/ecs_table_test.go`
- `pkg/output/network_table.go`
- `pkg/output/network_inventory_test.go`
- `cmd/batch_query_test.go`
- `pkg/output/storage_table.go`

### 节点和 Pod 诊断（26）

- `cmd/node.go`
- `cmd/node_test.go`
- `cmd/node_ecs_test.go`
- `cmd/node_usage_test.go`
- `cmd/node_workloads_test.go`
- `cmd/pod.go`
- `cmd/pod_test.go`
- `cmd/root.go`
- `internal/platform/ssp_node_client.go`
- `internal/platform/ssp_node_client_test.go`
- `internal/service/node_service.go`
- `internal/service/node_service_test.go`
- `internal/service/node_pod_details.go`
- `internal/service/node_usage.go`
- `internal/service/node_usage_test.go`
- `internal/service/node_workloads.go`
- `internal/service/node_workloads_test.go`
- `internal/service/pod_locate_service.go`
- `internal/service/pod_locate_service_test.go`
- `pkg/output/node_table.go`
- `pkg/output/node_table_test.go`
- `pkg/output/node_list_test.go`
- `pkg/output/node_workloads_table.go`
- `pkg/output/node_workloads_table_test.go`
- `pkg/output/pod_table.go`
- `pkg/output/pod_table_test.go`

### workspace/VC 负载与 AIR（21）

- `cmd/scoped_workload.go`
- `cmd/scoped_workload_test.go`
- `cmd/vc.go`
- `cmd/workspace.go`
- `cmd/air.go`
- `internal/platform/ssp_workload_client.go`
- `internal/platform/ssp_workload_client_test.go`
- `internal/platform/ssp_air_client.go`
- `internal/platform/ssp_air_client_test.go`
- `internal/service/scoped_workload_service.go`
- `internal/service/scoped_workload_service_test.go`
- `internal/service/vc_workload_service.go`
- `internal/service/vc_workload_service_test.go`
- `internal/service/ssp_air_service.go`
- `internal/service/ssp_air_service_test.go`
- `internal/service/ssp_resource_service.go`
- `pkg/output/workload_table.go`
- `pkg/output/workload_table_test.go`
- `pkg/output/ssp_air_table.go`
- `pkg/output/ssp_air_table_test.go`
- `pkg/output/ssp_resource_table.go`

### AID 与存储证据（8）

- `internal/podevidence/types.go`
- `internal/service/pod_evidence.go`
- `internal/service/pod_evidence_test.go`
- `internal/service/ssp_aid_service.go`
- `internal/service/job_storage.go`
- `internal/service/job_storage_test.go`
- `pkg/output/pod_evidence.go`
- `pkg/output/pod_evidence_test.go`

### 使用和部署文档（2）

- `README.md`
- `AGENTS.md`
