# Rayctl Architecture

## Layers

- `cmd`: Cobra command wiring, flags, input validation, and dependency construction.
- `internal/service`: use cases, platform/Kubernetes result composition, fallback policy, and diagnostics.
- `internal/platform`: signed HTTP clients and platform API DTOs. Domain clients should live in focused files such as `ecs_client.go`, `vc_client.go`, and `network_client.go`.
- `internal/kube`: Kubernetes client configuration only.
- `pkg/output`: terminal rendering only; it must not query data or make business decisions.

Dependencies flow in one direction: `cmd -> service -> platform/kube`. Platform and output packages must not import service command code.

## Service Boundaries

Services should depend on narrow interfaces declared in `internal/service/platform_ports.go`, not on every method exposed by `VirtualClusterClient`. Add a method to a service port only when that use case needs it.

Constructors used by commands may accept the concrete platform client for convenience. Tests and alternative implementations should use the `WithPlatform` constructors.

## Query Policy

1. Prefer a single-resource API or server-side exact filter.
2. Query independent sources concurrently.
3. Use the current profile before trying other profiles.
4. Use full pagination only for fuzzy search, historical resources, or compatibility fallback.
5. Keep fallbacks explicit and covered by tests so a fast path cannot silently regress into a full scan.

## Refactoring Direction

The large-file extraction is complete. Keep new code within these domain boundaries:

- `internal/platform/virtual_cluster_client.go` retains the client structure and shared constants. Configuration and profile selection live in `platform_config.go` and `profiles.go`. IAM, compute nodes, Kubernetes proxy calls, resource lookup, CMS logs, and HTTP transport live in `iam_client.go`, `ai_compute_node_client.go`, `vc_kube_proxy_client.go`, `resource_lookup_client.go`, `cms_logs_client.go`, and `http_client.go`. HTTP retries are part of `http_client.go`; shared value helpers live in `platform_util.go`. Existing `vc_client.go` and `ecs_client.go` also own their domain DTOs.
- `pkg/output/table.go` retains generic table layout, wrapping, width, and value helpers. Domain rendering lives in `auth_table.go`, `group_table.go`, `rbac_table.go`, `policy_table.go`, `node_table.go` (including user output), `vc_table.go`, `job_table.go`, `storage_table.go`, `network_table.go`, `logs_table.go`, and `ecs_table.go`.
- `internal/service/job_service.go` retains shared resource descriptors and constants. Types and construction live in `job_types.go`; behavior lives in `job_create.go`, `job_list.go`, `job_get.go`, `job_check.go`, `job_locate.go`, `job_podgroup.go`, `job_pods.go`, `job_storage.go`, and `job_image_secret.go`. Small shared helpers live in `job_util.go`, using Go's built-in `min`.
- `cmd/job.go` retains command assembly, shared client construction, and the default timeout. Get, list, and create commands live in `job_get.go`, `job_list.go`, and `job_create.go`; all create templates remain together.

Pure moves must preserve names, signatures, declaration bodies, and package visibility. Commit each domain move separately after `go build ./...`, `go vet ./...`, and `go test -race ./...` pass. Compare package symbol inventories before and after each move, and compare command help after each source file is split. Functional changes belong in separate commits.
