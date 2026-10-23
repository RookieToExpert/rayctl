package cmd

import "github.com/spf13/cobra"

func newAuthCheckQueueCmd() *cobra.Command {
	cmd := newAuthCheckResourceCmd("queue", "查看 SSP Queue 的用户/用户组授权")
	cmd.Use = "queue <queue-name-or-uid> [queue-name-or-uid...]"
	cmd.Long = "按名称、名称片段或完整 UID 查询 SSP Queue 的直接 IAM 授权；使用全局 -e 指定环境。"
	cmd.Example = "  rayctl auth check queue queue-d-elastic-a2-caicopilot\n" +
		"  rayctl auth check queue elastic-a2-caicopilot -e d -l\n" +
		"  rayctl auth check queue queue-a queue-b -o json"
	return cmd
}
