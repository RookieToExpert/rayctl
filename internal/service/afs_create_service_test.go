package service

import "testing"

func TestParseAFSCapacityGB(t *testing.T) {
	tests := map[string]int64{
		"500GB": 500,
		"1TB":   1024,
		"2TiB":  2048,
		"1PB":   1024 * 1024,
	}
	for input, want := range tests {
		got, err := parseAFSCapacityGB(input)
		if err != nil {
			t.Fatalf("parseAFSCapacityGB(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("parseAFSCapacityGB(%q) = %d, want %d", input, got, want)
		}
	}
	if _, err := parseAFSCapacityGB("1.5TB"); err == nil {
		t.Fatal("expected fractional size to be rejected")
	}
}
