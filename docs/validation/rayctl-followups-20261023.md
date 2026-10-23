# 2026-10-23 五项补测修复

基线 `cf9a30d`。此记录对应用户补测反馈，区分自动回归、真实二进制的本机模拟接口验收，以及 AIS D/PT 只读实测。

后续已定位本记录中两个 D 测试 workspace 的 403 来源，并统一 AID/AIR 默认查询范围；以下保留当时实测，最新行为见 [workspace 范围与输出验收](rayctl-workspace-scope-json-created-20261023.md)。

| 项目 | 提交 | 实现 |
|---|---|---|
| 1. AID 命中仍因 403 失败 | `a2856f2` | 有唯一命中时退出 0；未完成范围合成一行警告，403 只显示 region/workspace 和状态，保留 typed cause |
| 2. AIR list 丢掉部分错误 | `7bc66e0` | 任一 workspace 可读时保留结果并警告；全部失败、取消仍失败；成功空列表与全部失败分开处理 |
| 3. AIR 重复行防护 | `e2a10f3` | 合并后按 UID 去重、保留较新快照，再筛选/排序/限条数；同名不同 UID 保留 |
| 4. 挂载表和 no-image 噪音 | `93b0075` | VOLUME 等于 SOURCE 时省掉重复标识，隐藏全空列，保留不同别名、mount/subPath/只读证据；TYPE 不折行；显式 no-image 不产生跳过警告 |
| 5. GPU 相同缺口重复告警 | `6781f7e` | 按缺口起止去重并汇总受影响卡序列，相同覆盖与缺口合成一行；不同卡的独有缺口保留 |

## 查询状态原则

- AID 已有唯一命中：返回对象和退出码 0，其他范围失败写入一行警告。环境歧义和父 context 取消仍失败。此策略仅接入 AID；未改变 Pod 查询的现有返回约定。
- AID 没有命中且所有范围成功：明确未找到，并列出查询环境。
- AID 没有命中且部分范围 403/失败：提示“可访问范围内未找到”和未完成范围，退出非零；不把未授权区域当作不存在。
- AIR list 至少有一个 workspace 查询成功：保留成功列表（也允许空列表）并汇总其他失败，退出 0。没有任何成功范围时退出非零。
- JSON 保留成功数据与警告；AID `query_incomplete=true`、列表 `summary.complete=false` 和 metadata 中的标记保留未完成范围。不能用退出 0 推断全量扫描完整。
- AIR UID 去重发生在状态筛选和 CLI limit 之前。相同 UID 的较新 update_time 优先，避免旧状态参与筛选。JSON metadata 的 `duplicates_removed` 在实际移除记录时提供数量。同名不同 UID、不同 workspace 的无 UID 对象不会仅因名字相同而合并。

没有取得用户那次“17 行而 JSON 16 条”的原始 UID 和响应快照，因此没有断言已定位该次偶发重复的根因。新增防护和可观测计数覆盖单页重复、分页交界重复和别名 profile 重复。

## 自动回归

`go test -race ./... -count=1`、`go vet ./...`、`git diff --check` 均通过。

- AID：跨环境命中加两个 403、同环境命中加部分失败、成功/不完整的未命中、歧义、取消；警告一行、不含 URL 或原始 PermissionDenied JSON，typed HTTP status 仍可追踪。
- AIR：有数据加两个 403、可读范围为空、筛选后为空、全部 403、取消；部分成功的 JSON 无 errors 但 complete=false。
- 去重：单页重复按最新状态筛选、跨页交界重复、同名不同 UID、多 workspace/region 及 alias profile；记录去重数量。
- 表格：80/200 列下完整 PVC 仅显示一次、不同 ConfigMap 别名和源都保留、ConfigMap 连续显示、subPath/只读未丢失；原有 JSON 完整性测试继续通过。
- no-image：仍保留未知架构，不把未知判成不匹配；不再发出逐容器“image lookup skipped”警告。
- GPU：两个相同序列的覆盖/缺口仅一行；覆盖率不同但缺口相同仍只列一次该时间段；公共缺口、专属缺口及稳定排序；逐卡 coverage/gaps 未被合并或删掉。

## 命令级本机验收

以本次构建的 macOS 二进制运行，所有 HTTP/Kubernetes 请求指向本机模拟 D/PT 服务，无真实集群请求。D 含两个固定 403 workspace，PT 返回一个匹配 AID，AIR API 返回同一 UID 的两条数据。

