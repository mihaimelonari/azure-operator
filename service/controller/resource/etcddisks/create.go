package etcddisks

import (
	"context"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/azure-operator/v4/service/controller/key"
)

func (r *Resource) EnsureCreated(ctx context.Context, obj interface{}) error {
	cr, err := key.ToCustomResource(obj)
	if err != nil {
		return microerror.Mask(err)
	}

	// Check if all needed resources are ready for the ETCD cluster to be set up.
	ready, err := r.verifyPrerequisites(ctx, cr)
	if err != nil {
		return microerror.Mask(err)
	}

	if !ready {
		r.logger.LogCtx(ctx, "level", "debug", "message", "Prerequisites not fulfilled, waiting.")
		return nil
	}

	// Setup the ETCD cluster.
	err = r.attachDisks(ctx, cr)
	if err != nil {
		return microerror.Mask(err)
	}

	// TODO Cleanup any snapshot leftovers.

	return nil
}
