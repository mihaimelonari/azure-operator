package vnetpeeringcleaner

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2018-06-01/network"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"

	"github.com/giantswarm/azure-operator/client"
	"github.com/giantswarm/azure-operator/service/controller/setting"
	"github.com/giantswarm/azure-operator/service/controller/v5/controllercontext"
)

const (
	Name = "vnetpeeringcleanerv5"
)

// Config is the configuration required by Resource.
type Config struct {
	Logger micrologger.Logger

	Azure       setting.Azure
	AzureConfig client.AzureClientSetConfig
}

// Resource manages Azure virtual network peering.
type Resource struct {
	logger micrologger.Logger

	azure       setting.Azure
	azureConfig client.AzureClientSetConfig
}

func New(config Config) (*Resource, error) {
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}
	if err := config.AzureConfig.Validate(); err != nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.AzureConfig.%s", config, err)
	}
	if err := config.Azure.Validate(); err != nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Azure.%s", config, err)
	}

	r := &Resource{
		logger: config.Logger,

		azure:       config.Azure,
		azureConfig: config.AzureConfig,
	}

	return r, nil
}

func (r *Resource) Name() string {
	return Name
}

// getVnetPeeringHostClient return an azure client to interact with
// VirtualNetworkPeering resources in the host cluster account.
func (r *Resource) getVnetPeeringHostClient() (*network.VirtualNetworkPeeringsClient, error) {
	azureClients, err := client.NewAzureClientSet(r.azureConfig)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return azureClients.VnetPeeringClient, nil
}

// getVnetPeeringGuestClient return an azure client to interact with
// VirtualNetworkPeering resources in the guest cluster account.
func (r *Resource) getVnetPeeringGuestClient(ctx context.Context) (*network.VirtualNetworkPeeringsClient, error) {
	cc, err := controllercontext.FromContext(ctx)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return cc.AzureClientSet.VnetPeeringClient, nil
}