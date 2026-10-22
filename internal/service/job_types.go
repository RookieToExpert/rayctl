package service

import (
	"fmt"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"rayctl/internal/platform"
	"rayctl/internal/podevidence"
	"sort"
	"strings"
	"time"
)

type JobService struct {
	clientset     kubernetes.Interface
	dynamicClient dynamic.Interface
	vcClient      *platform.VirtualClusterClient
}

type jobIdentity struct {
	Name          string
	Namespace     string
	UID           string
	Submitter     string
	PodGroupName  string
	VClusterName  string
	VClusterUID   string
	ProfileName   string
	HostNamespace string
	HostJobName   string
}

type ambiguousJobMatchError struct {
	Identifier string
	Matches    []jobIdentity
}

func (e *ambiguousJobMatchError) Error() string {
	if e == nil {
		return "ambiguous job match"
	}
	parts := make([]string, 0, len(e.Matches))
	for _, match := range e.Matches {
		label := strings.TrimSpace(match.Name)
		if ns := strings.TrimSpace(match.Namespace); ns != "" {
			label = ns + "/" + label
		}
		if vc := strings.TrimSpace(match.VClusterName); vc != "" {
			label = label + "@" + vc
		}
		parts = append(parts, label)
	}
	sort.Strings(parts)
	return fmt.Sprintf("multiple jobs matched %q: %s", e.Identifier, strings.Join(parts, ", "))
}

type JobGetResult struct {
	Name                   string
	WorkloadType           string
	Namespace              string
	HostNamespace          string
	UID                    string
	Status                 string
	CreatedAt              string
	StartedAt              string
	EndedAt                string
	Terminal               bool
	Stage                  string
	Instruction            string
	Diagnosis              []string
	VClusterName           string
	Submitter              string
	PodGroupName           string
	ImagePullSecrets       []string
	SecretChecks           []SecretCheckItem
	PersistentVolumeClaims []VolumeClaimRef
	Pods                   []JobPodItem
	Nodes                  []string
	InspectPod             string
	CheckEvidence          []CheckEvidenceItem
	RecentEvents           []EventItem
	RecentLogLines         []string
	ResourceSpecs          []JobResourceSpecItem
	PodEvidence            *podevidence.Result `json:"pod_evidence,omitempty" public:"omitempty"`
	Timings                JobGetTimings
}

type JobClusterItem struct {
	ClusterName    string
	JobName        string
	Submitter      string
	Status         string
	CreatedAt      string
	CreatedAtShort string
	CreatedAtTime  time.Time
	eventAtTime    time.Time
}

type JobClusterListResult struct {
	ClusterName     string
	Items           []JobClusterItem
	ActiveJobCount  int
	ActivePodCount  int
	TotalJobCount   int
	TotalPodCount   int
	ShowClusterName bool
	StatusFilter    string
	Warnings        []string
}

type JobCheckResult struct {
	Name             string
	Namespace        string
	UID              string
	PodGroupName     string
	Stage            string
	Instruction      string
	Pods             []JobPodCheckItem
	PVCs             []PVCCheckItem
	SecretChecks     []SecretCheckItem
	PodEvidence      []CheckEvidenceItem
	PodGroupEvidence []CheckEvidenceItem
	Diagnosis        []string
}

type JobCreateRequest struct {
	Name                   string
	Namespace              string
	Submitter              string
	SPBlock                string
	FrameworkType          string
	MasterPort             string
	Replicas               int64
	MinAvailable           int64
	MasterReplicas         int64
	WorkerReplicas         int64
	Image                  string
	Command                string
	ImagePullSecret        string
	CPU                    string
	Memory                 string
	AcceleratorResource    string
	AcceleratorCount       string
	ExtraResourceName      string
	ExtraResourceValue     string
	DataPVCName            string
	AOSSPVCName            string
	SHMSize                string
	MachineType            string
	HostArch               string
	AcceleratorType        string
	UseDefaultNodeSelector bool
	UsePCILinkVolume       bool
	RequireIPCLock         bool
	PriorityClass          string
	Queue                  string
}

type VolumeClaimRef struct {
	Name             string
	ClaimName        string
	MountPath        string
	VolumeType       string
	PVName           string
	BackendPV        string
	DisplayPV        string
	HostPVCName      string
	HostPVCNamespace string
	FrontendVolume   string
	Status           string
	Message          string
}

type PVCCheckItem struct {
	Name             string
	ClaimName        string
	PVName           string
	BackendPV        string
	DisplayPV        string
	HostPVCName      string
	HostPVCNamespace string
	FrontendVolume   string
	Status           string
	Message          string
}

type SecretCheckItem struct {
	SecretName string
	Registry   string
	Username   string
	Password   string
	Status     string
	Message    string
}

type CheckEvidenceItem struct {
	Source string
	Status string
	Detail string
}

type JobGetTimings struct {
	Locate         time.Duration
	PlatformJob    time.Duration
	PlatformPods   time.Duration
	PlatformEvents time.Duration
	PlatformLogs   time.Duration
	Diagnosis      time.Duration
	Enrichment     time.Duration
	KubeJob        time.Duration
	KubePods       time.Duration
	KubeEvents     time.Duration
	KubeLogs       time.Duration
	Format         time.Duration
	Total          time.Duration
}

type JobPodItem struct {
	Name      string
	Namespace string
	NodeName  string
	Phase     string
	Reason    string
	Message   string
	TaskSpec  string
	TaskIndex string
}

type JobPodCheckItem struct {
	Name     string
	Phase    string
	Ready    string
	NodeName string
	Reason   string
}

type PodGroupGetResult struct {
	Name           string
	Namespace      string
	Status         string
	MinMember      string
	Queue          string
	CreatedAt      string
	StatusMessages []string
	RecentEvents   []EventItem
}

type EventItem struct {
	Time    string
	Type    string
	Reason  string
	Message string
}

type dockerCredential struct {
	Registry              string
	RegistryRaw           string
	RegistryHasWhitespace bool
	Username              string
	Password              string
}

func NewJobService(clientset kubernetes.Interface, dynamicClient dynamic.Interface, vcClient *platform.VirtualClusterClient) *JobService {
	return &JobService{
		clientset:     clientset,
		dynamicClient: dynamicClient,
		vcClient:      vcClient,
	}
}
