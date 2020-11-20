package azureclusterconditions

import (
	"context"

	"github.com/giantswarm/microerror"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/giantswarm/azure-operator/v5/service/controller/key"
)

func (r *Resource) EnsureCreated(ctx context.Context, cr interface{}) error {
	var err error
	azureCluster, err := key.ToAzureCluster(cr)
	if err != nil {
		return microerror.Mask(err)
	}

	// ensure Ready condition
	err = r.ensureReadyCondition(ctx, &azureCluster)
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.ctrlClient.Status().Update(ctx, &azureCluster)
	if apierrors.IsConflict(err) {
		r.logger.LogCtx(ctx, "level", "debug", "message", "conflict trying to save object in k8s API concurrently", "stack", microerror.JSON(microerror.Mask(err)))
		r.logger.LogCtx(ctx, "level", "debug", "message", "canceling resource")
		return nil
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
