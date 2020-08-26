package etcddisks

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-07-01/compute"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/apiextensions/pkg/apis/provider/v1alpha1"
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/azure-operator/v4/service/controller/key"
)

func (r *Resource) attachDisks(ctx context.Context, cr v1alpha1.AzureConfig) error {
	vmssVMsClient, err := r.clientFactory.GetVirtualMachineScaleSetVMsClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return microerror.Mask(err)
	}

	iterator, err := vmssVMsClient.ListComplete(ctx, key.ResourceGroupName(cr), key.MasterVMSSName(cr), "", "", "")
	if IsNotFound(err) {
		r.logger.LogCtx(ctx, "level", "info", "message", "VMSS not found, can't proceed with attachment of disks")
		return nil
	} else if err != nil {
		return microerror.Mask(err)
	}

	for iterator.NotDone() {
		instance := iterator.Value()

		found := false
		// Check if VM has an ETCD disk attached.
		for _, dataDisk := range *instance.StorageProfile.DataDisks {
			// We assume etcd disk is the only one attached to lun 0.
			if *dataDisk.Lun == 0 {
				r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Found that instance %s has a disk on lun 0", *instance.InstanceID))
				found = true
				break
			}
		}

		if !found {
			diskName, err := r.findAvailableDisk(ctx, cr)
			if err != nil {
				return microerror.Mask(err)
			}
			if diskName == "" {
				// No disks available.
				return noDisksAvailableError
			}
			diskID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Compute/disks/%s", vmssVMsClient.SubscriptionID, key.ResourceGroupName(cr), diskName)
			r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Attaching disk %s to instance %s", diskName, *instance.InstanceID))
			*instance.StorageProfile.DataDisks = append(*instance.StorageProfile.DataDisks, compute.DataDisk{
				Lun:          to.Int32Ptr(0),
				Name:         to.StringPtr(diskName),
				CreateOption: compute.DiskCreateOptionTypesAttach,
				ManagedDisk: &compute.ManagedDiskParameters{
					ID: to.StringPtr(diskID),
				},
			})

			_, err = vmssVMsClient.Update(ctx, key.ResourceGroupName(cr), key.MasterVMSSName(cr), *instance.InstanceID, instance)
			if err != nil {
				return microerror.Mask(err)
			}

			r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Attached disk %s to instance %s", diskName, *instance.InstanceID))
		}

		err := iterator.NextWithContext(ctx)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	return nil
}

func (r *Resource) findAvailableDisk(ctx context.Context, cr v1alpha1.AzureConfig) (string, error) {
	disksClient, err := r.clientFactory.GetDisksClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return "", microerror.Mask(err)
	}

	iterator, err := disksClient.ListByResourceGroupComplete(ctx, key.ResourceGroupName(cr))
	if err != nil {
		return "", microerror.Mask(err)
	}

	for iterator.NotDone() {
		candidate := iterator.Value()

		if strings.HasPrefix(*candidate.Name, "etcd") {
			// TODO check availabilty zone.
			// TODO This does not take into account disks being attached.
			if candidate.ManagedBy == nil {
				// Disk is unattached.
				r.logger.LogCtx(ctx, "level", "info", fmt.Sprintf("Found available disk: %s", *candidate.Name))

				return *candidate.Name, nil
			}
		}

		err := iterator.NextWithContext(ctx)
		if err != nil {
			return "", microerror.Mask(err)
		}
	}

	return "", nil
}
