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

package mem

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/costinm/dns-sync/endpoint"
	"github.com/costinm/dns-sync/provider"
)

// Memory provider, based on the external-dns 'inmemory' provider.
// Refactored/simplified - mainly intended for testing.

var (
	// ErrZoneAlreadyExists error returned when zoneRecords cannot be created when it already exists
	ErrZoneAlreadyExists = errors.New("specified zoneRecords already exists")
	// ErrZoneNotFound error returned when specified zoneRecords does not exists
	ErrZoneNotFound = errors.New("specified zoneRecords not found")
	// ErrRecordAlreadyExists when create request is sent but record already exists
	ErrRecordAlreadyExists = errors.New("record already exists")
	// ErrRecordNotFound when update/delete request is sent but record not found
	ErrRecordNotFound = errors.New("record not found")
	// ErrDuplicateRecordFound when record is repeated in create/update/delete
	ErrDuplicateRecordFound = errors.New("invalid batch request")
)

type zoneRecords map[endpoint.EndpointKey]*endpoint.Endpoint

// InMemoryProvider - dns provider only used for testing purposes
// initialized as dns provider with no records
type InMemoryProvider struct {
	provider.BaseProvider

	domain         endpoint.DomainFilter

	Name string

	ZoneDomains map[string]string

	zones map[string]zoneRecords

	filter         *filter

	// Callbacks for logging and testing
	OnApplyChanges func(ctx context.Context, changes *endpoint.Changes)
	OnRecords      func()
}

// NewInMemoryProvider returns InMemoryProvider DNS provider interface implementation
func NewInMemoryProvider(n string, sync *endpoint.SyncConfig) *InMemoryProvider {
	var domainFilter endpoint.DomainFilter
	domainFilter = endpoint.NewDomainFilterWithExclusions(sync.DomainFilter, sync.ExcludeDomains)
	im := &InMemoryProvider{
		filter:         &filter{},
		OnApplyChanges: func(ctx context.Context, changes *endpoint.Changes) {},
		OnRecords:      func() {},
		domain:         domainFilter,
		Name: n,
	}
	im.zones = map[string]zoneRecords{}
	im.ZoneDomains = sync.Zones

	if len(sync.Zones) == 0 {
		im.ZoneDomains = map[string]string{"default": ""}
	}
	for k := range im.ZoneDomains {
		im.CreateZone(k)
	}
	return im
}

// Zones returns filtered zones as specified by domain
func (im *InMemoryProvider) Zones() map[string]string {
	return im.filter.Zones(im.Zones())
}

// Records returns the list of endpoints
func (im *InMemoryProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	defer im.OnRecords()

	endpoints := make([]*endpoint.Endpoint, 0)

	for zoneID := range im.ZoneDomains {
		records, err := im.RecordsZone(zoneID)
		if err != nil {
			return nil, err
		}

		endpoints = append(endpoints, copyEndpoints(records)...)
	}

	return endpoints, nil
}

// ApplyChanges simply modifies records in memory
// error checking occurs before any modifications are made, i.e. batch processing
// create record - record should not exist
// update/delete record - record should exist
// create/update/delete lists should not have overlapping records
func (im *InMemoryProvider) ApplyChanges(ctx context.Context, changes *endpoint.Changes) error {
	defer im.OnApplyChanges(ctx, changes)

	perZoneChanges := map[string]*endpoint.Changes{}

	zones := im.ZoneDomains
	for zoneID := range zones {
		perZoneChanges[zoneID] = &endpoint.Changes{}
	}

	for _, ep := range changes.Create {
		zoneID := im.filter.EndpointZoneID(ep, zones)
		if zoneID == "" {
			continue
		}
		perZoneChanges[zoneID].Create = append(perZoneChanges[zoneID].Create, ep)
	}
	for _, ep := range changes.UpdateNew {
		zoneID := im.filter.EndpointZoneID(ep, zones)
		if zoneID == "" {
			continue
		}
		perZoneChanges[zoneID].UpdateNew = append(perZoneChanges[zoneID].UpdateNew, ep)
	}
	for _, ep := range changes.UpdateOld {
		zoneID := im.filter.EndpointZoneID(ep, zones)
		if zoneID == "" {
			continue
		}
		perZoneChanges[zoneID].UpdateOld = append(perZoneChanges[zoneID].UpdateOld, ep)
	}
	for _, ep := range changes.Delete {
		zoneID := im.filter.EndpointZoneID(ep, zones)
		if zoneID == "" {
			continue
		}
		perZoneChanges[zoneID].Delete = append(perZoneChanges[zoneID].Delete, ep)
	}

	for zoneID := range perZoneChanges {
		change := &endpoint.Changes{
			Create:    perZoneChanges[zoneID].Create,
			UpdateNew: perZoneChanges[zoneID].UpdateNew,
			UpdateOld: perZoneChanges[zoneID].UpdateOld,
			Delete:    perZoneChanges[zoneID].Delete,
		}
		err := im.ApplyChangesZone(ctx, zoneID, change)
		if err != nil {
			return err
		}
	}

	return nil
}

