package service

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"time"
)

var (
	volcanoJobGVR = schema.GroupVersionResource{
		Group:    "batch.volcano.sh",
		Version:  "v1alpha1",
		Resource: "jobs",
	}
	volcanoPodGroupGVR = schema.GroupVersionResource{
		Group:    "scheduling.volcano.sh",
		Version:  "v1beta1",
		Resource: "podgroups",
	}
	utcPlus8 = time.FixedZone("UTC+8", 8*60*60)
)

const (
	defaultTailLogLines         = int64(10)
	defaultEventLimit           = 10
	defaultRegistryHost         = "registry2.d.pjlab.org.cn"
	jobUIDLookupPageSize        = int64(5000)
	platformJobScanTimeout      = 4 * time.Second
	platformJobProbeConcurrency = 16
	registryLookupTimeout       = 750 * time.Millisecond
	dockerLoginTimeout          = 3 * time.Second
)
