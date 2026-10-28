# list -l 增量列

`-l/--long` 表格只显示主标识和默认表没有的列。先运行默认命令看概况，再加 `-l` 看新增信息；使用相同的筛选和条数参数时，两张表保持相同行序。`-o json` 保留完整对象，不应用表格投影。

普通和 `-l` 视图最后都显示 `本次输出 N 条。`，实际条数相同，空表占位行不计数。多对象合计和错误边界见[列表条数汇总](list-output-summary.md)。

| 命令 | 主标识 | -l 新增列 |
|---|---|---|
| `air job list` | NAME | QUEUE、CREATOR、CREATED |
| `aid list` | NAME | NODE、RESOURCE、CREATED |
| `user list` | USERNAME | ID、TENANT CODE、CREATED |
| `group list` | GROUP | ID、POSIX、TENANT ID、CREATED |
| `afs list` | AFS | TENANT |
| `vpc list` | VPC | TENANT |
| `subnet list` | SUBNET | TENANT |
| `natgw list` | NATGW | TENANT |
| `ecs list` | NAME | TENANT、UID、MACHINE、VPC |
| `node list` | HOSTNAME | MODEL、PROD、QUEUE、TENANT |
| `queue node list` | HOST | ACN UID |
| `vc node list`，含多 VC | HOST | MODEL、ACN、ACN UID |

队列节点的 MODEL、machine type 已在默认表中，所以不在增量表重印。VC/queue 的默认概况表也不在 `-l` 重印；ELASTIC 共享候选池仍保留一行范围说明。

AIR/AID 的 NAME 在所查范围内重复时，才在第二列加入 WORKSPACE。如果同名且 workspace 名也相同（例如不同 subscription），第二列改用 UID，避免误认。其他命令的新列已包含资源 UID 或租户信息，不额外重复默认上下文列。

ECS 将 NAME 调整为默认表第一列，TYPE 移到第二列；多 VC 节点列表将 HOST 置首、VC 放第二列。两处调整让默认表和增量表都以实际资源为主标识，不改变数据排序。多 VC 增量表已有 ACN UID，可以区分节点，无需重复 VC 列。

名称、UID、资源引用、租户标识和日期时间保持完整，不截断、不折行。很长的名称、UID 或多个标识相加可让表超出终端宽度；优先保证可以复制定位。

```bash
# 概况和增量使用完全相同的筛选及 limit。
rayctl air job list -e d -w ws-d-a3-trustrouter -s RUNNING -n 5
rayctl air job list -e d -w ws-d-a3-trustrouter -s RUNNING -n 5 -l
# 默认：NAME STATE READY WORKSPACE
# 增量：NAME QUEUE CREATOR CREATED

rayctl aid list -e pt -n 5
rayctl aid list -e pt -n 5 -l
# 默认：NAME STATE WORKSPACE QUEUE CREATOR RESTARTS
# 增量：NAME NODE RESOURCE CREATED

rayctl user list -n 5 -l
# USERNAME ID TENANT CODE CREATED
rayctl group list -n 5 -l
# GROUP ID POSIX TENANT ID CREATED
rayctl afs list -l
# AFS TENANT
rayctl ecs list -n 5 -l
# NAME TENANT UID MACHINE VPC
rayctl queue node list <queue> -l
# HOST ACN UID
rayctl vc node list <vc-a> <vc-b> -l
# HOST MODEL ACN ACN UID

# JSON 仍包含状态、workspace 等默认字段。
rayctl air job list -e d -n 5 -l -o json
```
