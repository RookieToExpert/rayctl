# rayctl
强大的 k8s 工具 for 商汤 dcluster
=======

`rayctl` is a custom Kubernetes CLI written in Go for AI infrastructure and cluster management workflows.

## Command Examples

```bash
rayctl node get
rayctl node get ecp
rayctl node get "accelerator=huawei-ascend"
rayctl node cordon worker-01
rayctl node uncordon worker-01
rayctl node describe worker-01
rayctl node check worker-01
rayctl --kubeconfig=/path/to/config node get ecp

# 列出当前 profile 下的资源，或按名称/UID 查看单个资源
rayctl vpc get
rayctl vpc get vpc-muxi-ailab
rayctl subnet get
rayctl subnet get subnet-muxi-ailab
rayctl natgw get
rayctl natgw get nat-muxi-ailab
rayctl afs get
rayctl afs get afs-tangrui
```

## Project Initialization

```bash
go mod init rayctl
go get github.com/spf13/cobra@latest
go get k8s.io/client-go@latest
go get k8s.io/api@latest
go get k8s.io/apimachinery@latest
go mod tidy
```
