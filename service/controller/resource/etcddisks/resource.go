package etcddisks

import (
	"github.com/giantswarm/k8sclient/v3/pkg/k8sclient"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"

	"github.com/giantswarm/azure-operator/v4/client"
	"github.com/giantswarm/azure-operator/v4/service/controller/setting"
)

const (
	Name           = "ETCDDisks"
	DiskLabelName  = "gs-role"
	DiskLabelValue = "etcd"
)

type Config struct {
	K8sClient k8sclient.Interface
	Logger    micrologger.Logger

	Azure         setting.Azure
	ClientFactory *client.Factory
}

type Resource struct {
	k8sClient k8sclient.Interface
	logger    micrologger.Logger

	azure         setting.Azure
	clientFactory *client.Factory
}

func New(config Config) (*Resource, error) {
	if config.K8sClient == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.K8sClient must not be empty", config)
	}
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}
	if err := config.Azure.Validate(); err != nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Azure.%s", config, err)
	}
	if config.ClientFactory == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.ClientFactory must not be empty", config)
	}

	r := &Resource{
		k8sClient: config.K8sClient,
		logger:    config.Logger,

		azure:         config.Azure,
		clientFactory: config.ClientFactory,
	}

	return r, nil
}

// Name returns the resource name.
func (r *Resource) Name() string {
	return Name
}
