// +build k8srequired

package clusterdeletion

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Azure/go-autorest/autorest"
	"github.com/giantswarm/backoff"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capiv1alpha3 "sigs.k8s.io/cluster-api/api/v1alpha3"

	"github.com/giantswarm/azure-operator/v5/e2e/setup"
)

func Test_ClusterDeletion(t *testing.T) {
	err := deletecluster.Test(context.Background())
	if err != nil {
		t.Fatalf("%#v", err)
	}
}

type Config struct {
	ClusterID string
	Logger    micrologger.Logger
	Provider  *Provider
}

type ClusterDeletion struct {
	clusterID string
	logger    micrologger.Logger
	provider  *Provider
}

func New(config Config) (*ClusterDeletion, error) {
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}
	if config.Provider == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Provider must not be empty", config)
	}
	if config.ClusterID == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.ClusterID must not be empty", config)
	}

	s := &ClusterDeletion{
		logger:    config.Logger,
		provider:  config.Provider,
		clusterID: config.ClusterID,
	}

	return s, nil
}

func (s *ClusterDeletion) Test(ctx context.Context) error {
	s.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("deleting Cluster CR %#q", s.provider.clusterID))
	cluster := &capiv1alpha3.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: setup.OrganizationNamespace,
			Name:      s.clusterID,
		},
		Spec: capiv1alpha3.ClusterSpec{},
	}
	err := s.provider.ctrlClient.Delete(ctx, cluster)
	if err != nil {
		return microerror.Mask(err)
	}

	s.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("ensuring deletion of Azure Resource Group %#q", s.provider.clusterID))
	o := func() error {
		_, err := s.provider.azureClient.ResourceGroupsClient.Get(ctx, s.provider.clusterID)
		if err != nil {
			reqError, ok := err.(autorest.DetailedError)
			if ok {
				if reqError.StatusCode == http.StatusNotFound {
					return nil
				}
			}

			return microerror.Mask(err)
		}

		return microerror.Maskf(executionFailedError, "The resource group still exists")
	}
	b := backoff.NewExponential(240*time.Minute, backoff.LongMaxInterval)
	n := backoff.NewNotifier(s.logger, ctx)
	err = backoff.RetryNotify(o, b, n)
	if err != nil {
		s.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("did not ensure deletion of Azure Resource Group %#q", s.provider.clusterID))
		return microerror.Mask(err)
	}

	return nil
}
