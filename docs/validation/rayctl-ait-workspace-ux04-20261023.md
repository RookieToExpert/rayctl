# 2026-10-23 AIT workspace 与 UX-04 验收

基线 `e5f6d76`。实现分开提交：AIT 范围修复 `1edb1a3`（已先行推送），全部 list -l 增量列 `1dd678d`。所有线上操作仅查询，不修改数据库、权限、工作负载或平台资源。

## AIT 默认 workspace 范围

旧 AIT 与旧 AID 共用 HC namespace 标签和 RMH 的并集逻辑，还会按 workspace 名合并不同 subscription，并在某个范围成功后吞掉其他失败。

现在 AIT/AID 共用平台候选范围规则，默认只枚举 RMH 清单，保留 subscription/profile。HC 的 namespace/Pod 仅用于定位和诊断，不添加或缩小默认平台查询范围。显式 `--workspace` 保留直接查询能力。部分 workspace/profile/分页失败不再被成功范围掩盖：唯一可读命中仍返回结果，附一行警告及 query_incomplete；无命中且有失败时保留真实错误；取消仍返回取消。

| AIS 只读场景 | 结果 |
|---|---|
| 从当前 D `ait list` 选择 `aircraft-grpo-64k-1023-r16`，`ait get <name> -e all -o json` | 退出 0，0.582s；命中同一名称，无不完整标记、无部分查询警告 |
| `ait get rayctl-verified-absent-ait-20261023-s2jxpkxo -e all -o json` | 退出 1，1.574s；明确 not found in regions cn-pj-01, cn-pj-03，无两个测试 namespace 的 403 |
| 从 PT `ait list` 选择 `samask-smk-1023-095657`，`ait get <name> -e pt -k ~/kubeconfigpt -o json` | 退出 0，0.996s；命中同一名称，无不完整标记、无部分查询警告 |

回归覆盖 HC 独有 namespace、平台独有 workspace、Pod 仅命中一个 workspace 时仍保留完整平台范围、同名不同 subscription、显式 workspace、部分成功/未命中与取消；此前 AID/AIR 的范围测试继续通过。

补测发现两项既有问题，分别使用同机备份的 `e5f6d76` 与 `1edb1a3` 复现，并非此次范围修复引入，也未记为验收通过：

- 上述 D 任务的 UID `bbacc30a-8bb9-4973-a5fa-6c1fd75cd445` 查询均失败；后端 trainingJobs 不支持 uid 过滤，旧版原始 400 的原因是 undeclared identifier uid。
- `aircraft-grpo-64k-1023-r16-master-0` Pod 名查询两版均未找到；识别出工作负载类型后，没有把 Pod 名转成平台任务名。

这两项独立记录在 [AIT-02](../rayctl-todo.md)，未在本轮扩展修复。

## 全部 list -l 的表头与表宽

通过实际 Cobra 命令树枚举，共 12 个命令；VC 多对象路径另测，合计 13 种表格。表宽为 `COLUMNS=80`、`NO_COLOR=1` 下的数据表显示宽度；不包含默认 VC/queue 的 FIELD/VALUE 概况表，该概况在 -l 中不再重印。

有 limit 的 AIR/AID/user/group/ECS 使用 `-n 5`；AFS/VPC/subnet/NATGW 使用完整默认列表。平台命令使用 `-e d`；node 使用 `-k ~/kubeconfig`、默认最多显示 100 条。VC 样本为当前 list 选出的 `vc-a2-ai4astro`（15 节点）和 `vc-a2-ai4chem2`，多 VC 合计 19 节点；queue 样本为 `queue-d-reserved-a3-trustrouter`（48 节点）。

