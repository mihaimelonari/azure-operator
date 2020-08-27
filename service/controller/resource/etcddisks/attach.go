package etcddisks

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-07-01/compute"
	"github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"
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
			diskName, err = r.findAvailableDisk(ctx, cr)
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

		// The instance now has a disk attached. I need to update the DNS record.
		ipAddr, err := r.getVMSSInstanceIPAddress(ctx, cr, *instance.InstanceID)
		if err != nil {
			return microerror.Mask(err)
		}

		err = r.updateDNSRecord(ctx, cr, diskName, ipAddr)
		if err != nil {
			return microerror.Mask(err)
		}

		err = iterator.NextWithContext(ctx)
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
			r.logger.LogCtx(ctx, "level", "info", fmt.Sprintf("Provisioning state for %s is: %s", *candidate.Name, *candidate.ProvisioningState))
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

func (r *Resource) updateDNSRecord(ctx context.Context, cr v1alpha1.AzureConfig, nodeName string, ipAddr string) error {
	dnsClient, err := r.clientFactory.GetDNSRecordSetsClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return microerror.Mask(err)
	}

	r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Ensuring A record %s => %s", nodeName, ipAddr))

	record, err := dnsClient.Get(ctx, key.ResourceGroupName(cr), key.ClusterDNSDomain(cr), nodeName, dns.A)
	if IsNotFound(err) {
		// Initialize a new record.
		record = dns.RecordSet{
			Name: to.StringPtr(nodeName),
			RecordSetProperties: &dns.RecordSetProperties{
				TTL: to.Int64Ptr(60),
				ARecords: &[]dns.ARecord{
					{
						Ipv4Address: to.StringPtr(ipAddr),
					},
				},
			},
		}
	} else if err != nil {
		return microerror.Mask(err)
	}

	// Persist the record set.
	_, err = dnsClient.CreateOrUpdate(ctx, key.ResourceGroupName(cr), key.ClusterDNSDomain(cr), nodeName, dns.A, record, "", "")
	if err != nil {
		return microerror.Mask(err)
	}

	r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Ensured A record %s => %s", nodeName, ipAddr))

	return nil
}
