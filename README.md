---
hide:
  - toc
  - navigation
---

# DNS Sync

Forked from [external-dns]() - and using the same codebase, but with a different UX.


Current interface is extended but compatible with external-dns HTTP protocol - but it is using
URL path to provide multiple webhooks in the same service, and to allow domain/zone to be reflected in the path 
for L7 Authz.

## What It Does

Like external-dns, will synchronize DNS with K8S resources. 

## Changes

- Instead of CLI flags, all the config is based on structs - can be loaded from a file, and eventually will be 
  used as CRDs in the cluster. Anything configurable should be defined as part of the CRDs
- Removed all providers except 'webhok' and inmemory. You can use external-dns to expose any of the other 
  provider as a webhook or an out of tree provider.
- Removed most sources - in future we may add the external-dns sources as a webhook or linked in.
- Removed regex and other options that are tricky to use and not common - use external-dns for that.
- The server can run multiple sync loops for different providers/zones. Each zone can get different sources and domains.
  The informers are shared.
- removed 'legacy' as much as possible
- 
