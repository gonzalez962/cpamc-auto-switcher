package quota

import (
	"testing"
)

func TestParseQuotaSummary(t *testing.T) {
	rawJSON := []byte(`{
		"groups": [
			{
				"displayName": "Claude Models",
				"buckets": [
					{
						"displayName": "Weekly Limit",
						"remainingFraction": 0.20,
						"resetTime": "2026-04-03T12:00:00Z"
					},
					{
						"displayName": "5-Hour Limit",
						"remainingFraction": 0.05,
						"resetTime": "2026-04-01T15:00:00Z"
					}
				]
			},
			{
				"displayName": "Gemini Models",
				"buckets": [
					{
						"displayName": "Weekly Limit",
						"remainingFraction": 0.10
					}
				]
			}
		]
	}`)

	aq, err := ParseQuotaSummary(rawJSON)
	if err != nil {
		t.Fatalf("ParseQuotaSummary failed: %v", err)
	}

	if !aq.HasFiveHour || !aq.HasWeekly {
		t.Fatalf("expected both 5h and weekly windows, got 5h=%v, weekly=%v", aq.HasFiveHour, aq.HasWeekly)
	}

	// 5h remaining 5% -> consumed 95%
	if aq.WorstFiveHour.ConsumedPercentage != 95.0 {
		t.Errorf("expected 5h consumed 95.0, got %f", aq.WorstFiveHour.ConsumedPercentage)
	}

	// Weekly worst case across Claude (20% rem -> 80% cons) and Gemini (10% rem -> 90% cons) is 90% (Policy 2A)
	if aq.WorstWeekly.ConsumedPercentage != 90.0 {
		t.Errorf("expected worst weekly consumed 90.0, got %f", aq.WorstWeekly.ConsumedPercentage)
	}

	// 5h consumed (95%) >= 90% threshold -> Should rotate
	rotate, reason := aq.ShouldRotate(90.0, 95.0)
	if !rotate {
		t.Errorf("expected rotation due to 5h limit, got reason: %s", reason)
	}
}

func TestParseQuotaSummaryMissingFiveHour(t *testing.T) {
	// Upstream returns only Weekly (Policy 1A)
	rawJSON := []byte(`{
		"groups": [
			{
				"displayName": "Models",
				"buckets": [
					{
						"displayName": "Weekly",
						"remainingFraction": 0.04
					}
				]
			}
		]
	}`)

	aq, err := ParseQuotaSummary(rawJSON)
	if err != nil {
		t.Fatalf("ParseQuotaSummary failed: %v", err)
	}

	if aq.HasFiveHour {
		t.Errorf("expected HasFiveHour to be false")
	}
	if !aq.HasWeekly {
		t.Fatalf("expected HasWeekly to be true")
	}

	// Weekly consumed = 96%, Weekly threshold = 95% -> Should rotate
	rotate, reason := aq.ShouldRotate(90.0, 95.0)
	if !rotate {
		t.Errorf("expected rotate true on weekly threshold, got false. reason: %s", reason)
	}
}

func TestMinAvailableRemaining(t *testing.T) {
	rawJSON := []byte(`{
		"groups": [
			{
				"displayName": "Models",
				"buckets": [
					{ "displayName": "5 Hours", "remainingFraction": 0.50 },
					{ "displayName": "Weekly", "remainingFraction": 0.80 }
				]
			}
		]
	}`)

	aq, err := ParseQuotaSummary(rawJSON)
	if err != nil {
		t.Fatalf("ParseQuotaSummary failed: %v", err)
	}

	// Min remaining between 50% and 80% is 50%
	minRem := aq.MinAvailableRemaining()
	if minRem != 50.0 {
		t.Errorf("expected MinAvailableRemaining 50.0, got %f", minRem)
	}
}

