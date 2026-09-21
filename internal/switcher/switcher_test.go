package switcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"cpamc-auto-switcher/internal/client"
	"cpamc-auto-switcher/internal/config"
)

func TestSwitcherWorkflow(t *testing.T) {
	var mu sync.Mutex
	prefixes := map[string]string{
		"acc-1.json": "agy",   // active
		"acc-2.json": "agy_1", // reserve 1
		"acc-3.json": "agy_2", // reserve 2
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch r.URL.Path {
		case "/v0/management/auth-files":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"id": "acc-1", "name": "acc-1.json", "auth_index": "idx-1", "provider": "antigravity"},
					{"id": "acc-2", "name": "acc-2.json", "auth_index": "idx-2", "provider": "antigravity"},
					{"id": "acc-3", "name": "acc-3.json", "auth_index": "idx-3", "provider": "antigravity"},
				},
			})
		case "/v0/management/auth-files/download":
			name := r.URL.Query().Get("name")
			p := prefixes[name]
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prefix": p,
			})
		case "/v0/management/api-call":
			var req client.APICallRequest
			_ = json.NewDecoder(r.Body).Decode(&req)

			w.Header().Set("Content-Type", "application/json")
			switch req.AuthIndex {
			case "idx-1":
				// Active account: 5h limit reached 92% (threshold 90%) -> Trigger rotation
				_, _ = w.Write([]byte(`{
					"groups": [{
						"displayName": "Models",
						"buckets": [
							{"displayName": "5-Hour Limit", "remainingFraction": 0.08},
							{"displayName": "Weekly Limit", "remainingFraction": 0.20}
						]
					}]
				}`))
			case "idx-2":
				// Reserve 1: 5h remaining 60%, weekly remaining 70% (min available = 60%)
				_, _ = w.Write([]byte(`{
					"groups": [{
						"displayName": "Models",
						"buckets": [
							{"displayName": "5-Hour Limit", "remainingFraction": 0.60},
							{"displayName": "Weekly Limit", "remainingFraction": 0.70}
						]
					}]
				}`))
			case "idx-3":
				// Reserve 2: 5h remaining 80%, weekly remaining 85% (min available = 80%) -> BETTER CANDIDATE
				_, _ = w.Write([]byte(`{
					"groups": [{
						"displayName": "Models",
						"buckets": [
							{"displayName": "5-Hour Limit", "remainingFraction": 0.80},
							{"displayName": "Weekly Limit", "remainingFraction": 0.85}
						]
					}]
				}`))
			}
		case "/v0/management/auth-files/fields":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			name := req["name"]
			prefixes[name] = req["prefix"]
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		Endpoint:            server.URL,
		ManagementKey:       "dummy",
		Provider:            "antigravity",
		ActivePrefix:        "agy",
		ReservePrefixPrefix: "agy_",
		FiveHourThreshold:   90.0,
		WeeklyThreshold:     95.0,
	}

	cli, err := client.New(server.URL, "dummy")
	if err != nil {
		t.Fatalf("client.New failed: %v", err)
	}

	sw := New(cfg, cli)

	// Test ListAccounts
	accounts, err := sw.ListAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListAccounts failed: %v", err)
	}
	if len(accounts) != 3 {
		t.Fatalf("expected 3 accounts, got %d", len(accounts))
	}
	if !accounts[0].IsActive || accounts[0].Entry.ID != "acc-1" {
		t.Errorf("expected acc-1 to be active and first in list, got %+v", accounts[0])
	}

	// Test Manual SwitchToAccount by prefix
	manualRes, err := sw.SwitchToAccount(context.Background(), "agy_1", false)
	if err != nil {
		t.Fatalf("SwitchToAccount failed: %v", err)
	}
	if !manualRes.Rotated {
		t.Errorf("expected manual switch rotation to be true, got %s", manualRes.Reason)
	}
	if prefixes["acc-2.json"] != "agy" || prefixes["acc-1.json"] != "agy_1" {
		t.Errorf("prefixes after manual switch unexpected: %+v", prefixes)
	}

	// Revert prefixes for automatic flow test
	prefixes["acc-1.json"] = "agy"
	prefixes["acc-2.json"] = "agy_1"
	prefixes["acc-3.json"] = "agy_2"

	// 1. Dry run test
	dryRes, err := sw.Run(context.Background(), true)
	if err != nil {
		t.Fatalf("dry run failed: %v", err)
	}
	if !dryRes.Rotated {
		t.Fatalf("expected rotation in dry run, got reason: %s", dryRes.Reason)
	}
	if dryRes.SelectedReserve != "acc-3" {
		t.Errorf("expected acc-3 to be selected (highest quota), got %s", dryRes.SelectedReserve)
	}

	// Confirm prefixes were not changed in dry run
	if prefixes["acc-1.json"] != "agy" || prefixes["acc-3.json"] != "agy_2" {
		t.Fatalf("prefixes modified during dry run: %+v", prefixes)
	}

	// 2. Real execution test
	res, err := sw.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("real run failed: %v", err)
	}
	if !res.Rotated {
		t.Fatalf("expected real run rotation, got reason: %s", res.Reason)
	}

	// Verify direct swap occurred (acc-3 became agy, acc-1 became agy_2)
	mu.Lock()
	defer mu.Unlock()
	if prefixes["acc-3.json"] != "agy" {
		t.Errorf("acc-3.json expected prefix 'agy', got %q", prefixes["acc-3.json"])
	}
	if prefixes["acc-1.json"] != "agy_2" {
		t.Errorf("acc-1.json expected prefix 'agy_2', got %q", prefixes["acc-1.json"])
	}
}

