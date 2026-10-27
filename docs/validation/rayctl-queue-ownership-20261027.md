# ELASTIC --free 节点归属过滤验收

验收时间：2026-10-27 19:27–19:28（UTC+8）。环境：AIS，Linux aarch64，D 真实平台及 `vc-muxi-550pl-ailab`。查询均为只读，未修改队列、Pod 或调度配置。基线为已安装的 `6cbad2d`。

## 变更口径

`queue node usage <queue> --free` 对 ELASTIC 先保留本队列已占用节点及公共池未归属节点，再检查 CPU、内存和加速卡余量。其他队列节点上的碎片排除；未知归属排除并汇总警告。队列剩余额度继续使用平台队列详情的配额表，不按节点数推算，也不按配额任意截取节点。

归属取自指定 VC 的完整 Node/活跃 Pod 分页及已有资源分配数据。实测仅 15 台固定队列节点有 queue 标签，而 ELASTIC 的动态归属体现在已绑定 Pod 上，所以不能将“节点无 queue 标签”视为公共池。以 ACN UID、VC UID 校验节点对应关系；存在其他队列证据时优先排除。仍活跃的 Terminating Pod、已绑定的 Pending Pod 继续占用；只有找不到归属证据且分配资源明确归零，才进入未归属类别。

默认不加 `--free` 仍显示整个 VC 共享池；非 ELASTIC 的节点筛选沿用原逻辑。

## AIS 实测

```bash
rayctl queue node usage queue-d-elastic-muxi-830pdf -o json
rayctl queue node usage queue-d-elastic-muxi-830pdf --free -o json
NO_COLOR=1 COLUMNS=80 rayctl queue node usage queue-d-elastic-muxi-830pdf --free
NO_COLOR=1 COLUMNS=200 rayctl queue node usage queue-d-elastic-muxi-830pdf --free
rayctl queue node usage queue-d-elastic-muxi-830pdf queue-d-elastic-muxi-llmagent-muxi --free -o json
rayctl queue node usage queue-b07cf9fc --free -o json
```

在旧版、新版命令前后，独立使用对应 VC kubeconfig 查询 Node 及所有 namespace 的活跃 Pod，逐节点核对归属。102 条新版结果全部通过，前后归属变化 0 条。

| 归属/筛选 | 节点数 |
|---|---:|
| 原始 VC 共享池 | 315 |
| 本队列占用 | 172 |
| 公共池未归属 | 79 |
| 其他队列 | 64 |
| 未知归属 | 0 |
| 按归属保留 | 251 |
| 旧版 `--free` | 121 |
| 新版 `--free` | 102 |
| 新版中的本队列有余量节点 | 23 |
| 新版中的未归属节点 | 79 |
| 旧版混入、现已排除的其他队列碎片节点 | 19 |

所有新版结果的 CPU、内存均有余量，有加速卡时剩余卡数大于 0；未归属节点的分配资源均为 0。节点数随实时任务变化，不是固定验收常量。

表格先显示与旧版、默认视图逐字段一致的配额：

| 资源 | 总量 | 已分配 | REMAINING |
|---|---:|---:|---:|
| 节点 | 209 | 172 | 37 |
| CPU（核） | 23408 | 23280 | 128 |
| 内存（GiB） | 300960 | 180098 | 120862 |
| 加速卡 | 1672 | 1238 | 434 |

节点表包含 `OWNERSHIP`，值为 `owned` 或 `unassigned`。实际摘要：

```text
VC 共 315 台：本队列占用 172，公共池未归属 79，其他队列 64，归属未知 0；按归属保留 251 台。
归属候选共 251 台，本次显示 102 台。
```

80/200 列下配额、归属和节点数据均完整；`--free` 明确说明不保证可调度，剩余额度见 `REMAINING`。

JSON 的 `scope=queue_owned_or_unassigned`，`free_only=true`，`node_count=315` 保留筛选前数量；`ownership.eligible_nodes=251`。节点的 `ownership`、`owner_queue_uids`、`owner_queue_names` 提供归属证据；所有命令 `summary.complete=true`、退出码 0。

| 回归场景 | 结果 |
|---|---|
| 默认 ELASTIC | 315 条，`scope=vc_shared_candidate_pool`，1.610 秒 |
| 旧版 ELASTIC `--free` | 121 条，2.777 秒 |
| 新版 ELASTIC `--free` | 102 条，2.810 秒 |
| 80/200 列表格 | 2.727/3.740 秒，配额先于节点表且有归属列 |
| 两个 ELASTIC 队列 | 183 条，2.406 秒；每行 context 指向对应队列，本队列 UID 精确匹配，未归属节点无队列 UID |
| EXCLUSIVE `queue-b07cf9fc` | 1 条，0.866 秒；与旧版节点集合一致，`scope=queue_nodes` |

## 自动回归及失败边界

通过 `go test -race ./... -count=1`、`go vet ./...`、`git diff --check`。全量测试需要 httptest 回环监听，因此在允许本地监听的执行环境运行。

新增/调整测试覆盖：

- 本队列、公共池、其他队列固定归属、其他队列碎片、混合归属；UID 优先于同名队列。
- 非本队列 Running/Terminating/已绑定 Pending Pod 排除；最后占用释放且资源归零后才进入公共池。
- 未识别负载、分配未清零、节点缺失、ACN/VC UID 不匹配、重复 ACN UID 均排除。
- Volcano 兼容标签/注解；无归属的系统加速卡负载仍按未知排除。
- 完整分页、重复 token、缺少 token 的残缺结果、跨页 resourceVersion 变化和取消。
- VC/profile 正确作用域、平台代理回退、后续 Pod 分页中的其他队列归属不能漏读。
- 归属读取 403 时保留配额、隐藏未核实节点并返回错误；不回退到整池，也不误报没有空闲节点。
- 未知节点只打印一条汇总警告，JSON `summary.complete=false`；节点归属 UID 数组使用固定 JSON 字段名。
- 配额不因归属/资源筛选改变，归属候选数量与完整 VC 数量区分，帮助文本同步更新。

这些数据是查询时可见 Node、Pod、资源分配的快照，不读取调度器内存中的预留状态。配额、任务规格、亲和性及其他调度约束仍可能阻止候选节点被使用。

## 构建与部署

通过 AIS 验收的 Linux ARM64 候选 SHA-256：`d6776a2557c29859852818394a562320cb7787b45b4feb0e2904cf90f4b0e076`。

发布流程：提交源码及验收记录，从干净提交重建 macOS ARM64、Linux ARM64；备份并原子替换本机 `~/.local/libexec/rayctl` 及 AIS `/usr/local/bin/rayctl`。校验上传、安装 SHA-256 和构建 revision，安装后执行真实命令冒烟。最终构建包含提交后的 VCS 元数据，哈希与候选不同。
