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
