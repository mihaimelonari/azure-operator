package etcddisks

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-07-01/compute"
	"github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/apiextensions/pkg/apis/provider/v1alpha1"
	"github.com/giantswarm/certs/v2/pkg/certs"
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

func (r *Resource) findAvailableDisk(ctx context.Context, cr v1alpha1.AzureConfig, az string) (string, error) {
	disksClient, err := r.clientFactory.GetDisksClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return "", microerror.Mask(err)
	}

	iterator, err := disksClient.ListByResourceGroupComplete(ctx, key.ResourceGroupName(cr))
	if err != nil {
		return "", microerror.Mask(err)
	}

	var availableInAnotherAZ *compute.Disk
	for iterator.NotDone() {
		candidate := iterator.Value()

		if val, ok := candidate.Tags[DiskLabelName]; ok && *val == DiskLabelValue {
			// TODO This does not take into account disks being attached.
			if *candidate.ProvisioningState == "Succeeded" && candidate.ManagedBy == nil {
				// Disk is unattached.

				// Check availabilty zone.
				if az != "" && (*candidate.Zones)[0] != az {
					r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Disk %s can't be used because availability zone does not match.", *candidate.Name))
					availableInAnotherAZ = &candidate
				} else {
					r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Found available disk: %s", *candidate.Name))
					return *candidate.Name, nil
				}
			}
		}

		err := iterator.NextWithContext(ctx)
		if err != nil {
			return "", microerror.Mask(err)
		}
	}

	// We didn't find any disk ready to be attached.
	if availableInAnotherAZ != nil {
		r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Moving disk %s to zone %s.", *availableInAnotherAZ.Name, az))
		// There an available disk in a different AZ.
		// We migrate it to the desired AZ.
		//availableInAnotherAZ.Zones = to.StringSlicePtr([]string{az})

		// Clean up tags to make this disk not selectable any more.
		{
			r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Removing all tags from disk %s.", *availableInAnotherAZ.Name))
			availableInAnotherAZ.Tags = map[string]*string{}

			future, err := disksClient.CreateOrUpdate(ctx, key.ResourceGroupName(cr), *availableInAnotherAZ.Name, *availableInAnotherAZ)
			if err != nil {
				return "", microerror.Mask(err)
			}

			// Wait for the tag to be removed.
			err = future.WaitForCompletionRef(ctx, disksClient.Client)
			if err != nil {
				return "", microerror.Mask(err)
			}

			r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Removed all tags from disk %s.", *availableInAnotherAZ.Name))
		}

		// Create a snapshot of the source disk.
		{
			r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Creating a snapshot from disk %s.", *availableInAnotherAZ.Name))

			snapshotsClient, err := r.clientFactory.GetSnapshotsClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
			if err != nil {
				return "", microerror.Mask(err)
			}

			snapshotName := fmt.Sprintf("%s-snapshot", *availableInAnotherAZ.Name)
			future, err := snapshotsClient.CreateOrUpdate(ctx, key.ResourceGroupName(cr), snapshotName, compute.Snapshot{
				SnapshotProperties: &compute.SnapshotProperties{
					CreationData: &compute.CreationData{
						CreateOption:     compute.Copy,
						SourceResourceID: availableInAnotherAZ.ID,
					},
					Incremental: to.BoolPtr(false),
				},
				Location: availableInAnotherAZ.Location,
				Tags: map[string]*string{
					DiskLabelName:         to.StringPtr(DiskLabelValue),
					SnapshotDiskNameLabel: availableInAnotherAZ.Name,
				},
			})
			if err != nil {
				return "", microerror.Mask(err)
			}

			err = future.WaitForCompletionRef(ctx, snapshotsClient.Client)
			if err != nil {
				return "", microerror.Mask(err)
			}

			r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Created snapshot %s from disk %s.", snapshotName, *availableInAnotherAZ.Name))
		}

		// Delete source disk.
		{
			r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Deleting disk %s.", *availableInAnotherAZ.Name))
			future, err := disksClient.Delete(ctx, key.ResourceGroupName(cr), *availableInAnotherAZ.Name)
			if err != nil {
				return "", microerror.Mask(err)
			}

			// Wait for the disk to be deleted.
			err = future.WaitForCompletionRef(ctx, disksClient.Client)
			if err != nil {
				return "", microerror.Mask(err)
			}

			r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Deleted disk %s.", *availableInAnotherAZ.Name))
		}

		// We triggered the AZ change but we still return no disk available.
		// Disk will be attached during next reconciliation loop.
	}

	return "", nil
}

func (r *Resource) getTLSPeerCert(ctx context.Context, cr v1alpha1.AzureConfig, memberName string) (*certs.TLS, error) {
	var certName certs.Cert
	switch memberName {
	case "etcd1":
		certName = certs.Etcd1Cert
	case "etcd2":
		certName = certs.Etcd2Cert
	case "etcd3":
		certName = certs.Etcd3Cert
	default:
		return nil, certUnavailableError
	}

	tls, err := r.certsSearcher.SearchTLS(key.ClusterID(&cr), certName)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return &tls, nil
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
