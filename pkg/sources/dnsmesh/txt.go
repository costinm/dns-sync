/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dnsmesh

import (
	"context"
	"strings"
	"time"

	"github.com/costinm/dns-sync/endpoint"
)

const (
	recordTemplate              = "%{record_type}"
)

// TXTRegistry implements registry interface with ownership implemented via associated TXT records
// It wraps a provider (DNS server).
type TXTRegistry struct {
	provider endpoint.Provider

	ownerID  string // refers to the owner id of the current instance

	// optional string to use to replace the asterisk in wildcard entries - without using this,
	// registry TXT records corresponding to wildcard records will be invalid (and rejected by most providers), due to
	// having a '*' appear (not as the first character) - see https://tools.ietf.org/html/rfc1034#section-4.3.3
	wildcardReplacement string

	managedRecordTypes []string
	excludeRecordTypes []string

}

// NewTXTRegistry returns new TXTRegistry object
func NewTXTRegistry(provider endpoint.Provider, txtPrefix, txtSuffix, ownerID string, cacheInterval time.Duration, txtWildcardReplacement string, managedRecordTypes, excludeRecordTypes []string, txtEncryptEnabled bool, txtEncryptAESKey []byte) (*TXTRegistry, error) {

	return &TXTRegistry{
		provider:            provider,
		ownerID:             ownerID,
		wildcardReplacement: txtWildcardReplacement,
		managedRecordTypes:  managedRecordTypes,
		excludeRecordTypes:  excludeRecordTypes,
	}, nil
}

func getSupportedTypes() []string {
	return []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME, endpoint.RecordTypeNS}
}

func (im *TXTRegistry) GetDomainFilter() endpoint.DomainFilter {
	return im.provider.GetDomainFilter()
}

func (im *TXTRegistry) OwnerID() string {
	return im.ownerID
}

// Records returns the current records from the registry excluding TXT Records
// If TXT records was created previously to indicate ownership its corresponding value
// will be added to the endpoints Labels map
func (im *TXTRegistry) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {

	records, err := im.provider.Records(ctx)
	if err != nil {
		return nil, err
	}

	endpoints := []*endpoint.Endpoint{}

	// All other labels for this record, if part of the mesh.
	labelMap := map[endpoint.EndpointKey]endpoint.Labels{}

	// Will hold all entries that have a mesh TXT record, in the same mesh.
	txtRecordsMap := map[string]struct{}{}

	for _, record := range records {
		if record.RecordType != endpoint.RecordTypeTXT {
			endpoints = append(endpoints, record)
			continue
		}

		// We simply assume that TXT records for the registry will always have only one target.
		labels, _ := endpoint.NewLabelsFromStringPlain(record.Targets[0])
		if labels["mesh"] != im.OwnerID() {
			endpoints = append(endpoints, record)
			continue
		}

		key := endpoint.EndpointKey{
			DNSName:       record.DNSName,
		}
		labelMap[key] = labels
		txtRecordsMap[record.DNSName] = struct{}{}
	}

	for _, ep := range endpoints {
		if ep.Labels == nil {
			ep.Labels = endpoint.NewLabels()
		}
		dnsNameSplit := strings.Split(ep.DNSName, ".")

		// If specified, replace a leading asterisk in the generated txt record name with some other string
		//if im.wildcardReplacement != "" && dnsNameSplit[0] == "*" {
		//	dnsNameSplit[0] = im.wildcardReplacement
		//}
		dnsName := strings.Join(dnsNameSplit, ".")

		key := endpoint.EndpointKey{
			DNSName:       dnsName,
			//RecordType:    ep.RecordType,
			//SetIdentifier: ep.SetIdentifier,
		}

		// Handle both new and old registry format with the preference for the new one
		labels, labelsExist := labelMap[key]
		if labelsExist {
			for k, v := range labels {
				ep.Labels[k] = v
			}
		}
	}

	return endpoints, nil
}

// The TXT record is associated with a name - may have multiple targets.
func (im *TXTRegistry) generateTXTRecord(r *endpoint.Endpoint) []*endpoint.Endpoint {
	endpoints := make([]*endpoint.Endpoint, 0)

	//if !im.txtEncryptEnabled && !im.mapper.recordTypeInAffix() && r.RecordType != endpoint.RecordTypeAAAA {
	//	txt := endpoint.NewEndpoint(im.mapper.toTXTName(r.DNSName), endpoint.RecordTypeTXT, r.Labels.SerializePlain(true))
	//	if txt != nil {
	//		txt.WithSetIdentifier(r.SetIdentifier)
	//		txt.Labels[endpoint.OwnedRecordLabelKey] = r.DNSName
	//		txt.ProviderSpecific = r.ProviderSpecific
	//		endpoints = append(endpoints, txt)
	//	}
	//}

	txt := endpoint.NewEndpoint(r.DNSName, endpoint.RecordTypeTXT, r.Labels.SerializePlain(true))
	if txt != nil {
		txt.WithSetIdentifier(r.SetIdentifier)
		txt.Labels[endpoint.OwnedRecordLabelKey] = r.DNSName
		endpoints = append(endpoints, txt)
	}

	return endpoints
}

// ApplyChanges updates dns provider with the changes
// for each created/deleted record it will also take into account TXT records for creation/deletion
func (im *TXTRegistry) ApplyChanges(ctx context.Context, changes *endpoint.Changes) error {
	filteredChanges := &endpoint.Changes{
		Create:    changes.Create,
		UpdateNew: endpoint.FilterEndpointsByOwnerID(im.ownerID, changes.UpdateNew),
		UpdateOld: endpoint.FilterEndpointsByOwnerID(im.ownerID, changes.UpdateOld),
		Delete:    endpoint.FilterEndpointsByOwnerID(im.ownerID, changes.Delete),
	}
	for _, r := range filteredChanges.Create {
		if r.Labels == nil {
			r.Labels = make(map[string]string)
		}
		r.Labels["mesh"] = im.ownerID

		filteredChanges.Create = append(filteredChanges.Create, im.generateTXTRecord(r)...)
	}

	for _, r := range filteredChanges.Delete {
		filteredChanges.Delete = append(filteredChanges.Delete, im.generateTXTRecord(r)...)
	}

	// make sure TXT records are consistently updated as well
	for _, r := range filteredChanges.UpdateOld {
		// when we updateOld TXT records for which value has changed (due to new label) this would still work because
		// !!! TXT record value is uniquely generated from the Labels of the endpoint. Hence old TXT record can be uniquely reconstructed
		filteredChanges.UpdateOld = append(filteredChanges.UpdateOld, im.generateTXTRecord(r)...)
	}

	// make sure TXT records are consistently updated as well
	for _, r := range filteredChanges.UpdateNew {
		filteredChanges.UpdateNew = append(filteredChanges.UpdateNew, im.generateTXTRecord(r)...)
	}

	return im.provider.ApplyChanges(ctx, filteredChanges)
}

// AdjustEndpoints modifies the endpoints as needed by the specific provider
func (im *TXTRegistry) AdjustEndpoints(endpoints []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	return im.provider.AdjustEndpoints(endpoints)
}
