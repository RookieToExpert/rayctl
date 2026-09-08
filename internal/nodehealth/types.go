package nodehealth

import "time"

type Status string

const (
	StatusOK   Status = "OK"
	StatusWarn Status = "WARN"
	StatusFail Status = "FAIL"
	StatusSkip Status = "SKIP"
)

const (
	CheckNodeBasic    = "node-basic"
	CheckDisk         = "disk"
	CheckDevicePlugin = "device-plugin"
	CheckClockSkew    = "clock-skew"
	CheckKernelErrors = "kernel-errors"
	CheckMindXNPU     = "mindx-npu"
)

var DefaultChecks = []string{
	CheckNodeBasic,
	CheckDisk,
	CheckDevicePlugin,
	CheckClockSkew,
	CheckKernelErrors,
	CheckMindXNPU,
}

type NodeLabels struct {
	MachineType string `json:"machine_type"`
	VC          string `json:"vc"`
}

type CheckResult struct {
	ID         string         `json:"id"`
	Status     Status         `json:"status"`
	DurationMS int64          `json:"duration_ms"`
	Evidence   map[string]any `json:"evidence"`
	Message    string         `json:"-"`
}

type NodeResult struct {
	Node       string        `json:"node"`
	Status     Status        `json:"status"`
	Labels     NodeLabels    `json:"labels"`
	Checks     []CheckResult `json:"checks"`
	Warnings   []string      `json:"warnings"`
	Ready      string        `json:"-"`
	InternalIP string        `json:"-"`
	VCUID      string        `json:"-"`
}

type Summary struct {
	Total int `json:"total"`
	OK    int `json:"ok"`
	Warn  int `json:"warn"`
	Fail  int `json:"fail"`
	Skip  int `json:"skip"`
}

type Result struct {
	Nodes   []NodeResult `json:"nodes"`
	Summary Summary      `json:"summary"`
	Batch   bool         `json:"-"`
}

type Options struct {
	Checks      []string
	NoExec      bool
	MaxParallel int
	Now         func() time.Time
}

func WorstStatus(values ...Status) Status {
	worst := StatusSkip
	for _, value := range values {
		if statusRank(value) > statusRank(worst) {
			worst = value
		}
	}
	return worst
}

func statusRank(status Status) int {
	switch status {
	case StatusFail:
		return 4
	case StatusWarn:
		return 3
	case StatusOK:
		return 2
	case StatusSkip:
		return 1
	default:
		return 0
	}
}

func HasCheck(checks []string, target string) bool {
	for _, check := range checks {
		if check == target {
			return true
		}
	}
	return false
}
