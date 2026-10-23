package service

import (
	"errors"
	"fmt"
	"strings"

	"rayctl/internal/platform"
)

// Workspace failures retain their cause for classification, but the CLI needs
// the scope and failure kind rather than an HTTP URL and a gateway JSON body.
type SSPWorkspaceQueryError struct {
	Workspace string
	Region    string
	Cause     error
}

func (e *SSPWorkspaceQueryError) Unwrap() error { return e.Cause }

func (e *SSPWorkspaceQueryError) Error() string {
	scope := e.Workspace
	if e.Region != "" {
		scope = e.Region + "/" + scope
	}
	var status *platform.HTTPStatusError
	if errors.As(e.Cause, &status) {
		if status.StatusCode == 403 {
			return scope + " 无权限（403）"
		}
		return fmt.Sprintf("%s 查询失败（HTTP %d）", scope, status.StatusCode)
	}
	return fmt.Sprintf("%s 查询失败：%s", scope, singleLineQueryText(e.Cause))
}

func singleLineQueryText(err error) string {
	if err == nil {
		return ""
	}
	return strings.Join(strings.Fields(strings.ReplaceAll(err.Error(), "\n", "；")), " ")
}

type singleLineQueryError struct{ error }

func (e singleLineQueryError) Error() string { return singleLineQueryText(e.error) }
func (e singleLineQueryError) Unwrap() error { return e.error }

func partialSSPQueryWarning(err error) string {
	return "部分查询未完成：" + singleLineQueryText(err)
}
