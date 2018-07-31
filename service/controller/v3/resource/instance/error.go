package instance

import (
	"github.com/Azure/go-autorest/autorest"
	"github.com/giantswarm/microerror"
)

var invalidConfigError = microerror.New("invalid config")

// IsInvalidConfig asserts invalidConfigError.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == invalidConfigError
}

var deploymentNotFoundError = microerror.New("not found")

// IsDeploymentNotFound asserts deploymentNotFoundError.
func IsDeploymentNotFound(err error) bool {
	if err == nil {
		return false
	}

	c := microerror.Cause(err)

	if c == deploymentNotFoundError {
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

var missingLabelError = microerror.New("missing label")

// IsMissingLabel asserts missingLabelError.
func IsMissingLabel(err error) bool {
	return microerror.Cause(err) == missingLabelError
}

var scaleSetNotFoundError = microerror.New("scale set not found")

// IsScaleSetNotFound asserts scaleSetNotFoundError.
func IsScaleSetNotFound(err error) bool {
	if err == nil {
		return false
	}

	c := microerror.Cause(err)

	if c == scaleSetNotFoundError {
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

var versionBlobEmptyError = microerror.New("version blob empty")

// IsVersionBlobEmpty asserts versionBlobEmptyError.
func IsVersionBlobEmpty(err error) bool {
	return microerror.Cause(err) == versionBlobEmptyError
}
