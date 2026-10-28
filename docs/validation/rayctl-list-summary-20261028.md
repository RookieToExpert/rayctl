# 所有 list 末行条数汇总验收

环境：AIS Linux aarch64，D/PT 真实平台。开始：`2026-10-28T18:36:43.055253+08:00`；结束：`2026-10-28T18:41:16+08:00`。集群操作只读，基线为 `fcba3b3`。

## 结果

24 个列表入口全部通过。加上长表、空结果、多对象及 PT 检查，共 44 组：43 组表格成功输出，1 组 PT ATE 服务不可用的预期失败，无其他查询失败。每个成功表格只有一个末行汇总，条数与同范围 JSON 的实际 items 数一致。

主要范围为 `queue-d-elastic-muxi-830pdf`、`vc-muxi-550pl-ailab`、`ws-d-muxi-830pdf`。ATE 从当前列表选择 `vc-a2-ai4earth`。有 `-n` 的清单使用 `-n 2`；AID/AIT/AIR 使用上述 workspace。

| 命令（D） | 默认条数 | -l 条数 |
|---|---:|---:|
| `afs list` | 313 | 313 |
| `aid list` | 2 | 2 |
| `ait list` | 2 | - |
| `ate list` | 2 | - |
| `cluster list` | 9 | - |
| `ecs list` | 2 | 2 |
| `group list` | 2 | 2 |
| `natgw list` | 11 | 11 |
| `node list` | 1 | 1 |
| `policy list` | 9 | - |
| `queue list` | 46 | - |
| `subnet list` | 18 | 18 |
| `user list` | 2 | 2 |
| `vc list` | 31 | - |
| `vpc list` | 11 | 11 |
| `workspace list` | 30 | - |
| `air gateway list` | 1 | - |
| `air job list` | 2 | 2 |
| `ate list cluster` | 27 | - |
| `queue node list` | 315 | 315 |
| `queue workload list` | 551 | - |
| `vc node list` | 315 | 315 |
| `vc workload list` | 601 | - |
| `workspace workload list` | 551 | - |

条数是实时快照，不是固定值。不把空表占位行、表头或概况字段计入。

## 额外边界

| 场景 | 结果 |
|---|---|
| node list empty | `本次输出 0 条。` (exit 0) |
| ecs list empty | `本次输出 0 条。` (exit 0) |
| queue node list multiple | `本次输出 326 条。` (exit 0) |
| vc node list multiple | `本次输出 330 条。` (exit 0) |
| aid list PT | `本次输出 2 条。` (exit 0) |
| ait list PT | `本次输出 2 条。` (exit 0) |
| air job list PT | `本次输出 2 条。` (exit 0) |
| PT ate list | 保留非零退出码，友好提示只打印一次，不出现伪造的 0 条汇总 |
| queue node usage --free | 保留原有视图，不添加 list 汇总 |
| queue list --help | 不添加汇总 |

多对象合计所有表格实际输出的记录，不是最后一个对象的数量；空范围占位行不计数。普通及 -l 输出的最后一行都是 `本次输出 N 条。`。

## 自动回归与实现

通过 `go test -race ./... -count=1`、`go vet ./...`、`git diff --check`。

- 24 个入口的领域打印器：普通、-l、空结果、有数据及告警等 77 种渲染情况，验证只生成一个最终汇总。
- 按真实命令树验证 list 及其子命令统一包装，含 ate list cluster 和 ws/wl/gw 别名；其他命令不添加汇总。
- 多对象累计、nil 结果、空范围占位行，以及限量后条数与后端匹配总数区分。
- 警告先于汇总；部分查询失败时先打印真实错误，最后标注已输出数量和查询未完整完成，保留错误链及非零退出码，main 不重复打印。
- 整次查询失败且没有结果时不打印 0 条；取消保留 context.Canceled；后续查询不继承旧计数。
- JSON 与未安装汇总包装时逐字节一致，不追加中文文字，显式不完整标记保留。

新增 `pkg/output/list_summary.go` 在结构化打印入口记录实际数据，`cmd/list_summary.go` 在列表命令结束时汇总；移除领域打印器的旧计数。不解析终端文本或按物理行数统计。JSON 捕获分支不执行表格计数。

## 构建与更新

通过 AIS 验收的 Linux ARM64 候选 SHA-256：`e1c8980a0c048a8181ae5a1198c0d09c272adb42f28b6e80f87400f3ebcf22c3`。

提交后从干净源码重建 macOS/Linux ARM64，备份并原子替换本机 `~/.local/libexec/rayctl` 和 AIS `/usr/local/bin/rayctl`，核对哈希及构建 revision，并对安装版本执行列表、空结果、JSON 和失败边界冒烟。最终构建包含提交后的 VCS 元数据，哈希与候选不同。
