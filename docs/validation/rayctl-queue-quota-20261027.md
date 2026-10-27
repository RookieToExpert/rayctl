# queue node usage 队列配额与共享候选池验收

验收时间：2026-10-27 18:27（UTC+8）。环境：AIS，Linux aarch64，D 真实平台。平台及 Kubernetes 查询均为只读。

后续已将 ELASTIC `--free` 的节点范围收窄为本队列占用及公共池未归属节点，见[归属过滤验收](rayctl-queue-ownership-20261027.md)。本文保留当时的共享池筛选结果。

## 问题与数据来源

`queue-d-elastic-muxi-830pdf` 是 ELASTIC 队列，关联 `vc-muxi-550pl-ailab`。原命令直接展示整个 VC 的资源水位，无法与网页的队列配额比较。

平台队列详情的 `properties.quota` 与网页一致；按机型配额来自 `properties.machine_type_allocation[].queue_quota`。通用 `/summary` 与 `node_status` 对这个队列的已分配节点返回 0，不能用来替换网页中的 172。本次新增的配额查询读取队列详情，不将共享池容量、节点 allocatable 或缺失值充当配额。

## 实测数据

命令：

```bash
rayctl queue node usage queue-d-elastic-muxi-830pdf -e d
rayctl queue node usage queue-d-elastic-muxi-830pdf -e d --free
rayctl queue node usage queue-d-elastic-muxi-830pdf -e d -o json
rayctl queue node usage queue-d-elastic-muxi-830pdf -e d --free -o json
```

第一个表是机型 `x2ls.ri.i80` 的队列配额：

| 资源 | 总量 | 已分配 | 剩余 |
|---|---:|---:|---:|
| 节点 | 209 | 172 | 37 |
| CPU（核） | 23408 | 23280 | 128 |
| 内存（GiB） | 300960 | 180098 | 120862 |
| 加速卡 | 1672 | 1238 | 434 |

与同一次验收读取的平台原始队列详情逐字段比较，全部一致。按机型 JSON 中节点总额/已分配也分别为 209/172。

第二部分标题为 `VC 共享候选池 [vc-muxi-550pl-ailab] / 队列 [queue-d-elastic-muxi-830pdf]`：

| 检查 | 结果 |
|---|---|
| 默认节点范围 | 315 台，显示 315 台 |
| `--free` 节点范围 | 315 台，显示 119 台 |
| 逐条检查 CPU、内存、卡余量 | 119 台均满足，违反条件 0 台 |
| 配额是否随 `--free` 改变 | 不变，两个配额表及 JSON 配额逐项一致 |
| 范围声明 | 明确说明共享池非队列独占、候选池余量不等于队列剩余配额 |
| `--free` 声明 | 只筛节点余量，不校验队列配额、不保证可调度；配额表不受筛选影响 |
| CPU/内存容量声明 | 节点可分配容量优先使用 Kubernetes allocatable，与配额口径不同 |
| 窄终端 | `COLUMNS=80 NO_COLOR=1` 默认及 `--free` 的配额、标识、数字完整 |
| 宽终端 | `COLUMNS=200 NO_COLOR=1` 的 `--free` 配额完整 |

119 是本次实时快照，不是固定值。先前排查时同一池的 `--free` 为 121 台，池中任务变化会改变结果。

JSON 保留节点 `items`，在 `metadata` 中分别带有：

- `quota.resources`、`quota.machine_types`：平台配额。
- `scope=vc_shared_candidate_pool`。
- `node_count=315`：筛选前数量。
- 默认 `free_only=false`，筛选后 `free_only=true`。
- 两次 `summary.complete=true`、退出码 0；默认 JSON 耗时 1.933 秒，`--free` JSON 为 1.676 秒。

## 其他真实命令回归

| 命令/场景 | 结果 |
|---|---|
| 从当前 `queue list -e d` 选择 1 节点 EXCLUSIVE 队列 `queue-b07cf9fc` | 配额与平台详情一致；`scope=queue_nodes`，`shared_vc_pool=false`，1 条节点，0.834 秒，退出 0 |
| 上述 ELASTIC + EXCLUSIVE 两队列 `--free -o json` | 2 个 metadata，节点含所属 queue 的 context，两个 scope 均正确；120 条节点，1.872 秒，退出 0 |

## 自动回归

通过：`go test -race ./... -count=1`、`go vet ./...`、`git diff --check`。

新增测试覆盖：

- 读取网页同源队列详情，而非节点为 0 的 summary；使用队列所属 profile、subscription、region。
- 不用 `cluster_capacity` 代替队列的 `node_max_num`。
- 缺失配额与显式 0 区分；缺失节点计数时不伪造 0 节点配额。
- 配额或节点一侧 403 时保留另一侧数据；两侧失败仍报错；保留错误链且 HTTP 提示不附完整 URL。
- 命令实际 RunE 的部分数据保留、非零错误及 JSON `summary.complete=false`。
- 取消保留 `context.Canceled`，不误报为空。
- 队列 CPU 配额为 0 时，`--free` 仍只筛节点，不暗中变成调度检查；重复筛选不改变范围节点数或配额。
- 配额在共享池之前、多个队列各自有标题、多个机型区分、80/200 列完整输出。
- JSON 配额、scope、筛选前节点数、free_only 和不完整状态。
- 帮助文本明确两种统计口径与 `--free` 的限制。

workspace/VC 用量以及 queue node list 沿用原有查询，不新增其配额请求。

## 构建与部署

通过 AIS 验收的候选 Linux ARM64 二进制 SHA-256：`3569d4537737e7489099ff55b29ae544413e658969b2e927a3b86a12520f5457`。候选在独立临时路径完成验收。

发布流程：验收后提交源码与记录，从干净提交重建 macOS ARM64 和 Linux ARM64；本机替换 `~/.local/libexec/rayctl`，AIS 校验上传哈希后原子替换 `/usr/local/bin/rayctl`，保留旧二进制备份。最终安装后再检查 SHA-256、构建 revision、帮助和真实命令冒烟。最终构建因包含新提交的 VCS 元数据，其哈希与上面的候选哈希不同。
