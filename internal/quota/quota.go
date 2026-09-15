package quota

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Bucket represents a single rate/quota window in Antigravity.
type Bucket struct {
	DisplayName       string  `json:"displayName"`
	Label             string  `json:"label"`
	Window            string  `json:"window"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime"`
}

// Group represents a grouping of models and buckets.
type Group struct {
	DisplayName string   `json:"displayName"`
	Buckets     []Bucket `json:"buckets"`
}

type quotaSummaryResponse struct {
	Groups []Group `json:"groups"`
}

// QuotaWindow identifies the classified window kind.
type QuotaWindow string

const (
	WindowFiveHour QuotaWindow = "5h"
	WindowWeekly   QuotaWindow = "weekly"
	WindowUnknown  QuotaWindow = "unknown"
)

// EvaluatedBucket holds computed percentages for a classified window.
type EvaluatedBucket struct {
	Window              QuotaWindow
	DisplayName         string
	RemainingPercentage float64 // 0.0 to 100.0
	ConsumedPercentage  float64 // 0.0 to 100.0
	ResetTime           string
}

// AccountQuota aggregates evaluated windows across all groups.
type AccountQuota struct {
	HasFiveHour bool
	HasWeekly   bool

	// Worst-case (highest consumed / lowest remaining) across groups (Policy 2A)
	WorstFiveHour *EvaluatedBucket
	WorstWeekly   *EvaluatedBucket
}

// ClassifyWindow identifies if a bucket corresponds to 5 hours or Weekly.
func ClassifyWindow(name string) QuotaWindow {
	lower := strings.ToLower(strings.TrimSpace(name))
	if strings.Contains(lower, "5") || strings.Contains(lower, "five") {
		return WindowFiveHour
	}
	if strings.Contains(lower, "week") {
		return WindowWeekly
	}
	return WindowUnknown
}

// ParseQuotaSummary decodes Antigravity retrieveUserQuotaSummary JSON and calculates
// worst-case limits across groups (Policy 2A).
func ParseQuotaSummary(raw []byte) (*AccountQuota, error) {
	var resp quotaSummaryResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal quota response: %w", err)
	}

	result := &AccountQuota{}

	for _, group := range resp.Groups {
		for _, b := range group.Buckets {
			name := b.DisplayName
			if name == "" {
				name = b.Label
			}
			if name == "" {
				name = b.Window
			}
			if name == "" {
				name = "Limit"
			}

			wType := ClassifyWindow(name)
			if wType == WindowUnknown {
				continue
			}

			remPct := b.RemainingFraction * 100.0
			if remPct < 0 {
				remPct = 0
			}
			if remPct > 100.0 {
				remPct = 100.0
			}
			consPct := 100.0 - remPct

			eval := &EvaluatedBucket{
				Window:              wType,
				DisplayName:         name,
				RemainingPercentage: remPct,
				ConsumedPercentage:  consPct,
				ResetTime:           b.ResetTime,
			}

			switch wType {
			case WindowFiveHour:
				result.HasFiveHour = true
				if result.WorstFiveHour == nil || eval.ConsumedPercentage > result.WorstFiveHour.ConsumedPercentage {
					result.WorstFiveHour = eval
				}
			case WindowWeekly:
				result.HasWeekly = true
				if result.WorstWeekly == nil || eval.ConsumedPercentage > result.WorstWeekly.ConsumedPercentage {
					result.WorstWeekly = eval
				}
			}
		}
	}

	return result, nil
}

// ShouldRotate evaluates rotation conditions:
// - 5h consumed >= fiveHourThreshold OR weekly consumed >= weeklyThreshold
// - Policy 1A: If 5h window is absent, evaluate only weekly.
func (aq *AccountQuota) ShouldRotate(fiveHourThreshold, weeklyThreshold float64) (bool, string) {
	if aq == nil {
		return false, "nil quota data"
	}

	if aq.HasFiveHour && aq.WorstFiveHour != nil {
		if aq.WorstFiveHour.ConsumedPercentage >= fiveHourThreshold {
			return true, fmt.Sprintf("5-hour quota reached %.1f%% (threshold: %.1f%%)",
				aq.WorstFiveHour.ConsumedPercentage, fiveHourThreshold)
		}
	}

	if aq.HasWeekly && aq.WorstWeekly != nil {
		if aq.WorstWeekly.ConsumedPercentage >= weeklyThreshold {
			return true, fmt.Sprintf("weekly quota reached %.1f%% (threshold: %.1f%%)",
				aq.WorstWeekly.ConsumedPercentage, weeklyThreshold)
		}
	}

	return false, "quotas within acceptable thresholds"
}

// MinAvailableRemaining calculates the minimum remaining percentage across available windows (Policy 3B).
// Higher value means more available capacity.
func (aq *AccountQuota) MinAvailableRemaining() float64 {
	if aq == nil {
		return 0.0
	}

	var values []float64
	if aq.HasFiveHour && aq.WorstFiveHour != nil {
		values = append(values, aq.WorstFiveHour.RemainingPercentage)
	}
	if aq.HasWeekly && aq.WorstWeekly != nil {
		values = append(values, aq.WorstWeekly.RemainingPercentage)
	}

	if len(values) == 0 {
		return 0.0
	}

	minVal := values[0]
	for _, v := range values[1:] {
		if v < minVal {
			minVal = v
		}
	}
	return minVal
}
