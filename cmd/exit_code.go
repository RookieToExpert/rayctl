package cmd

import "sync/atomic"

var requestedProcessExitCode atomic.Int32

func requestProcessExitCode(code int) {
	if code > int(requestedProcessExitCode.Load()) {
		requestedProcessExitCode.Store(int32(code))
	}
}

func RequestedProcessExitCode() int {
	return int(requestedProcessExitCode.Load())
}

func resetRequestedProcessExitCode() {
	requestedProcessExitCode.Store(0)
}
