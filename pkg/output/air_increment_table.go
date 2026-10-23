package output

import (
	"rayctl/internal/service"
	"strings"
)

func printAIRIncrement(r *service.SSPAIRJobItem) {
	nodes := []string{}
	for _, worker := range r.Workers {
		if worker.Node != "" {
			nodes = append(nodes, worker.Node)
		}
	}
	printDetailAnchor(r.Name, r.State, r.HostNamespace, strings.Join(nodes, ","), r.Region)
	printResourceDetail([][]string{{"RESOURCE", formatAIRResource(r.Resource)}, {"IMAGE", r.Resource.Image}})
	printAIRDNATRules(r.DNATRules)
	claims := []service.VolumeClaimRef{}
	for _, volume := range r.Volumes {
		claims = append(claims, service.VolumeClaimRef{ClaimName: volume.Name, MountPath: volume.MountPath, VolumeType: volume.Type, FrontendVolume: volume.Endpoint})
	}
	printPodEvidence(r.PodEvidence, podEvidenceView{PVCs: claims, BaselineImage: r.Resource.Image})
	if r.PodEvidence == nil {
		printIncrementPVCs(claims, nil)
	}
	for _, group := range [][]service.SSPAIRWorkerItem{r.Workers, r.AuxiliaryWorkers} {
		rows := [][]string{}
		for _, worker := range group {
			rows = append(rows, []string{worker.Name, emptyDash(worker.StartedAt), emptyDash(worker.LastStarted)})
		}
		if len(rows) > 0 {
			printBoxTable([]string{"INSTANCE", "STARTED", "LAST STARTED"}, rows)
		}
	}
}

func printGatewayIncrement(r *service.SSPAIRGatewayItem) {
	printDetailAnchor(r.Name, r.State, r.HostNamespace, "", r.Region)
	printResourceDetail([][]string{{"RESOURCE", formatAIRResource(r.Resource)}, {"IMAGE", r.Resource.Image}})
	printAIRDNATRules(r.DNATRules)
}
