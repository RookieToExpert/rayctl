package service

import "testing"

func TestFilterAndSortSSPCatalogItems(t *testing.T) {
	items := []SSPCatalogListItem{
		{Name: "old", State: "Running", CreatedAt: "2026-09-01 10:00:00"},
		{Name: "pending", State: "Pending", CreatedAt: "2026-09-02 11:00:00"},
		{Name: "new", State: "RUNNING", CreatedAt: "2026-09-02 12:00:00"},
	}
	got := filterAndSortSSPCatalogItems(items, "running", 1)
	if len(got) != 1 || got[0].Name != "new" {
		t.Fatalf("filterAndSortSSPCatalogItems() = %#v", got)
	}
}

func TestValidateSSPCatalogLimit(t *testing.T) {
	if got, err := resolveSSPCatalogLimit(0, false); err != nil || got != 50 {
		t.Fatalf("default limit = %d, %v", got, err)
	}
	if got, err := resolveSSPCatalogLimit(50, true); err != nil || got != -1 {
		t.Fatalf("all limit = %d, %v", got, err)
	}
	if _, err := resolveSSPCatalogLimit(1001, false); err == nil {
		t.Fatal("limit above 1000 was accepted")
	}
}

func TestNormalizeSSPCatalogAPIState(t *testing.T) {
	if got := normalizeSSPCatalogAPIState(" Running "); got != "RUNNING" {
		t.Fatalf("normalizeSSPCatalogAPIState() = %q", got)
	}
}
