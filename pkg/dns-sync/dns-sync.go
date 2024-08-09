package dns_sync

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/costinm/dns-sync/controller"
	"github.com/costinm/dns-sync/endpoint"
	"github.com/costinm/dns-sync/pkg/config"
	"github.com/costinm/dns-sync/pkg/mem"
	"github.com/costinm/dns-sync/pkg/sources/dnsmesh"
	"github.com/costinm/dns-sync/plan"
	"github.com/costinm/dns-sync/provider/webhook"
	"github.com/costinm/dns-sync/source"
	"k8s.io/apimachinery/pkg/labels"
)

// Configure the sync controllers.
// This is called from main(), after loading the config and setting up telemetry and other dependencies.

type DNSSync struct {

}

func LoadAndRun(ctx context.Context, once bool) error {
	dnsSyncCfg, err := config.Get[endpoint.ExtDNSConfig](ctx, "dnssync")
	if err != nil {
		fmt.Println("Missing config, will use default", "err", err)
		// Use an in-memory config that will show all entries.
		dnsSyncCfg.Sync = map[string]*endpoint.SyncConfig {
			"inmemory": &endpoint.SyncConfig{
				Policy: "create-only",
				TXTPrefix:  "sync_",
				TXTOwnerID: "dnssync",
				Sources: []*endpoint.SourceSpec{
					{Name: "node", Suffix: ".nodes.i.webinf.info"},
					{Name: "istio-se"},
				},
			},
		}
	}
	return Run(ctx, dnsSyncCfg, once)
}

func Run(ctx context.Context, dnsSyncCfg *endpoint.ExtDNSConfig, once bool) (err error) {

	UpdateDefaults(dnsSyncCfg)


	k8s := &source.SingletonClientGenerator{
		KubeConfig:   os.Getenv("KUBECONFIG"),
		// If update events are enabled, disable timeout.
		RequestTimeout: func() time.Duration {
			if dnsSyncCfg.UpdateEvents {
				return 0
			}
			return 10 * time.Second
		}(),
	}

	for n, sync := range dnsSyncCfg.Sync {
		var p endpoint.Provider


		if sync.Address == "" {
			p = mem.NewInMemoryProvider(n, sync)
			// inmemory.InMemoryInitZones(sync.InMemoryZones), inmemory.InMemoryWithDomain(domainFilter), inmemory.InMemoryWithLogging())
		} else {
			// Will get the domain filter from the handshake.
			p, err = webhook.NewWebhookProvider(sync.Address)
		}
		if err != nil {
			return err
		}

		sources := []endpoint.Source{}
		for _, srcCfg := range sync.Sources {
			// Create a source.Config from the flags passed by the user.
			// The flags are stored in the Config struct, so we can pass it directly - but
			// there are 3 exceptions
			sourceCfg := &source.Config{
				FQDNTemplate: srcCfg.FQDNTemplate,
			}

			// TODO: rename to avoid confusion and keep the config struct clean
			labelSelector, err := labels.Parse(srcCfg.Labels)
			if err != nil {
				slog.Error("Invalid source labels, ignoring", "name", srcCfg.Name, "labelFilter", srcCfg.Labels, "err", err)
				continue
			}
			sourceCfg.LabelFilter = labelSelector
			sourceCfg.ResolveLoadBalancerHostname = srcCfg.ResolveServiceLoadBalancerHostname

			// Lookup all the selected sources by names and pass them the desired configuration.
			newSrc, err := source.BuildWithConfig(ctx, srcCfg.Name, k8s, sourceCfg, srcCfg, sync)
			if err != nil {
				slog.Error("Invalid source, ignoring", "name", srcCfg.Name, "err", err)
				continue
			}
			sources = append(sources, newSrc)

		}
		// Filter targets
		targetFilter := endpoint.NewTargetNetFilterWithExclusions(sync.TargetNetFilter, sync.ExcludeTargetNets)

		// Combine multiple sources into a single, deduplicated source.
		endpointsSource := source.NewDedupSource(source.NewMultiSource(sources, sync.DefaultTargets))
		endpointsSource = source.NewTargetFilterSource(endpointsSource, targetFilter)


		// Mesh registry is opinionated:
		// - no heritage or 'owner'
		// - value is ns=NAMESPACE o=OWNER a=ACCOUNT
		// - presence of o and matching the ID of the sync (which in turn should use leader election or be singleton or indempotent)
		//  indicates the record is managed by the mesh DNS syncer.
		// - ideally the OWNER should be the meshID.

		// Not using a prefix allows the entry to be returned in DNS requests so namespace/sa can be used.

		// This wraps the provider and adds a cache and labels for the records using TXT records.
		//
		r, _ := dnsmesh.NewTXTRegistry(p, "_mesh.", "", sync.TXTOwnerID, sync.TXTCacheInterval, sync.TXTWildcardReplacement, sync.ManagedDNSRecordTypes, sync.ExcludeDNSRecordTypes, false, nil)

		policy, exists := plan.Policies[sync.Policy]
		if !exists {
			policy = plan.Policies["create-only"]
		}

		ctrl := controller.Controller{
			Source:               endpointsSource,
			Registry:             r,
			Policy:               policy,
			Interval:             sync.Interval,
			DomainFilter:         p.GetDomainFilter(),
			ManagedRecordTypes:   sync.ManagedDNSRecordTypes,
			ExcludeRecordTypes:   sync.ExcludeDNSRecordTypes,
			MinEventSyncInterval: sync.MinEventSyncInterval,
			Owner: "mesh",
		}

		if once {
			ctrl.RunOnce(ctx)
			continue
		}

		// Add RunOnce as the handler function that will be called when ingress/service sources have changed.
		// Note that k8s Informers will perform an initial list operation, which results in the handler
		// function initially being called for every Service/Ingress that exists
		ctrl.Source.AddEventHandler(ctx, func() { ctrl.ScheduleRunOnce(time.Now()) })

		ctrl.ScheduleRunOnce(time.Now())

		go ctrl.Run(ctx)

		slog.Info("Starting sync", "target", sync.Address, "interval", sync.Interval, "sources", sync.Sources)
	}
	return nil
}


func UpdateDefaults(cfg *endpoint.ExtDNSConfig) {
	for _, sync := range cfg.Sync {
		if sync.Interval == 0 {
			sync.Interval = 60 * time.Second
		}
		if sync.MinEventSyncInterval == 0 {
			sync.MinEventSyncInterval = 10 * time.Second
		}
		if sync.Policy == "" {
			sync.Policy = "create-only"
		}
		if sync.TXTCacheInterval == 0 {
			sync.TXTCacheInterval = 10 * time.Second
		}
	}
}
