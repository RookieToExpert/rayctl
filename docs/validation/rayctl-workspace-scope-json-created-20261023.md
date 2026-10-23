# 2026-10-23 workspace 查询范围、JSON requested 与 CREATED 验收

基线 `36b6554`。以下保留当时三项提交与验收结果。

后续 AIT 已统一平台 workspace 范围，所有 list -l 已改为增量列；以下保留当时对照，最新列及宽度见 [AIT 与 UX-04 验收](rayctl-ait-workspace-ux04-20261023.md)。

| 项目 | 提交 | 结果 |
|---|---|---|
| AID/AIR workspace 范围 | `d0bbb7b` | AID 默认与 AIR 共用平台 RMH workspace 枚举；保留不同 subscription 的同名 workspace；AID/AIR 保留部分 profile/分页失败 |
| JSON summary.requested | `2f34dbd` | 有 limit、无位置参数的 list 使用条数上限；SSP 时间窗使用实际解析的上限；多目标查询保留位置参数数量 |
| AIR list -l CREATED | `0fa83fa` | 日期时间单元格不折行、不截断，在窄终端保留完整年月日和时分秒 |

## workspace 范围核对

AIS 上同时核对平台 RMH 原始分页、`workspace list -e d/pt -o json` 与对应 HC kubeconfig 的 namespace 标签。只读查询，没有修改数据库、平台资源、权限或 kubeconfig。

| Region | Profile | RMH 返回条数 | total_size |
|---|---|---:|---:|
| D | ailabdev | 28 | 28 |
| D | pjailab | 2 | 2 |
| D | Interndiscovery、Internstudio | 各 0 | 各 0 |
| PT | ailabdev-pt | 48 | 48 |
| PT | pjailab-pt | 2 | 2 |
| PT | Interndiscovery-pt、Internstudio-pt | 各 0 | 各 0 |

这 8 个 profile 均成功，原始资源无重复，返回条数与 total_size 一致；本次各 profile 均一页。平台清单 D 共 30 个 workspace、PT 共 50 个。

D HC 的 workspace 标签清单有 29 个名称，其中 27 个与平台相交，另外两个是：

- `vcluster-37a8eec1-717a533f` → `skip-hostpath-webhook`
- `vcluster-37a8eec1-a38dbbfe` → `skip-webhook-test`

两个 namespace 均为 Active、没有删除标记，但没有 tenant-id/subscription-id 标签，也不在任何上述 profile 的 RMH 返回中。仅凭 namespace 标签，不能认定它们属于当前配置的平台查询范围。此次没有查询数据库，因此不据此断言它们在数据库中是否存在。

旧 AID 在未找到匹配 Pod 时，将 HC namespace 标签与 RMH 清单合并，再使用默认 subscription 查询两个 HC 独有名称，触发 403；AIR 只遍历 RMH，所以不包含它们。原先是 AID 额外扩大了范围，没有发现 AIR 遗漏当前配置可见的 RMH workspace。D 另有 3 个、PT 另有 2 个平台 workspace 不在 HC 标签清单中，它们仍保留在默认查询范围，不能用 HC 标签反向过滤平台清单。

修复后，AID 默认只按 RMH 的 subscription/profile/workspace 独立范围查询。Pod 和 HC 标签继续参与定位、环境推断和诊断；显式 `aid get --workspace <name>` 仍允许直接查询未列入 RMH 的名称。同名不同 subscription 不再按名字合并；清单中任一 profile 或分页失败会进入不完整状态，不能在缺少范围时报告完整“未找到”。AIR list 也保留成功范围及这些错误，JSON complete=false。其他平台清单调用者保留既有 best-effort 返回约定。

## AIS 命令前后对照

使用同机备份的 `36b6554` 和本轮二进制，所有查询均只读。JSON/表格可能包含业务配置，验收脚本仅输出白名单字段和统计，不打印镜像凭据或配置密钥。

| 命令/场景 | 旧版 | 新版 |
|---|---|---|
| `aid get dev-yifan-gpu2 -e all -o json` | 退出 0，query_incomplete=true，包含两个 403 的一行警告 | 退出 0，0.489s；PT 命中，无不完整标记、无部分查询警告、errors=[] |
| `aid get rayctl-verified-absent-scope-20261023-2krdnakx -e all -o json` | 退出 1，报“未完整完成”及两个 403 | 退出 1，0.446s；明确“未找到（已查询 d/cn-pj-01, pt/cn-pj-03）” |
| 将上面现有 AID 的 UID 用于 `get -e all -o json` | — | 退出 0，1.334s；命中同一 name 和 UID |
| `ait list -e d -o json` | requested=0，返回 50 | requested=50，返回 50 |
| AIT 同命令加 `-n 3` / `--limit 7` | requested 都为 0，分别返回 3/7 | requested 分别为 3/7，返回数量仍为 3/7 |
| `air job list -e d -w ws-d-a3-trustrouter -o json` | requested=0，返回 21 | requested=50，返回 21 |
| AIR 同命令加 `-n 3` / `--limit 7` | requested 都为 0，分别返回 3/7 | requested 分别为 3/7，返回数量仍为 3/7 |
| AIR 同 workspace `list -l -n 3`，COLUMNS=80 | 3 条记录没有任何完整日期时间单元格 | 3 条记录均完整显示 `YYYY-MM-DD HH:MM:SS` |

宽表为保留标识符和完整日期可超过终端宽度；本次 80 列设置下实际表宽由 134 变为 145，不以破坏字段内容换取整表宽度。测得耗时是当次观测，不作为固定性能承诺。

## 自动验证与安装

`go test -race ./... -count=1`、`go vet ./...`、`git diff --check` 均通过。

- 比较 AID/AIR 的 subscription/profile/workspace 范围；覆盖 HC 独有 namespace、平台独有 workspace、同名不同 subscription、显式 workspace 直查、完整未命中与清单失败。
- 覆盖一个 profile 成功另一个 403、短页带 next token、大于 200 条的清单、缺失/重复 token。部分清单仍保留成功数据，同时标记不完整；错误保留 typed HTTP cause，403 不打印原始 URL。
- 运行实际 Cobra AIT/AIR 命令验证默认/短长 limit、空列表、AIT 窗口默认 5000 和显式覆盖；多对象 get 的 requested 仍为目标数量。
- 80/120/200 列下断言 CREATED 和对应任务名在同一行且完整出现一次。

最终从干净 Git HEAD 构建本机及 Linux ARM64 文件，分别替换 `~/.local/libexec/rayctl` 与 AIS `/usr/local/bin/rayctl`，保留旧版备份并核对 SHA-256。交付前核对远端 main 与本地 HEAD 一致。
