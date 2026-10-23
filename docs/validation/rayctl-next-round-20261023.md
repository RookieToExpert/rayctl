# rayctl 下一轮需求验收（2026-10-23）

基线 `cea6867`。目标 AIS：用户指定的 `10.140.158.130:22225`，Linux ARM64，安装路径 `/usr/local/bin/rayctl`；本机路径 `~/.local/libexec/rayctl`。D/PT 查询均只读，未改变 VC 规格、数据库、权限或限流配置。任务取自当时清单，完整任务输出及镜像凭据没有写入本报告。

## 提交对应关系

| 项目 | 实现提交 | 结果 |
|---|---|---|
| BUG-02 / PT ATE list | `65e3a5c` | 全部 404 的完整扫描才提示无 ATE 服务 |
| BUG-01 / AID 跨环境 | `bf2b3ff` | D/PT 使用各自 HC，保留部分失败 |
| BUG-03 / Pod 跨环境 | `c0ded9d` | all 遍历 D/PT，输出命中环境 |
| PERF-01 / 最近点索引 | `1d1d9fa` | 每序列排序一次，二分查找 |
| BUG-04 / 指标覆盖 | `da96e28` | 结构化 coverage、UTC+8 首尾/内部缺口 |
| RISK-01 / 排队超时 | `8286402` | 排队与网络各 20s，父 context 仍约束总时间 |
| UX-02 / 完整标识 | `3b05222` | 80/200 列不截断、不折行 |
| RISK-02 / 物理卡身份 | `94e50af` | UUID 匹配；SBE 仅信息 |
| UX-03 / AIR list | `07bbce0` | 紧凑列、READY 合并、默认 50、-n 校验 |
| UX-01 / 增量详情 | `5cfed1d` | 锚点 + 新增字段、挂载去重、去噪、完整 JSON |
| VC-01 / 规格查询 | `dbc1a30` | 平台期望、HC 下发、实际资源交叉验证 |
| VC-02 / 规格更新 | `926ee79` | 持久 API、dry-run、确认、等待收敛 |

审查补漏：`5e49b17` 防止规范化阶段凭卡序号补出 UUID，并保留全空指标的 coverage/错误；`a0f7b55` 补完整 binding ID 和空白日志；`cd407c6` 保留 Dcloud 原有 VC 映射；`1f8aa72` 修复多对象 AFS `-l` 未进入增量视图。

## 自动测试

- 最终源码 `go test -race ./... -count=1`、`go vet ./...`、`git diff --check` 通过。Go 1.26.4，使用可写 GOCACHE。
- 环境查询覆盖 D 未命中/PT 命中、严格单环境、全部未找到、配置/权限失败、同名歧义、取消。某范围失败时保留另一范围结果，不能把不完整查询说成不存在。
- ATE 可用性覆盖全部 404、混合 403/500、成功空结果、VC 清单不完整、取消/超时。
- GPU 索引覆盖 72h/168h 全点、乱序、空序列、120s 边界、等距时固定选择较早时间点。覆盖率测试包含完整、首尾缺失、内部断档、无数据和未知采样周期；缺失不填 0。
- 物理卡测试包含相同容器编号下不同 UUID、缺 UUID、UUID 冲突、编号重排与 SBE-only。没有 DBE 等严重度证据时不凭 SBE=0 判健康。
- 排队测试先复现旧 Client.Timeout 会在未发出请求时到期；分别验证并发槽位等待、冷却等待不再消耗网络预算，以及排队上限、父取消、429/504 重试边界。
- 详情测试覆盖多对象锚点、默认摘要不重印、PVC 合并且保留 mount/subPath/只读、注入卷折叠、空日志、AIT 无采样噪音、80/200 列完整标识、JSON 不被表格过滤且 DTO 未被修改。
- VC 测试覆盖 minimum/small 不靠资源反推、无标签、权限失败的 HC 回退、Deployment ownerReference、资源量数值比较、未完成 rollout；更新测试覆盖非法/未提供规格、dry-run 零写入、同规格零写入、提交前变化、成功收敛、提交失败、取消和等待超时。测试 HTTP 服务检查精确 PATCH 契约，403/429/504 均不自动重复写。

索引 benchmark（本机 Apple M5，`-benchtime=1x -benchmem`，单次观测）：72h 3.59ms、277560 B、17283 alloc；168h 8.88ms、650296 B、40323 alloc。它衡量建索引及逐点查询，不包含网络；下表另有 AIS 整命令对比。

## AIS 跨环境与详情

| 检查 | 耗时 | 证据 |
|---|---:|---|
| PT `ate list -n 2` | 0.063s | 退出 1，仅一行“PT 环境没有 ATE 服务，请用 ait”，无 URL、无 404 |
| D `ate list -s running -n 2` | 约 2.9s | 正常返回两个运行中任务 |
| `aid get dev-yifan-gpu2 -e all -l --no-image` | 0.56s | 命中 PT、1 Pod、4 容器；D 两个测试 workspace 真正返回 403，保留结果并退出 1 |
| 同 AID 显式 `-e pt -w ws-t-eailabos` | 0.666s | 退出 0，1 Pod、4 容器、12 挂载、38 行日志证据（未记录内容） |
| PT Pod 的 `pod get -e all --no-platform` | 0.031s | pt、Running；`-e pt` 同样命中 |
| 同 Pod 的 `pod get -e d` | 0.027s | 严格未找到，只列出 d/cn-pj-01 |
| 不存在 AID，完整 PT 指定 workspace 查询 | 0.097s | 明确未找到，列出 pt/cn-pj-03 |
| 不存在 AID，all 查询 | 约 1.04s | 列出 D/PT；D 的真实 403 导致“未完整完成”，没有误报未找到 |

