package dnsregister

import (
	"net/netip"
	"testing"
	"time"

	"github.com/libdns/libdns"
)

func TestToLibdnsRecord(t *testing.T) {
	app := &App{}

	tests := []struct {
		name     string
		record   *Record
		wantType string
	}{
		{
			name:     "A record",
			record:   &Record{Name: "www", Type: "A", Value: "192.168.1.100", TTL: 300},
			wantType: "A",
		},
		{
			name:     "AAAA record",
			record:   &Record{Name: "www", Type: "AAAA", Value: "2001:db8::1", TTL: 300},
			wantType: "AAAA",
		},
		{
			name:     "TXT record",
			record:   &Record{Name: "_test", Type: "TXT", Value: "hello world", TTL: 60},
			wantType: "TXT",
		},
		{
			name:     "CNAME record",
			record:   &Record{Name: "www", Type: "CNAME", Value: "example.com.", TTL: 300},
			wantType: "CNAME",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := app.toLibdnsRecord(tc.record)
			rr := result.RR()

			if rr.Type != tc.wantType {
				t.Errorf("Type: got %q, want %q", rr.Type, tc.wantType)
			}
			if rr.Name != tc.record.Name {
				t.Errorf("Name: got %q, want %q", rr.Name, tc.record.Name)
			}
		})
	}
}

func TestMakeTXTMarker(t *testing.T) {
	app := &App{OwnerID: "test-caddy"}

	marker := app.makeTXTMarker("www")

	txt, ok := marker.(libdns.TXT)
	if !ok {
		t.Fatalf("expected libdns.TXT, got %T", marker)
	}

	if txt.Name != "_cdr.www" {
		t.Errorf("Name: got %q, want %q", txt.Name, "_cdr.www")
	}

	expectedText := "owner=test-caddy,heritage=caddy-dns-register"
	if txt.Text != expectedText {
		t.Errorf("Text: got %q, want %q", txt.Text, expectedText)
	}
}

func TestParseOwnedRecords(t *testing.T) {
	app := &App{OwnerID: "test-caddy"}

	records := []libdns.Record{
		// Our marker
		libdns.TXT{
			Name: "_cdr.www",
			Text: "owner=test-caddy,heritage=caddy-dns-register",
			TTL:  300 * time.Second,
		},
		// The actual record
		libdns.Address{
			Name: "www",
			IP:   netip.MustParseAddr("192.168.1.100"),
			TTL:  300 * time.Second,
		},
		// Another instance's marker (should be ignored)
		libdns.TXT{
			Name: "_cdr.api",
			Text: "owner=other-caddy,heritage=caddy-dns-register",
			TTL:  300 * time.Second,
		},
		// Another instance's record
		libdns.Address{
			Name: "api",
			IP:   netip.MustParseAddr("192.168.1.101"),
			TTL:  300 * time.Second,
		},
		// Unmanaged record (no marker)
		libdns.Address{
			Name: "manual",
			IP:   netip.MustParseAddr("192.168.1.200"),
			TTL:  300 * time.Second,
		},
	}

	owned := app.parseOwnedRecords(records)

	// Should only have www:A
	if len(owned) != 1 {
		t.Errorf("expected 1 owned record, got %d", len(owned))
	}

	wwwA, exists := owned["www:A"]
	if !exists {
		t.Fatal("expected www:A to be owned")
	}

	if wwwA.Value != "192.168.1.100" {
		t.Errorf("Value: got %q, want %q", wwwA.Value, "192.168.1.100")
	}

	// Should not own api or manual
	if _, exists := owned["api:A"]; exists {
		t.Error("api:A should not be owned (different owner)")
	}
	if _, exists := owned["manual:A"]; exists {
		t.Error("manual:A should not be owned (no marker)")
	}
}

func TestExtractValue(t *testing.T) {
	app := &App{}

	tests := []struct {
		name   string
		record libdns.Record
		want   string
	}{
		{
			name:   "Address",
			record: libdns.Address{Name: "www", IP: netip.MustParseAddr("192.168.1.100")},
			want:   "192.168.1.100",
		},
		{
			name:   "TXT",
			record: libdns.TXT{Name: "test", Text: "hello world"},
			want:   "hello world",
		},
		{
			name:   "CNAME",
			record: libdns.CNAME{Name: "www", Target: "example.com."},
			want:   "example.com.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := app.extractValue(tc.record)
			if result != tc.want {
				t.Errorf("got %q, want %q", result, tc.want)
			}
		})
	}
}
