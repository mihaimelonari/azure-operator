package etcddisks

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-07-01/compute"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/apiextensions/pkg/apis/provider/v1alpha1"
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/azure-operator/v4/service/controller/key"
)

func (r *Resource) ensureDisks(ctx context.Context, cr v1alpha1.AzureConfig) error {
	count := len(cr.Spec.Azure.Masters)
	if count%2 == 0 {
		// There is an even number of Master nodes. We want only odd number of ETCD members.
		count = count - 1
	}

	// Cluster operator currently generates no more than 3 certificate for ETCD nodes.
	// This constitutes an upper bound on the number of ETCD members in a cluster.
	if count > 3 {
		count = 3
	}

	r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Expected number of disks: %d", count))

	disksClient, err := r.clientFactory.GetDisksClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return microerror.Mask(err)
	}

	desiredAZs := key.AvailabilityZones(cr, r.azure.Location)

	// TODO create disks asynchronously.
	for i := 1; i <= count; i += 1 {
		name := fmt.Sprintf("etcd%d", i)
		_, err := disksClient.Get(ctx, key.ResourceGroupName(cr), name)
		if IsNotFound(err) {
			r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Creating disk %s", name))

			// Gets an Availability Zone from the list of the ones used by the cluster.
			// If there are enough instances in the cluster, there should be at least one Disk per AZ.
			zone := strconv.Itoa(desiredAZs[(i-1)%len(desiredAZs)])

			// Disk not found, have to create it.
			future, err := disksClient.CreateOrUpdate(ctx, key.ResourceGroupName(cr), name, compute.Disk{
				DiskProperties: &compute.DiskProperties{
					CreationData: &compute.CreationData{
						CreateOption: compute.Empty,
					},
					DiskSizeGB: to.Int32Ptr(100),
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

			err = future.WaitForCompletionRef(ctx, disksClient.Client)
			if err != nil {
				return microerror.Mask(err)
			}

			r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Disk %s created", name))
		} else if err != nil {
			return microerror.Mask(err)
		} else {
			r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Disk %s already exists", name))
		}
	}

	r.logger.LogCtx(ctx, "level", "debug", "message", "All disks created")

	return nil
}
