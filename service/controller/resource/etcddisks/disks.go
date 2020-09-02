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

func (r *Resource) ensureDisks(ctx context.Context, cr v1alpha1.AzureConfig, count int, desiredAZs []string) error {
	r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Expected number of disks: %d", count))

	disksClient, err := r.clientFactory.GetDisksClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return microerror.Mask(err)
	}

	snapshotsClient, err := r.clientFactory.GetSnapshotsClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return microerror.Mask(err)
	}

	for i := 1; i <= count; i += 1 {
		name := fmt.Sprintf("etcd%d", i)
		_, err := disksClient.Get(ctx, key.ResourceGroupName(cr), name)
		if IsNotFound(err) {
			// Disk not found, have to create it.
			r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Creating disk %s", name))

			// Gets an Availability Zone from the list of desired ones then remove it from the list.
			var zone string
			{
				if len(desiredAZs) == 0 {
					r.logger.LogCtx(ctx, "level", "warning", "message", "The desired Availability Zones are still unknown, skipping creation of ETCD disk.")
					return nil
				}
				// "POP" the next zone to be used from the slice.
				zone = desiredAZs[0]
				desiredAZs = append(desiredAZs[1:])
			}

			// Look for a snapshot to create the disk from.
			var creationData compute.CreationData
			{
				r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Looking for a snapshot for disk %s", name))
				iterator, err := snapshotsClient.ListByResourceGroupComplete(ctx, key.ResourceGroupName(cr))
				if err != nil {
					return microerror.Mask(err)
				}

				creationData = compute.CreationData{
					CreateOption: compute.Empty,
				}
				for iterator.NotDone() {
					snapshot := iterator.Value()

					// Check if this snapshot comes from an ETCD backup by checking the tag.
					if val, ok := snapshot.Tags[DiskLabelName]; ok && *val == DiskLabelValue {
						if val, ok := snapshot.Tags[SnapshotDiskNameLabel]; ok && *val == name {
							r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Found snapshot %s for disk %s", *snapshot.Name, name))
							creationData.CreateOption = compute.Copy
							creationData.SourceResourceID = snapshot.ID
							break
						}
					}

					err := iterator.NextWithContext(ctx)
					if err != nil {
						return microerror.Mask(err)
					}
				}
			}

			// Create the managed disk.
			{
				_, err = disksClient.CreateOrUpdate(ctx, key.ResourceGroupName(cr), name, compute.Disk{
					DiskProperties: &compute.DiskProperties{
						CreationData: &creationData,
						DiskSizeGB:   to.Int32Ptr(100),
					},
					Location: to.StringPtr(r.azure.Location),
					Tags: map[string]*string{
						DiskLabelName: to.StringPtr(DiskLabelValue),
					},
					Zones: to.StringSlicePtr([]string{zone}),
				})
				if err != nil {
					return microerror.Mask(err)
				}

				r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Disk %s created", name))
			}
		} else if err != nil {
			return microerror.Mask(err)
		} else {
			r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Disk %s already exists", name))
		}
	}

	r.logger.LogCtx(ctx, "level", "debug", "message", "All disks created")

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
