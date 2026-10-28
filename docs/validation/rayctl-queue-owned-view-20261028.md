# queue node usage 默认归属范围与输出精简验收

验收时间：2026-10-28 17:38–17:40（UTC+8），AIS Linux aarch64，D 平台及 `vc-muxi-550pl-ailab`；查询只读。基线：已安装的 `558b2a4`。

## 行为

- 默认只显示 ELASTIC 本队列已占用节点，包含资源已满的节点，不再展示整个 VC。
- `--free` 继续显示本队列有余量节点及公共池未归属节点，排除其他队列碎片和未知归属。
- 固定口径解释移至 `--help` 和文档；正常输出只保留配额、节点标题、简短计数和表格。真实警告与错误保留。
- 剩余配额仍由平台队列详情提供，两种节点视图的配额逐字段一致。
- 完整 VC 池使用 `rayctl vc node usage <vc>`；其他类型队列及 `queue node list`、workspace/VC 查询范围不变。

## AIS 对照

```bash
rayctl queue node usage queue-d-elastic-muxi-830pdf -o json
rayctl queue node usage queue-d-elastic-muxi-830pdf --free -o json
NO_COLOR=1 COLUMNS=80 rayctl queue node usage queue-d-elastic-muxi-830pdf
NO_COLOR=1 COLUMNS=200 rayctl queue node usage queue-d-elastic-muxi-830pdf --free
rayctl vc node usage vc-muxi-550pl-ailab -o json
```

新旧命令前后独立读取 VC 的全部 Node/活跃 Pod，315 个平台池节点的归属前后稳定。以实际队列 UID 对照，本队列节点集合完整匹配，行序保持原始节点顺序。

| 检查 | 结果 |
|---|---|
| 旧默认输出 | 完整 VC，315 台，3.597 秒 |
| 新默认输出 | 仅本队列占用，162 台，4.113 秒 |
| 默认是否误用空闲筛选 | 没有；158 台资源已满节点仍显示 |
| 公共池未归属 / 其他队列 / 未知 | 89 / 63 / 1 台，均不出现在默认列表 |
| 新旧 `--free` | 均为相同的 93 台：本队列 4 台、公共池 89 台 |
| 新 `--free` 耗时 | 4.021 秒 |
| 完整 VC 独立命令 | 315 台，0.928 秒 |
| 两个 ELASTIC 队列默认 / `--free` | 185 / 184 行；逐行 context scope 和队列 UID 均正确 |
| EXCLUSIVE 默认 / `--free` | 均为 1 台，与旧版节点集合一致 |
| 80/200 列，默认及 `--free` | 标题和计数正确，配额在节点表之前，没有固定解释段 |

本次剩余配额：47 节点、61 CPU 核、106463 GiB 内存、382 张卡。统计随实时任务变化，不能当作固定常量。

输出摘要：

```text
本队列占用节点 [queue-d-elastic-muxi-830pdf] / VC [vc-muxi-550pl-ailab]
警告：1 个节点归属无法确认，已排除
共 162 台。

本队列候选节点 [queue-d-elastic-muxi-830pdf] / VC [vc-muxi-550pl-ailab]
警告：1 个节点归属无法确认，已排除
共 93 台：本队列 4，公共池未归属 89。
```

现场未知节点为 `host-10-12-139-10`：平台分配仍为 4 CPU 核、8 GiB，活跃 Pod 仅见 `kube-system/coredns-*`，未见队列归属证据。按原有保守规则排除，不伪装为未归属公共节点；旧版 `--free` 也报告同一个未知节点。

这些查询退出码均为 0；默认和 `--free` 的 JSON `summary.complete=false`，原因是上述未知归属，且全局仅有一条对应警告，没有查询错误。独立 VC 用量命令 `summary.complete=true`。验收同时检查未知计数与 incomplete 状态一致，没有为通过验收而隐藏警告。

默认 JSON `scope=queue_owned_nodes`、`free_only=false`、`ownership.eligible_nodes=162`，所有返回项 `ownership=owned`。`--free` 保持 `scope=queue_owned_or_unassigned`、`free_only=true`。`node_count=315` 继续表示筛选前原始池数量，完整归属统计保留在 metadata 中。

## 回归与发布

通过 `go test -race ./... -count=1`、`go vet ./...`、`git diff --check`；收紧失败场景输出断言后，队列表格测试再次通过。

新增覆盖默认保留满载节点、排除公共池和其他队列、默认归属 403/缺 UID 时保留配额且不回退整池、JSON scope、输出无固定解释、真实警告和错误、`--free` 简短分类计数。原有完整分页、身份匹配和未知归属保护测试继续通过。

通过 AIS 验收的 Linux ARM64 候选 SHA-256：`0f16cb6cdc881088a6dd0fd900ef45be22cb0f1af696802ce5d641008863ffbd`。

发布时从干净提交重建 macOS/Linux ARM64，备份并原子替换本机 `~/.local/libexec/rayctl` 与 AIS `/usr/local/bin/rayctl`，核对 SHA-256 和构建 revision，并对已安装版本再次执行真实命令冒烟。
