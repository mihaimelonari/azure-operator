package vnetpeering

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2019-11-01/network"
	"github.com/giantswarm/apiextensions/pkg/apis/provider/v1alpha1"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/operatorkit/controller/context/reconciliationcanceledcontext"
	"github.com/giantswarm/to"

	"github.com/giantswarm/azure-operator/v3/service/controller/key"
)

const (
	ProvisioningStateDeleting = "Deleting"
)

// This resource manages the VNet peering between the control plane and tenant cluster.
func (r *Resource) EnsureCreated(ctx context.Context, obj interface{}) error {
	cr, err := key.ToCustomResource(obj)
	if err != nil {
		return microerror.Mask(err)
	}

	// Check if TC vnet exists.
	r.logger.LogCtx(ctx, "level", "debug", "message", "Checking if TC virtual network exists")
	tcVnetClient, err := r.getTCVnetClient(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	tcVnet, err := tcVnetClient.Get(ctx, key.ResourceGroupName(cr), key.VnetName(cr), "")
	if IsNotFound(err) {
		r.logger.LogCtx(ctx, "level", "debug", "message", "TC Virtual network does not exist")
		reconciliationcanceledcontext.SetCanceled(ctx)
		r.logger.LogCtx(ctx, "level", "debug", "message", "canceling reconciliation")
		return nil
	} else if err != nil {
		return microerror.Mask(err)
	}

	r.logger.LogCtx(ctx, "level", "debug", "message", "TC Virtual network exists")

	// Check if CP vnet exists.
	r.logger.LogCtx(ctx, "level", "debug", "message", "Checking if CP virtual network exists")
	cpVnetClient, err := r.getCPVnetClient()
	if err != nil {
		return microerror.Mask(err)
	}

	cpVnet, err := cpVnetClient.Get(ctx, r.installationName, r.installationName, "")
	if IsNotFound(err) {
		r.logger.LogCtx(ctx, "level", "debug", "message", "CP Virtual network does not exist")
		reconciliationcanceledcontext.SetCanceled(ctx)
		r.logger.LogCtx(ctx, "level", "debug", "message", "canceling reconciliation")
		return nil
	} else if err != nil {
		return microerror.Mask(err)
	}

	r.logger.LogCtx(ctx, "level", "debug", "message", "CP Virtual network exists")

	// Create vnet peering on the tenant cluster side.
	tcPeering := r.getTCVnetPeering(*cpVnet.ID)
	tcPeeringClient, err := r.getTCVnetPeeringsClient(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	r.logger.LogCtx(ctx, "level", "debug", "message", "Ensuring vnet peering exists on the tenant cluster vnet")
	_, err = tcPeeringClient.CreateOrUpdate(ctx, key.ResourceGroupName(cr), key.VnetName(cr), r.installationName, tcPeering)
	if err != nil {
		return microerror.Mask(err)
	}

	// Create vnet peering on the control plane side.
	cpPeering := r.getCPVnetPeering(*tcVnet.ID)
	cpPeeringClient, err := r.getCPVnetPeeringsClient()
	if err != nil {
		return microerror.Mask(err)
	}

	r.logger.LogCtx(ctx, "level", "debug", "message", "Ensuring vnet peering exists on the control plane vnet")
	_, err = cpPeeringClient.CreateOrUpdate(ctx, r.installationName, r.installationName, key.ResourceGroupName(cr), cpPeering)
	if err != nil {
		return microerror.Mask(err)
	}

	// Delete VPN Gateway.
	err = r.ensureVnetGatewayIsDeleted(ctx, cr)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *Resource) ensureVnetGatewayIsDeleted(ctx context.Context, cr v1alpha1.AzureConfig) error {
	gc, err := r.getVnetGatewaysClient(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	r.logger.LogCtx(ctx, "level", "debug", "message", "Checking if the VPN gateway still exists")

	gw, err := gc.Get(ctx, key.ResourceGroupName(cr), key.VPNGatewayName(cr))
	if IsNotFound(err) {
		// VPN gateway not found. That's our goal, all good.
		// Let's check if the public IP address still exist and delete that as well.
		r.logger.LogCtx(ctx, "level", "debug", "message", "VPN gateway does not exists")

		ipsClient, err := r.getPublicIPAddressesClient(ctx)
		if err != nil {
			return microerror.Mask(err)
		}

		r.logger.LogCtx(ctx, "level", "debug", "message", "Checking if the VPN gateway's public IP still exists")

		_, err = ipsClient.Get(ctx, key.ResourceGroupName(cr), key.VPNGatewayPublicIPName(cr), "")
		if IsNotFound(err) {
			// That's the desired state, all good.
			r.logger.LogCtx(ctx, "level", "debug", "message", "VPN gateway's public IP does not exists")
			return nil
		}

		r.logger.LogCtx(ctx, "level", "debug", "message", "VPN gateway's public IP still exists, requesting deletion")

		_, err = ipsClient.Delete(ctx, key.ResourceGroupName(cr), key.VPNGatewayPublicIPName(cr))
		if err != nil {
			return microerror.Mask(err)
		}

		r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Requested deletion of public IP %s", key.VPNGatewayPublicIPName(cr)))

		return nil
	} else if err != nil {
		return microerror.Mask(err)
	}

	r.logger.LogCtx(ctx, "level", "debug", "message", "VPN Gateway still exists")

	if gw.ProvisioningState == ProvisioningStateDeleting {
		r.logger.LogCtx(ctx, "level", "debug", "message", "VPN Gateway deletion in progress")
		return nil
	}

	r.logger.LogCtx(ctx, "level", "debug", "message", "Checking if there are existing connections")

	cc, err := r.getVnetGatewaysConnectionsClient(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	results, err := cc.ListComplete(ctx, key.ResourceGroupName(cr))
	if err != nil {
		return microerror.Mask(err)
	}

	found := false
	for results.NotDone() {
		c := results.Value()

		if *c.VirtualNetworkGateway1.ID == *gw.ID || *c.VirtualNetworkGateway2.ID == *gw.ID {
			r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Found VPN connection %s to be deleted", *c.Name))

			_, err := cc.Delete(ctx, key.ResourceGroupName(cr), *c.Name)
			if err != nil {
				return microerror.Mask(err)
			}

			found = true
		}

		err = results.NextWithContext(ctx)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	if !found {
		// No connections have been found, safe to delete the VPN Gateway.
		r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("No connections found, deleting VPN Gateway %s", *gw.Name))

		_, err := gc.Delete(ctx, key.ResourceGroupName(cr), *gw.Name)
		if err != nil {
			return microerror.Mask(err)
		}
		return nil
	}

	return nil
}

func (r *Resource) getCPVnetPeering(vnetId string) network.VirtualNetworkPeering {
	peering := network.VirtualNetworkPeering{
		VirtualNetworkPeeringPropertiesFormat: &network.VirtualNetworkPeeringPropertiesFormat{
			AllowVirtualNetworkAccess: to.BoolP(true),
			AllowForwardedTraffic:     to.BoolP(false),
			AllowGatewayTransit:       to.BoolP(false),
			UseRemoteGateways:         to.BoolP(false),
			RemoteVirtualNetwork: &network.SubResource{
				ID: &vnetId,
			},
		},
	}

	return peering
}

func (r *Resource) getTCVnetPeering(vnetId string) network.VirtualNetworkPeering {
	peering := network.VirtualNetworkPeering{
		VirtualNetworkPeeringPropertiesFormat: &network.VirtualNetworkPeeringPropertiesFormat{
			AllowVirtualNetworkAccess: to.BoolP(true),
			AllowForwardedTraffic:     to.BoolP(false),
			AllowGatewayTransit:       to.BoolP(false),
			UseRemoteGateways:         to.BoolP(false),
			RemoteVirtualNetwork: &network.SubResource{
				ID: &vnetId,
			},
		},
	}

	return peering
}