Pod 样本：`vcluster-f166adcd-ebe33691/dev-yifan-gpu2-0`。以上 D 403 属于当次权限状态，没有改权限或隐藏错误；“能够命中 PT”不等于“所有 D 范围查询成功”。

运行中 ATE 样本从当前清单获取：

- `breeze-cap1023-014-n8-k2-business`：0.957s，1 个检查 Pod、2 容器、12 挂载。表格 41 行，有锚点与折叠挂载，没有重复默认 workspace、无空日志标题，镜像完整连续。
- `cluster-compute-2025090718-allnpu-hccl-a5f59aa1`：1.174s，1 个检查 Pod、1 容器、4 挂载、25 行日志。表格 59 行，同样为增量视图。

AIT 再从 D 运行中 list 取两个对象：`pah-cc73539cbdc7-r1`、`jm-v3-1022-anmerge2-r000-a0`，合并查询 JSON 0.539s、表格 0.418s；每个对象都有 1 Pod、2 容器，表格恰有两个锚点、不重复默认 workspace、不含“采样间隔约”警告，注入挂载折叠。另测 PT 已失败任务 `lib-nwam-probe4-1023`：当前没有 Pod，增量视图没有伪造 Pod 证据。

AIR `infer-zcl-dsv41-flash-pd-rc1-eval`：3 个检查 Pod、5 容器、16 挂载、90 行日志证据；`-l` 有锚点、无重复默认 workspace、镜像完整。总行数仍受实际日志数量影响。Gateway `service-pdf-postprocessing` 的增量表格 13 行。多对象 AFS 锚点及完整 JSON 另有针对性测试，未把其他资源命令的单元测试描述为全部线上验收。

`air job list -n 1` 在 COLUMNS=80/200 下最大宽度均为 78；名称 `infer-zcl-dsv41-flash-pd-rc1-eval` 完整，只有 NAME/STATE/READY/WORKSPACE，QUEUE 在 `-l`。超长标识允许超过终端宽度，不能为满足 120 列而裁剪。

## GPU 断档与性能

PT 使用用户原始 HC 配置 `-k ~/kubeconfigpt`。`dev-5dc5d665 -w ws-t-vcg --since 24h` 当次为 778 个点/每卡预期 5761，覆盖约 13.5%；15s 采样，观测从 2026-10-23 09:21:23 到 12:35:38（UTC+8），明确列出窗口开始到 09:21:23 的缺口。JSON 有 requested、observed、ratio、ratio_known、expected/actual_points、gaps。

72h/168h 覆盖分别出现 4/6 个历史卡序号序列，各自列出首尾缺口，没有把不同卡的点数相加成覆盖率。不同时间窗中的卡数/序号变化不能直接解释为当前 Pod 同时占用卡数。

固定结束时间 **2026-10-23 12:39:36 UTC+8**，对比基线备份和新版本，均使用同一 `--start/--end --by gpu -o json`：

| 窗口 | 序列 / 采样点 | 基线 wall / CPU | 新版本 wall / CPU | 全部 samples（含显存）逐点一致 |
|---|---|---|---|---|
| 72h | 4 / 17240 | 4.122s / 4.172s | 0.737s / 0.713s | 是 |
| 168h | 6 / 23212 | 4.781s / 4.769s | 0.906s / 0.909s | 是 |

这不是固定延迟承诺。与独立 benchmark 一起证明最近点查询已移出平方级热路径。

## 健康指标证据

D 瞬时抽样 NPU utilization 和 DBE 各 8 条，都有 `Hostname`、`UUID`、`gpu`、`id`、`exported_pod`。两个任意 topk 集合可能来自不同卡，不能靠集合不相交判定错位；另外从一个利用率样本取得 UUID，再按该 UUID 精确查询 DBE，返回 3 个 view，UUID 与节点全部相同，gpu/id 都为 5。代码不以容器编号代替物理身份。

PT DCGM GPU_UTIL 抽样 8 条，含 Hostname/UUID/gpu；DBE 当次 0 条。因此未完成真实 DCGM DBE 关联验收。PT `dev-2a39cf7f --health` 当次 4 张历史卡均为 unknown，没有把“没采到 DBE”报成健康。

SBE 保留 `ecc_sbe` 信息项；DBE、隔离页、已映射 RAS/错误码参与严重度判定。缺 UUID 时健康不可确认并提示，规范化阶段也不会凭卡序号补出一个 UUID。

## VC 配置与未执行项

D `vc-a2-discovery-prod` 为 medium，平台/HC 一致，3/3、每副本 8 CPU / 32Gi；PT `vc-p-cluster` 为 large，3/3、32 CPU / 128Gi。`config get` 分别 0.165s/0.068s；普通 `vc get` 也带相同配置证据。

D 的 medium→large dry-run 0.108s，PT 的 large→medium dry-run 0.068s。前后期望值不变，Deployment generation 分别保持 25/17。规格与正式 API 契约见 [VC 配置说明](../vc-flavor-plan.md)。

未执行生产 PATCH、实际控制面重启/滚动更新、数据库操作、权限变更、真实 429/504 或网络拥塞注入；这些失败与收敛路径由测试后端和 Kubernetes fake 覆盖。没有原子条件写入支持，不承诺解决确认与提交之间最后一个瞬间的竞争。

## 交付

各功能独立提交，待办与 README 已更新。本机和 AIS 按最终完整 HEAD 重建并核对安装文件 SHA-256；不把构建自身的哈希写入参与该构建的文档，以免形成自引用。安装位置遵守 AGENTS.md，旧版本保留备份。最终 Git HEAD、安装哈希和推送状态另在交付时核对。
