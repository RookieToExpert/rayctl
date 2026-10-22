# ATE Pod 定位复验及 R1/R3 提交核查（2026-10-22）

## 修复范围和证据边界

用户报告 `breeze-ascend-next-native-asr-1022-004` 在 `vc-a2-llm-frontier` 的 VC/HC 上有 Running Pod，但 ATE 显示 `INSPECT POD -`、`NODES -` 和证据 0/0。此前单个任务的验收不足以覆盖这一情况，原 R3-2 的完整通过结论已更正。

本次直接使用 AIS 的 `rayctl ate list -s running -n 10` 和 `ate list cluster vc-a2-llm-frontier running` 选择存活样本。原任务已不在当前 D/PT、对应 VC 和 HC 查询结果中，未将其列为复测通过，也不声称已确认它当时索引为空的具体服务端原因。

代码中可以确认的缺口：ATE 查询平台 Pod 索引后，空集合被直接用于结果和诊断；HTTP 200 的空结果没有触发 HC 查找。传入任务名或 Pod 名最终都会进入这条路径。新增可控 API 回归在旧代码上复现 `inspect="-" nodes=[] pods=[]`；索引返回 403 时也直接中止。

## 本次实现

- 平台索引为空或查询失败时，在已经映射到该 VC 的 HC namespace 查询 Pod；不扫描所有 HC namespace。先查询 Volcano 标签，无匹配时在该 namespace 使用每页 500 条的 Kubernetes continue 分页，并按 Job owner UID 和名称核对；支持 vcluster 的 owner-references 注解以及原生 ownerReferences。
- 同时检查 VC、逻辑 namespace 和 Job UID，拒绝同名旧 Job、其他 namespace 和其他 VC 的 Pod。保留实际 HC 名称，转换逻辑 Pod 名、namespace 和 UID，避免用 HC UID 冒充 VC UID，也支持 HC 名称经过转换的镜像 Pod。
- 回退有 3 秒预算，服从外层取消，拒绝循环分页游标。恢复的 Pod 同时用于 INSPECT POD、NODES、容器状态、当前/上次日志和挂载。
- Running 任务仍没有匹配 Pod 时，明确提示“容器状态与日志尚未核实”，同时保留 Pod 查询警告；不再只展示无说明的 0/0。

## 实测

正常 CLI：5 个当前 Running 任务、8 个入口通过 `-l --no-image --timeout 10s`；每次均有 INSPECT POD、NODES、1/1 证据和 Running 容器，应用日志均为 30 行。耗时均小于 2 秒。

| 当前任务 | 验证入口 |
| --- | --- |
| qwen38-27b-mm16-video1 | 任务名、master-0 Pod 名 |
| ddumm-a | 任务名、master-0 Pod 名 |
| qwen38-prefix-20261020 | 任务名、master-0 Pod 名 |
| breeze-tts-d-1021 | 任务名 |
| qwen38-27b-c32 | 任务名 |

真实 HC 回退：独立只读探针先从 VC 直接读取实时 Job UID，再显式进入空索引的同一恢复函数，并调用相同的证据采集器。没有修改平台索引或集群数据，未将它表述为线上自然复现的空索引故障。

| 样本 | 恢复 Pod / 证据 | 节点 | 应用日志行数 | 挂载总数（含 init） |
| --- | --- | --- | --- | --- |
| qwen38-27b-mm16-video1 | 1 / 1/1 | host-10-140-213-5 | 30 | 12 |
| ddumm-a | 1 / 1/1 | host-10-140-214-42 | 30 | 15 |
| qwen38-prefix-20261020 | 1 / 1/1 | host-10-140-214-167 | 30 | 12 |

扩展探测中，`lgl1-copy` 在 list 与候选版 get 之间转为 Aborted，未算作 Running 验收；`vcjob-r8pzp` 在补修前的 20 秒查询预算内超时，未算作通过，也没有将该超时归因于本次空索引问题。

当前样本重启数为 0，真实上次日志未产生；403、上次 OOM 日志、无标签分页、HC 名称转换、namespace 映射、Job 重建、跨 VC 排除、缺 Pod 警告和取消由回归测试覆盖。完整工作区及仅含本次暂存改动的独立源码均通过 `go test -race ./...` 和 `go vet ./...`；原生/Linux ARM64 构建通过。

## 提交和旧工作区

| 范围 | 已有独立提交 | 核查结果 |
| --- | --- | --- |
| R1 | `0473485`、`0e88bd1`、`672f023` | 修改文件与开工前的 118 个脏文件无重叠 |
| R3 | `b334e6f` | 独立提交，38 个文件；与原脏文件重叠的 11 个文件按差异暂存，没有整体加入旧改动 |
| IAM 后续修复 | `43e9a70` | 独立于 R1/R3 的授权预检分页修复 |

核查依据是各提交的 `git show`、当前 `git diff`，以及 `/private/tmp/rayctl-r123-20261022` 的原始 patch、未跟踪文件归档和 SHA-256 清单。R3 对 Node 输出额外保留了兼容调用签名，没有把原有 Node workload 功能整段提交。原 118 个路径仍保留为未提交改动；相对于本轮开工快照，本轮没有改动这 118 个文件的任何字节。

## 安装和记录

本轮修改仅限 ATE Pod 定位、证据映射、对应测试及验证记录；已原子更新本机 `~/.local/libexec/rayctl` 和 AIS `/usr/local/bin/rayctl`，保留旧版本备份。AIS 正式安装路径再次复验两个任务名和一个 Pod 名，3/3 通过，均有 1/1 证据、节点、容器状态和应用日志。

| 安装版本 | SHA-256 |
| --- | --- |
| Darwin ARM64 | `5c78a652aa1bb085ae093f074ab3f7dc1812cd3f4bcfbb7fa88072a5847661c2` |
| Linux ARM64 | `78f117840b1c7ff4ea591cd2c0bff45fdcdaeedee2100ada4e2b756644b28843` |

快照、脱敏 CLI 统计、真实 HC 探针日志和构建产物位于 `/private/tmp/rayctl-ate-pods-20261022/`。未向仓库保存 AK/SK、kubeconfig 或容器日志正文。
