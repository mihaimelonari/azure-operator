package masters

import (
	"context"

	"github.com/giantswarm/azure-operator/v5/pkg/handler/nodes/state"
)

func (r *Resource) provisioningSuccessfulTransition(ctx context.Context, obj interface{}, currentState state.State) (state.State, error) {
	r.Logger.Debugf(ctx, "Master VMSS deployment successfully provisioned")
	return ClusterUpgradeRequirementCheck, nil
}
