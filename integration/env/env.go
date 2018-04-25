package env

import (
	"crypto/sha1"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/giantswarm/azure-operator/integration/network"
)

const (
	EnvClusterName   = "CLUSTER_NAME"
	EnvKeepResources = "KEEP_RESOURCES"

	EnvCircleBuildNumber = "CIRCLE_BUILD_NUM"
	EnvCircleCI          = "CIRCLECI"
	EnvCircleJobName     = "CIRCLE_JOB"
	EnvCircleJobID       = "CIRCLE_WORKFLOW_JOB_ID"
	EnvCircleSHA         = "CIRCLE_SHA1"

	EnvAzureCIDR             = "AZURE_CIDR"
	EnvAzureCalicoSubnetCIDR = "AZURE_CALICO_SUBNET_CIDR"
	EnvAzureMasterSubnetCIDR = "AZURE_MASTER_SUBNET_CIDR"
	EnvAzureWorkerSubnetCIDR = "AZURE_WORKER_SUBNET_CIDR"

	EnvAzureClientID       = "AZURE_CLIENTID"
	EnvAzureClientSecret   = "AZURE_CLIENTSECRET"
	EnvAzureSubscriptionID = "AZURE_SUBSCRIPTIONID"
	EnvAzureTenantID       = "AZURE_TENANTID"
)

var (
	circleCI      string
	circleSHA     string
	circleJobName string
	circleJobID   string
	clusterName   string

	keepResources string

	azureClientID       string
	azureClientSecret   string
	azureSubscriptionID string
	azureTenantID       string
)

func init() {
	circleCI = os.Getenv(EnvCircleCI)
	keepResources = os.Getenv(EnvKeepResources)

	circleSHA = os.Getenv(EnvCircleSHA)
	if circleSHA == "" {
		panic(fmt.Sprintf("env var '%s' must not be empty", EnvCircleSHA))
	}

	circleJobName = os.Getenv(EnvCircleJobName)
	if circleJobName == "" {
		circleJobName = "local"
	}

	circleJobID = os.Getenv(EnvCircleJobID)
	if circleJobID == "" {
		// poor man's id generator
		circleJobID = fmt.Sprintf("%x", sha1.Sum([]byte(time.Now().Format(time.RFC3339Nano))))
	}

	clusterName = os.Getenv(EnvClusterName)
	if clusterName == "" {
		clusterName = generateClusterName(CircleJobName(), CircleJobID(), CircleSHA())
		os.Setenv(EnvClusterName, clusterName)
	}

	azureClientID = os.Getenv(EnvAzureClientID)
	if azureClientID == "" {
		panic(fmt.Sprintf("env var '%s' must not be empty", EnvAzureClientID))
	}

	azureClientSecret = os.Getenv(EnvAzureClientSecret)
	if azureClientSecret == "" {
		panic(fmt.Sprintf("env var '%s' must not be empty", EnvAzureClientSecret))
	}

	azureSubscriptionID = os.Getenv(EnvAzureSubscriptionID)
	if azureSubscriptionID == "" {
		panic(fmt.Sprintf("env var '%s' must not be empty", EnvAzureSubscriptionID))
	}

	azureTenantID = os.Getenv(EnvAzureTenantID)
	if azureTenantID == "" {
		panic(fmt.Sprintf("env var '%s' must not be empty", EnvAzureTenantID))
	}

	// azureCDIR must be provided along with other CIDRs,
	// otherwise we compute CIDRs base on EnvCircleBuildNumber value.
	azureCDIR := os.Getenv(EnvAzureCIDR)
	if azureCDIR == "" {
		buildNumber, err := strconv.ParseUint(os.Getenv(EnvCircleBuildNumber), 10, 32)
		if err != nil {
			panic(err)
		}

		cidrs, err := network.ComputeCIDR(uint(buildNumber))
		if err != nil {
			panic(err)
		}

		os.Setenv(EnvAzureCIDR, cidrs.AzureCIDR)
		os.Setenv(EnvAzureMasterSubnetCIDR, cidrs.MasterSubnetCIDR)
		os.Setenv(EnvAzureWorkerSubnetCIDR, cidrs.WorkerSubnetCIDR)
		os.Setenv(EnvAzureCalicoSubnetCIDR, cidrs.CalicoSubnetCIDR)
	} else {
		if os.Getenv(EnvAzureCalicoSubnetCIDR) == "" {
			panic(fmt.Sprintf("env var '%s' must not be empty when AZURE_CIDR is set", EnvAzureCalicoSubnetCIDR))
		}
		if os.Getenv(EnvAzureMasterSubnetCIDR) == "" {
			panic(fmt.Sprintf("env var '%s' must not be empty when AZURE_CIDR is set", EnvAzureMasterSubnetCIDR))
		}
		if os.Getenv(EnvAzureWorkerSubnetCIDR) == "" {
			panic(fmt.Sprintf("env var '%s' must not be empty when AZURE_CIDR is set", EnvAzureWorkerSubnetCIDR))
		}
	}
}

func generateClusterName(jobName, jobID, sha string) string {
	var parts []string

	parts = append(parts, "ci")
	parts = append(parts, jobName)
	parts = append(parts, jobID[0:5])
	parts = append(parts, sha[0:5])

	return strings.ToLower(strings.Join(parts, "-"))
}

func CircleJobName() string {
	return circleJobName
}

func CircleJobID() string {
	return circleJobID
}

func ClusterName() string {
	return clusterName
}

func CircleSHA() string {
	return circleSHA
}

func CircleCI() string {
	return circleCI
}

func KeepResources() string {
	return keepResources
}

func AzureClientID() string {
	return azureClientID
}

func AzureClientSecret() string {
	return azureClientSecret
}

func AzureSubscriptionID() string {
	return azureSubscriptionID
}

func AzureTenantID() string {
	return azureTenantID
}