| 命令场景 | 退出码 | 结果 |
|---|---:|---|
| AID 命中 PT、D 部分 403，表格 | 0 | 一行 Warning，无 URL |
| 同场景 JSON | 0 | environment=pt，query_incomplete=true，errors 为空 |
| AID 无匹配且 D 部分 403 | 1 | 明确限定“可访问范围内未找到”，无 URL |
| AIR list -n 1，JSON | 0 | 一条唯一数据，duplicates_removed=1，complete=false |
| AIR list -n 1，表格 | 0 | 一条数据、一行 Warning，无 URL |

这验证实际命令的退出码、输出封装和 limit 顺序，不能代替 AIS 真实接口验收。

## AIS D/PT 只读实测

SSH 起初在身份认证前被 reset；按用户“再试试”重连后恢复。已备份远端旧版本，安装本地源码构建的 Linux ARM64 版本至 `/usr/local/bin/rayctl`，SHA-256 与本地构建产物一致。以下在同一 AIS 上用旧版 `cf9a30d` 和新版执行，只读查询，没有修改 workspace 权限或集群资源。

| 实测 | 结果 |
|---|---|
| `aid get dev-yifan-gpu2 -e all` | PT 命中，表格退出 0，1.785s；stderr 仅一行部分查询警告，包含 D 两个测试 workspace 的 403，不含 URL |
| 同命令 `-o json` | 退出 0，0.506s；environment=pt、query_incomplete=true、errors=[]，保留未完成范围 |
| `aid get rayctl-verified-absent-followups-20261023-b3nw2tm -e all -o json` | 退出 1，0.429s；明确“可访问范围内未找到”，列出两个无权限 workspace，不含 URL，没有把部分扫描说成完整不存在 |
| `air job list -e d -n 1000 -o json` | 退出 0，412 条、412 个唯一 UID。本次 AIR workspace 清单不包含两个测试 workspace，显式指定各自返回 workspace not found；因此此次线上没有触发 AIR 的部分 403 分支，该分支由本机命令级模拟和 service/JSON 回归覆盖 |
| `air job list -e d -w ws-d-a3-trustrouter -n 1000`，表格与 JSON 各连续 3 次 | 每次都退出 0；各为 21 条，JSON 21 个唯一 UID，名称集合完全一致。包含用户原始 `infer-glm-53-flash-w8a8c16-zjw1`；没有复现原先偶发重复，未把本次无重复等同于已确认其根因 |
| 上述 AIR 任务 `get -l --no-image`，200 列终端 | 主挂载表从 212 列降至 126 列；3 条 PVC 行重复的 VOLUME/SOURCE 单元格从 3 处降至 0，ConfigMap 表从 182 列降至 139 列；跳过镜像警告从 3 条降至 0 |
| 上述 AIR 任务，80 列终端及 JSON | 旧版 ConfigMap 被折行，新版完整显示；旧新版 JSON 均保留 9 项挂载（含 3 项 PVC），名称、source、path、subPath、read_only 证据完全一致 |
| `aid get dev-yifan-gpu2 -e pt -l --no-image` | 挂载表从 167 列降至 157 列，镜像跳过警告从 4 条降至 0；不同的 VOLUME 别名仍保留 |
| PT `metrics gpu dev-5dc5d665 -w ws-t-vcg -k ~/kubeconfigpt --start '2026-10-22 12:00:00' --end '2026-10-23 11:00:00' -o json` | 旧新版都退出 0；两张卡各 395/5521 点，覆盖约 7.2%，共同断档为 10-22 12:00:00 至 10-23 09:21:23（UTC+8）；覆盖/断档从 4 行合并为 1 行，另保留原有一行采样间隔提示；逐卡 coverage、series、samples 完全一致 |

耗时为当次观测，不作为固定性能承诺。表格宽度统计仅针对挂载表；标识符保持完整时可超过终端宽度。

## 最终构建与安装

本机 `~/.local/libexec/rayctl` 和 AIS `/usr/local/bin/rayctl` 均以最终干净 Git HEAD 重新构建、原子替换，并核对 SHA-256；旧版本备份保留。最终 Git 提交、文件校验值与远端推送状态在交付时再核对。
