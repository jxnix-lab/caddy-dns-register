# caddy-dns-register

A Caddy module for declarative DNS record management. Define DNS zones and records in your Caddyfile, and the module automatically creates/updates/deletes records via configured DNS providers.

## Use Case

Automatically provision DNS records when deploying services with Caddy as a reverse proxy. Particularly useful with [caddy-docker-proxy](https://github.com/lucaslorentz/caddy-docker-proxy) where Docker labels generate both reverse proxy routes AND DNS records.

## Features

- Declarative DNS record management in Caddyfile syntax
- Zone-based configuration with per-zone DNS providers
- Supports A, AAAA, CNAME, and TXT records
- TXT-based ownership tracking (safe for multiple Caddy instances)
- Reconciliation on startup (creates, updates, deletes)
- Integrates with existing [caddy-dns](https://github.com/caddy-dns) providers
- Works with caddy-docker-proxy for Docker label-based configuration

## Installation

Build Caddy with this module using xcaddy:

```bash
xcaddy build \
  --with github.com/jxnix-lab/caddy-dns-register \
  --with github.com/caddy-dns/cloudflare
```

## Usage

### Caddyfile Syntax

All configuration goes inside a single `dns_register` global option block:

```caddyfile
{
    dns_register {
        # Optional: Set unique owner ID for this Caddy instance
        owner_id my-caddy-instance

        # Define DNS zones and their providers
        domain example.com {
            dns cloudflare {
                api_token {$CF_API_TOKEN}
            }

            # Static records
            record www A 192.0.2.1
            record api A 192.0.2.1
            record mail A 192.0.2.2 3600
        }

        domain internal.example.com {
            dns rfc2136 {
                server ns1.internal.example.com
                key_name external-dns
                key_alg hmac-sha256
                key {$TSIG_SECRET}
            }

            record grafana A 10.0.0.50
            record prometheus A 10.0.0.51
        }
    }
}

# Site blocks as usual
www.example.com {
    reverse_proxy backend:8080
}
```

### Docker Labels (with caddy-docker-proxy)

**On Caddy container** (global options):
```yaml
labels:
  # Global dns_register config
  caddy.dns_register.owner_id: my-caddy-instance
  caddy.dns_register.domain_0: example.com
  caddy.dns_register.domain_0.dns: cloudflare
  caddy.dns_register.domain_0.dns.api_token: "{$$CF_API_TOKEN}"
  caddy.dns_register.domain_0.record_0: "www A 192.0.2.1"
  caddy.dns_register.domain_0.record_1: "api A 192.0.2.1"
```

**On service containers** (reverse proxy):
```yaml
labels:
  caddy: www.example.com
  caddy.reverse_proxy: "{{upstreams 8080}}"
```

## Supported Providers

This module uses [libdns](https://github.com/libdns) providers. Any caddy-dns provider should work:

- `cloudflare` - Cloudflare API
- `rfc2136` - RFC2136 Dynamic DNS (TSIG)
- `route53` - AWS Route 53
- And many more from [caddy-dns](https://github.com/caddy-dns)

## Record Ownership

Records are tracked using TXT registry records (similar to external-dns):

```
www.example.com.         A    192.0.2.1
_cdr.www.example.com.    TXT  "owner=my-caddy-instance,heritage=caddy-dns-register"
```

This allows:
- Multiple Caddy instances managing different records in the same zone
- Safe cleanup of only records owned by this instance
- Manual records are never touched

## Record Lifecycle

- **Config Load**: Records are created/updated to match declared state
- **Config Reload**: Records are updated if changed, removed if deleted from config
- **Reconciliation**: On startup, owned records not in config are deleted

## License

Apache 2.0
