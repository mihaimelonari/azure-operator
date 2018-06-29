package endpoints

import (
	"github.com/Azure/go-autorest/autorest"
	"github.com/giantswarm/microerror"
)

var incorrectNumberNetworkInterfacesError = microerror.New("incorrect number network interfaces")

// IsincorrectNumberNetworkInterfacesError asserts incorrectNumberNetworkInterfacesError.
func IsIncorrectNumberNetworkInterfacesError(err error) bool {
	return microerror.Cause(err) == incorrectNumberNetworkInterfacesError
}

var invalidConfigError = microerror.New("invalid config")

// IsInvalidConfig asserts invalidConfigError.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == invalidConfigError
}

var notFoundError = microerror.New("not found")

// IsNotFound asserts notFoundError.
func IsNotFound(err error) bool {
	return microerror.Cause(err) == notFoundError
}

var privateIPAddressEmptyError = microerror.New("private ip address empty")

// IsPrivateIPAddressEmptyError asserts privateIPAddressEmptyError.
func IsPrivateIPAddressEmptyError(err error) bool {
	return microerror.Cause(err) == privateIPAddressEmptyError
}

var networkInterfacesNotFoundError = microerror.New("network interfaces not found")

// IsNetworkInterfacesNotFound asserts networkInterfacesNotFoundError.
func IsNetworkInterfacesNotFound(err error) bool {
	if err == nil {
		return false
	}

	c := microerror.Cause(err)

	if c == networkInterfacesNotFoundError {
		return true
	}

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

var wrongTypeError = microerror.New("wrong type")

// IsWrongTypeError asserts wrongTypeError.
func IsWrongTypeError(err error) bool {
	return microerror.Cause(err) == wrongTypeError
}