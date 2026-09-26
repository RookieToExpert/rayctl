# Repository Rules

新增的平台 API 按领域放进 `internal/platform/<domain>_client.go`，输出按领域放进 `pkg/output/<domain>_table.go`；不要往 `virtual_cluster_client.go`、`table.go`、`job_service.go` 里追加与其保留职责无关的代码。

重新构建本机版本，替换 `~/.local/libexec/rayctl`。