func copyEndpoints(endpoints []*endpoint.Endpoint) []*endpoint.Endpoint {
	records := make([]*endpoint.Endpoint, 0, len(endpoints))
	for _, ep := range endpoints {
		newEp := endpoint.NewEndpointWithTTL(ep.DNSName, ep.RecordType, ep.RecordTTL, ep.Targets...).WithSetIdentifier(ep.SetIdentifier)
		newEp.Labels = endpoint.NewLabels()
		for k, v := range ep.Labels {
			newEp.Labels[k] = v
		}
		newEp.ProviderSpecific = append(endpoint.ProviderSpecific(nil), ep.ProviderSpecific...)
		records = append(records, newEp)
	}
	return records
}

type filter struct {
	domain string
}

// Zones filters map[zoneID]zoneName for names having f.domain as suffix
func (f *filter) Zones(zones map[string]string) map[string]string {
	result := map[string]string{}
	for zoneID, zoneName := range zones {
		if strings.HasSuffix(zoneName, f.domain) {
			result[zoneID] = zoneName
		}
	}
	return result
}

// EndpointZoneID determines zoneID for endpoint from map[zoneID]zoneDomain by taking longest suffix zoneName match in endpoint DNSName
// returns empty string if no match found
func (f *filter) EndpointZoneID(endpoint *endpoint.Endpoint, zones map[string]string) (zoneID string) {
	var matchZoneID, matchZoneName string
	for zoneID, zoneDomain := range zones {
		if strings.HasSuffix(endpoint.DNSName, zoneDomain) && len(zoneDomain) >= len(matchZoneName) {
			matchZoneName = zoneDomain
			matchZoneID = zoneID
		}
	}
	return matchZoneID
}


func (c *InMemoryProvider) RecordsZone(zone string) ([]*endpoint.Endpoint, error) {
	if _, ok := c.zones[zone]; !ok {
		return nil, ErrZoneNotFound
	}

	var records []*endpoint.Endpoint
	for _, rec := range c.zones[zone] {
		records = append(records, rec)
	}
	return records, nil
}

func (c *InMemoryProvider) ZonesZone() map[string]string {
	zones := map[string]string{}
	for zone := range c.zones {
		zones[zone] = zone
	}
	return zones
}

func (c *InMemoryProvider) CreateZone(zone string) error {
	if _, ok := c.zones[zone]; ok {
		return ErrZoneAlreadyExists
	}
	c.zones[zone] = map[endpoint.EndpointKey]*endpoint.Endpoint{}

	return nil
}

func (c *InMemoryProvider) ApplyChangesZone(ctx context.Context, zoneID string, changes *endpoint.Changes) error {
	if err := c.validateChangeBatch(zoneID, changes); err != nil {
		return err
	}

	for _, v := range changes.Create {
		slog.Info("CREATE", "dns", v, "sync", c.Name, "zoneRecords", zoneID)
	}
	for _, v := range changes.UpdateOld {
		slog.Info("UPDATE-OLD", "dns", v, "sync", c.Name, "zoneRecords", zoneID)
	}
	for _, v := range changes.UpdateNew {
		slog.Info("UPDATE-NEW", "dns", v, "sync", c.Name, "zoneRecords", zoneID)
	}
	for _, v := range changes.Delete {
		slog.Info("DELETE", "dns", v, "sync", c.Name,"zoneRecords", zoneID)
	}
	for _, newEndpoint := range changes.Create {
		c.zones[zoneID][newEndpoint.Key()] = newEndpoint
	}
	for _, updateEndpoint := range changes.UpdateNew {
		c.zones[zoneID][updateEndpoint.Key()] = updateEndpoint
	}
	for _, deleteEndpoint := range changes.Delete {
		delete(c.zones[zoneID], deleteEndpoint.Key())
	}
	return nil
}

func (c *InMemoryProvider) updateMesh(mesh sets.Set[endpoint.EndpointKey], record *endpoint.Endpoint) error {
	if mesh.Has(record.Key()) {
		return ErrDuplicateRecordFound
	}
	mesh.Insert(record.Key())
	return nil
}

// validateChangeBatch validates that the changes passed to InMemory DNS provider is valid
func (c *InMemoryProvider) validateChangeBatch(zone string, changes *endpoint.Changes) error {
	curZone, ok := c.zones[zone]
	if !ok {
		return ErrZoneNotFound
	}
	mesh := sets.New[endpoint.EndpointKey]()
	for _, newEndpoint := range changes.Create {
		if _, exists := curZone[newEndpoint.Key()]; exists {
			return ErrRecordAlreadyExists
		}
		if err := c.updateMesh(mesh, newEndpoint); err != nil {
			return err
		}
	}
	for _, updateEndpoint := range changes.UpdateNew {
		if _, exists := curZone[updateEndpoint.Key()]; !exists {
			return ErrRecordNotFound
		}
		if err := c.updateMesh(mesh, updateEndpoint); err != nil {
			return err
		}
	}
	for _, updateOldEndpoint := range changes.UpdateOld {
		if rec, exists := curZone[updateOldEndpoint.Key()]; !exists || rec.Targets[0] != updateOldEndpoint.Targets[0] {
			return ErrRecordNotFound
		}
	}
	for _, deleteEndpoint := range changes.Delete {
		if rec, exists := curZone[deleteEndpoint.Key()]; !exists || rec.Targets[0] != deleteEndpoint.Targets[0] {
			return ErrRecordNotFound
		}
		if err := c.updateMesh(mesh, deleteEndpoint); err != nil {
			return err
		}
	}
	return nil
}
