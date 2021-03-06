package azuremachinepoolconditions

import (
	"context"

	"github.com/giantswarm/microerror"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/cluster-api-provider-azure/exp/api/v1alpha3"

	"github.com/giantswarm/azure-operator/v5/service/controller/key"
)

func (r *Resource) EnsureCreated(ctx context.Context, cr interface{}) error {
	var err error
	azureMachinePool, err := key.ToAzureMachinePool(cr)
	if err != nil {
		return microerror.Mask(err)
	}

	// ensure Ready condition
	err = r.ensureReadyCondition(ctx, &azureMachinePool)
	if err != nil {
		return microerror.Mask(err)
	}

	// TODO this is temporary, needed until we fix https://github.com/giantswarm/giantswarm/issues/15471 .
	if azureMachinePool.Status.Instances == nil {
		azureMachinePool.Status.Instances = make([]*v1alpha3.AzureMachinePoolInstanceStatus, 0)
	}

	err = r.ctrlClient.Status().Update(ctx, &azureMachinePool)
	if apierrors.IsConflict(err) {
		r.logger.Debugf(ctx, "conflict trying to save object in k8s API concurrently")
		r.logger.Debugf(ctx, "canceling resource")
		return nil
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
