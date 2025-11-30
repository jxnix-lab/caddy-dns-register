# Technical Plan: caddy-dns-register

## Overview

A Caddy "app" module that manages DNS records declaratively. Records are defined in the Caddyfile (or via Docker labels through caddy-docker-proxy) and automatically provisioned/updated/deleted via configured DNS providers.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│ Docker Labels (on containers)                                       │
│   caddy_0.domain_0: lab.jaxon.cloud                                │
│   caddy_0.domain_0.dns: rfc2136 ...                                │
│   caddy_0.domain_0.1_record: komodo A 192.168.8.254                │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│ caddy-docker-proxy                                                  │
│   Aggregates labels → Generates Caddyfile                          │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│ Caddyfile                                                           │
│   {                                                                 │
│       domain lab.jaxon.cloud {                                     │
│           dns rfc2136 { ... }                                      │
│           record komodo A 192.168.8.254                            │
│       }                                                             │
│   }                                                                 │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│ caddy-dns-register (this module)                                    │
│   - Parses domain/dns/record directives                            │
│   - Loads appropriate libdns provider per domain                   │
│   - Manages record lifecycle (create/update/delete)                │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│ libdns Providers                                                    │
│   - caddy-dns/rfc2136 (for Technitium primary zones)               │
│   - caddy-dns/cloudflare (for Cloudflare)                          │
│   - libdns-technitium (NEW - for Technitium HTTP API)              │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│ DNS Servers                                                         │
│   - Technitium (lab.jaxon.cloud, jaxon.cloud)                      │
│   - Cloudflare (phx.jaxon.cloud)                                   │
└─────────────────────────────────────────────────────────────────────┘
```

## Module Design

### 1. Caddy App Registration

```go
func init() {
    caddy.RegisterModule(App{})
    httpcaddyfile.RegisterGlobalOption("domain", parseGlobalDomain)
}

type App struct {
    Domains []*Domain `json:"domains,omitempty"`

    // Runtime state
    logger  *zap.Logger
    ctx     context.Context
    cancel  context.CancelFunc
}

func (App) CaddyModule() caddy.ModuleInfo {
    return caddy.ModuleInfo{
        ID:  "dns_register",
        New: func() caddy.Module { return new(App) },
    }
}
```

### 2. Domain Configuration

```go
type Domain struct {
    // Zone name (e.g., "lab.jaxon.cloud")
    Zone string `json:"zone"`

    // DNS provider configuration (raw JSON, loaded as module)
    DNSProviderRaw json.RawMessage `json:"dns_provider,omitempty" caddy:"namespace=dns.providers inline_key=name"`

    // Records to manage
    Records []*Record `json:"records,omitempty"`

    // Runtime: loaded provider
    provider libdns.RecordSetter
}

type Record struct {
    Name  string `json:"name"`           // e.g., "komodo" or "@" for apex
    Type  string `json:"type"`           // "A", "AAAA", "CNAME"
    Value string `json:"value"`          // IP address or target domain
    TTL   int    `json:"ttl,omitempty"`  // Optional, defaults to 300
}
```

### 3. Lifecycle Implementation

```go
// Provision loads DNS providers and validates configuration
func (a *App) Provision(ctx caddy.Context) error {
    a.logger = ctx.Logger(a)
    a.ctx, a.cancel = context.WithCancel(ctx)

    for _, domain := range a.Domains {
        // Load DNS provider module
        val, err := ctx.LoadModule(domain, "DNSProviderRaw")
        if err != nil {
            return fmt.Errorf("loading DNS provider for %s: %v", domain.Zone, err)
        }
        domain.provider = val.(libdns.RecordSetter)
    }
    return nil
}

// Start creates/updates DNS records
func (a *App) Start() error {
    for _, domain := range a.Domains {
        if err := a.syncRecords(domain); err != nil {
            a.logger.Error("failed to sync records",
                zap.String("zone", domain.Zone),
                zap.Error(err))
            // Continue with other domains, don't fail completely
        }
    }
    return nil
}

// Stop optionally cleans up managed records
func (a *App) Stop() error {
    a.cancel()
    // TODO: Option to delete managed records on shutdown
    return nil
}

