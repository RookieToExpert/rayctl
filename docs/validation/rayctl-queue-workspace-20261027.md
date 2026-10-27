# queue list workspace 关联补全验收

时间：2026-10-27 18:50（UTC+8）。AIS Linux aarch64，当前默认 D 环境，平台查询均为只读。

## 行为

- RMH 索引中的队列状态不是 RUNNING，或者缺少 workspace 名称/UID 时，补查平台权威队列数据。
- 同一个 profile、subscription、resource group、region、SSP cluster 的多个缺失项使用 cluster 的队列列表批量补全；批量数据中仍然缺失的对象才逐个读取详情。单个对象直接查详情，并发上限仍为 6。
- 按原请求的完整范围及队列 UID/名称回填，避免跨租户、subscription、cluster 的同名队列串关联。
- `queue list` 与 `queue get` 共用 workspace 合并逻辑，详情在 workspace UID 缺失时也补查。删除了 `queue get` 按 queue 名拼接 workspace 的回退逻辑。
- 平台没有提供关联时保持未知；补查失败和取消保留真实错误，不能当成成功查到“无关联”。原有列表排序规则不变：先 workspace，再 queue 名。补齐 workspace 后，原先空值的行会按真实关联重新分组。

## AIS 验收

已安装旧版基线：`7c88c47`。候选二进制独立路径验收，SHA-256 为 `317195a13f73c55119054406b33579adaa9f06a33d0f19631ade7fe4f036e801`。

| 检查 | 结果 |
|---|---|
| 旧版 `queue list -o json` | 46 个队列，37 个缺 workspace；3.618 秒 |
| 新版 `queue list -o json` | 46 个队列，缺 workspace 名称 0，缺 UID 0；3.015 秒 |
| 队列集合 | profile + UID 集合与旧版完全一致，没有漏条目或新增重复条目 |
| 原有字段回归 | name/state/type/profile/subscription/resource group/region/cluster/VC/node count 共 10 个字段，46 条逐项比较差异 0；状态均为 RUNNING |
| 权威关联比对 | 46/46 的 workspace 名称和 UID 均与各自所属平台队列详情一致 |
| 批量数据核对 | 8 个 SSP cluster 的队列列表均提供完整关联，UID 与 RMH 对应；无需逐个补查 |
| 80 列终端 | 46 条表格行，所有 queue 和 workspace 都能在同一行完整找到；4.441 秒 |
| 200 列终端 | 同样 46 条完整行；5.643 秒 |
| `queue get` 名称查询 | 下表 4 个对象均与 list 的名称及 workspace UID 一致 |
| `queue get` UID 查询 | `queue-d-elastic-muxi-830pdf` 的完整 UID 查询成功，关联一致；18.032 秒（本次未优化 UID 查找耗时） |
| `queue node usage ... --free -o json` | 配额和 VC 共享候选池元数据保留，`summary.complete=true` |

关联样例均来自平台，不能从 queue 名推断：

| Queue | Workspace | get 耗时 |
|---|---|---:|
| `queue-d-elastic-muxi-830pdf` | `ws-d-muxi-830pdf` | 1.657 秒 |
| `queue-b07cf9fc` | `ws-d-jcpt` | 0.404 秒 |
| `queue-d-reserved-muxi-mohe-jcpt` | `ws-d-jcpt` | 1.485 秒 |
| `queue-2d817228` | `ws-llm` | 0.595 秒 |

上述命令均退出 0；JSON 的 `summary.complete=true`。耗时为本次快照，受平台负载影响，不作为固定性能承诺。

## 自动回归

通过 `go test -race ./... -count=1`、`go vet ./...`、`git diff --check`。

新增及调整的回归覆盖：

- RUNNING 队列缺名称、缺 UID 都补查；完整关联不单独请求详情。
- 保留旧有非 RUNNING 状态校验，以及缺元数据的队列条目。
- 关联名称与 queue 命名规则完全不一致时仍正确回填。
- 平台确无关联时，list/get 均不伪造 workspace。
- 同名队列在不同 profile、subscription、cluster 中分别回填，状态也不串用。
- 批量结果完整时只请求一次；缺失/不完整条目才单独 GET。
- 批量 403 不以大量单条请求放大压力；真实失败、身份不匹配与取消不会误报成功。

## 发布流程

验收后提交源码和记录，从干净提交分别构建 macOS ARM64 与 Linux ARM64。本机安装到 `~/.local/libexec/rayctl`，AIS 安装到 `/usr/local/bin/rayctl`，保留旧版备份，校验上传与安装后的 SHA-256。安装后再次查询正式路径的列表，并核对构建 revision、完整关联数量及命令输出。最终二进制含新提交的 VCS 元数据，哈希会与候选不同。