func TestParseCodexQuotaSummary(t *testing.T) {
	rawJSON := []byte(`{
		"rate_limit": {
			"allowed": true,
			"limit_reached": false,
			"primary_window": {
				"used_percent": 11.5,
				"limit_window_seconds": 18000,
				"reset_at": 1789508751
			},
			"secondary_window": {
				"used_percent": 25.0,
				"limit_window_seconds": 604800,
				"reset_at": 1789844787
			}
		},
		"plan_type": "plus"
	}`)

	aq, err := ParseCodexQuotaSummary(rawJSON)
	if err != nil {
		t.Fatalf("ParseCodexQuotaSummary failed: %v", err)
	}

	if !aq.HasFiveHour || !aq.HasWeekly {
		t.Fatalf("expected both 5h and weekly, got 5h=%v, weekly=%v", aq.HasFiveHour, aq.HasWeekly)
	}

	if aq.WorstFiveHour.ConsumedPercentage != 11.5 || aq.WorstFiveHour.RemainingPercentage != 88.5 {
		t.Errorf("unexpected 5h percentages: consumed=%f, remaining=%f",
			aq.WorstFiveHour.ConsumedPercentage, aq.WorstFiveHour.RemainingPercentage)
	}

	if aq.WorstWeekly.ConsumedPercentage != 25.0 || aq.WorstWeekly.RemainingPercentage != 75.0 {
		t.Errorf("unexpected weekly percentages: consumed=%f, remaining=%f",
			aq.WorstWeekly.ConsumedPercentage, aq.WorstWeekly.RemainingPercentage)
	}

	rotate, _ := aq.ShouldRotate(90.0, 95.0)
	if rotate {
		t.Errorf("expected no rotation for healthy codex account")
	}

	minRem := aq.MinAvailableRemaining()
	if minRem != 75.0 {
		t.Errorf("expected min remaining 75.0, got %f", minRem)
	}
}

func TestParseCodexQuotaSummaryLimitReached(t *testing.T) {
	rawJSON := []byte(`{
		"rate_limit": {
			"allowed": false,
			"limit_reached": true,
			"primary_window": {
				"used_percent": 0.0
			}
		}
	}`)

	aq, err := ParseCodexQuotaSummary(rawJSON)
	if err != nil {
		t.Fatalf("ParseCodexQuotaSummary failed: %v", err)
	}

	if !aq.HasFiveHour {
		t.Fatal("expected 5h window present")
	}

	if aq.WorstFiveHour.ConsumedPercentage != 100.0 || aq.WorstFiveHour.RemainingPercentage != 0.0 {
		t.Errorf("expected 100%% consumed on limit_reached, got consumed=%f, remaining=%f",
			aq.WorstFiveHour.ConsumedPercentage, aq.WorstFiveHour.RemainingPercentage)
	}

	rotate, reason := aq.ShouldRotate(90.0, 95.0)
	if !rotate {
		t.Errorf("expected rotation when limit is reached, got reason: %s", reason)
	}
}

func TestParseQuotaSummaryForProvider(t *testing.T) {
	antigravityJSON := []byte(`{"groups":[{"displayName":"A","buckets":[{"displayName":"5h","remainingFraction":0.2}]}]}`)
	codexJSON := []byte(`{"rate_limit":{"allowed":true,"primary_window":{"used_percent":80.0}}}`)

	aqAg, err := ParseQuotaSummaryForProvider("antigravity", antigravityJSON)
	if err != nil {
		t.Fatalf("antigravity parse failed: %v", err)
	}
	if !aqAg.HasFiveHour || aqAg.WorstFiveHour.ConsumedPercentage != 80.0 {
		t.Errorf("unexpected antigravity parsed: %+v", aqAg.WorstFiveHour)
	}

	aqCodex, err := ParseQuotaSummaryForProvider("codex", codexJSON)
	if err != nil {
		t.Fatalf("codex parse failed: %v", err)
	}
	if !aqCodex.HasFiveHour || aqCodex.WorstFiveHour.ConsumedPercentage != 80.0 {
		t.Errorf("unexpected codex parsed: %+v", aqCodex.WorstFiveHour)
	}
}
