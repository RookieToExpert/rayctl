package podevidence

import "time"

type Termination struct {
	Reason      string     `json:"reason"`
	ExitCode    int32      `json:"exit_code"`
	StartedAt   *time.Time `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	InstantExit *bool      `json:"instant_exit"`
}

type Logs struct {
	Current           []string `json:"current"`
	Previous          []string `json:"previous"`
	CurrentTruncated  bool     `json:"current_truncated"`
	PreviousTruncated bool     `json:"previous_truncated"`
}

type Image struct {
	Ref          string  `json:"ref"`
	Digest       string  `json:"digest"`
	Architecture *string `json:"architecture"`
	OS           *string `json:"os"`
}

type Comparison struct {
	Item  string  `json:"item"`
	Left  *string `json:"left"`
	Right *string `json:"right"`
	Match *bool   `json:"match"`
}

type Container struct {
	Name         string       `json:"name"`
	Kind         string       `json:"kind"`
	Ready        bool         `json:"ready"`
	RestartCount int32        `json:"restart_count"`
	State        any          `json:"state"`
	LastState    *Termination `json:"last_state"`
	Image        Image        `json:"image"`
	Comparison   Comparison   `json:"comparison"`
	Logs         Logs         `json:"logs"`
}

type Pod struct {
	Name             string      `json:"name"`
	UID              string      `json:"uid"`
	Namespace        string      `json:"namespace"`
	Phase            string      `json:"phase"`
	Node             string      `json:"node"`
	NodeArchitecture *string     `json:"node_architecture"`
	Containers       []Container `json:"containers"`
	Warnings         []string    `json:"warnings"`
}

type Result struct {
	Pods        []Pod    `json:"pods"`
	Total       int      `json:"total"`
	Omitted     int      `json:"omitted"`
	Restarts    int64    `json:"restarts"`
	Restarts24h *int64   `json:"restarts_24h"`
	HistoryPods []string `json:"history_pods"`
	Warnings    []string `json:"warnings"`
}

type Options struct {
	NoImage    bool
	PodLimit   int
	Kubeconfig string
}
