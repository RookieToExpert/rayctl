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

The remaining large files should be split incrementally by behavior, not through a single rewrite:

- `job_service.go`: locator, ECP detail, diagnostics, storage, image credential checks.
- `virtual_cluster_client.go`: IAM, Kubernetes proxy, CMS logs, storage resources.
- `table.go`: output grouped by command domain.
- `cmd/job.go`: get, cluster list, and create templates.

Each extraction should preserve public behavior and include focused tests before the next domain is moved.
