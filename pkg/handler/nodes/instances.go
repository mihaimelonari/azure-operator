package nodes

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-07-01/compute"
	azureresource "github.com/Azure/azure-sdk-for-go/services/resources/mgmt/2019-05-01/resources"
	providerv1alpha1 "github.com/giantswarm/apiextensions/v3/pkg/apis/provider/v1alpha1"
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/azure-operator/v5/pkg/handler/nodes/scalestrategy"
	"github.com/giantswarm/azure-operator/v5/service/controller/key"
)

func (r *Resource) AllInstances(ctx context.Context, customObject providerv1alpha1.AzureConfig, deploymentNameFunc func(customObject providerv1alpha1.AzureConfig) string) ([]compute.VirtualMachineScaleSetVM, error) {
	r.Logger.Debugf(ctx, "looking for the scale set '%s'", deploymentNameFunc(customObject))

	c, err := r.ClientFactory.GetVirtualMachineScaleSetVMsClient(ctx, customObject.ObjectMeta)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	g := key.ResourceGroupName(customObject)
	s := deploymentNameFunc(customObject)
	result, err := c.List(ctx, g, s, "", "", "")
	if IsScaleSetNotFound(err) {
		return nil, microerror.Mask(scaleSetNotFoundError)
	} else if err != nil {
		return nil, microerror.Mask(err)
	}

	var instances []compute.VirtualMachineScaleSetVM

	for result.NotDone() {
		instances = append(instances, result.Values()...)

		err := result.NextWithContext(ctx)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	r.Logger.Debugf(ctx, "found the scale set '%s'", deploymentNameFunc(customObject))

	return instances, nil
}

func (r *Resource) GetVMSSInstances(ctx context.Context, virtualMachineScaleSetVMsClient *compute.VirtualMachineScaleSetVMsClient, resourceGroupName, vmssName string) ([]compute.VirtualMachineScaleSetVM, error) {
	r.Logger.Debugf(ctx, "looking for the scale set %#q", vmssName)

	result, err := virtualMachineScaleSetVMsClient.List(ctx, resourceGroupName, vmssName, "", "", "")
	if err != nil {
		return nil, microerror.Mask(err)
	}

	var instances []compute.VirtualMachineScaleSetVM

	for result.NotDone() {
		instances = append(instances, result.Values()...)

		err := result.NextWithContext(ctx)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	r.Logger.Debugf(ctx, "found %d instances in the scale set %#q", len(instances), vmssName)

	return instances, nil
}

func (r *Resource) GetInstancesCount(ctx context.Context, virtualMachineScaleSetsClient *compute.VirtualMachineScaleSetsClient, resourceGroupName, vmssName string) (int64, error) {
	vmss, err := virtualMachineScaleSetsClient.Get(ctx, resourceGroupName, vmssName)
	if err != nil {
		return 0, microerror.Mask(err)
	}

	return *vmss.Sku.Capacity, nil
}

func (r *Resource) ScaleVMSS(ctx context.Context, virtualMachineScaleSetsClient *compute.VirtualMachineScaleSetsClient, resourceGroup, vmssName string, desiredNodeCount int64, scaleStrategy scalestrategy.Interface) error {
	vmss, err := virtualMachineScaleSetsClient.Get(ctx, resourceGroup, vmssName)
	if err != nil {
		return microerror.Mask(err)
	}

	computedCount := scaleStrategy.GetNodeCount(*vmss.Sku.Capacity, desiredNodeCount)
	*vmss.Sku.Capacity = computedCount
	res, err := virtualMachineScaleSetsClient.CreateOrUpdate(ctx, resourceGroup, vmssName, vmss)
	if err != nil {
		return microerror.Mask(err)
	}

	_, err = virtualMachineScaleSetsClient.CreateOrUpdateResponder(res.Response())
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *Resource) CreateARMDeployment(ctx context.Context, deploymentsClient *azureresource.DeploymentsClient, computedDeployment azureresource.Deployment, resourceGroupName, deploymentName string) error {
	res, err := deploymentsClient.CreateOrUpdate(ctx, resourceGroupName, deploymentName, computedDeployment)
	if err != nil {
		return microerror.Mask(err)
	}

	_, err = deploymentsClient.CreateOrUpdateResponder(res.Response())
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
