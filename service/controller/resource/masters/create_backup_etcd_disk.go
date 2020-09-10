package masters

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-07-01/compute"
	"github.com/Azure/go-autorest/autorest/to"
	providerv1alpha1 "github.com/giantswarm/apiextensions/pkg/apis/provider/v1alpha1"
	"github.com/giantswarm/microerror"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/giantswarm/azure-operator/v4/pkg/label"
	"github.com/giantswarm/azure-operator/v4/service/controller/controllercontext"
	"github.com/giantswarm/azure-operator/v4/service/controller/internal/state"
	"github.com/giantswarm/azure-operator/v4/service/controller/key"
)

const (
	DiskLabelName  = "gs-role"
	DiskLabelValue = "etcd"

	SnapshotDiskNameLabel = "gs-disk-name"
)

func (r *Resource) backupETCDDiskTransition(ctx context.Context, obj interface{}, currentState state.State) (state.State, error) {
	cr, err := key.ToCustomResource(obj)
	if err != nil {
		return "", microerror.Mask(err)
	}

	// Drain all master nodes.
	// This is needed to get the node labels updated at next reboot.
	cc, err := controllercontext.FromContext(ctx)
	if err != nil {
		return "", microerror.Mask(err)
	}

	if cc.Client.TenantCluster.K8s != nil {
		r.Logger.LogCtx(ctx, "level", "debug", "message", "Draining master nodes.")

		nodeList, err := cc.Client.TenantCluster.K8s.CoreV1().Nodes().List(metav1.ListOptions{})
		if err != nil {
			return "", microerror.Mask(err)
		}

		for _, node := range nodeList.Items {
			if isMaster(node) {
				r.Logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Creating DrainerConfig for node %s.", node.Name))
				err = r.CreateDrainerConfig(ctx, cr, node.Name)
				if err != nil {
					return "", microerror.Mask(err)
				}
				r.Logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Created DrainerConfig for node %s.", node.Name))
			}
		}

		r.Logger.LogCtx(ctx, "level", "debug", "message", "DrainerConfigs created for master nodes")
	} else {
		// Unable to connect to the k8s API.
		return "", nil
	}

	return WaitForMastersToDrain, nil
}

func (r *Resource) waitForMasterToDrainTransition(ctx context.Context, obj interface{}, currentState state.State) (state.State, error) {
	cr, err := key.ToCustomResource(obj)
	if err != nil {
		return "", microerror.Mask(err)
	}

	n := v1.NamespaceAll
	o := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", label.Cluster, key.ClusterID(&cr)),
	}

	list, err := r.G8sClient.CoreV1alpha1().DrainerConfigs(n).List(o)
	if err != nil {
		return "", microerror.Mask(err)
	}

	pending := false
	for _, drainerConfig := range list.Items {
		if drainerConfig.Status.HasDrainedCondition() || drainerConfig.Status.HasTimeoutCondition() {
			err := r.G8sClient.CoreV1alpha1().DrainerConfigs(drainerConfig.GetNamespace()).Delete(drainerConfig.GetName(), &metav1.DeleteOptions{})
			if IsNotFound(err) {
				r.Logger.LogCtx(ctx, "level", "debug", "message", "did not delete drainer config for tenant cluster node")
				r.Logger.LogCtx(ctx, "level", "debug", "message", "drainer config for tenant cluster node does not exist")
			} else if err != nil {
				return "", microerror.Mask(err)
			} else {
				r.Logger.LogCtx(ctx, "level", "debug", "message", "deleted drainer config for tenant cluster master")
			}
		} else {
			pending = true
		}
	}

	if pending {
		r.Logger.LogCtx(ctx, "level", "debug", "message", "master node still pending draining")
		return currentState, nil
	}

	return StopMasters, nil
}

func (r *Resource) stopMastersTransition(ctx context.Context, obj interface{}, currentState state.State) (state.State, error) {
	cr, err := key.ToCustomResource(obj)
	if err != nil {
		return "", microerror.Mask(err)
	}

	// Ensure VMSS instance is stopped.
	{
		isStopped, err := r.isVMSSInstanceStopped(ctx, cr)
		if err != nil {
			return "", microerror.Mask(err)
		}

		if !isStopped {
			r.Logger.LogCtx(ctx, "level", "debug", "message", "Waiting for VMSS instance to be stopped.")
			err = r.touchCR(ctx, cr)
			if err != nil {
				r.Logger.LogCtx(ctx, "level", "warning", "message", fmt.Sprintf("Error touching the CR. %s", err))
			}
			return currentState, nil
		}
	}

	return EnsureSnapshot, nil
}

func (r *Resource) ensureSnapshotTransition(ctx context.Context, obj interface{}, currentState state.State) (state.State, error) {
	cr, err := key.ToCustomResource(obj)
	if err != nil {
		return "", microerror.Mask(err)
	}

	// Create a snapshot of the disk attached to lun0.
	snapshotReady, err := r.isSnapshotReady(ctx, cr)
	if err != nil {
		return "", microerror.Mask(err)
	}

	if !snapshotReady {
		r.Logger.LogCtx(ctx, "level", "debug", "message", "Waiting for VMSS's ETCD disk snapshot to be ready.")
		err = r.touchCR(ctx, cr)
		if err != nil {
			r.Logger.LogCtx(ctx, "level", "warning", "message", fmt.Sprintf("Error touching the CR. %s", err))
		}
		return currentState, nil
	}

	// Go on with the state machine.
	return DeploymentUninitialized, nil
}

