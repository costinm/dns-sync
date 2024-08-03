package endpoint

import (
	"context"

	"github.com/google/go-cmp/cmp"
)

// Interfaces used in the other packages.



// Source defines the interface Endpoint sources should implement.
type Source interface {
	Endpoints(ctx context.Context) ([]*Endpoint, error)
	// AddEventHandler adds an event handler that should be triggered if something in source changes
	AddEventHandler(context.Context, func())
}


// Changes holds lists of actions to be executed by dns providers
type Changes struct {
	// Records that need to be created
	Create []*Endpoint
	// Records that need to be updated (current data)
	UpdateOld []*Endpoint
	// Records that need to be updated (desired data)
	UpdateNew []*Endpoint
	// Records that need to be deleted
	Delete []*Endpoint
}

func (c *Changes) HasChanges() bool {
	if len(c.Create) > 0 || len(c.Delete) > 0 {
		return true
	}
	return !cmp.Equal(c.UpdateNew, c.UpdateOld)
}

// Provider defines the interface DNS providers should implement.
type Provider interface {
	Records(ctx context.Context) ([]*Endpoint, error)
	ApplyChanges(ctx context.Context, changes *Changes) error
	// AdjustEndpoints canonicalizes a set of candidate endpoints.
	// It is called with a set of candidate endpoints obtained from the various sources.
	// It returns a set modified as required by the provider. The provider is responsible for
	// adding, removing, and modifying the ProviderSpecific properties to match
	// the endpoints that the provider returns in `Records` so that the change plan will not have
	// unnecessary (potentially failing) changes. It may also modify other fields, add, or remove
	// Endpoints. It is permitted to modify the supplied endpoints.
	AdjustEndpoints(endpoints []*Endpoint) ([]*Endpoint, error)
	GetDomainFilter() DomainFilter
}


const (
	MediaTypeFormatAndVersion = "application/external.dns.webhook+json;version=1"
	ContentTypeHeader         = "Content-Type"
)
