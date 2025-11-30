// Package dnsregister provides a Caddy app for declarative DNS record management.
package dnsregister

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/libdns/libdns"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(App{})
}

// App is a Caddy app that manages DNS records declaratively.
type App struct {
	// OwnerID uniquely identifies this Caddy instance for record ownership.
	// Used in TXT registry records to track which instance owns which records.
	OwnerID string `json:"owner_id,omitempty"`

	// Domains contains the DNS zones and records to manage.
	Domains []*Domain `json:"domains,omitempty"`

	// Runtime state
	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc
}

// Domain represents a DNS zone with its provider and records.
type Domain struct {
	// Zone is the DNS zone name (e.g., "example.com").
	Zone string `json:"zone"`

	// DNSProviderRaw is the DNS provider module configuration.
	DNSProviderRaw json.RawMessage `json:"dns_provider,omitempty" caddy:"namespace=dns.providers inline_key=name"`

	// Records are the DNS records to manage in this zone.
	Records []*Record `json:"records,omitempty"`

	// Runtime: loaded provider (implements libdns interfaces)
	provider any
}

// Record represents a DNS record to manage.
type Record struct {
	// Name is the record name relative to the zone (e.g., "www" or "@" for apex).
	Name string `json:"name"`

	// Type is the record type (A, AAAA, CNAME, TXT, MX, NS).
	Type string `json:"type"`

	// Value is the record value (IP address, target domain, text, etc.).
	Value string `json:"value"`

	// TTL is the time-to-live in seconds. Defaults to 300 if not specified.
	TTL int `json:"ttl,omitempty"`
}

// CaddyModule returns the Caddy module information.
func (App) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "dns_register",
		New: func() caddy.Module { return new(App) },
	}
}

// Provision sets up the app.
func (a *App) Provision(ctx caddy.Context) error {
	a.logger = ctx.Logger()
	a.ctx, a.cancel = context.WithCancel(ctx)

	// Default owner ID
	if a.OwnerID == "" {
		a.OwnerID = "caddy"
	}

	// Load DNS providers for each domain
	for _, domain := range a.Domains {
		if len(domain.DNSProviderRaw) == 0 {
			return fmt.Errorf("domain %s: dns_provider is required", domain.Zone)
		}

		val, err := ctx.LoadModule(domain, "DNSProviderRaw")
		if err != nil {
			return fmt.Errorf("domain %s: loading DNS provider: %v", domain.Zone, err)
		}
		domain.provider = val

		a.logger.Debug("loaded DNS provider",
			zap.String("zone", domain.Zone),
			zap.String("provider", fmt.Sprintf("%T", val)))
	}

	return nil
}

// Start begins managing DNS records.
func (a *App) Start() error {
	for _, domain := range a.Domains {
		if err := a.reconcileDomain(domain); err != nil {
			a.logger.Error("failed to reconcile domain",
				zap.String("zone", domain.Zone),
				zap.Error(err))
			// Continue with other domains
		}
	}
	return nil
}

// Stop cleans up resources.
func (a *App) Stop() error {
	a.cancel()
	return nil
}

// reconcileDomain syncs DNS records for a domain.
func (a *App) reconcileDomain(domain *Domain) error {
	// Get provider interfaces
	getter, hasGetter := domain.provider.(libdns.RecordGetter)
	setter, hasSetter := domain.provider.(libdns.RecordSetter)
	appender, hasAppender := domain.provider.(libdns.RecordAppender)
	deleter, hasDeleter := domain.provider.(libdns.RecordDeleter)

	if !hasGetter {
		return fmt.Errorf("provider does not implement RecordGetter")
	}
	if !hasSetter && !hasAppender {
		return fmt.Errorf("provider does not implement RecordSetter or RecordAppender")
	}

	// Get existing records
	existing, err := getter.GetRecords(a.ctx, domain.Zone)
	if err != nil {
		return fmt.Errorf("getting existing records: %w", err)
	}

	// Parse ownership markers from existing TXT records
	owned := a.parseOwnedRecords(existing)

	// Build desired state from config
	desired := make(map[string]*Record)
	for _, rec := range domain.Records {
		key := rec.Name + ":" + rec.Type
		desired[key] = rec
	}

	// Compute diff
	var toCreate, toUpdate []*Record
	var toDelete []string

	// Find records to delete (owned but not in desired)
	for key := range owned {
		if _, exists := desired[key]; !exists {
			toDelete = append(toDelete, key)
		}
	}

	// Find records to create or update
	for key, rec := range desired {
		if existingRec, exists := owned[key]; exists {
			// Check if update needed
			if existingRec.Value != rec.Value || (rec.TTL > 0 && existingRec.TTL != rec.TTL) {
				toUpdate = append(toUpdate, rec)
			}
		} else {
			toCreate = append(toCreate, rec)
		}
	}

	// Log what we're about to do with actual record names
	createNames := make([]string, len(toCreate))
	for i, r := range toCreate {
		createNames[i] = r.Name + ":" + r.Type
	}
	updateNames := make([]string, len(toUpdate))
	for i, r := range toUpdate {
		updateNames[i] = r.Name + ":" + r.Type
	}

	a.logger.Info("reconciling DNS records",
		zap.String("zone", domain.Zone),
		zap.Int("create", len(toCreate)),
		zap.Int("update", len(toUpdate)),
		zap.Int("delete", len(toDelete)),
		zap.Strings("create_records", createNames),
		zap.Strings("update_records", updateNames),
		zap.Strings("delete_records", toDelete))

	// Apply deletions
	if hasDeleter && len(toDelete) > 0 {
		for _, key := range toDelete {
			parts := strings.SplitN(key, ":", 2)
			if len(parts) != 2 {
				continue
			}

			rec := owned[key]
			libRec := a.toLibdnsRecord(rec)
			marker := a.makeTXTMarker(rec.Name)

			// Delete the record and its marker
			_, err := deleter.DeleteRecords(a.ctx, domain.Zone, []libdns.Record{libRec, marker})
			if err != nil {
				a.logger.Warn("failed to delete record",
					zap.String("name", rec.Name),
					zap.String("type", rec.Type),
					zap.Error(err))
			} else {
				a.logger.Info("deleted record",
					zap.String("name", rec.Name),
					zap.String("type", rec.Type))
			}
		}
	}

	// Apply creates
	if len(toCreate) > 0 {
		for _, rec := range toCreate {
			libRec := a.toLibdnsRecord(rec)
			marker := a.makeTXTMarker(rec.Name)

			var err error
			if hasSetter {
				_, err = setter.SetRecords(a.ctx, domain.Zone, []libdns.Record{libRec, marker})
			} else {
				_, err = appender.AppendRecords(a.ctx, domain.Zone, []libdns.Record{libRec, marker})
			}

			if err != nil {
				a.logger.Warn("failed to create record",
					zap.String("name", rec.Name),
					zap.String("type", rec.Type),
					zap.Error(err))
			} else {
				a.logger.Info("created record",
					zap.String("name", rec.Name),
					zap.String("type", rec.Type),
					zap.String("value", rec.Value))
			}
		}
	}

	// Apply updates
	if hasSetter && len(toUpdate) > 0 {
		for _, rec := range toUpdate {
			libRec := a.toLibdnsRecord(rec)

			_, err := setter.SetRecords(a.ctx, domain.Zone, []libdns.Record{libRec})
			if err != nil {
				a.logger.Warn("failed to update record",
					zap.String("name", rec.Name),
					zap.String("type", rec.Type),
					zap.Error(err))
			} else {
				a.logger.Info("updated record",
					zap.String("name", rec.Name),
					zap.String("type", rec.Type),
					zap.String("value", rec.Value))
			}
		}
	}

	return nil
}

