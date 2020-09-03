package masters

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-07-01/compute"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/azure-operator/v4/service/controller/internal/state"
	"github.com/giantswarm/azure-operator/v4/service/controller/key"
)

func (r *Resource) etcdMigrationCheckTransition(ctx context.Context, obj interface{}, currentState state.State) (state.State, error) {
	cr, err := key.ToCustomResource(obj)
	if err != nil {
		return "", microerror.Mask(err)
	}

	vmssClient, err := r.ClientFactory.GetVirtualMachineScaleSetsClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return "", microerror.Mask(err)
	}

	instance, err := r.getFirstMasterVMSSInstance(ctx, cr)
	if err != nil {
		return "", microerror.Mask(err)
	}

	// 1) upgrade the Master VMSS instance
	if !*instance.LatestModelApplied {
		r.Logger.LogCtx(ctx, "level", "info", "message", "Updating Master VMSS instance.")
		ids := to.StringSlicePtr([]string{
			*instance.InstanceID,
		})
		future, err := vmssClient.UpdateInstances(ctx, key.ResourceGroupName(cr), key.MasterVMSSName(cr), compute.VirtualMachineScaleSetVMInstanceRequiredIDs{InstanceIds: ids})
		if err != nil {
			return "", microerror.Mask(err)
		}

		err = future.WaitForCompletionRef(ctx, vmssClient.Client)
		if err != nil {
			return "", microerror.Mask(err)
		}

		r.Logger.LogCtx(ctx, "level", "info", "message", "Updated Master VMSS instance.")

		r.Logger.LogCtx(ctx, "level", "info", "message", "Reimaging Master VMSS instance.")
		// 2) reimage the Master VMSS instance
		_, err = vmssClient.Reimage(ctx, key.ResourceGroupName(cr), key.MasterVMSSName(cr), &compute.VirtualMachineScaleSetReimageParameters{InstanceIds: ids})
		if err != nil {
			return "", microerror.Mask(err)
		}
		r.Logger.LogCtx(ctx, "level", "info", "message", "Reimaged Master VMSS instance.")
		return currentState, nil
	}

	if *instance.ProvisioningState == "Succeeded" {
		// If the VM has only 2 disks it is already reimaged.
		r.Logger.LogCtx(ctx, "level", "info", "message", "Starting Master VMSS instance.")
		// 3) start the Master VMSS instance
		ids := &compute.VirtualMachineScaleSetVMInstanceIDs{
			InstanceIds: to.StringSlicePtr([]string{
				*instance.InstanceID,
			}),
		}

		_, err = vmssClient.Start(ctx, key.ResourceGroupName(cr), key.MasterVMSSName(cr), ids)
		if err != nil {
			return "", microerror.Mask(err)
		}
		r.Logger.LogCtx(ctx, "level", "info", "message", "Started Master VMSS instance.")
	} else {
		r.Logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Instance is in state %s: waiting", *instance.ProvisioningState))
		return currentState, nil
	}

	return ClusterUpgradeRequirementCheck, nil
}
