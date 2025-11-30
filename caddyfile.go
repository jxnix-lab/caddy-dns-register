package dnsregister

import (
	"encoding/json"
	"strconv"

	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
)

func init() {
	httpcaddyfile.RegisterGlobalOption("dns_register", parseGlobalDNSRegister)
}

// parseGlobalDNSRegister parses the dns_register global option block.
//
// Syntax:
//
//	dns_register {
//	    owner_id <id>
//	    domain <zone> {
//	        dns <provider> {
//	            <provider-specific-options>
//	        }
//	        record <name> <type> <value> [<ttl>]
//	    }
//	}
//
// Example:
//
//	dns_register {
//	    owner_id my-caddy-instance
//	    domain example.com {
//	        dns cloudflare {
//	            api_token {$CF_API_TOKEN}
//	        }
//	        record www A 192.0.2.1
//	        record mail A 192.0.2.2 3600
//	    }
//	}
func parseGlobalDNSRegister(d *caddyfile.Dispenser, existingVal any) (any, error) {
	app := &App{}
	if existingVal != nil {
		switch v := existingVal.(type) {
		case *App:
			app = v
		case httpcaddyfile.App:
			if err := json.Unmarshal(v.Value, app); err != nil {
				return nil, d.Errf("failed to unmarshal existing app: %v", err)
			}
		}
	}

	for d.Next() {
		for nesting := d.Nesting(); d.NextBlock(nesting); {
			switch d.Val() {
			case "owner_id":
				if !d.NextArg() {
					return nil, d.ArgErr()
				}
				app.OwnerID = d.Val()

			case "domain":
				// Parse domain block
				if !d.NextArg() {
					return nil, d.ArgErr()
				}
				zone := d.Val()

				domain := &Domain{Zone: zone}

				for innerNesting := d.Nesting(); d.NextBlock(innerNesting); {
					switch d.Val() {
					case "dns":
						// Parse DNS provider
						if !d.NextArg() {
							return nil, d.ArgErr()
						}
						providerName := d.Val()

						providerConfig := map[string]any{
							"name": providerName,
						}

						// Parse provider block if present
						for providerNesting := d.Nesting(); d.NextBlock(providerNesting); {
							key := d.Val()
							if !d.NextArg() {
								return nil, d.ArgErr()
							}
							value := d.Val()
							providerConfig[key] = value
						}

						providerJSON, err := json.Marshal(providerConfig)
						if err != nil {
							return nil, d.Errf("marshaling DNS provider config: %v", err)
						}
						domain.DNSProviderRaw = providerJSON

					case "record":
						// Parse record: <name> <type> <value> [<ttl>]
						rec := &Record{}

						if !d.NextArg() {
							return nil, d.ArgErr()
						}
						rec.Name = d.Val()

						if !d.NextArg() {
							return nil, d.ArgErr()
						}
						rec.Type = d.Val()

						if !d.NextArg() {
							return nil, d.ArgErr()
						}
						rec.Value = d.Val()

						// Optional TTL
						if d.NextArg() {
							ttl, err := strconv.Atoi(d.Val())
							if err != nil {
								return nil, d.Errf("invalid TTL: %s", d.Val())
							}
							rec.TTL = ttl
						}

						domain.Records = append(domain.Records, rec)

					default:
						return nil, d.Errf("unrecognized domain option: %s", d.Val())
					}
				}

				app.Domains = append(app.Domains, domain)

			default:
				return nil, d.Errf("unrecognized dns_register option: %s", d.Val())
			}
		}
	}

	return httpcaddyfile.App{
		Name:  "dns_register",
		Value: caddyconfig.JSON(app, nil),
	}, nil
}

// UnmarshalCaddyfile implements caddyfile.Unmarshaler for App.
func (a *App) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for nesting := d.Nesting(); d.NextBlock(nesting); {
			switch d.Val() {
			case "owner_id":
				if !d.NextArg() {
					return d.ArgErr()
				}
				a.OwnerID = d.Val()

			case "domain":
				if !d.NextArg() {
					return d.ArgErr()
				}
				zone := d.Val()

				domain := &Domain{Zone: zone}

				for innerNesting := d.Nesting(); d.NextBlock(innerNesting); {
					switch d.Val() {
					case "dns":
						if !d.NextArg() {
							return d.ArgErr()
						}
						providerName := d.Val()

						providerConfig := map[string]any{
							"name": providerName,
						}

						for providerNesting := d.Nesting(); d.NextBlock(providerNesting); {
							key := d.Val()
							if !d.NextArg() {
								return d.ArgErr()
							}
							value := d.Val()
							providerConfig[key] = value
						}

						providerJSON, err := json.Marshal(providerConfig)
						if err != nil {
							return d.Errf("marshaling DNS provider config: %v", err)
						}
						domain.DNSProviderRaw = providerJSON

					case "record":
						rec := &Record{}
						if !d.NextArg() {
							return d.ArgErr()
						}
						rec.Name = d.Val()
						if !d.NextArg() {
							return d.ArgErr()
						}
						rec.Type = d.Val()
						if !d.NextArg() {
							return d.ArgErr()
						}
						rec.Value = d.Val()
						if d.NextArg() {
							ttl, err := strconv.Atoi(d.Val())
							if err != nil {
								return d.Errf("invalid TTL: %s", d.Val())
							}
							rec.TTL = ttl
						}
						domain.Records = append(domain.Records, rec)
					}
				}

				a.Domains = append(a.Domains, domain)

			default:
				return d.Errf("unrecognized option: %s", d.Val())
			}
		}
	}

	return nil
}

// Interface guards
var (
	_ caddyfile.Unmarshaler = (*App)(nil)
)
