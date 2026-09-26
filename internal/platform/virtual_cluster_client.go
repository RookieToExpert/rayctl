package platform

import (
	"net/http"
)

const (
	ClusterD                      = "d"
	ClusterDCloud                 = "dcloud"
	defaultSubscriptionDCloud     = "019a575c-9a53-71ab-8028-2b0383d7a02f"
	defaultAPIBaseURL             = "https://management.d.pjlab.org.cn"
	defaultKubernetesBaseURL      = "https://compute.d.pjlab.org.cn"
	defaultIAMBaseURL             = "https://iam.d.pjlab.org.cn"
	defaultPTIAMBaseURL           = "https://iam-api.pjlab.org.cn"
	defaultMonitorBaseURL         = "https://monitor.d.pjlab.org.cn"
	defaultCloudAPIBaseURL        = "https://management-cloud.d.pjlab.org.cn"
	defaultCloudKubernetesBaseURL = "https://compute-cloud.d.pjlab.org.cn"
	defaultCloudIAMBaseURL        = "https://iam-cloud.d.pjlab.org.cn"
	defaultCloudMonitorBaseURL    = "https://monitor-cloud.d.pjlab.org.cn"
	defaultResourceGroup          = "default"
	defaultRegion                 = "cn-pj-01"
	defaultPageLimit              = 100
	defaultConfigFileName         = "platform.json"
)

type VirtualClusterClient struct {
	accessKey         string
	secretKey         string
	baseURL           string
	kubernetesBaseURL string
	iamBaseURL        string
	subscription      string
	resourceGroup     string
	region            string
	metricsBaseURL    string
	metricsUsername   string
	metricsPassword   string
	currentProfile    string
	profiles          map[string]clientProfile
	httpClient        *http.Client
}