func TestCodexSwitcherWorkflow(t *testing.T) {
	var mu sync.Mutex
	prefixes := map[string]string{
		"codex-1.json": "codex",   // active
		"codex-2.json": "codex_1", // reserve
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch r.URL.Path {
		case "/v0/management/auth-files":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"id": "codex-1", "name": "codex-1.json", "auth_index": "idx-c1", "provider": "codex"},
					{"id": "codex-2", "name": "codex-2.json", "auth_index": "idx-c2", "provider": "codex"},
				},
			})
		case "/v0/management/auth-files/download":
			name := r.URL.Query().Get("name")
			p := prefixes[name]
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prefix":             p,
				"chatgpt_account_id": "acc-" + name,
			})
		case "/v0/management/api-call":
			var req client.APICallRequest
			_ = json.NewDecoder(r.Body).Decode(&req)

			w.Header().Set("Content-Type", "application/json")
			switch req.AuthIndex {
			case "idx-c1":
				// Active account 5h limit used 93% -> Triggers rotation
				_, _ = w.Write([]byte(`{
					"rate_limit": {
						"allowed": true,
						"primary_window": {
							"used_percent": 93.0
						},
						"secondary_window": {
							"used_percent": 30.0
						}
					}
				}`))
			case "idx-c2":
				// Reserve account 5h limit used 10%
				_, _ = w.Write([]byte(`{
					"rate_limit": {
						"allowed": true,
						"primary_window": {
							"used_percent": 10.0
						},
						"secondary_window": {
							"used_percent": 20.0
						}
					}
				}`))
			}
		case "/v0/management/auth-files/fields":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			name := req["name"]
			prefixes[name] = req["prefix"]
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		Endpoint:            server.URL,
		ManagementKey:       "dummy",
		Provider:            "codex",
		FiveHourThreshold:   90.0,
		WeeklyThreshold:     95.0,
	}

	cli, err := client.New(cfg.Endpoint, cfg.ManagementKey)
	if err != nil {
		t.Fatalf("client.New failed: %v", err)
	}
	sw := New(cfg, cli)

	// Test ListAccounts
	accounts, err := sw.ListAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListAccounts failed: %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accounts))
	}
	if !accounts[0].IsActive || accounts[0].Prefix != "codex" {
		t.Errorf("expected first account to be active with prefix 'codex', got %+v", accounts[0])
	}

	// Automatic rotation run
	res, err := sw.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !res.Rotated {
		t.Fatalf("expected rotation, got: %s", res.Reason)
	}

	mu.Lock()
	if prefixes["codex-2.json"] != "codex" {
		t.Errorf("expected codex-2.json to become active 'codex', got %q", prefixes["codex-2.json"])
	}
	if prefixes["codex-1.json"] != "codex_1" {
		t.Errorf("expected codex-1.json to be demoted to 'codex_1', got %q", prefixes["codex-1.json"])
	}
	mu.Unlock()

	// Test Manual switch back
	manualRes, err := sw.SwitchToAccount(context.Background(), "codex-1.json", false)
	if err != nil {
		t.Fatalf("SwitchToAccount failed: %v", err)
	}
	if !manualRes.Rotated {
		t.Fatalf("expected manual rotation true, got: %s", manualRes.Reason)
	}
	mu.Lock()
	if prefixes["codex-1.json"] != "codex" || prefixes["codex-2.json"] != "codex_1" {
		t.Errorf("unexpected prefixes after manual switch: %+v", prefixes)
	}
	mu.Unlock()
}