// syncRecords ensures DNS records match declared state
func (a *App) syncRecords(domain *Domain) error {
    var libdnsRecords []libdns.Record
    for _, r := range domain.Records {
        libdnsRecords = append(libdnsRecords, libdns.Record{
            Name:  r.Name,
            Type:  r.Type,
            Value: r.Value,
            TTL:   time.Duration(r.TTL) * time.Second,
        })
    }

    _, err := domain.provider.SetRecords(a.ctx, domain.Zone, libdnsRecords)
    if err != nil {
        return err
    }

    a.logger.Info("synced DNS records",
        zap.String("zone", domain.Zone),
        zap.Int("count", len(domain.Records)))
    return nil
}
```

### 4. Caddyfile Parsing

```go
func parseGlobalDomain(d *caddyfile.Dispenser, existingVal any) (any, error) {
    app := &App{}
    if existingVal != nil {
        app = existingVal.(*App)
    }

    for d.Next() {
        // domain <zone> { ... }
        if !d.NextArg() {
            return nil, d.ArgErr()
        }
        zone := d.Val()

        domain := &Domain{Zone: zone}

        for nesting := d.Nesting(); d.NextBlock(nesting); {
            switch d.Val() {
            case "dns":
                // dns <provider> [args...]
                // Parse provider name and config

            case "record":
                // record <name> <type> <value>
                record := &Record{}
                if !d.NextArg() {
                    return nil, d.ArgErr()
                }
                record.Name = d.Val()
                if !d.NextArg() {
                    return nil, d.ArgErr()
                }
                record.Type = d.Val()
                if !d.NextArg() {
                    return nil, d.ArgErr()
                }
                record.Value = d.Val()
                domain.Records = append(domain.Records, record)
            }
        }

        app.Domains = append(app.Domains, domain)
    }

    return app, nil
}
```

## Dependencies

### Required (existing)
- `github.com/caddyserver/caddy/v2` - Caddy core
- `github.com/libdns/libdns` - DNS provider interface
- `github.com/caddy-dns/cloudflare` - Cloudflare provider
- `github.com/caddy-dns/rfc2136` - RFC2136/TSIG provider

### Required (new - separate repo)
- `github.com/jxnix-lab/caddy-dns-technitium` - Technitium HTTP API provider

## libdns-technitium Provider

Separate repo needed for Technitium HTTP API support:

```go
package technitium

type Provider struct {
    Server   string `json:"server"`    // e.g., "http://192.168.4.53:5380"
    APIToken string `json:"api_token"`
}

func (p *Provider) GetRecords(ctx context.Context, zone string) ([]libdns.Record, error) {
    // GET /api/zones/records/get?token=...&domain=...&zone=...
}

func (p *Provider) SetRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
    // POST /api/zones/records/add or /api/zones/records/update
}

func (p *Provider) DeleteRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
    // POST /api/zones/records/delete
}
```

Reference: [Technitium DNS API Documentation](https://github.com/TechnitiumSoftware/DnsServer/blob/master/APIDOCS.md)

## Record Ownership & Reconciliation

### TXT Registry Pattern (from external-dns)

We use TXT records to track ownership, similar to [external-dns](https://kubernetes-sigs.github.io/external-dns/v0.14.1/registry/txt/).

For each managed record:
```
komodo.lab.jaxon.cloud.                    A    192.168.8.254
_cdr.komodo.lab.jaxon.cloud.               TXT  "owner=caddy-n5,heritage=caddy-dns-register"
```

**TXT Record Format:**
- **Prefix**: `_cdr.` (caddy-dns-register) - avoids collision with actual TXT records
- **Owner ID**: Unique per Caddy instance, configured via `owner_id` (e.g., `caddy-n5`)
- **Heritage**: Always `caddy-dns-register` to identify our records

**Configuration:**
```caddyfile
{
    dns_register {
        owner_id caddy-n5  # Unique identifier for this Caddy instance
    }

    domain lab.jaxon.cloud {
        dns rfc2136 { ... }
        record komodo A 192.168.8.254
    }
}
```

### Reconciliation on Startup

On `Start()`, we perform a full reconciliation:

```
Start()
  └── For each domain:
        ├── 1. GetRecords() - fetch all records from provider
        ├── 2. Filter TXT records with our prefix (_cdr.*)
        ├── 3. Parse ownership info from TXT values
        ├── 4. Filter to records owned by our owner_id
        ├── 5. Compute diff:
        │     ├── To Delete: owned records NOT in declared config
        │     ├── To Create: declared records NOT currently owned
        │     └── To Update: declared records with different values
        ├── 6. Apply changes:
        │     ├── DeleteRecords() for removals (record + TXT marker)
        │     ├── SetRecords() for creates/updates
        │     └── Create TXT markers for new records
        └── 7. Log summary
