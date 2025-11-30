//go:build integration

package dnsregister

import (
	"context"
	"os"
	"testing"
	"time"

	technitium "github.com/jxnix-lab/caddy-dns-technitium"
	"go.uber.org/zap"
)

// Integration test - requires TECHNITIUM_URL and TECHNITIUM_TOKEN env vars
// Run with: go test -tags=integration -v ./...

func TestIntegration_Reconcile(t *testing.T) {
	serverURL := os.Getenv("TECHNITIUM_URL")
	apiToken := os.Getenv("TECHNITIUM_TOKEN")

	if serverURL == "" || apiToken == "" {
		t.Skip("Set TECHNITIUM_URL and TECHNITIUM_TOKEN to run integration tests")
	}

	zone := os.Getenv("TECHNITIUM_TEST_ZONE")
	if zone == "" {
		zone = "example.com"
	}

	// Create provider
	provider := &technitium.Provider{
		ServerURL: serverURL,
		APIToken:  apiToken,
	}

	// Create logger
	logger, _ := zap.NewDevelopment()

	// Create app
	ctx := context.Background()
	app := &App{
		OwnerID: "test-caddy-" + time.Now().Format("150405"),
		Domains: []*Domain{
			{
				Zone:     zone,
				provider: provider,
				Records: []*Record{
					{Name: "_dnsreg-test", Type: "TXT", Value: "hello-world", TTL: 60},
				},
			},
		},
		logger: logger,
		ctx:    ctx,
	}

	// Test reconciliation (create)
	t.Log("Testing record creation...")
	err := app.reconcileDomain(app.Domains[0])
	if err != nil {
		t.Fatalf("reconcileDomain failed: %v", err)
	}

	// Verify record was created
	records, err := provider.GetRecords(ctx, zone)
	if err != nil {
		t.Fatalf("GetRecords failed: %v", err)
	}

	foundRecord := false
	foundMarker := false
	for _, rec := range records {
		rr := rec.RR()
		if rr.Name == "_dnsreg-test" && rr.Type == "TXT" {
			foundRecord = true
			t.Logf("Found record: %s %s %s", rr.Name, rr.Type, rr.Data)
		}
		if rr.Name == "_cdr._dnsreg-test" && rr.Type == "TXT" {
			foundMarker = true
			t.Logf("Found marker: %s %s %s", rr.Name, rr.Type, rr.Data)
		}
	}

	if !foundRecord {
		t.Error("Record was not created")
	}
	if !foundMarker {
		t.Error("Ownership marker was not created")
	}

	// Test reconciliation (update)
	t.Log("Testing record update...")
	app.Domains[0].Records[0].Value = "updated-value"
	err = app.reconcileDomain(app.Domains[0])
	if err != nil {
		t.Fatalf("reconcileDomain (update) failed: %v", err)
	}

	// Test reconciliation (delete by removing from config)
	t.Log("Testing record deletion...")
	app.Domains[0].Records = nil
	err = app.reconcileDomain(app.Domains[0])
	if err != nil {
		t.Fatalf("reconcileDomain (delete) failed: %v", err)
	}

	// Verify deletion
	records, err = provider.GetRecords(ctx, zone)
	if err != nil {
		t.Fatalf("GetRecords failed: %v", err)
	}

	for _, rec := range records {
		rr := rec.RR()
		if rr.Name == "_dnsreg-test" || rr.Name == "_cdr._dnsreg-test" {
			t.Errorf("Record still exists after deletion: %s %s", rr.Name, rr.Type)
		}
	}

	t.Log("Integration test completed successfully")
}
