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
	httpcaddyfile.RegisterGlobalOption("domain", parseGlobalDomain)
}

// parseGlobalDNSRegister parses the dns_register global option block.
//
// Syntax:
//
//	dns_register {
//	    owner_id <id>
//	}
func parseGlobalDNSRegister(d *caddyfile.Dispenser, existingVal any) (any, error) {
	app := &App{}
	if existingVal != nil {
		app = existingVal.(*App)
	}

	for d.Next() {
		for nesting := d.Nesting(); d.NextBlock(nesting); {
			switch d.Val() {
			case "owner_id":
				if !d.NextArg() {
					return nil, d.ArgErr()
				}
				app.OwnerID = d.Val()

			default:
				return nil, d.Errf("unrecognized dns_register option: %s", d.Val())
			}
		}
	}

	return app, nil
}

// parseGlobalDomain parses domain blocks in the global options.
//
// Syntax:
//
//	domain <zone> {
//	    dns <provider> {
//	        <provider-specific-options>
//	    }
//	    record <name> <type> <value> [<ttl>]
//	}
func parseGlobalDomain(d *caddyfile.Dispenser, existingVal any) (any, error) {
	app := &App{}
	if existingVal != nil {
		app = existingVal.(*App)
	}

	for d.Next() {
		// Get zone name
		if !d.NextArg() {
			return nil, d.ArgErr()
		}
		zone := d.Val()

		domain := &Domain{Zone: zone}

		for nesting := d.Nesting(); d.NextBlock(nesting); {
			switch d.Val() {
			case "dns":
				// Parse DNS provider
				if !d.NextArg() {
					return nil, d.ArgErr()
				}
				providerName := d.Val()

				// Build provider config
				providerConfig := map[string]any{
					"name": providerName,
				}

				// Parse provider block if present
				for innerNesting := d.Nesting(); d.NextBlock(innerNesting); {
					key := d.Val()
					if !d.NextArg() {
						return nil, d.ArgErr()
					}
					value := d.Val()
					providerConfig[key] = value
				}

				// Marshal to JSON for Caddy's module loader
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
	}

	return app, nil
}

// UnmarshalCaddyfile implements caddyfile.Unmarshaler for App.
// This allows the app to be configured via JSON adapter as well.
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

// parseCaddyfile is an adapter to allow parsing Caddyfile into the JSON config.
func parseCaddyfile(d *caddyfile.Dispenser, existingVal any) (any, error) {
	app := &App{}
	if existingVal != nil {
		var ok bool
		app, ok = existingVal.(*App)
		if !ok {
			return nil, d.Errf("existing value is not *App")
		}
	}

	err := app.UnmarshalCaddyfile(d)
	if err != nil {
		return nil, err
	}

	return httpcaddyfile.App{
		Name:  "dns_register",
		Value: caddyconfig.JSON(app, nil),
	}, nil
}

// Interface guards
var (
	_ caddyfile.Unmarshaler = (*App)(nil)
)
