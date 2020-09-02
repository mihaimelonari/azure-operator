package etcddisks

import (
	"context"

	"github.com/giantswarm/apiextensions/pkg/apis/provider/v1alpha1"
	"github.com/giantswarm/certs/v2/pkg/certs"
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/azure-operator/v4/service/controller/key"
)

func (r *Resource) getTLSPeerCert(ctx context.Context, cr v1alpha1.AzureConfig, memberName string) (*certs.TLS, error) {
	var certName certs.Cert
	switch memberName {
	case "etcd1":
		certName = certs.Etcd1Cert
	case "etcd2":
		certName = certs.Etcd2Cert
	case "etcd3":
		certName = certs.Etcd3Cert
	default:
		return nil, certUnavailableError
	}

	tls, err := r.certsSearcher.SearchTLS(key.ClusterID(&cr), certName)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return &tls, nil
}
