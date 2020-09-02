package etcddisks

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-07-01/compute"

	"github.com/giantswarm/apiextensions/pkg/apis/provider/v1alpha1"
	"github.com/giantswarm/certs/v2/pkg/certs"
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/azure-operator/v4/service/controller/key"
)

func (r *Resource) writeEnvFile(ctx context.Context, cr v1alpha1.AzureConfig, memberName string, memberUrl string, members []string, tls *certs.TLS, instanceID string) error {
	vmssVMsClient, err := r.clientFactory.GetVirtualMachineScaleSetVMsClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return microerror.Mask(err)
	}

	initialCluster := strings.Join(members, ",")
	initialClusterState := "existing"
	if len(members) == 1 {
		initialClusterState = "new"
	}
	vars := []string{
		fmt.Sprintf("ETCD_NAME=%s", memberName),
		fmt.Sprintf("ETCD_PEER_URL=%s", memberUrl),
		fmt.Sprintf("ETCD_INITIAL_CLUSTER=%s", initialCluster),
		fmt.Sprintf("ETCD_INITIAL_CLUSTER_STATE=%s", initialClusterState),
		fmt.Sprintf("ETCD_PEER_CA_PATH=%s", "/var/lib/etcd/ssl/peer-ca.pem"),
		fmt.Sprintf("ETCD_PEER_CERT_PATH=%s", "/var/lib/etcd/ssl/peer-crt.pem"),
		fmt.Sprintf("ETCD_PEER_KEY_PATH=%s", "/var/lib/etcd/ssl/peer-key.pem"),
		fmt.Sprintf("ETCD_PEER_CA=%s", base64.StdEncoding.EncodeToString(tls.CA)),
		fmt.Sprintf("ETCD_PEER_CRT=%s", base64.StdEncoding.EncodeToString(tls.Crt)),
		fmt.Sprintf("ETCD_PEER_KEY=%s", base64.StdEncoding.EncodeToString(tls.Key)),
	}
	commandId := "RunShellScript"
	script := []string{
		fmt.Sprintf(
			"echo -e '%s' | sudo tee /etc/etcd-bootstrap-env",
			strings.Join(vars, "\\n"),
		),
	}
	r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Writing ETCD env file to instance %s", instanceID))

	runCommandInput := compute.RunCommandInput{
		CommandID: &commandId,
		Script:    &script,
	}

	runCommandFuture, err := vmssVMsClient.RunCommand(ctx, key.ResourceGroupName(cr), key.MasterVMSSName(cr), instanceID, runCommandInput)
	if err != nil {
		return microerror.Mask(err)
	}
	_, err = vmssVMsClient.RunCommandResponder(runCommandFuture.Response())
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
