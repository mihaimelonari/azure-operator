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

	r.logger.LogCtx(ctx, "level", "debug", "message", "Ensuring ETCD disks are created")

	err = r.ensureDisks(ctx, cr)
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.attachDisks(ctx, cr)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
