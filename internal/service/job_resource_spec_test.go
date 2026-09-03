package service

import (
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestJobResourceSpecsFromVolcanoJob(t *testing.T) {
	job := &unstructured.Unstructured{}
	if err := json.Unmarshal([]byte(`{
		"spec":{"tasks":[{
			"name":"worker","replicas":2,
			"template":{"spec":{
				"containers":[{"resources":{"requests":{"cpu":"144","memory":"1920Gi","huawei.com/Ascend910":"8"}}}],
				"nodeSelector":{"accelerator-type":"module-910b-8"},
				"affinity":{"nodeAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"nodeSelectorTerms":[
					{"matchExpressions":[{"key":"resource.compute.sensecore.cn/machine-type","operator":"In","values":["h1ls.rp.k60a"]}]}
				]}}}
			}}
		}]}
	}`), &job.Object); err != nil {
		t.Fatal(err)
	}

	specs := jobResourceSpecsFromVolcanoJob(job)

	if len(specs) != 1 {
		t.Fatalf("specs = %#v", specs)
	}
	got := specs[0]
	if got.Task != "worker" || got.Replicas != 2 || got.CPU != "144" || got.Memory != "1920Gi" || got.Accelerator != "8" || got.Model != "module-910b-8" || got.MachineType != "h1ls.rp.k60a" {
		t.Fatalf("unexpected spec: %#v", got)
	}
}