| 命令 | 新版默认表头 | 新版 -l 表头 | 默认宽度 旧→新 | -l 宽度 旧→新 | 新版逻辑行数 |
|---|---|---|---:|---:|---:|
| air job list | NAME STATE READY WORKSPACE | NAME QUEUE CREATOR CREATED | 80→80 | 154→109 | 5 |
| aid list | NAME STATE WORKSPACE QUEUE CREATOR RESTARTS | NAME NODE RESOURCE CREATED | 115→115 | 186→87 | 5 |
| user list | USERNAME NAME STATUS SOURCE | USERNAME ID TENANT CODE CREATED | 43→43 | 107→99 | 5 |
| group list | GROUP DISPLAY NAME | GROUP ID POSIX TENANT ID CREATED | 62→62 | 132→142 | 5 |
| afs list | AFS STATE CAPACITY STORAGE CLASS ZONE CREATED | AFS TENANT | 96→99 | 107→48 | 313 |
| vpc list | VPC STATE CIDR RDMA SUBNETS NATGW REGION | VPC TENANT | 85→98 | 96→42 | 11 |
| subnet list | SUBNET STATE VPC CIDR AVAILABLE ZONE | SUBNET TENANT | 86→97 | 97→45 | 18 |
| natgw list | NATGW STATE VPC SUBNET TYPE EIPS SNAT DNAT | NATGW TENANT | 91→113 | 102→41 | 11 |
| ecs list | NAME TYPE STATE INTERNAL IP CREATOR | NAME TENANT UID MACHINE VPC | 65→65 | 189→139 | 5 |
| node list | HOSTNAME RDY SCH RPR USE IP CLUSTER | HOSTNAME MODEL PROD QUEUE TENANT | 103→103 | 138→91 | 100 |
| queue node list | HOST IP STATE MACHINE TYPE MODEL | HOST ACN UID | 77→77 | 113→61 | 48 |
| vc node list 单 VC | HOST IP STATE MACHINE TYPE | HOST MODEL ACN ACN UID | 65→65 | 92→92 | 15 |
| vc node list 多 VC | HOST VC IP STATE MACHINE TYPE | HOST MODEL ACN ACN UID | 82→82 | 109→92 | 19 |

每一项均退出 0；各自默认/-l 的主标识序列及数量一致。AIR 原始基线采集后出现了新的前五条任务，已再用备份旧二进制对新样本重测：旧/新主标识序列摘要同为 `563c4e53957f7d6e`，表中采用这组相同样本的 80/154 与 80/109 宽度。

group 的增量列更少，但表宽增加 10：旧表中的租户 ID/日期曾折行，现在按 UX-02 完整保留。AFS 默认宽度略增，是创建时间不再折行；VPC/Subnet/NATGW 默认宽度增加，是主名称及资源引用保持完整。名称、UID、日期等字段总长度超过终端时允许超宽，不用截断字段换取宽度。单 VC 节点表本来已是增量列，保留其列；去掉了重复概况。

旧网络列表的主名称折行还会制造视觉行：原基线计数脚本看到 VPC 14、subnet 23、NATGW 13 行。已进一步用旧/新二进制分别读取默认/-l JSON，确认真实记录数都是 **11/18/11**，且有序 name/uid/tenant 三元组及完整字段集合一致。新版一条主名称一行，没有删除这些记录。

## 标识与跨范围歧义

- 唯一主标识时只保留一列；STATE、WORKSPACE 等其他默认列全部移除。
- AID/AIR 同名但 workspace 不同时，第二列保留 WORKSPACE；同名且 workspace 名也相同时，第二列使用 UID，覆盖不同 subscription 的同名对象。两种分支都有回归测试，本次线上样本未触发。
- 其他资源的新增列已含租户或 UID；VC/queue 的 ACN UID 可区分节点，不额外重印默认上下文列。
- ECS 的 TYPE 是资源类型，不是行标识，因此默认将 NAME 置首、TYPE 次列；多 VC 默认将 HOST 置首、VC 次列。仅调整这两处列顺序，不改变数据排序。
- ELASTIC queue 的共享候选池仍保留一行范围说明，避免把共享节点误认为队列独占节点。

完整列定义和用户示例见 [列表增量输出](../list-incremental-output.md)。

## 自动回归、构建与推送

`go test -race ./... -count=1`、`go vet ./...`、`git diff --check` 均通过。

- 命令树测试明确覆盖全部 12 个 list -l 并检查帮助文本；以后新增该类命令会要求补充验收覆盖。
- 每个数据表分别断言默认表头和增量表头，后者等于主标识加默认表头补集；覆盖普通、空列表和多 VC。
- 80/200 列下验证长主标识、UID、资源引用及完整日期；使用倒序名称断言不重排。
- 每个命令先采集完整 JSON，再真实渲染增量表，然后再次采集 JSON；完整字段、值和 item 顺序一致。仅规范化既有 map 遍历造成无序的 missing-field warning 集合，不忽略数据差异。
- AID 的 -q/-n/时间窗口组合与 -l 同时测试；所有线上默认/-l 对照使用相同参数。

最终从干净 Git HEAD 重建本机和 Linux ARM64 文件，分别原子替换 `~/.local/libexec/rayctl` 与 AIS `/usr/local/bin/rayctl`，核对 SHA-256，并保留本轮旧版本备份；交付时核对 main、安装产物和工作区状态。
