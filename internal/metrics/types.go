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
	Health      bool
	Detail      bool
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
	Detail                bool              `json:"detail"`
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
	Hardware           string            `json:"hardware,omitempty"`
	MemoryUsedGiB      *float64          `json:"memory_used_gib,omitempty"`
	MemoryTotalGiB     *float64          `json:"memory_total_gib,omitempty"`
	MemoryPercent      *float64          `json:"memory_percent,omitempty"`
	MemoryUsedPeakMiB  float64           `json:"-"`
	MemoryTotalMiB     float64           `json:"-"`
}

type Sample struct {
	Series         string   `json:"series"`
	Timestamp      string   `json:"t"`
	Value          float64  `json:"v"`
	Percent        float64  `json:"-"`
	Delta          float64  `json:"-"`
	Reason         string   `json:"reason,omitempty"`
	UID            string   `json:"uid,omitempty"`
	Event          string   `json:"event,omitempty"`
	MemoryUsedGiB  *float64 `json:"memory_used_gib,omitempty"`
	MemoryTotalGiB *float64 `json:"memory_total_gib,omitempty"`
	MemoryPercent  *float64 `json:"memory_percent,omitempty"`
}

type SeriesSummary struct {
	Peak           float64 `json:"peak"`
	PeakAt         string  `json:"peak_at,omitempty"`
	Latest         float64 `json:"latest"`
	SampleCount    int     `json:"sample_count"`
	PeakPercent    float64 `json:"-"`
	Average        float64 `json:"avg"`
	P95            float64 `json:"p95"`
	NonzeroPercent float64 `json:"nonzero_percent"`
}

type Summary struct {
	SeriesCount int `json:"series_count"`
	SampleCount int `json:"sample_count"`
	EventCount  int `json:"event_count,omitempty"`
}

type Result struct {
	Metadata     Metadata        `json:"metadata"`
	Series       []Series        `json:"series"`
	Samples      []Sample        `json:"samples"`
	Summary      Summary         `json:"summary"`
	Warnings     []string        `json:"warnings"`
	Capabilities []GPUCapability `json:"capabilities,omitempty"`
	Health       *GPUHealth      `json:"health,omitempty"`
	Baseline     *GPUBaseline    `json:"baseline,omitempty"`
}

type GPUCapability struct {
	Hardware   string            `json:"hardware"`
	Dimensions map[string]string `json:"dimensions"`
}

type GPUHealth struct {
	TotalCards   int             `json:"total_cards"`
	HealthyCards int             `json:"healthy_cards"`
	UnknownCards int             `json:"unknown_cards"`
	Anomalies    []GPUHealthCard `json:"anomalies"`
}

type GPUHealthCard struct {
	Node          string   `json:"node"`
	GPU           string   `json:"gpu"`
	UUID          string   `json:"uuid"`
	Model         string   `json:"model"`
	Pod           string   `json:"pod"`
	RAS           *float64 `json:"ras"`
	ECC           *float64 `json:"ecc"`
	ErrorCode     *float64 `json:"error_code"`
	IsolatedPages *float64 `json:"isolated_pages"`
}

type GPUBaseline struct {
	PodCount                 int      `json:"pod_count"`
	UtilP50                  float64  `json:"util_p50"`
	UtilP95                  float64  `json:"util_p95"`
	UtilMax                  float64  `json:"util_max"`
	MemoryP50GiB             *float64 `json:"memory_p50_gib"`
	MemoryP95GiB             *float64 `json:"memory_p95_gib"`
	MemoryMinGiB             *float64 `json:"memory_min_gib"`
	SelectedMemoryPercentile *float64 `json:"selected_memory_percentile"`
	SignificantlyLow         bool     `json:"significantly_low"`
}

type EndpointConfig struct {
	BaseURL        string
	Username       string
	Password       string
	Kubeconfig     string
	RequestTimeout time.Duration
}
