package budget

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"lupusaria/internal/ai"
)

type Config struct {
	DailyBudgetUSD        float64
	MonthlyBudgetUSD      float64
	MaxRequestsPerHour    int
	InputPricePerMillion  float64
	OutputPricePerMillion float64
	StatePath             string
}

type Guard struct {
	cfg Config

	mu                sync.Mutex
	requestTimestamps []time.Time
	dailyKey          string
	monthlyKey        string
	dailySpendUSD     float64
	monthlySpendUSD   float64
}

type Decision struct {
	Allowed bool
	Reason  string
}

type Receipt struct {
	InputTokens     int
	OutputTokens    int
	CostUSD         float64
	Estimated       bool
	DailySpendUSD   float64
	MonthlySpendUSD float64
}

func NewGuard(cfg Config) *Guard {
	guard := &Guard{cfg: cfg}
	guard.load()
	return guard
}

func (g *Guard) Allow(now time.Time) Decision {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.rotatePeriods(now)
	g.pruneOldRequests(now)

	if g.cfg.MaxRequestsPerHour > 0 && len(g.requestTimestamps) >= g.cfg.MaxRequestsPerHour {
		return Decision{Allowed: false, Reason: fmt.Sprintf("hourly AI request limit reached (%d/hour)", g.cfg.MaxRequestsPerHour)}
	}
	if g.cfg.DailyBudgetUSD > 0 && g.dailySpendUSD >= g.cfg.DailyBudgetUSD {
		return Decision{Allowed: false, Reason: fmt.Sprintf("daily AI budget reached ($%.4f / $%.4f)", g.dailySpendUSD, g.cfg.DailyBudgetUSD)}
	}
	if g.cfg.MonthlyBudgetUSD > 0 && g.monthlySpendUSD >= g.cfg.MonthlyBudgetUSD {
		return Decision{Allowed: false, Reason: fmt.Sprintf("monthly AI budget reached ($%.4f / $%.4f)", g.monthlySpendUSD, g.cfg.MonthlyBudgetUSD)}
	}

	g.requestTimestamps = append(g.requestTimestamps, now)
	return Decision{Allowed: true}
}

func (g *Guard) Record(now time.Time, messages []ai.Message, response ai.Response) Receipt {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.rotatePeriods(now)

	inputTokens := response.Usage.InputTokens
	outputTokens := response.Usage.OutputTokens
	costUSD := response.Usage.CostUSD
	estimated := response.Usage.Estimated

	if inputTokens == 0 && outputTokens == 0 {
		inputTokens = EstimateTokens(messagesText(messages))
		outputTokens = EstimateTokens(response.Text)
		estimated = true
	}
	if costUSD == 0 && (inputTokens > 0 || outputTokens > 0) {
		costUSD = CostUSD(inputTokens, outputTokens, g.cfg.InputPricePerMillion, g.cfg.OutputPricePerMillion)
		estimated = true
	}

	g.dailySpendUSD += costUSD
	g.monthlySpendUSD += costUSD
	g.save()

	return Receipt{
		InputTokens:     inputTokens,
		OutputTokens:    outputTokens,
		CostUSD:         costUSD,
		Estimated:       estimated,
		DailySpendUSD:   g.dailySpendUSD,
		MonthlySpendUSD: g.monthlySpendUSD,
	}
}

type stateFile struct {
	DailyKey        string  `json:"daily_key"`
	MonthlyKey      string  `json:"monthly_key"`
	DailySpendUSD   float64 `json:"daily_spend_usd"`
	MonthlySpendUSD float64 `json:"monthly_spend_usd"`
}

func (g *Guard) load() {
	if g.cfg.StatePath == "" {
		return
	}
	data, err := os.ReadFile(g.cfg.StatePath)
	if err != nil {
		return
	}
	var state stateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}
	g.dailyKey = state.DailyKey
	g.monthlyKey = state.MonthlyKey
	g.dailySpendUSD = state.DailySpendUSD
	g.monthlySpendUSD = state.MonthlySpendUSD
}

func (g *Guard) save() {
	if g.cfg.StatePath == "" {
		return
	}
	state := stateFile{
		DailyKey:        g.dailyKey,
		MonthlyKey:      g.monthlyKey,
		DailySpendUSD:   g.dailySpendUSD,
		MonthlySpendUSD: g.monthlySpendUSD,
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(g.cfg.StatePath, data, 0600)
}

func EstimateTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	// Conservative enough for chat budgeting without pulling in a tokenizer.
	return (len(text) + 3) / 4
}

func CostUSD(inputTokens, outputTokens int, inputPricePerMillion, outputPricePerMillion float64) float64 {
	inputCost := (float64(inputTokens) / 1_000_000) * inputPricePerMillion
	outputCost := (float64(outputTokens) / 1_000_000) * outputPricePerMillion
	return inputCost + outputCost
}

func (g *Guard) rotatePeriods(now time.Time) {
	dailyKey := now.Format("2006-01-02")
	monthlyKey := now.Format("2006-01")
	if g.dailyKey != dailyKey {
		g.dailyKey = dailyKey
		g.dailySpendUSD = 0
	}
	if g.monthlyKey != monthlyKey {
		g.monthlyKey = monthlyKey
		g.monthlySpendUSD = 0
	}
}

func (g *Guard) pruneOldRequests(now time.Time) {
	cutoff := now.Add(-time.Hour)
	keepFrom := 0
	for keepFrom < len(g.requestTimestamps) && g.requestTimestamps[keepFrom].Before(cutoff) {
		keepFrom++
	}
	if keepFrom > 0 {
		g.requestTimestamps = append([]time.Time(nil), g.requestTimestamps[keepFrom:]...)
	}
}

func messagesText(messages []ai.Message) string {
	var builder strings.Builder
	for _, message := range messages {
		builder.WriteString(message.Role)
		builder.WriteString(": ")
		builder.WriteString(message.Content)
		builder.WriteByte('\n')
	}
	return builder.String()
}
