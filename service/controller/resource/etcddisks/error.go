package etcddisks

import (
	"github.com/Azure/go-autorest/autorest"
	"github.com/giantswarm/microerror"
)

var ipAddressUnavailableError = &microerror.Error{
	Kind: "ipAddressUnavailableError",
}

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

// IsNotFound asserts notFoundError.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}

	c := microerror.Cause(err)

	{
		dErr, ok := c.(autorest.DetailedError)
		if ok {
			if dErr.StatusCode == 404 {
				return true
			}
		}
	}

	return false
}
