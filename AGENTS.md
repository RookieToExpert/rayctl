# Repository Rules

新增的平台 API 按领域放进 `internal/platform/<domain>_client.go`，输出按领域放进 `pkg/output/<domain>_table.go`；不要往 `virtual_cluster_client.go`、`table.go`、`job_service.go` 里追加与其保留职责无关的代码。

重新构建本机版本，替换 `~/.local/libexec/rayctl`。

开发机是 AIS：`ssh -p 22225 ecs-user@10.140.158.130`，系统为 Linux ARM64。部署时从当前本地代码构建 `GOOS=linux GOARCH=arm64` 版本，覆盖开发机的 `/usr/local/bin/rayctl`；以后默认使用此远端安装路径，不要误装到本机的 `/usr/local/bin/rayctl`。
