package etcddisks

import (
	"context"
	"fmt"

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

	var members []string

	for iterator.NotDone() {
		instance := iterator.Value()

		// Check if instance is running before going on with ETCD initialization.
		if *instance.ProvisioningState != "Succeeded" {
			r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Instance %s has provisioning state %s: skipping", *instance.InstanceID, *instance.ProvisioningState))
		} else {
			diskName := ""
			// Check if VM has an ETCD disk attached.
			for _, dataDisk := range *instance.StorageProfile.DataDisks {
				// We assume etcd disk is the only one attached to lun 0.
				if *dataDisk.Lun == 0 {
					r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Found that instance %s has a disk on lun 0", *instance.InstanceID))
					diskName = *dataDisk.Name
					break
				}
			}

			if diskName == "" {
				// This instance has no disk attached for etcd, search for an available one.
				r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Looking for an available disk for instance %s", *instance.InstanceID))
				zone := ""
				if len(*instance.Zones) > 0 {
					zone = (*instance.Zones)[0]
				}
				diskName, err = r.findAvailableDisk(ctx, cr, zone)
				if err != nil {
					return microerror.Mask(err)
				}
				if diskName != "" {
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
			}

			if diskName != "" {
				// The instance now has a disk attached. I need to update the DNS record.
				ipAddr, err := r.getVMSSInstanceIPAddress(ctx, cr, *instance.InstanceID)
				if err != nil {
					return microerror.Mask(err)
				}

				// Create/Update DNS record for this ETCD member.
				err = r.updateDNSRecord(ctx, cr, diskName, ipAddr)
				if err != nil {
					return microerror.Mask(err)
				}

				// Get the TLS certificate for this member.
				tls, err := r.getTLSPeerCert(ctx, cr, diskName)
				if err != nil {
					return microerror.Mask(err)
				}

				// Write the ETCD bootstrap env file.
				memberUrl := fmt.Sprintf("https://%s.%s:%d", diskName, key.ClusterDNSDomain(cr), 2380)
				members = append(members, fmt.Sprintf("%s=%s", diskName, memberUrl))

				err = r.writeEnvFile(ctx, cr, diskName, memberUrl, members, tls, *instance.InstanceID)
				if err != nil {
					return microerror.Mask(err)
				}
			}
		}

		err = iterator.NextWithContext(ctx)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	return nil
}

func (r *Resource) getVMSSInstanceIPAddress(ctx context.Context, cr v1alpha1.AzureConfig, instanceID string) (string, error) {
	netIfClient, err := r.clientFactory.GetInterfacesClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return "", microerror.Mask(err)
	}

	netIf, err := netIfClient.GetVirtualMachineScaleSetNetworkInterface(ctx, key.ResourceGroupName(cr), key.MasterVMSSName(cr), instanceID, key.MasterNICName(cr), "")
	if err != nil {
		return "", microerror.Mask(err)
	}

	ipcs := *netIf.IPConfigurations
	if len(ipcs) == 0 {
		return "", microerror.Mask(ipAddressUnavailableError)
	}

	return *ipcs[0].PrivateIPAddress, nil
}
