package metrics

import "testing"

func TestGPUHealthRequiresPhysicalIdentity(t *testing.T) {
	base := map[string]string{"node": "n", "gpu": "0", "pod": "p", "extended_resource__type": "nvidia.com/gpu"}
	for _, uuids := range [][2]string{{"", ""}, {"physical-a", "physical-b"}, {"physical-a", ""}} {
		a, b := cloneLabels(base), cloneLabels(base)
		a["uuid"], b["uuid"] = uuids[0], uuids[1]
		b["__name__"] = "DCGM_FI_DEV_ECC_DBE_VOL_TOTAL"
		h := gpuHealth([]RawSeries{{Metric: a}}, map[string][]RawSeries{"ecc": {{Metric: b, Values: []float64{100}}}})
		if h.UnknownCards != 1 || len(h.Anomalies) != 0 {
			t.Fatalf("unverified association %v: %+v", uuids, h)
		}
	}
}

func TestGPUInventoryDoesNotInventUUIDFromContainerIndex(t *testing.T) {
	for _, uuids := range [][]string{{"", "physical-a", "physical-b"}, {"physical-b", "physical-a", ""}} {
		raw := []RawSeries{}
		for _, uuid := range uuids {
			raw = append(raw, RawSeries{Metric: map[string]string{"node": "n", "pod": "p", "gpu": "0", "uuid": uuid, "extended_resource__type": "nvidia.com/gpu"}})
		}
		canonical := canonicalGPUInventory(raw)
		for i, uuid := range uuids {
			if canonical[i].Metric["uuid"] != uuid {
				t.Fatalf("UUID changed: want=%q got=%q", uuid, canonical[i].Metric["uuid"])
			}
		}
		healthLabels := cloneLabels(raw[0].Metric)
		healthLabels["uuid"] = "physical-a"
		healthLabels["__name__"] = "DCGM_FI_DEV_ECC_DBE_VOL_TOTAL"
		h := gpuHealth(canonical, map[string][]RawSeries{"ecc": {{Metric: healthLabels, Values: []float64{3}}}})
		if h.TotalCards != 3 || len(h.Anomalies) != 1 || h.Anomalies[0].UUID != "physical-a" || h.UnknownCards != 2 {
			t.Fatalf("incorrect card association: %+v", h)
		}
	}
}

func TestGPUHealthSingleBitCounterIsInformational(t *testing.T) {
	labels := map[string]string{"node": "n", "gpu": "0", "uuid": "physical-a", "pod": "p", "extended_resource__type": "nvidia.com/gpu"}
	sbe, dbe := cloneLabels(labels), cloneLabels(labels)
	sbe["__name__"] = "DCGM_FI_DEV_ECC_SBE_VOL_TOTAL"
	dbe["__name__"] = "DCGM_FI_DEV_ECC_DBE_VOL_TOTAL"
	h := gpuHealth([]RawSeries{{Metric: labels}}, map[string][]RawSeries{"ecc": {{Metric: sbe, Values: []float64{99}}, {Metric: dbe, Values: []float64{0}}}})
	if len(h.Anomalies) != 0 || h.HealthyCards != 1 || len(h.Information) != 1 || *h.Information[0].ECCSingleBit != 99 {
		t.Fatalf("%+v", h)
	}
	h = gpuHealth([]RawSeries{{Metric: labels}}, map[string][]RawSeries{"ecc": {{Metric: sbe, Values: []float64{99}}}})
	if len(h.Anomalies) != 0 || h.UnknownCards != 1 || h.HealthyCards != 0 {
		t.Fatalf("SBE alone is not complete health evidence: %+v", h)
	}
}
