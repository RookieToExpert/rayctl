package service

import "testing"

func TestResolveListLimit(t *testing.T) {
	tests := []struct {
		name              string
		limit             int
		windowed, changed bool
		want              int
		wantErr           bool
	}{
		{"default", 0, false, false, 50, false},
		{"ordinary explicit", 3, false, true, 3, false},
		{"ordinary maximum", 1000, false, true, 1000, false},
		{"ordinary over maximum", 1001, false, true, 0, true},
		{"window default", 50, true, false, 5000, false},
		{"window explicit", 3, true, true, 3, false},
		{"window explicit default value", 50, true, true, 50, false},
		{"window above ordinary maximum", 1001, true, true, 1001, false},
		{"window maximum", 5000, true, true, 5000, false},
		{"window capped", 6000, true, true, 5000, false},
		{"ordinary explicit zero", 0, false, true, 0, true},
		{"window explicit zero", 0, true, true, 0, true},
		{"ordinary negative", -1, false, true, 0, true},
		{"window negative", -1, true, true, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveListLimit(tt.limit, tt.windowed, tt.changed)
			if got != tt.want || (err != nil) != tt.wantErr {
				t.Fatalf("ResolveListLimit() = %d, %v; want %d, error=%v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}
