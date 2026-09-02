package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Baranigsiz/kurisu/internal/config"
	"github.com/Baranigsiz/kurisu/internal/domain"
	"github.com/Baranigsiz/kurisu/internal/engine"
	"github.com/Baranigsiz/kurisu/internal/metrics"
	"github.com/Baranigsiz/kurisu/internal/providers"
	"github.com/Baranigsiz/kurisu/internal/server"
)

func TestVirtualKeyManager_AuthenticationAndBudget(t *testing.T) {
	expFuture := time.Now().Add(24 * time.Hour)
	expPast := time.Now().Add(-24 * time.Hour)

	keys := []domain.VirtualKey{
		{
			Key:              "kg-valid-1",
			Name:             "Valid Team",
			MonthlyBudgetUSD: 10.0,
			SpentUSD:         2.5,
			Enabled:          true,
			ExpiresAt:        &expFuture,
		},
		{
			Key:              "kg-disabled-2",
			Name:             "Disabled Team",
			MonthlyBudgetUSD: 10.0,
			Enabled:          false,
		},
		{
			Key:              "kg-expired-3",
			Name:             "Expired Key",
			MonthlyBudgetUSD: 10.0,
			Enabled:          true,
			ExpiresAt:        &expPast,
		},
		{
			Key:              "kg-budget-exceeded-4",
			Name:             "Broke Team",
			MonthlyBudgetUSD: 5.0,
			SpentUSD:         5.01,
			Enabled:          true,
		},
		{
			Key:              "kg-restricted-models-5",
			Name:             "Mini Only",
			AllowedModels:    []string{"gpt-4o-mini", "gemini-2.0-flash"},
			Enabled:          true,
		},
	}

	mgr := domain.NewVirtualKeyManager(keys)

	// 1. Valid Key Check
	vk, err := mgr.ValidateKey("kg-valid-1", "gpt-4o")
	if err != nil || vk == nil {
		t.Fatalf("expected valid key to pass, got err: %v", err)
	}

	// 2. Disabled Key Check
	_, err = mgr.ValidateKey("kg-disabled-2", "gpt-4o")
	if err == nil {
		t.Errorf("expected error for disabled key")
	}

	// 3. Expired Key Check
	_, err = mgr.ValidateKey("kg-expired-3", "gpt-4o")
	if err == nil {
		t.Errorf("expected error for expired key")
	}

	// 4. Budget Exceeded Check
	_, err = mgr.ValidateKey("kg-budget-exceeded-4", "gpt-4o")
	if err == nil {
		t.Errorf("expected error for budget exceeded key")
	}

	// 5. Model Permissions Check
	_, err = mgr.ValidateKey("kg-restricted-models-5", "gpt-4o-mini")
	if err != nil {
		t.Errorf("expected allowed model gpt-4o-mini to pass, got: %v", err)
	}
	_, err = mgr.ValidateKey("kg-restricted-models-5", "claude-3-5-sonnet")
	if err == nil {
		t.Errorf("expected disallowed model claude-3-5-sonnet to fail")
	}

	// 6. Record Spend Test
	mgr.RecordSpend("kg-valid-1", 1.25)
	updated, ok := mgr.Get("kg-valid-1")
	if !ok || updated.SpentUSD != 3.75 {
		t.Errorf("expected spent $3.75, got $%.2f", updated.SpentUSD)
	}
}

func TestServer_VirtualKeys_EndToEnd(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.MasterKeys = []string{"admin-secret-key"}
	cfg.Server.VirtualKeys = []domain.VirtualKey{
		{
			Key:              "kg-virtual-test",
			Name:             "Test Project",
			MonthlyBudgetUSD: 50.0,
			SpentUSD:         0.0,
			AllowedModels:    []string{"gpt-4o"},
			Enabled:          true,
		},
	}

	mockProv := &MockProvider{name: "mock-openai", supportedModels: []string{"gpt-4o"}}
	provs := []providers.Provider{mockProv}
	collector := metrics.NewCollector()
	router := engine.NewRouter(cfg, provs)
	executor := engine.NewExecutor(cfg, router, nil, nil, collector)
	srv := server.NewServer(cfg, executor, router, collector)

	// Test 1: Authorized Virtual Key for Allowed Model
	reqBody, _ := json.Marshal(domain.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{
			{Role: "user", Content: "Hello from virtual key client"},
		},
	})

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer kg-virtual-test")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with virtual key, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify spend was recorded
	vk, ok := srv.VirtualKeyManager().Get("kg-virtual-test")
	if !ok || vk.SpentUSD <= 0 {
		t.Errorf("expected virtual key spent_usd to increase after request, got %.6f", vk.SpentUSD)
	}

	// Test 2: Virtual Key requesting a Forbidden Model (not in AllowedModels)
	reqForbidden, _ := json.Marshal(domain.ChatCompletionRequest{
		Model: "claude-3-5-sonnet",
		Messages: []domain.Message{
			{Role: "user", Content: "Forbidden request"},
		},
	})
	req2, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(reqForbidden))
	req2.Header.Set("Authorization", "Bearer kg-virtual-test")
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for disallowed model, got %d: %s", rec2.Code, rec2.Body.String())
	}
}
