# list 输出条数汇总

所有列表命令的表格输出最后统一显示：

```text
本次输出 12 条。
```

计数取筛选、`-n/--limit` 等限量之后实际输出的数据条目。普通与 `-l` 计数一致；空结果显示 `本次输出 0 条。`，不把表格中的 `-` 占位行算成一条数据。后端匹配总数、节点容量、任务实例数不用于代替本次输出条数。

一次查询多个 queue、VC 或 workspace 时，所有对象输出完后合计一次。同一节点在两个对象下各显示一次，计为两条输出；空对象的占位行不计数。

## 覆盖命令

| 类别 | 命令 |
|---|---|
| 用户与策略 | `user list`、`group list`、`policy list` |
| 存储与网络 | `afs list`、`vpc list`、`subnet list`、`natgw list` |
| 任务 | `aid list`、`ait list`、`ate list`、`ate list cluster`、`air job list`、`air gateway list` |
| 集群与队列 | `cluster list`、`workspace list`、`vc list`、`queue list` |
| 节点与 ECS | `node list`、`queue node list`、`vc node list`、`ecs list` |
| 工作负载 | `queue workload list`、`vc workload list`、`workspace workload list` |

共 24 个入口；`ws`、`wl`、`gw` 等别名执行同一逻辑。`ate list cluster` 也属于列表命令。

node/ECS 被截断时，原有的匹配总数和查看全部的提示仍保留在汇总前；末行始终是本次输出条数。

## 警告与错误

警告和错误先打印，条数汇总最后打印。命令返回查询错误且有已确认的部分结果时，保留数据和非零退出码，末行标明：

```text
本次输出 3 条（查询未完整完成）。
```

结果对象明确标记查询不完整时，同样使用此标记，不改变原命令的退出码规则。整次查询失败且没有得到结果时只打印真实错误，不伪装成“输出 0 条”。取消仍返回取消错误。

`-o json` 保留原有 JSON 结构及错误、警告、数量字段，不追加中文汇总。`get`、`usage`、`check` 等其他命令的输出范围不变。

## 开发约定

根命令安装列表汇总包装，覆盖名为 `list` 的命令及其子命令。现有列表打印器在 `captureJSON` 的结构化入口记录实际结果；追加空表占位行、生成列和折行发生在记录之后。列表结果可以是对象切片、含 `Items` 的结果对象、多个范围结果的切片，或 node list 使用的 `items` map。

新增列表打印器继续使用这一结构化入口；不解析终端文本或按绘制出的行数计数。一个列表命令只生成一个最终汇总，不在领域打印器中另加一份计数。

实际命令及 D/PT 前后对照见 [AIS 验收记录](validation/rayctl-list-summary-20261028.md)。
