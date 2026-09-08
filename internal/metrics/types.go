package metrics

import "time"

const BeijingOffset = 8 * time.Hour

var BeijingLocation = time.FixedZone("UTC+8", int(BeijingOffset.Seconds()))

type RawSeries struct {
	Metric     map[string]string
	Values     []float64
	Timestamps []int64
}

type QueryOptions struct {
	Workload    string
	Type        string
	Workspace   string
	Environment string
	Pods        []string
	By          string
	Start       time.Time
	End         time.Time
	Step        time.Duration
	UseStep     bool
}

type TimeRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type Metadata struct {
	Command               string            `json:"command"`
	Workload              string            `json:"workload,omitempty"`
	WorkloadType          string            `json:"workload_type,omitempty"`
	Workspace             string            `json:"workspace,omitempty"`
	Environment           string            `json:"environment,omitempty"`
	Metric                string            `json:"metric,omitempty"`
	Unit                  string            `json:"unit"`
	Mode                  string            `json:"query_mode"`
	Step                  *string           `json:"step"`
	Range                 TimeRange         `json:"range"`
	SampleIntervalSeconds float64           `json:"sample_interval_seconds"`
	Pods                  []string          `json:"-"`
	Start                 string            `json:"-"`
	End                   string            `json:"-"`
	Timezone              string            `json:"-"`
	SampleInterval        string            `json:"-"`
	Labels                map[string]string `json:"-"`
}

type Series struct {
	Name               string            `json:"key"`
	Pod                *string           `json:"pod"`
	Container          *string           `json:"container"`
	Node               *string           `json:"node"`
	GPU                *string           `json:"gpu"`
	Unit               string            `json:"unit"`
	Limit              float64           `json:"limit"`
	MergedFrom         int               `json:"merged_from"`
	DroppedSCOSRMTypes []string          `json:"dropped_sco_srm_types"`
	Summary            SeriesSummary     `json:"summary"`
	Labels             map[string]string `json:"labels,omitempty"`
	DroppedSeries      int               `json:"-"`
	Hardware           string            `json:"-"`
	MemoryUsedPeakMiB  float64           `json:"-"`
	MemoryTotalMiB     float64           `json:"-"`
}

type Sample struct {
	Series    string  `json:"series"`
	Timestamp string  `json:"t"`
	Value     float64 `json:"v"`
	Percent   float64 `json:"-"`
	Delta     float64 `json:"-"`
	Reason    string  `json:"reason,omitempty"`
	UID       string  `json:"uid,omitempty"`
	Event     string  `json:"event,omitempty"`
}

type SeriesSummary struct {
	Peak        float64 `json:"peak"`
	PeakAt      string  `json:"peak_at,omitempty"`
	Latest      float64 `json:"latest"`
	SampleCount int     `json:"sample_count"`
	PeakPercent float64 `json:"-"`
}

type Summary struct {
	SeriesCount int `json:"series_count"`
	SampleCount int `json:"sample_count"`
	EventCount  int `json:"event_count,omitempty"`
}

type Result struct {
	Metadata Metadata `json:"metadata"`
	Series   []Series `json:"series"`
	Samples  []Sample `json:"samples"`
	Summary  Summary  `json:"summary"`
	Warnings []string `json:"warnings"`
}

type EndpointConfig struct {
	BaseURL        string
	Username       string
	Password       string
	Kubeconfig     string
	RequestTimeout time.Duration
}