const (
	txtPrefix   = "_cdr."
	txtHeritage = "caddy-dns-register"
)

// parseOwnedRecords finds records owned by this instance based on TXT markers.
func (a *App) parseOwnedRecords(records []libdns.Record) map[string]*Record {
	owned := make(map[string]*Record)

	// First pass: find our ownership markers
	markers := make(map[string]bool)
	for _, rec := range records {
		rr := rec.RR()
		if rr.Type != "TXT" || !strings.HasPrefix(rr.Name, txtPrefix) {
			continue
		}

		// Check if this marker is ours
		expectedValue := fmt.Sprintf("owner=%s,heritage=%s", a.OwnerID, txtHeritage)
		if rr.Data == expectedValue || rr.Data == "\""+expectedValue+"\"" {
			// Extract the original record name
			origName := strings.TrimPrefix(rr.Name, txtPrefix)
			markers[origName] = true
		}
	}

	// Second pass: collect records that have our markers
	for _, rec := range records {
		rr := rec.RR()
		if strings.HasPrefix(rr.Name, txtPrefix) {
			continue // Skip markers themselves
		}

		if markers[rr.Name] {
			owned[rr.Name+":"+rr.Type] = &Record{
				Name:  rr.Name,
				Type:  rr.Type,
				Value: a.extractValue(rec),
				TTL:   int(rr.TTL.Seconds()),
			}
		}
	}

	return owned
}

// makeTXTMarker creates a TXT record to mark ownership.
func (a *App) makeTXTMarker(name string) libdns.Record {
	return libdns.TXT{
		Name: txtPrefix + name,
		TTL:  300 * time.Second,
		Text: fmt.Sprintf("owner=%s,heritage=%s", a.OwnerID, txtHeritage),
	}
}

// toLibdnsRecord converts our Record to a libdns.Record.
func (a *App) toLibdnsRecord(rec *Record) libdns.Record {
	ttl := time.Duration(rec.TTL) * time.Second
	if ttl == 0 {
		ttl = 300 * time.Second
	}

	switch rec.Type {
	case "A", "AAAA":
		ip, err := netip.ParseAddr(rec.Value)
		if err != nil {
			// Fall back to RR
			return libdns.RR{
				Name: rec.Name,
				Type: rec.Type,
				TTL:  ttl,
				Data: rec.Value,
			}
		}
		return libdns.Address{
			Name: rec.Name,
			IP:   ip,
			TTL:  ttl,
		}

	case "TXT":
		return libdns.TXT{
			Name: rec.Name,
			Text: rec.Value,
			TTL:  ttl,
		}

	case "CNAME":
		return libdns.CNAME{
			Name:   rec.Name,
			Target: rec.Value,
			TTL:    ttl,
		}

	default:
		return libdns.RR{
			Name: rec.Name,
			Type: rec.Type,
			TTL:  ttl,
			Data: rec.Value,
		}
	}
}

// extractValue gets the value from a libdns.Record.
func (a *App) extractValue(rec libdns.Record) string {
	switch r := rec.(type) {
	case libdns.Address:
		return r.IP.String()
	case libdns.TXT:
		return r.Text
	case libdns.CNAME:
		return r.Target
	case libdns.MX:
		return fmt.Sprintf("%d %s", r.Preference, r.Target)
	case libdns.NS:
		return r.Target
	default:
		return rec.RR().Data
	}
}

// Interface guards
var (
	_ caddy.App         = (*App)(nil)
	_ caddy.Module      = (*App)(nil)
	_ caddy.Provisioner = (*App)(nil)
)
