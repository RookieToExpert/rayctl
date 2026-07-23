package service

import "testing"

func TestParseAFSProperties(t *testing.T) {
	capacity, storageClass := parseAFSProperties(`{"resources":{"billing_items":{"capacity":200,"capacity_unit":"TB"}},"storage_class":"OCEANSTOR"}`)
	if capacity != "200TB" {
		t.Fatalf("capacity = %q, want 200TB", capacity)
	}
	if storageClass != "OCEANSTOR" {
		t.Fatalf("storage class = %q, want OCEANSTOR", storageClass)
	}
}

func TestFindResourceIndex(t *testing.T) {
	resources := [][]string{
		{"vpc-ailab", "uid-1"},
		{"vpc-muxi-ailab", "uid-2"},
	}

	index, err := findResourceIndex("UID-2", "vpc", len(resources), func(i int) []string { return resources[i] })
	if err != nil {
		t.Fatalf("find exact resource: %v", err)
	}
	if index != 1 {
		t.Fatalf("index = %d, want 1", index)
	}

	if _, err := findResourceIndex("ailab", "vpc", len(resources), func(i int) []string { return resources[i] }); err == nil {
		t.Fatal("expected ambiguous fuzzy match error")
	}
}
