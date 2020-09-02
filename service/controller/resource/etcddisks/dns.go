package etcddisks

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/apiextensions/pkg/apis/provider/v1alpha1"
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/azure-operator/v4/service/controller/key"
)

func (r *Resource) updateDNSRecord(ctx context.Context, cr v1alpha1.AzureConfig, nodeName string, ipAddr string) error {
	dnsClient, err := r.clientFactory.GetDNSRecordSetsClient(cr.Spec.Azure.CredentialSecret.Namespace, cr.Spec.Azure.CredentialSecret.Name)
	if err != nil {
		return microerror.Mask(err)
	}

	r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Ensuring A record %s => %s", nodeName, ipAddr))

	record, err := dnsClient.Get(ctx, key.ResourceGroupName(cr), key.ClusterDNSDomain(cr), nodeName, dns.A)
	if IsNotFound(err) {
		// Initialize a new record.
		record = dns.RecordSet{
			Name: to.StringPtr(nodeName),
			RecordSetProperties: &dns.RecordSetProperties{
				TTL: to.Int64Ptr(60),
				ARecords: &[]dns.ARecord{
					{
						Ipv4Address: to.StringPtr(ipAddr),
					},
				},
			},
		}
	} else if err != nil {
		return microerror.Mask(err)
	}

	// Persist the record set.
	_, err = dnsClient.CreateOrUpdate(ctx, key.ResourceGroupName(cr), key.ClusterDNSDomain(cr), nodeName, dns.A, record, "", "")
	if err != nil {
		return microerror.Mask(err)
	}

	r.logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Ensured A record %s => %s", nodeName, ipAddr))

	return nil
}
