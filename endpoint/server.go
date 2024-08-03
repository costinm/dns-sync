package endpoint

import (
	"time"
)

// ExtDNSConfig defines the configuration for a multi-provider server, capable
// of syncing DNS entries operating multiple syncs.
//
// This is enabled with a JSON or yaml configuration instead of CLI.
// Using CLI it is possible to operate a single provider.
type ExtDNSConfig struct {
	ServerAddress  string
	MetricsAddress string

	Once           bool
	DryRun      bool
	UpdateEvents   bool


	// Sync is the map of providers to associated sources and settings.
	Sync map[string]*SyncConfig
}

type SyncConfig struct {

	// Address is the URL of the DNS service. If empty, the in-memory provider will be used, for debugging.
	Address       string

	// Policy defines deletion/update model - default is create, no delete or update.
	// 'sync' will delete/update entries, but only if the TXT record matches
	// 'upsert' will update entries - but not delete.
	Policy string

	Sources []*SourceSpec

	Zones map[string]string


	DomainFilter   []string
	ExcludeDomains []string

	// TXTPrefix will enable use of the 'registry' mode, creating TXT records with the specified prefix for each record.
	TXTPrefix  string
	TXTOwnerID string
	TXTCacheInterval       time.Duration
	TXTWildcardReplacement string

	// TargetNetFilter will only sync endpoints with the A or AAAA records in the specified networks.
	// This is only effective for addresses.
	TargetNetFilter        []string
	ExcludeTargetNets      []string

	DefaultTargets         []string

	ManagedDNSRecordTypes  []string
	ExcludeDNSRecordTypes  []string

	// How often will the full sync be triggered.
	Interval               time.Duration
	MinEventSyncInterval   time.Duration
}

// Config holds shared configuration options for all Sources.
type SourceSpec struct {
	Name string

	// Labels allows selecting only resources with specific labels.
	Labels string

	// Namespace allows selecting only resources in a specific namespace.
	Namespace string

	ResolveServiceLoadBalancerHostname bool

	Options map[string]string

	// FQDNTemplate is a template for generating the hostname based on the object.
	FQDNTemplate string
	// Append this suffix to all names, after name.namespace
	// TODO: support including a cluster name
	Suffix       string

	// Suffix for internal domain. If not set, "[SRC].mesh.internal" is used.
	//
	InternalDomain string

	// Default external domain, used for public IPs.
	ExternalDomain string
}
