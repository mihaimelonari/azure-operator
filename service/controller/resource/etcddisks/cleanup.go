package etcddisks

import (
	"context"
	"fmt"

	"github.com/giantswarm/apiextensions/pkg/apis/provider/v1alpha1"
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/azure-operator/v4/service/controller/key"
)

func (r *Resource) cleanupSnapshots(ctx context.Context, cr v1alpha1.AzureConfig) error {
	disksClient, err := r.clientFactory.GetDisksClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return microerror.Mask(err)
	}

	snapshotsClient, err := r.clientFactory.GetSnapshotsClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return microerror.Mask(err)
	}

	iterator, err := snapshotsClient.ListByResourceGroupComplete(ctx, key.ResourceGroupName(cr))
	if err != nil {
		return microerror.Mask(err)
	}

	for iterator.NotDone() {
		snapshot := iterator.Value()

		if val, ok := snapshot.Tags[DiskLabelName]; ok && *val == DiskLabelValue {
			diskName := *snapshot.Tags[SnapshotDiskNameLabel]

			// Check if disk exists and is provisioned.
			disk, err := disksClient.Get(ctx, key.ResourceGroupName(cr), diskName)
			if IsNotFound(err) {
				// Snapshot has to be kept.
			} else if err != nil {
				return microerror.Mask(err)
			} else {
				if *disk.ProvisioningState == "Succeeded" {
					// Disk is provisioned, we can safely delete the snapshot.
					r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Snapshot %s was used for disk %s, deleting it.", *snapshot.Name, *disk.Name))
					_, err := snapshotsClient.Delete(ctx, key.ResourceGroupName(cr), *snapshot.Name)
					if err != nil {
						return microerror.Mask(err)
					}
				}
			}
		}

		err := iterator.NextWithContext(ctx)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	return nil
}
