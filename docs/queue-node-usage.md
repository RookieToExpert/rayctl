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

`--free` 只筛选第二部分：CPU、内存都还有余量；有加速卡的节点还必须有未分配的卡。它不修改第一部分配额，也不根据配额扣减节点数量，不检查任务规格、亲和性或其他调度条件。因此，即使队列 CPU 配额已经用完，`--free` 仍可能列出池中有余量的节点；这些节点不能直接解释为该队列可申请的节点。

JSON 仍在 `items` 中输出节点，在每个队列的 `metadata` 中增加：

| 字段 | 含义 |
|---|---|
| `quota.resources` | 页面口径的队列整体配额 |
| `quota.machine_types` | 按机型划分的配额 |
| `scope` | `vc_shared_candidate_pool` 或 `queue_nodes` |
| `node_count` | `--free` 筛选前的节点数量 |
| `free_only` | 是否应用 `--free` 节点余量筛选 |
| `quota_error` / `node_usage_error` | 对应来源查询失败时的错误 |

配额或节点查询一侧失败时，保留另一侧成功的数据并返回非零退出码，JSON 的 `summary.complete=false`。不会把查询失败表述为“配额为 0”或“没有空闲节点”。`workspace node usage`、`vc usage` 和 `queue node list` 的现有查询范围保持原样。