func TestConcurrentQuotaFetchingAndDecision(t *testing.T) {
	var inFlight int32
	var maxConcurrent int32
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"id": "c1", "name": "c1.json", "auth_index": "idx-1", "provider": "codex"},
					{"id": "c2", "name": "c2.json", "auth_index": "idx-2", "provider": "codex"},
					{"id": "c3", "name": "c3.json", "auth_index": "idx-3", "provider": "codex"},
				},
			})
		case "/v0/management/auth-files/download":
			name := r.URL.Query().Get("name")
			p := "codex_1"
			if name == "c1.json" {
				p = "codex"
			} else if name == "c3.json" {
				p = "codex_2"
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"prefix": p})
		case "/v0/management/api-call":
			mu.Lock()
			inFlight++
			if inFlight > maxConcurrent {
				maxConcurrent = inFlight
			}
			mu.Unlock()

			// Artificially simulate non-instantaneous API response
			time.Sleep(20 * time.Millisecond)

			mu.Lock()
			inFlight--
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"rate_limit": {
					"allowed": true,
					"primary_window": {"used_percent": 10.0},
					"secondary_window": {"used_percent": 20.0}
				}
			}`))
		case "/v0/management/auth-files/fields":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		Endpoint:            server.URL,
		ManagementKey:       "dummy",
		Provider:            "codex",
		FiveHourThreshold:   90.0,
		WeeklyThreshold:     95.0,
	}

	cli, err := client.New(cfg.Endpoint, cfg.ManagementKey)
	if err != nil {
		t.Fatalf("client.New failed: %v", err)
	}
	sw := New(cfg, cli)

	// ListAccounts should fetch accounts concurrently
	accounts, err := sw.ListAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListAccounts failed: %v", err)
	}
	if len(accounts) != 3 {
		t.Fatalf("expected 3 accounts, got %d", len(accounts))
	}

	mu.Lock()
	concurrentObserved := maxConcurrent
	mu.Unlock()

	if concurrentObserved < 2 {
		t.Errorf("expected concurrent requests > 1, got %d", concurrentObserved)
	}
}

func TestProfileListing(t *testing.T) {
	prefixes := map[string]string{
		"a1.json": "agy",
		"a2.json": "agy_1",
		"a3.json": "agy_team_a",
		"a4.json": "agy_p1_1",
		"a5.json": "agy_p1",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"id": "a1", "name": "a1.json", "auth_index": "idx-1", "provider": "antigravity"},
					{"id": "a2", "name": "a2.json", "auth_index": "idx-2", "provider": "antigravity"},
					{"id": "a3", "name": "a3.json", "auth_index": "idx-3", "provider": "antigravity"},
					{"id": "a4", "name": "a4.json", "auth_index": "idx-4", "provider": "antigravity"},
					{"id": "a5", "name": "a5.json", "auth_index": "idx-5", "provider": "antigravity"},
				},
			})
		case "/v0/management/auth-files/download":
			name := r.URL.Query().Get("name")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prefix": prefixes[name],
			})
		case "/v0/management/api-call":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"groups": [{
					"displayName": "Models",
					"buckets": [{"displayName": "5-Hour Limit", "remainingFraction": 0.80}]
				}]
			}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		Endpoint:      server.URL,
		ManagementKey: "dummy",
		Provider:      "antigravity",
	}
	cli, _ := client.New(server.URL, "dummy")
	sw := New(cfg, cli)

	accounts, err := sw.ListAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListAccounts failed: %v", err)
	}
	if len(accounts) != 5 {
		t.Fatalf("expected 5 accounts, got %d", len(accounts))
	}

	// Expected order:
	// 1. Profile: "", Active: true (a1, agy)
	// 2. Profile: "", Active: false, Reserve: true (a2, agy_1)
	// 3. Profile: "p1", Active: true (a5, agy_p1)
	// 4. Profile: "p1", Active: false, Reserve: true (a4, agy_p1_1)
	// 5. Profile: "team_a", Active: true (a3, agy_team_a)
	expectedOrder := []struct {
		id      string
		profile string
		active  bool
		reserve bool
	}{
		{"a1", "", true, false},
		{"a2", "", false, true},
		{"a5", "p1", true, false},
		{"a4", "p1", false, true},
		{"a3", "team_a", true, false},
	}

	for i, exp := range expectedOrder {
		if accounts[i].Entry.ID != exp.id ||
			accounts[i].Profile != exp.profile ||
			accounts[i].IsActive != exp.active ||
			accounts[i].IsReserve != exp.reserve {
			t.Errorf("account[%d] = %+v, want id=%s profile=%s active=%v reserve=%v",
				i, accounts[i], exp.id, exp.profile, exp.active, exp.reserve)
		}
	}
}

func TestProfileManualSwitch(t *testing.T) {
	var mu sync.Mutex
	prefixes := map[string]string{
		"def-act.json":  "agy",
		"def-res.json":  "agy_1",
		"p1-act.json":   "agy_p1",
		"p1-res.json":   "agy_p1_1",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/v0/management/auth-files":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"id": "def-act", "name": "def-act.json", "auth_index": "i1", "provider": "antigravity"},
					{"id": "def-res", "name": "def-res.json", "auth_index": "i2", "provider": "antigravity"},
					{"id": "p1-act", "name": "p1-act.json", "auth_index": "i3", "provider": "antigravity"},
					{"id": "p1-res", "name": "p1-res.json", "auth_index": "i4", "provider": "antigravity"},
				},
			})
		case "/v0/management/auth-files/download":
			name := r.URL.Query().Get("name")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"prefix": prefixes[name]})
		case "/v0/management/auth-files/fields":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			prefixes[req["name"]] = req["prefix"]
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		Endpoint:      server.URL,
		ManagementKey: "dummy",
		Provider:      "antigravity",
	}
	cli, _ := client.New(server.URL, "dummy")
	sw := New(cfg, cli)

	// Dry run switch within profile p1
	dryRes, err := sw.SwitchToAccount(context.Background(), "agy_p1_1", true)
	if err != nil {
		t.Fatalf("dry run failed: %v", err)
	}
	if !dryRes.Rotated {
		t.Fatalf("expected dry run rotated true")
	}
	mu.Lock()
	if prefixes["p1-res.json"] != "agy_p1_1" || prefixes["p1-act.json"] != "agy_p1" {
		t.Errorf("prefixes changed during dry-run: %+v", prefixes)
	}
	mu.Unlock()

	// Real switch within profile p1
	res, err := sw.SwitchToAccount(context.Background(), "agy_p1_1", false)
	if err != nil {
		t.Fatalf("SwitchToAccount failed: %v", err)
	}
	if !res.Rotated {
		t.Fatalf("expected rotation: %s", res.Reason)
	}

	mu.Lock()
	defer mu.Unlock()
	// p1 accounts swapped
	if prefixes["p1-res.json"] != "agy_p1" {
		t.Errorf("expected p1-res.json to have prefix agy_p1, got %s", prefixes["p1-res.json"])
	}
	if prefixes["p1-act.json"] != "agy_p1_1" {
		t.Errorf("expected p1-act.json to have prefix agy_p1_1, got %s", prefixes["p1-act.json"])
	}
	// default pool accounts unchanged!
	if prefixes["def-act.json"] != "agy" || prefixes["def-res.json"] != "agy_1" {
		t.Errorf("default pool accounts unexpectedly modified: %+v", prefixes)
	}
}

func TestProfileAutomaticRotationIsolation(t *testing.T) {
	var mu sync.Mutex
	prefixes := map[string]string{
		"d-act.json":  "agy",
		"d-res.json":  "agy_1",
		"p1-act.json": "agy_p1",
		"p1-r1.json":  "agy_p1_1",
		"p1-r2.json":  "agy_p1_2",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/v0/management/auth-files":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"id": "d-act", "name": "d-act.json", "auth_index": "idx-d1", "provider": "antigravity"},
					{"id": "d-res", "name": "d-res.json", "auth_index": "idx-d2", "provider": "antigravity"},
					{"id": "p1-act", "name": "p1-act.json", "auth_index": "idx-p1", "provider": "antigravity"},
					{"id": "p1-r1", "name": "p1-r1.json", "auth_index": "idx-p2", "provider": "antigravity"},
					{"id": "p1-r2", "name": "p1-r2.json", "auth_index": "idx-p3", "provider": "antigravity"},
				},
			})
		case "/v0/management/auth-files/download":
			name := r.URL.Query().Get("name")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"prefix": prefixes[name]})
		case "/v0/management/api-call":
			var req client.APICallRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			w.Header().Set("Content-Type", "application/json")
			switch req.AuthIndex {
			case "idx-d1":
				// Default pool active: healthy (10% consumed, 90% remaining)
				_, _ = w.Write([]byte(`{"groups":[{"buckets":[{"displayName":"5-Hour Limit","remainingFraction":0.90}]}]}`))
			case "idx-d2":
				// Default pool reserve: healthy
				_, _ = w.Write([]byte(`{"groups":[{"buckets":[{"displayName":"5-Hour Limit","remainingFraction":0.90}]}]}`))
			case "idx-p1":
				// Profile p1 active: EXCEEDED (95% consumed, 5% remaining) -> triggers rotation in p1
				_, _ = w.Write([]byte(`{"groups":[{"buckets":[{"displayName":"5-Hour Limit","remainingFraction":0.05}]}]}`))
			case "idx-p2":
				// Profile p1 reserve 1: 50% remaining
				_, _ = w.Write([]byte(`{"groups":[{"buckets":[{"displayName":"5-Hour Limit","remainingFraction":0.50}]}]}`))
			case "idx-p3":
				// Profile p1 reserve 2: 80% remaining -> BEST RESERVE IN P1
				_, _ = w.Write([]byte(`{"groups":[{"buckets":[{"displayName":"5-Hour Limit","remainingFraction":0.80}]}]}`))
			}
		case "/v0/management/auth-files/fields":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			prefixes[req["name"]] = req["prefix"]
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		Endpoint:          server.URL,
		ManagementKey:     "dummy",
		Provider:          "antigravity",
		FiveHourThreshold: 90.0,
		WeeklyThreshold:   95.0,
	}
	cli, _ := client.New(server.URL, "dummy")
	sw := New(cfg, cli)

	// 1. Evaluate only default profile -> should not rotate
	resDef, err := sw.Run(context.Background(), false, "default")
	if err != nil {
		t.Fatalf("sw.Run default failed: %v", err)
	}
	if resDef.Rotated {
		t.Errorf("expected default pool not rotated, got reason: %s", resDef.Reason)
	}

	// 2. Evaluate all profiles -> profile p1 rotates with p1-r2, default pool untouched
	resAll, err := sw.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("sw.Run all failed: %v", err)
	}
	if !resAll.Rotated {
		t.Fatalf("expected rotation across profiles, got: %s", resAll.Reason)
	}

	mu.Lock()
	defer mu.Unlock()
	// Default pool remains intact
	if prefixes["d-act.json"] != "agy" || prefixes["d-res.json"] != "agy_1" {
		t.Errorf("default pool modified: %+v", prefixes)
	}
	// Profile p1 rotated: p1-r2 promoted to agy_p1, p1-act demoted to agy_p1_2
	if prefixes["p1-r2.json"] != "agy_p1" {
		t.Errorf("expected p1-r2.json to become agy_p1, got %s", prefixes["p1-r2.json"])
	}
	if prefixes["p1-act.json"] != "agy_p1_2" {
		t.Errorf("expected p1-act.json to become agy_p1_2, got %s", prefixes["p1-act.json"])
	}
}

