package budget

import (
	"path/filepath"
	"testing"
	"time"

	"lupusaria/internal/ai"
)

func TestGuardBlocksHourlyLimit(t *testing.T) {
	guard := NewGuard(Config{MaxRequestsPerHour: 1})
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)

	if decision := guard.Allow(now); !decision.Allowed {
		t.Fatalf("first request should be allowed: %s", decision.Reason)
	}
	if decision := guard.Allow(now.Add(time.Minute)); decision.Allowed {
		t.Fatal("second request in same hour should be blocked")
	}
}

func TestGuardPersistsSpend(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "budget.json")
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)

	guard := NewGuard(Config{
		DailyBudgetUSD:        1,
		MonthlyBudgetUSD:      1,
		InputPricePerMillion:  1,
		OutputPricePerMillion: 1,
		StatePath:             statePath,
	})
	receipt := guard.Record(now, []ai.Message{{Role: "user", Content: "hello"}}, ai.Response{
		Text:  "hello",
		Usage: ai.Usage{InputTokens: 500_000, OutputTokens: 250_000},
	})
	if receipt.CostUSD != 0.75 {
		t.Fatalf("cost = %f, want 0.75", receipt.CostUSD)
	}

	reloaded := NewGuard(Config{
		DailyBudgetUSD:   0.75,
		MonthlyBudgetUSD: 0.75,
		StatePath:        statePath,
	})
	if decision := reloaded.Allow(now.Add(time.Minute)); decision.Allowed {
		t.Fatal("reloaded guard should remember spend and block at budget")
	}
}
