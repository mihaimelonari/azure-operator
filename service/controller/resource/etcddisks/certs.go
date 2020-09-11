package etcddisks

import (
	"context"
	"fmt"

	"github.com/giantswarm/apiextensions/pkg/apis/core/v1alpha1"
	providerv1alpha1 "github.com/giantswarm/apiextensions/pkg/apis/provider/v1alpha1"
	"github.com/giantswarm/microerror"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/giantswarm/azure-operator/v4/pkg/label"
	"github.com/giantswarm/azure-operator/v4/pkg/project"
	"github.com/giantswarm/azure-operator/v4/service/controller/key"
)

const (
	certAPIVersion = "core.giantswarm.io"
	certKind       = "CertConfig"

	loopbackIP = "127.0.0.1"

	// TODO Unhardcode me!
	certTTL                 = "4320h"
	certConfigVersionBundle = "0.1.0"
	namespace               = "default"
)

func (r *Resource) ensureCertconfig(ctx context.Context, cr providerv1alpha1.AzureConfig, memberName string) (*v1alpha1.CertConfig, error) {
	certConfig := newEtcdCertConfig(cr, memberName, namespace)
	r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Ensuring CertConfig for %s", memberName))
	certConfig, err := r.k8sClient.G8sClient().CoreV1alpha1().CertConfigs(certConfig.Namespace).Create(certConfig)
	if apierrors.IsAlreadyExists(err) {
		// fall through
	} else if err != nil {
		return nil, microerror.Mask(err)
	}

	r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Ensured CertConfig for %s", memberName))

	return certConfig, nil
}

func newEtcdCertConfig(cr providerv1alpha1.AzureConfig, memberName string, namespace string) *v1alpha1.CertConfig {
	clusterID := cr.GetName()

	certName := memberName
	return &v1alpha1.CertConfig{
		TypeMeta: apimetav1.TypeMeta{
			Kind:       certKind,
			APIVersion: certAPIVersion,
		},
		ObjectMeta: apimetav1.ObjectMeta{
			Name:      key.ETCDCertConfigName(clusterID, memberName),
			Namespace: namespace,
			Labels: map[string]string{
				label.Certificate:     certName,
				label.Cluster:         clusterID,
				label.LegacyClusterID: clusterID,
				label.LegacyComponent: certName,
				label.ManagedBy:       project.Name(),
				label.Organization:    key.OrganizationID(&cr),
			},
		},
		Spec: v1alpha1.CertConfigSpec{
			Cert: v1alpha1.CertConfigSpecCert{
				AllowBareDomains: true,
				AltNames: []string{
					key.ETCDMemberDnsName(cr, memberName),
				},
				ClusterComponent:    certName,
				ClusterID:           clusterID,
				CommonName:          cr.Spec.Cluster.Etcd.Domain,
				DisableRegeneration: false,
				IPSANs:              []string{loopbackIP},
				TTL:                 certTTL,
			},
			VersionBundle: v1alpha1.CertConfigSpecVersionBundle{
				Version: certConfigVersionBundle,
			},
		},
	}
}
