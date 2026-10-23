# VC 控制面规格：数据来源与命令

2026-10-23 在 AIS 通过 D/PT HC 和 ECP 运维服务完成只读核对。查询实现 `dbc1a30`，更新实现 `926ee79`，Dcloud 查询兼容补漏 `cd407c6`。本轮没有连接数据库、读取数据库密码或执行真实规格变更。

## 使用方式

```bash
rayctl vc get vc-a2-discovery-prod -e d
rayctl vc config get vc-p-cluster -e pt -o json
rayctl vc config update vc-a2-discovery-prod --flavor large --dry-run -e d
```

实际提交去掉 `--dry-run`，默认展示差异并交互确认；`--yes` 跳过确认。默认等待收敛，`--timeout` 默认 2m，`--wait=false` 只等待提交结果。相同规格不会发送 PATCH；仍可等待尚未完成的下发。JSON 真实提交须显式 `--yes`；dry-run 不要求。

`vc get --platform-only` 保留原有仅查询普通平台接口的行为，不访问 HC 或运维接口。`-e all` 下按命中对象的 region 选择 HC；默认 D 配置 `~/kubeconfig`、PT 配置 `~/kubeconfigpt`，可用 `RAYCTL_HC_KUBECONFIG_D/PT` 覆盖。Dcloud 的规格接口未验证，普通 `vc get` 保留原有 HC 映射并提示未采集规格，`config` 不猜测支持方式。

## 平台返回的规格定义

以下是 D/PT 当次 `GetVirtualClusterConfig` 返回值，包含单副本资源，不能把副本数等同于 size：

| flavor | 副本 | CPU request / limit | 内存 request / limit | 临时存储 request / limit | 节点上限 |
|---|---:|---|---|---|---:|
| minimum | 1 | 2 / 2 | 8Gi / 8Gi | 16Gi / 16Gi | 15 |
| small | 1 | 2 / 2 | 8Gi / 8Gi | 16Gi / 16Gi | 30 |
| medium | 3 | 8 / 8 | 32Gi / 32Gi | 64Gi / 64Gi | 150 |
| large | 3 | 32 / 32 | 128Gi / 128Gi | 256Gi / 256Gi | 500 |
| unlimit | 3 | 32 / 无 | 128Gi / 无 | 256Gi / 无 | 2000 |

代码从服务端读取定义，不内置这些资源值。`unlimit` 没有资源 limits，不建议使用；默认拒绝，只有额外提供 `--allow-unlimit` 才允许。

## 权威期望、已下发声明与实际资源

普通 ECP VC get/list 响应没有明确的 flavor 字段。持久期望值由 **ECP OperationService** 返回；HC 的 VCluster2ControlPlane 标签表示已经下发的规格：

```text
controlplane.cluster.x-k8s.io/v1alpha2/vcluster2controlplanes
metadata.labels["resource.compute.sensecore.cn/vc-flavor"]
```

ControlPlane 名称和 namespace 均为 `vc-<VC UID>`。声明资源位于 `spec.controlPlane.statefulSet.resources`，副本位于 `spec.replicas`。字段名虽然包含 statefulSet，当次业务控制面实际由同 namespace、同名称的 **Deployment** 承载，其 ownerReference 指向对应 VCluster2ControlPlane，主容器名为 `syncer`。

命令直接 GET 这两个对象，避免扫描整个集群 Pod。没有 flavor 标签时显示未知；平台期望接口没有权限时保留 HC 标签、实际资源和明确警告。minimum 与 small 的 CPU/内存相同，medium 与 large 同为 3 副本，均不能反推规格。

真实交叉验证：

| 环境 / VC | UID | 平台 / HC | Ready | 实际单副本 CPU / 内存 | Deployment generation |
|---|---|---|---|---|---:|
| D / vc-a2-discovery-prod | `019d4c85-01c5-7963-b7f7-15f849a934f3` | medium / medium | 3/3 | 8 / 32Gi | 25 |
| PT / vc-p-cluster | `019e90cf-65ff-771a-b3fc-c14aa61c8eae` | large / large | 3/3 | 32 / 128Gi | 17 |

D 曾枚举到 31 个带 flavor 的业务 ControlPlane（medium 15、small 3、large 6、minimum 7），PT 8 个（large 3、minimum 5）；不把没有该标签的 `prod-lepton/karmada` 算作业务 VC。这些清单是当次观测，不是固定总数。

## 持久 API 契约

通过只读 gRPC reflection 核对服务：

```text
interior.sensetime.core.compute.ecp.v1.OperationService
GetVirtualClusterConfig
GetVirtualCluster
UpdateVirtualCluster
```

服务为 HC `prod-lepton/lepton-ecp-operation`，HTTP 8080、gRPC 9090。公网管理网关没有开放当次 interior 路由，命令通过已有 HC kubeconfig 的 Kubernetes Service proxy 访问：

```text
/api/v1/namespaces/prod-lepton/services/http:lepton-ecp-operation:8080/proxy/compute/ecp/interior/v1

GET   /virtualClusterConfig
GET   /virtualClusters/{UID}
PATCH /virtualClusters/{UID}?update_mask=control_plane_flavor&request_id={UUID4}
```

PATCH 请求体仅包含可写字段：

```json
{"control_plane_flavor":{"name":"medium"}}
```

服务端 schema 将 replicas、resources、max_node_count 标为 output-only；不发送它们。反射的权限标注为 `compute.vcluster.get`、`compute.vcluster.update`，同时需要对应 HC Service proxy、ControlPlane 和 Deployment 的访问权限。接口经 GET 真实核验，PATCH 由测试 HTTP 后端验证精确路径、field mask、UUID4、载荷和错误；本轮未向生产发送 PATCH。

每次提交生成新 request_id，写请求不自动重试。确认之后会重新读取当前期望值及目标规格定义，发现变化即中止。接口没有条件写入令牌，因此这只是提交前检查，不能声称具有原子 CAS。网络失败或响应无法确认时提示通过 `vc config get` 核对，保留真实错误。

## 收敛检查与边界

“请求已接收”与“已收敛”分开显示。只有以下条件同时满足才报告收敛：

1. 平台期望 flavor 与 HC 标签均等于目标。
2. ControlPlane 声明副本、requests/limits 与平台目标定义一致。
3. 经 ownerReference 核实的实际 Deployment 容器资源和期望副本一致；资源 quantity 按数值比较，如 `8` 与 `8000m` 相等。
4. observedGeneration 覆盖当前 generation，total、updated、ready 均等于目标副本数，available 达标，旧副本已退出。

等待超时或取消保留最近一次观测和“尚未确认收敛”状态。D/PT 的 dry-run 前后期望规格及 Deployment generation 均未变化。真实写入和真实滚动更新未执行，不能将契约测试或 dry-run 描述为生产更新验收。

完整回归与实测记录见 [本轮验收](validation/rayctl-next-round-20261023.md)。
