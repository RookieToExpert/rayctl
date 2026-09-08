package metrics

import (
	"strings"
	"testing"
)

func TestDecodeExportSupportsStringAndNumericValues(t *testing.T) {
	body := strings.Join([]string{
		`{"metric":{"__name__":"m","pod":"p1"},"values":["1.5",2],"timestamps":[1000,2000]}`,
		`{"metric":{"__name__":"m","pod":"p2"},"values":[3],"timestamps":[1000]}`,
	}, "\n")

	series, err := decodeExport([]byte(body))
	if err != nil {
		t.Fatalf("decodeExport() error = %v", err)
	}
	if len(series) != 2 || len(series[0].Values) != 2 {
		t.Fatalf("unexpected series: %#v", series)
	}
	if series[0].Values[0] != 1.5 || series[0].Values[1] != 2 {
		t.Fatalf("unexpected values: %#v", series[0].Values)
	}
}

func TestDecodePrometheusMatrix(t *testing.T) {
	body := []byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"pod":"p1"},"values":[[10,"2.5"],[20,"3"]]}]}}`)
	series, err := decodePrometheusResponse(body)
	if err != nil {
		t.Fatalf("decodePrometheusResponse() error = %v", err)
	}
	if got := series[0].Timestamps[1]; got != 20000 {
		t.Fatalf("timestamp = %d, want 20000", got)
	}
	if got := series[0].Values[0]; got != 2.5 {
		t.Fatalf("value = %v, want 2.5", got)
	}
}