```

**Benefits:**
- Safe for multiple Caddy instances (each has unique owner_id)
- Handles config changes (removed records get cleaned up)
- Doesn't touch records created manually or by other systems
- Idempotent - can restart Caddy without issues

### Implementation

```go
type App struct {
    OwnerID string    `json:"owner_id,omitempty"`
    Domains []*Domain `json:"domains,omitempty"`
    // ...
}

const (
    txtPrefix  = "_cdr."
    txtHeritage = "caddy-dns-register"
)

func (a *App) Start() error {
    for _, domain := range a.Domains {
        if err := a.reconcileDomain(domain); err != nil {
            a.logger.Error("reconciliation failed",
                zap.String("zone", domain.Zone),
                zap.Error(err))
        }
    }
    return nil
}

func (a *App) reconcileDomain(domain *Domain) error {
    // 1. Get current records
    getter := domain.provider.(libdns.RecordGetter)
    existing, err := getter.GetRecords(a.ctx, domain.Zone)
    if err != nil {
        return err
    }

    // 2. Find our ownership markers
    owned := a.parseOwnedRecords(existing)

    // 3. Compute diff
    toDelete, toCreate, toUpdate := a.diffRecords(owned, domain.Records)

    // 4. Apply deletions
    for _, rec := range toDelete {
        // Delete both the record and its TXT marker
    }

    // 5. Apply creates/updates
    for _, rec := range append(toCreate, toUpdate...) {
        // Create/update record and TXT marker
    }

    return nil
}

func (a *App) makeTXTMarker(name string) libdns.Record {
    return libdns.Record{
        Name:  txtPrefix + name,
        Type:  "TXT",
        Value: fmt.Sprintf("owner=%s,heritage=%s", a.OwnerID, txtHeritage),
        TTL:   300 * time.Second,
    }
}
```

## Resolved Questions

1. **Record ownership tracking**: ✅ TXT registry pattern with `_cdr.` prefix
2. **Cleanup**: ✅ Reconciliation on startup handles cleanup automatically
3. **Conflict handling**: Only modify records we own (have TXT marker with our owner_id)

## Open Questions

1. **caddy-docker-proxy integration**: Does our Caddyfile syntax work with how caddy-docker-proxy generates config from labels? Need to test/verify.

2. **Provider loading**: The `dns` subdirective in TLS blocks loads providers differently than apps. Need to verify our approach matches Caddy conventions.

3. **Provider requirements**: We need providers that implement both `RecordGetter` and `RecordSetter`. Most caddy-dns providers do, but need to verify.

## Testing Plan

1. **Unit tests**: Caddyfile parsing, record conversion
2. **Integration tests**:
   - Mock libdns provider
   - Test record sync logic
3. **E2E tests**:
   - Local Technitium in Docker
   - Verify records created/updated/deleted
4. **caddy-docker-proxy test**:
   - Docker Compose setup with labels
   - Verify full flow works

## Implementation Phases

### Phase 1: Core Module
- [ ] Go module scaffold (go.mod, main struct)
- [ ] Caddy app registration
- [ ] Caddyfile parsing for `domain`/`dns`/`record`
- [ ] Basic record sync with hardcoded provider

### Phase 2: Provider Integration
- [ ] Dynamic provider loading from config
- [ ] Test with cloudflare provider
- [ ] Test with rfc2136 provider

### Phase 3: libdns-technitium
- [ ] Create separate repo
- [ ] Implement Technitium HTTP API client
- [ ] Implement libdns interfaces
- [ ] Test with local Technitium

### Phase 4: Docker Integration
- [ ] Test with caddy-docker-proxy
- [ ] Verify label syntax works
- [ ] Document Docker label patterns

### Phase 5: Polish
- [ ] Logging improvements
- [ ] Error handling
- [ ] Record ownership tracking (optional)
- [ ] Cleanup options
- [ ] Documentation
