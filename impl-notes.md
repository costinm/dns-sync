# Ownership

This is the most critical and risky concept in external-dns.

For each record, it associates an 'owner' controller. This is set when loading from provider. 

Any record without matching owner is treated as 'external' - and removed from 'delete', 'update'. 
All created entries get the owner added.

The owner and other labels use a mangled (even encrypted) TXT record or uses a separate database. For dns-sync, the
onwer is explicitly exposed and defined - along with cluster, namespace, name that identify a resource as associated
with K8S and mesh, and can be used by clients.

# Sync vs export

The main purpose of external-dns is to export DNS records infered from K8S resources to a DNS provider. The zone
may contain other records - and a lot of care is taken to not delete entries from DNS. The 'upsert' or 'created' modes
never delete entries, while 'sync' only deletes and updates entries that have same owner. 

The dns-sync attempts to do a real sync - the zone is loaded and compared to existing resources in K8S, and 
if an entry is missing a ServiceEntry is created. 

# Istio Primary/remote and mesh domains

The basic operation mode is for DNS-sync to run with one K8S cluster and matching zones. Each cluster gets its own zone,
and may create any DNS entries. GKE operates in this mode for example. 

Having a shared domain for the entire mesh has some ownership issues: who creates entries? Who can delete them?



With Istio primary/remote or multi-primary the expectation is that all Istiods will have the same config - but it is also
common to have some namespaces only in a set of clusters.

DNS-sync can run as 'primary' - owning the zone - and 'remote', syncing from primary but not writing ServiceEntry 
to DNS. You can have one primary per region or zone - so ownership is still  important, but it is expected that
all primaries are eventually consistent. That still creates a problem and churn if the primary election moves around
and the ServiceEntry in different primaries are out of sync - but eventually they will get created. 

There are few options:
- 'config cluster' - one of the primaries are designated as control. It may change if the cluster/region is down, but
there is no risk of churn.
- master election for dns-sync with a cluster/region priority list - starting with the first canary or rollout region.
- have a single dns-sync that is watching ALL clusters. This is likely the best approach, and allows dns-sync to run
in a trusted cluster or outside k8s. 

