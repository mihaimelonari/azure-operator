package masters

import (
	"context"
	"strconv"
	"time"

	"github.com/giantswarm/apiextensions/pkg/apis/provider/v1alpha1"
	"github.com/giantswarm/microerror"
)

const (
	TouchStatusName = "Touch"
)

func (r *Resource) touchCR(ctx context.Context, cr v1alpha1.AzureConfig) error {

	err := r.SetResourceStatus(cr, TouchStatusName, strconv.FormatInt(time.Now().Unix(), 10))
	if err != nil {
		return microerror.Mask(err)
	}

	r.Logger.LogCtx(ctx, "level", "debug", "message", "Touched the CR to trigger another reconciliation loop")

	return nil
}
