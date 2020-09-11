package etcddisks

import (
	"context"
	"fmt"

	"github.com/giantswarm/apiextensions/pkg/apis/provider/v1alpha1"
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/azure-operator/v4/pkg/project"
	"github.com/giantswarm/azure-operator/v4/service/controller/key"
)

// This function checks if all the resources needed to initialize the ETCD cluster are available.
// Those resources are:
// - All Master VMSS instances are in 'Succeeded' state;
// - One certificate for each master instance (rounded to the maximum odd number, e.g. 4 masters -> 3 certificates);
// - One managed disks in 'Succeeded' state for each master instance (rounded as above).
func (r *Resource) verifyPrerequisites(ctx context.Context, cr v1alpha1.AzureConfig) (bool, error) {
	instancesDesiredCount := len(cr.Spec.Azure.Masters)

	membersDesiredCount := len(cr.Spec.Azure.Masters)

	if membersDesiredCount%2 == 0 {
		// There is an even number of Master nodes. We want only odd number of ETCD members.
		membersDesiredCount = membersDesiredCount - 1
	}

	// Cluster operator currently generates no more than 3 certificate for ETCD nodes.
	// This constitutes an upper bound on the number of ETCD members in a cluster.
	if membersDesiredCount > 3 {
		membersDesiredCount = 3
	}

	vmssClient, err := r.clientFactory.GetVirtualMachineScaleSetsClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return false, microerror.Mask(err)
	}

	vmssVMsClient, err := r.clientFactory.GetVirtualMachineScaleSetVMsClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return false, microerror.Mask(err)
	}

	disksClient, err := r.clientFactory.GetDisksClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return false, microerror.Mask(err)
	}

	// Check number of certificates.
	var readyCertificates int
	{
		r.logger.LogCtx(ctx, "level", "info", "message", "Checking availability of certificates for ETCD members")
		for i := 1; i <= membersDesiredCount; i += 1 {
			memberName := fmt.Sprintf("etcd%d", i)
			// Ensure the CertConfig for this member's certificate exists.
			_, err := r.ensureCertconfig(ctx, cr, memberName)
			if err != nil {
				return false, microerror.Mask(err)
			}

			// Retrieve the certificate for this member.
			_, err = r.getTLSPeerCert(ctx, cr, memberName)
			if err != nil {
				// Assuming certificate is not available.
				continue
			}

			readyCertificates = readyCertificates + 1
		}
		r.logger.LogCtx(ctx, "level", "info", "message", "Checked availability of certificates for ETCD members")
	}

	// Count the number of Succeeded VMSS master instances.
	var readyInstances int
	var desiredAZs []string
	{
		vmss, err := vmssClient.Get(ctx, key.ResourceGroupName(cr), key.MasterVMSSName(cr))
		if IsNotFound(err) {
			r.logger.LogCtx(ctx, "level", "info", "message", "VMSS not found, can't proceed with attachment of disks")
			return false, nil
		} else if err != nil {
			return false, microerror.Mask(err)
		}

		// If VMSS is running the previous azure operator version, we have to wait for it to be updated by the master resource.
		if *vmss.Tags[key.OperatorVersionTagName] != project.Version() {
			r.logger.LogCtx(ctx, "level", "info", "message", "VMSS is outdated, can't proceed with attachment of disks")
			return false, nil
		}

		iterator, err := vmssVMsClient.ListComplete(ctx, key.ResourceGroupName(cr), key.MasterVMSSName(cr), "", "", "")
		if IsNotFound(err) {
			r.logger.LogCtx(ctx, "level", "info", "message", "VMSS not found, can't proceed with attachment of disks")
			return false, nil
		} else if err != nil {
			return false, microerror.Mask(err)
		}

		for iterator.NotDone() {
			instance := iterator.Value()

			// Check if instance is up to date.
			if *instance.Tags[key.OperatorVersionTagName] == project.Version() {
				// Check if instance is provisioned.
				if *instance.ProvisioningState == "Succeeded" {
					readyInstances = readyInstances + 1
				}

				if len(*instance.Zones) > 0 {
					zone := (*instance.Zones)[0]

					// Add the zone into the slice (might be a duplicated, that's expected).
					desiredAZs = append(desiredAZs, zone)
				}
			}

			err := iterator.NextWithContext(ctx)
			if err != nil {
				return false, microerror.Mask(err)
			}
		}
	}

	// Check the number of ready managed disks.
	var readyDisks int
	var existingAZs []string
	{
		iterator, err := disksClient.ListByResourceGroupComplete(ctx, key.ResourceGroupName(cr))
		if err != nil {
			return false, microerror.Mask(err)
		}

		// TODO This function counts ANY disk with the right tags.
		// In case of scaling, this might be wrong.
		// Detect if the *right* disks exist instead.
		for iterator.NotDone() {
			disk := iterator.Value()

			if val, ok := disk.Tags[DiskLabelName]; ok && *val == DiskLabelValue {
				if *disk.ProvisioningState == "Succeeded" {
					readyDisks = readyDisks + 1
				}

				if len(*disk.Zones) > 0 {
					zone := (*disk.Zones)[0]

					// Keep a count of availability zones.
					existingAZs = append(existingAZs, zone)
				}
			}

			err := iterator.NextWithContext(ctx)
			if err != nil {
				return false, microerror.Mask(err)
			}
		}
	}

	ready := true
	if readyInstances == instancesDesiredCount {
		r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("%d/%d instances in 'Succeeded' state found", readyInstances, instancesDesiredCount))
	} else {
		ready = false
		r.logger.LogCtx(ctx, "level", "warning", "message", fmt.Sprintf("Expected %d instances in 'Succeeded' state, %d found", instancesDesiredCount, readyInstances))
	}

	if readyDisks == membersDesiredCount {
		r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("%d/%d disks in 'Succeeded' state found", readyDisks, membersDesiredCount))
	} else {
		ready = false
		r.logger.LogCtx(ctx, "level", "warning", "message", fmt.Sprintf("Expected %d disks in 'Succeeded' state, %d found", membersDesiredCount, readyDisks))

		// Trigger creation of missing disks.
		err = r.ensureDisks(ctx, cr, membersDesiredCount, difference(desiredAZs, existingAZs))
		if err != nil {
			return false, microerror.Mask(err)
		}
	}

	if readyCertificates == membersDesiredCount {
		r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("%d/%d certificates found", readyCertificates, membersDesiredCount))
	} else {
		ready = false
		r.logger.LogCtx(ctx, "level", "warning", "message", fmt.Sprintf("Expected %d certificates, %d found", membersDesiredCount, readyCertificates))
	}

	return ready, nil
}

// Function difference returns the elements in `a` that aren't in `b`.
// Example:
//   a = ["1", "1", "2"]
//   b = ["1"]
//   difference(a,b) = ["1", "2"]
func difference(a, b []string) []string {
	count := make(map[string]int)
	for _, x := range b {
		count[x] = count[x] + 1
	}

	var diff []string
	for _, x := range a {
		if count[x] > 0 {
			count[x] = count[x] - 1
		} else {
			diff = append(diff, x)
		}
	}
	return diff
}
