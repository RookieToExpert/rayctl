# 队列配额与节点水位

`ELASTIC` 队列有自己的资源配额，任务按需从关联 VC 的共享节点池调度。队列没有一组固定独占的节点；池里有余量，也不代表该队列还有额度或任务一定可以调度。

```bash
rayctl queue node usage queue-d-elastic-muxi-830pdf
rayctl queue node usage queue-d-elastic-muxi-830pdf --free
rayctl queue node usage queue-a queue-b -o json
```

输出顺序：

1. **队列配额**：显示机型、资源、总量、已分配、剩余额度。直接读取队列详情中的 `properties.quota` 和 `machine_type_allocation[].queue_quota`，与平台队列资源页一致。ELASTIC 的 `node_status` 和通用 `/summary` 接口可能显示 0 个已分配节点，因此不用于替代页面的节点配额。多机型时分别列出各机型和合计；没有配额数据时明确报错，不以 VC 容量代替。
2. **节点水位**：ELASTIC 展示 VC 共享候选池，其他队列展示其节点。`ALLOC/TOTAL` 是节点上的已申请资源/可分配资源；CPU、内存优先使用 Kubernetes 的 `allocatable`，与队列配额不同。列出筛选前的节点数和本次显示的节点数。

ELASTIC 的 `--free` 先把节点范围收窄为**本队列已占用节点 + 公共池未归属节点**，再要求 CPU、内存都有余量，有加速卡时还必须有未分配的卡。其他队列节点上的空闲碎片不计入。表格新增 `OWNERSHIP` 列：`owned` 是本队列占用，`unassigned` 是公共池未归属。归属统计列出完整 VC 的四类节点数及按归属保留的候选数。

归属使用指定 VC 的节点标签和已绑定活跃 Pod 的队列 UID/名称，结合节点已分配资源判断。ELASTIC 的动态归属通常只出现在 Pod 上，不能把缺少节点 queue 标签当作未归属。正在终止但仍活跃的 Pod 继续计入占用；已分配资源非零却找不到归属、节点不匹配或有无法识别的活跃负载时，按未知排除，不计入公共池。扫描必须完成全部分页；归属读取失败时保留配额并返回错误。

`--free` 不修改第一部分配额，也不按配额任意裁剪候选节点；**队列剩余额度见配额表的 REMAINING**。候选不等于已获准使用，仍受配额、任务规格、亲和性、预占等调度约束。默认不带 `--free` 时继续显示完整 VC 共享池，便于对照。

JSON 仍在 `items` 中输出节点，在每个队列的 `metadata` 中增加：

| 字段 | 含义 |
|---|---|
| `quota.resources` | 页面口径的队列整体配额 |
| `quota.machine_types` | 按机型划分的配额 |
| `scope` | 默认 ELASTIC 为 `vc_shared_candidate_pool`，ELASTIC `--free` 为 `queue_owned_or_unassigned`，其他队列为 `queue_nodes` |
| `node_count` | 归属及资源余量筛选前的节点数量；ELASTIC 为完整 VC 共享池数量，归属候选数见 `ownership.eligible_nodes` |
| `free_only` | 是否应用 `--free` 节点余量筛选 |
| `ownership` | `--free` 的归属来源、本队列占用/未归属/其他队列/未知节点数，以及归属候选数 |
| 节点 `ownership` / `owner_queue_uids` / `owner_queue_names` | 节点类别及可见归属证据 |
| `quota_error` / `node_usage_error` | 对应来源查询失败时的错误 |
| `ownership_error` | 归属读取失败；不得退回整个 VC 的余量表 |

配额或节点查询一侧失败时，保留另一侧成功的数据并返回非零退出码，JSON 的 `summary.complete=false`。不会把查询失败表述为“配额为 0”或“没有空闲节点”。`workspace node usage`、`vc usage` 和 `queue node list` 的现有查询范围保持原样。

归属扫描成功但部分节点仍未知时，排除这些节点并汇总一条警告，JSON 的 `summary.complete=false`。统计是查询时可见节点、Pod 和资源数据的快照，不读取调度器的内存预留状态。

真实环境的前后对照及回归结果见 [ELASTIC --free 归属过滤验收](validation/rayctl-queue-ownership-20261027.md)。
