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
  --with github.com/jxnix-lab/caddy-dns-technitium \
  --with github.com/caddy-dns/cloudflare \
  --with github.com/caddy-dns/rfc2136
```

## Usage

### Caddyfile Syntax

```caddyfile
{
    # Optional: Set unique owner ID for this Caddy instance
    dns_register {
        owner_id caddy-n5
    }

    # Define DNS zones and their providers
    domain lab.jaxon.cloud {
        dns rfc2136 {
            server ns1.lab.jaxon.cloud
            key_name external-dns
            key_alg hmac-sha256
            key {$TSIG_SECRET}
        }

        # Static records
        record grafana A 192.168.8.254
        record prometheus A 192.168.8.254
    }

    domain jaxon.cloud {
        dns technitium {
            server_url http://192.168.4.54:5380
            api_token {$TECHNITIUM_API_TOKEN}
        }

        # CNAME for split-horizon
        record grafana CNAME grafana.lab.jaxon.cloud
    }

    domain phx.jaxon.cloud {
        dns cloudflare {$CF_API_TOKEN}

        record uptime-kuma A 85.31.234.30
    }
}

# Site blocks as usual
grafana.lab.jaxon.cloud, grafana.jaxon.cloud {
    reverse_proxy grafana:3000
}
```

### Docker Labels (with caddy-docker-proxy)

**On Caddy container** (define zones once):
```yaml
labels:
  caddy_0:
  caddy_0.domain_0: lab.jaxon.cloud
  caddy_0.domain_0.dns: rfc2136 ns1.lab.jaxon.cloud
  caddy_0.domain_0.dns.key_name: external-dns
  caddy_0.domain_0.dns.key_alg: hmac-sha256
  caddy_0.domain_0.dns.key: ${TSIG_SECRET}

  caddy_0.domain_1: jaxon.cloud
  caddy_0.domain_1.dns: technitium
  caddy_0.domain_1.dns.server_url: http://192.168.4.54:5380
  caddy_0.domain_1.dns.api_token: ${TECHNITIUM_API_TOKEN}
```

**On service containers** (add records):
```yaml
labels:
  # DNS records
  caddy_0:
  caddy_0.domain_0: lab.jaxon.cloud
  caddy_0.domain_0.1_record: grafana A 192.168.8.254
  caddy_0.domain_1: jaxon.cloud
  caddy_0.domain_1.1_record: grafana CNAME grafana.lab.jaxon.cloud

  # Reverse proxy (standard caddy-docker-proxy)
  caddy_1: grafana.lab.jaxon.cloud, grafana.jaxon.cloud
  caddy_1.reverse_proxy: "{{upstreams 3000}}"
```

## Supported Providers

This module uses [libdns](https://github.com/libdns) providers. Any caddy-dns provider should work:

- `cloudflare` - Cloudflare API
- `rfc2136` - RFC2136 Dynamic DNS (TSIG)
- `route53` - AWS Route 53
- `technitium` - Technitium DNS HTTP API ([caddy-dns-technitium](https://github.com/jxnix-lab/caddy-dns-technitium))

## Record Ownership

Records are tracked using TXT registry records (similar to external-dns):

```
grafana.lab.jaxon.cloud.         A    192.168.8.254
_cdr.grafana.lab.jaxon.cloud.    TXT  "owner=caddy-n5,heritage=caddy-dns-register"
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