func (r *Resource) getFirstMasterVMSSInstance(ctx context.Context, cr providerv1alpha1.AzureConfig) (*compute.VirtualMachineScaleSetVM, error) {
	vmssVMsClient, err := r.ClientFactory.GetVirtualMachineScaleSetVMsClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	iterator, err := vmssVMsClient.ListComplete(ctx, key.ResourceGroupName(cr), key.MasterVMSSName(cr), "", "", "")
	if err != nil {
		return nil, microerror.Mask(err)
	}

	if iterator.NotDone() {
		instance := iterator.Value()

		return &instance, nil
	} else {
		r.Logger.LogCtx(ctx, "level", "error", "message", fmt.Sprintf("No VMSS instance found in VMSS %s", key.MasterVMSSName(cr)))
	}

	// Instance not found.
	return nil, executionFailedError
}

func (r *Resource) isSnapshotReady(ctx context.Context, cr providerv1alpha1.AzureConfig) (bool, error) {
	snapshotsClient, err := r.ClientFactory.GetSnapshotsClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return false, microerror.Mask(err)
	}

	// Look for an existing snapshot.
	{
		iterator, err := snapshotsClient.ListByResourceGroupComplete(ctx, key.ResourceGroupName(cr))
		if err != nil {
			return false, microerror.Mask(err)
		}

		for iterator.NotDone() {
			snapshot := iterator.Value()

			// Check if this snapshot comes from an ETCD backup by checking the tag.
			if val, ok := snapshot.Tags[DiskLabelName]; ok && *val == DiskLabelValue {
				if val, ok := snapshot.Tags[SnapshotDiskNameLabel]; ok && *val == "etcd1" {
					if *snapshot.ProvisioningState == "Succeeded" {
						r.Logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Found snapshot %s", *snapshot.Name))
						return true, nil
					}
				}
			}

			err := iterator.NextWithContext(ctx)
			if err != nil {
				return false, microerror.Mask(err)
			}
		}
	}

	// Snapshot not found, create one.
	{
		instance, err := r.getFirstMasterVMSSInstance(ctx, cr)
		if err != nil {
			return false, microerror.Mask(err)
		}
		r.Logger.LogCtx(ctx, "level", "error", "message", fmt.Sprintf("Looking for a disk attached to lun 0 of instance %s on VMSS %s", *instance.InstanceID, key.MasterVMSSName(cr)))
		var diskID string
		for _, disk := range *instance.StorageProfile.DataDisks {
			if to.Int32(disk.Lun) == 0 {
				// Found the desired disk.
				diskID = *disk.ManagedDisk.ID
				break
			}
		}

		// Create disk snapshot.
		r.Logger.LogCtx(ctx, "level", "error", "message", "Creating disk snapshot")
		snapshotName := "etcd1-snapshot"
		_, err = snapshotsClient.CreateOrUpdate(ctx, key.ResourceGroupName(cr), snapshotName, compute.Snapshot{
			SnapshotProperties: &compute.SnapshotProperties{
				CreationData: &compute.CreationData{
					CreateOption:     compute.Copy,
					SourceResourceID: to.StringPtr(diskID),
				},
				Incremental: to.BoolPtr(false),
			},
			Location: instance.Location,
			Tags: map[string]*string{
				DiskLabelName:         to.StringPtr(DiskLabelValue),
				SnapshotDiskNameLabel: to.StringPtr("etcd1"),
			},
		})
		if err != nil {
			return false, microerror.Mask(err)
		}
		r.Logger.LogCtx(ctx, "level", "error", "message", "Disk snapshot created")
	}

	return false, nil
}

func (r *Resource) isVMSSInstanceStopped(ctx context.Context, cr providerv1alpha1.AzureConfig) (bool, error) {
	instance, err := r.getFirstMasterVMSSInstance(ctx, cr)
	if err != nil {
		return false, microerror.Mask(err)
	}

	vmssVMsClient, err := r.ClientFactory.GetVirtualMachineScaleSetVMsClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return false, microerror.Mask(err)
	}

	instanceView, err := vmssVMsClient.GetInstanceView(ctx, key.ResourceGroupName(cr), key.MasterVMSSName(cr), *instance.InstanceID)
	if err != nil {
		return false, microerror.Mask(err)
	}

	for _, stateObj := range *instanceView.Statuses {
		if *stateObj.Code == "PowerState/deallocated" {
			r.Logger.LogCtx(ctx, "level", "error", "message", "First Master VMSS instance's is deallocated.")
			return true, nil
		}
	}

	r.Logger.LogCtx(ctx, "level", "error", "message", "Deallocating first Master VMSS instance")

	_, err = vmssVMsClient.Deallocate(ctx, key.ResourceGroupName(cr), key.MasterVMSSName(cr), *instance.InstanceID)
	if err != nil {
		return false, microerror.Mask(err)
	}

	r.Logger.LogCtx(ctx, "level", "error", "message", "Deallocated first Master VMSS instance")

	// Wait for next reconciliation loop.
	return false, nil
}
