package masters

import (
	"context"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/azure-operator/v4/pkg/project"
	"github.com/giantswarm/azure-operator/v4/service/controller/internal/state"
	"github.com/giantswarm/azure-operator/v4/service/controller/key"
)

func (r *Resource) emptyStateTransition(ctx context.Context, obj interface{}, currentState state.State) (state.State, error) {
	// Check if I have to create a snapshot of the current ETCD disk.
	cr, err := key.ToCustomResource(obj)
	if err != nil {
		return "", microerror.Mask(err)
	}

	vmssClient, err := r.ClientFactory.GetVirtualMachineScaleSetsClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return "", microerror.Mask(err)
	}

	vmss, err := vmssClient.Get(ctx, key.ResourceGroupName(cr), key.MasterVMSSName(cr))
	if IsNotFound(err) {
		// Scale set not found, ok to continue with deployment.
		return DeploymentUninitialized, nil
	} else if err != nil {
		return "", microerror.Mask(err)
	}

	if *vmss.Tags[key.OperatorVersionTagName] != project.Version() {
		// Running VMSS is an old one, I need to backup the ETCD disk.
		return BackupETCDDisk, nil
	}

	return DeploymentUninitialized, nil
}
