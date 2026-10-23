# 2026-10-23 五项补测修复

基线 `cf9a30d`。此记录对应用户补测反馈，区分自动回归、真实二进制的本机模拟接口验收，以及未完成的 AIS 实测。

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

## 构建、安装与 AIS 状态

本机 `~/.local/libexec/rayctl` 已备份并原子替换。已从本地源码构建 Linux ARM64 版本，准备安装至 AIS `/usr/local/bin/rayctl`。

本轮 AIS SSH 多次在身份认证前失败：TCP 建立后，在 SSH version/key exchange 阶段被关闭或 reset。已使用用户提供的同一入口直接连接、间隔重试及诊断，尚未成功读取远端版本、上传新二进制或完成真实 D/PT 复验。此限制与 HTTP 403 不同，未据此修改 SSH/VPN/集群配置，也没有把上一轮的线上结果充作本轮验收。

最终交付的 Git HEAD、本机/ARM64 文件校验值与远端推送状态在交付时核对；若 AIS 恢复可达，应先部署本轮版本，再复测用户原始 AID、AIR、挂载和 metrics 命令。
