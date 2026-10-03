package switcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cpamc-auto-switcher/internal/client"
	"cpamc-auto-switcher/internal/config"
	"cpamc-auto-switcher/internal/state"
)

func TestReviewMarksDisabledAccountUnresolved(t *testing.T) {
	st := &state.State{}
	st.RecordProblem("codex", "disabled", "quota query failed")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/auth-files" {
			_, _ = w.Write([]byte(`{"files":[{"id":"disabled","name":"d.json","auth_index":"i","provider":"codex","disabled":true}]}`))
		}
	}))
	defer server.Close()
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex"}, cli)
	sw.SetState(st)
	problems, err := sw.Review(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || problems[0].Status != "disabled" {
		t.Fatalf("disabled account not explicitly retained: %+v", problems)
	}
}

func TestReviewDoesNotClearEmptyQuotaAndMarksMissing(t *testing.T) {
	st := &state.State{}
	st.RecordProblem("codex", "missing", "quota query failed")
	st.RecordProblem("codex", "empty", "quota query failed")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"empty","name":"empty.json","auth_index":"idx","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			_, _ = w.Write([]byte(`{"prefix":"codex"}`))
		case "/v0/management/api-call":
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex"}, cli)
	sw.SetState(st)
	problems, err := sw.Review(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 2 {
		t.Fatalf("empty quota/missing records falsely resolved: %+v", problems)
	}
	for _, p := range problems {
		if p.Status == "" || p.Status == "pending" {
			t.Fatalf("expected explicit unresolved status: %+v", p)
		}
	}
}

func TestMixedQuotaListingWithoutStatePreservesHealthyAccounts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"bad","name":"bad.json","auth_index":"bad-index","provider":"codex"},{"id":"good","name":"good.json","auth_index":"good-index","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			_, _ = w.Write([]byte(`{"prefix":"codex"}`))
		case "/v0/management/api-call":
			var req client.APICallRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.AuthIndex == "bad-index" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":10}}}`))
		}
	}))
	defer server.Close()
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex"}, cli)
	accounts, err := sw.ListAccounts(context.Background())
	if err != nil {
		t.Fatalf("account-local quota failure should not abort a nil-state listing: %v", err)
	}
	if len(accounts) != 2 || accounts[0].Quota != nil || accounts[0].QuotaErr == nil || accounts[1].Quota == nil {
		t.Fatalf("mixed listing lost expected failed/healthy accounts: %+v", accounts)
	}
}

func TestOuterQuotaBadRequestListsHealthyAccountsAndRecordsSafeReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"bad","name":"bad.json","auth_index":"bad-index","provider":"codex"},{"id":"good","name":"good.json","auth_index":"good-index","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			_, _ = w.Write([]byte(`{"prefix":"codex"}`))
		case "/v0/management/api-call":
			var req client.APICallRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.AuthIndex == "bad-index" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("private request detail"))
				return
			}
			_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":10}}}`))
		}
	}))
	defer server.Close()
	st := &state.State{}
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex"}, cli)
	sw.SetState(st)
	accounts, err := sw.ListAccounts(context.Background())
	if err != nil || len(accounts) != 2 {
		t.Fatalf("listing should preserve healthy account alongside quota 400: accounts=%d err=%v", len(accounts), err)
	}
	problems := st.ProblemSnapshot()
	if len(problems) != 1 || problems[0].Identity != "bad" || problems[0].Reason != "account quota request rejected by management API (400)" {
		t.Fatalf("unexpected safe account diagnostic: %+v", problems)
	}
	if strings.Contains(problems[0].Reason, "private") {
		t.Fatal("private response detail persisted")
	}
}

func TestOuterQuota400ActiveDoesNotFailOverAcrossProfilesOrProviders(t *testing.T) {
	calls := map[string]int{}
	var callsMu sync.Mutex
	prefixes := map[string]string{"active.json": "codex", "reserve.json": "codex_1", "profile.json": "codex_p1", "agy.json": "agy", "agy-res.json": "agy_1"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"active","name":"active.json","auth_index":"a","provider":"codex"},{"id":"reserve","name":"reserve.json","auth_index":"r","provider":"codex"},{"id":"profile","name":"profile.json","auth_index":"p","provider":"codex"},{"id":"agy","name":"agy.json","auth_index":"g","provider":"antigravity"},{"id":"agy-res","name":"agy-res.json","auth_index":"gr","provider":"antigravity"}]}`))
		case "/v0/management/auth-files/download":
			_, _ = fmt.Fprintf(w, `{"prefix":%q}`, prefixes[r.URL.Query().Get("name")])
		case "/v0/management/api-call":
			var req client.APICallRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			callsMu.Lock()
			calls[req.AuthIndex]++
			callsMu.Unlock()
			if req.AuthIndex == "a" || req.AuthIndex == "p" || req.AuthIndex == "g" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("private detail"))
				return
			}
			_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":10}}}`))
		case "/v0/management/auth-files/fields":
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()
	st := &state.State{}
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "all", FiveHourThreshold: 90}, cli)
	sw.SetState(st)
	res, err := sw.Run(context.Background(), false)
	if err != nil || !res.Rotated {
		t.Fatalf("expected account-local failures to fail over where healthy reserves exist, result=%+v err=%v", res, err)
	}
	callsMu.Lock()
	defer callsMu.Unlock()
	if calls["a"] != 1 || calls["p"] != 1 || calls["r"] != 1 || calls["g"] != 3 || calls["gr"] != 1 {
		t.Fatalf("account-local failure reserve evaluation calls: %+v", calls)
	}
	problems := st.ProblemSnapshot()
	if len(problems) != 4 {
		t.Fatalf("missing safe account diagnostics: %+v", problems)
	}
	for _, p := range problems {
		if strings.Contains(p.Reason, "private") || p.Reason == "" {
			t.Fatalf("unsafe diagnostic: %+v", p)
		}
	}
}

func TestQuota400ReasonSurvivesReviewPersistence(t *testing.T) {
	path := t.TempDir() + "/state.json"
	st := &state.State{}
	st.RecordProblem("codex", "acct", "old generic diagnostic")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"acct","name":"acct.json","auth_index":"idx","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			_, _ = w.Write([]byte(`{"prefix":"codex"}`))
		case "/v0/management/api-call":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("private response"))
		}
	}))
	defer server.Close()
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex"}, cli)
	sw.SetState(st)
	if _, err := sw.Review(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := state.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	problems := loaded.ProblemSnapshot()
	if len(problems) != 1 || problems[0].Reason != "account quota request rejected by management API (400)" || problems[0].Status != "unresolved" {
		t.Fatalf("review overwrote safe typed reason: %+v", problems)
	}
}

func TestMetadataManagementAuthRejectionDoesNotQuarantineAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"acct","name":"acct.json","auth_index":"i","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("private auth detail"))
		}
	}))
	defer server.Close()
	st := &state.State{}
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex"}, cli)
	sw.SetState(st)
	_, err := sw.ListAccounts(context.Background())
	if err == nil {
		t.Fatal("management credential rejection should propagate")
	}
	if len(st.ProblemSnapshot()) != 0 {
		t.Fatalf("management credential rejection quarantined account: %+v", st.ProblemSnapshot())
	}
}

func TestQuotaTransportFailureDoesNotQuarantineAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"acct","name":"acct.json","auth_index":"i","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			_, _ = w.Write([]byte(`{"prefix":"codex"}`))
		case "/v0/management/api-call":
			h, ok := w.(http.Hijacker)
			if !ok {
				t.Error("response writer cannot hijack")
				return
			}
			conn, _, err := h.Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
		}
	}))
	defer server.Close()
	st := &state.State{}
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex"}, cli)
	sw.SetState(st)
	_, err := sw.ListAccounts(context.Background())
	var transportErr *client.TransportError
	if !errors.As(err, &transportErr) {
		t.Fatalf("quota transport failure must remain globally fatal: %v", err)
	}
	if len(st.ProblemSnapshot()) != 0 {
		t.Fatalf("transient transport failure was quarantined: %+v", st.ProblemSnapshot())
	}
}

func TestMalformedQuotaResponseIsQuarantinedAndSkipped(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"acct","name":"acct.json","auth_index":"i","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			_, _ = w.Write([]byte(`{"prefix":"codex"}`))
		case "/v0/management/api-call":
			calls++
			_, _ = w.Write([]byte(`{"rate_limit":{}}`))
		}
	}))
	defer server.Close()
	st := &state.State{}
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex"}, cli)
	sw.SetState(st)
	for i := 0; i < 2; i++ {
		if _, err := sw.ListAccounts(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 || !st.HasProblem("codex", "acct") {
		t.Fatalf("explicit listing should retry quarantined quota: calls=%d problems=%+v", calls, st.ProblemSnapshot())
	}
}

func TestAccountMetadataNotFoundIsRecordedWithoutAbortingBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"gone","name":"gone.json","auth_index":"x","provider":"codex"},{"id":"ok","name":"ok.json","auth_index":"y","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			if r.URL.Query().Get("name") == "gone.json" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("private account detail"))
			} else {
				_, _ = w.Write([]byte(`{"prefix":"codex"}`))
			}
		case "/v0/management/api-call":
			_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":10.0}}}`))
		}
	}))
	defer server.Close()
	st := &state.State{}
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex"}, cli)
	sw.SetState(st)
	accounts, err := sw.ListAccounts(context.Background())
	if err != nil {
		t.Fatalf("one metadata failure aborted batch: %v", err)
	}
	if len(accounts) != 2 || !st.HasProblem("codex", "gone") {
		t.Fatalf("missing account not retained: accounts=%d state=%+v", len(accounts), st.ProblemSnapshot())
	}
	if strings.Contains(st.ProblemSnapshot()[0].Reason, "private account detail") {
		t.Fatal("sensitive metadata response persisted")
	}
}

func TestManagementFailureDoesNotQuarantineAccounts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"acct","name":"acct.json","auth_index":"idx","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			_, _ = w.Write([]byte(`{"prefix":"codex"}`))
		case "/v0/management/api-call":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("service unavailable"))
		}
	}))
	defer server.Close()
	st := &state.State{}
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex"}, cli)
	sw.SetState(st)
	_, err := sw.ListAccounts(context.Background())
	if err == nil {
		t.Fatal("management outage must propagate")
	}
	if len(st.ProblemSnapshot()) != 0 {
		t.Fatalf("management outage quarantined account: %+v", st.ProblemSnapshot())
	}
}

func TestRoutineQuarantineAndReview(t *testing.T) {
	var mu sync.Mutex
	quotaCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"acct","name":"acct.json","auth_index":"idx","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			_, _ = w.Write([]byte(`{"prefix":"codex"}`))
		case "/v0/management/api-call":
			mu.Lock()
			quotaCalls++
			call := quotaCalls
			mu.Unlock()
			if call == 1 {
				_, _ = w.Write([]byte(`{"status_code":503,"body":"private upstream detail"}`))
				return
			}
			_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":10.0}}}`))
		}
	}))
	defer server.Close()
	cfg := &config.Config{Endpoint: server.URL, Provider: "codex", FiveHourThreshold: 90, WeeklyThreshold: 95}
	cli, err := client.New(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	st := &state.State{}
	sw := New(cfg, cli)
	sw.SetState(st)
	first, err := sw.ListAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(st.Problems) != 1 {
		t.Fatalf("expected failed account quarantined: accounts=%d problems=%+v", len(first), st.Problems)
	}
	_, err = sw.ListAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if quotaCalls != 2 {
		t.Fatalf("explicit listing should retry quarantined account: %d calls", quotaCalls)
	}
	mu.Unlock()
	remaining, err := sw.Review(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("successful review did not resolve account: %+v", remaining)
	}
	mu.Lock()
	if quotaCalls != 3 {
		t.Fatalf("review did not retry account after explicit listings: %d", quotaCalls)
	}
	mu.Unlock()
}

func TestFailedPromotionKeepsObservedCacheAndDoesNotPromote(t *testing.T) {
	prefixes := map[string]string{"active.json": "codex", "reserve.json": "codex_1"}
	patches := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"active","name":"active.json","auth_index":"a","provider":"codex"},{"id":"reserve","name":"reserve.json","auth_index":"r","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			_, _ = w.Write([]byte(`{"prefix":"` + prefixes[r.URL.Query().Get("name")] + `"}`))
		case "/v0/management/api-call":
			_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":10}}}`))
		case "/v0/management/auth-files/fields":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			patches = append(patches, req["name"]+":"+req["prefix"])
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	st := &state.State{}
	st.SetActive("codex", "", "old-cache")
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex", FiveHourThreshold: 90}, cli)
	sw.SetState(st)
	if _, err := sw.SwitchToAccount(context.Background(), "reserve", false); err == nil {
		t.Fatal("promotion should fail")
	}
	if len(patches) != 1 || patches[0] != "reserve.json:codex" {
		t.Fatalf("promotion patch sequence: %v", patches)
	}
	if got := st.Active("codex", ""); got != "active" {
		t.Fatalf("metadata reconciliation should preserve actual active, not failed target: %q", got)
	}
}

func TestFailedDemotionRollsBackAndKeepsObservedCache(t *testing.T) {
	prefixes := map[string]string{"active.json": "codex", "reserve.json": "codex_1"}
	var patches []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"active","name":"active.json","auth_index":"a","provider":"codex"},{"id":"reserve","name":"reserve.json","auth_index":"r","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			_, _ = w.Write([]byte(`{"prefix":"` + prefixes[r.URL.Query().Get("name")] + `"}`))
		case "/v0/management/api-call":
			var req client.APICallRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.AuthIndex == "a" {
				_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":95}}}`))
			} else {
				_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":10}}}`))
			}
		case "/v0/management/auth-files/fields":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			name, prefix := req["name"], req["prefix"]
			patches = append(patches, name+":"+prefix)
			if name == "active.json" {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			prefixes[name] = prefix
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()
	st := &state.State{}
	st.SetActive("codex", "", "previous-cache")
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex", FiveHourThreshold: 90}, cli)
	sw.SetState(st)
	if _, err := sw.RunProvider(context.Background(), "codex", false); err == nil {
		t.Fatal("expected demotion failure")
	}
	want := []string{"reserve.json:codex", "active.json:codex_1", "reserve.json:codex_1"}
	if strings.Join(patches, "|") != strings.Join(want, "|") {
		t.Fatalf("patch sequence %v, want %v", patches, want)
	}
	if prefixes["active.json"] != "codex" || prefixes["reserve.json"] != "codex_1" {
		t.Fatalf("rollback did not restore actual prefixes: %+v", prefixes)
	}
	if got := st.Active("codex", ""); got != "active" {
		t.Fatalf("cache does not reflect observed actual active: %q", got)
	}
}

func TestQuarantinedActiveMetadataIsRemovedFromCache(t *testing.T) {
	st := &state.State{}
	st.SetActive("codex", "", "stale")
	st.RecordProblem("codex", "active", "quota query failed")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"active","name":"active.json","auth_index":"a","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			_, _ = w.Write([]byte(`{"prefix":"codex"}`))
		case "/v0/management/api-call":
			t.Fatal("quarantined active quota must not be queried")
		}
	}))
	defer server.Close()
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex"}, cli)
	sw.SetState(st)
	if _, err := sw.RunProvider(context.Background(), "codex", false); err != nil {
		t.Fatal(err)
	}
	if got := st.Active("codex", ""); got != "" {
		t.Fatalf("quarantined active retained in cache: %q", got)
	}
}

func TestEmptyMetadataDiscoveryClearsActiveCache(t *testing.T) {
	st := &state.State{}
	st.SetActive("codex", "", "stale")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/auth-files" {
			_, _ = w.Write([]byte(`{"files":[]}`))
		}
	}))
	defer server.Close()
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex"}, cli)
	sw.SetState(st)
	if _, err := sw.RunProvider(context.Background(), "codex", false); err != nil {
		t.Fatal(err)
	}
	if got := st.Active("codex", ""); got != "" {
		t.Fatalf("stale cache retained after authoritative empty discovery: %q", got)
	}
}

func TestQuarantinedActiveEvaluatesHealthyReserves(t *testing.T) {
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"active","name":"active.json","auth_index":"a","provider":"codex"},{"id":"reserve","name":"reserve.json","auth_index":"r","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			if r.URL.Query().Get("name") == "active.json" {
				_, _ = w.Write([]byte(`{"prefix":"codex"}`))
			} else {
				_, _ = w.Write([]byte(`{"prefix":"codex_1"}`))
			}
		case "/v0/management/api-call":
			var req client.APICallRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			calls[req.AuthIndex]++
			_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":10}}}`))
		}
	}))
	defer server.Close()
	st := &state.State{}
	st.RecordProblem("codex", "active", "failed")
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex", FiveHourThreshold: 90}, cli)
	sw.SetState(st)
	if _, err := sw.RunProvider(context.Background(), "codex", false); err != nil {
		t.Fatal(err)
	}
	if calls["a"] != 0 || calls["r"] != 1 {
		t.Fatalf("quarantined active or eligible reserve quota calls = %+v", calls)
	}
}

func TestActiveManagementQuotaFailurePropagatesWithoutReserveOrQuarantine(t *testing.T) {
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"active","name":"active.json","auth_index":"a","provider":"codex"},{"id":"reserve","name":"reserve.json","auth_index":"r","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			if r.URL.Query().Get("name") == "active.json" {
				_, _ = w.Write([]byte(`{"prefix":"codex"}`))
			} else {
				_, _ = w.Write([]byte(`{"prefix":"codex_1"}`))
			}
		case "/v0/management/api-call":
			var req client.APICallRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			calls[req.AuthIndex]++
			if req.AuthIndex == "a" {
				w.WriteHeader(http.StatusServiceUnavailable)
			} else {
				_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":10}}}`))
			}
		}
	}))
	defer server.Close()
	st := &state.State{}
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex", FiveHourThreshold: 90}, cli)
	sw.SetState(st)
	if _, err := sw.RunProvider(context.Background(), "codex", false); err == nil {
		t.Fatal("active management outage must propagate")
	}
	if calls["a"] != 1 || calls["r"] != 0 {
		t.Fatalf("active/reserve calls = %+v", calls)
	}
	if len(st.ProblemSnapshot()) != 0 {
		t.Fatalf("management outage quarantined account: %+v", st.ProblemSnapshot())
	}
}

func TestRunProviderQuarantinedActiveDoesNotQueryReserve(t *testing.T) {
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"active","name":"active.json","auth_index":"a","provider":"codex"},{"id":"reserve","name":"reserve.json","auth_index":"r","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			if r.URL.Query().Get("name") == "active.json" {
				_, _ = w.Write([]byte(`{"prefix":"codex"}`))
			} else {
				_, _ = w.Write([]byte(`{"prefix":"codex_1"}`))
			}
		case "/v0/management/api-call":
			var req client.APICallRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			calls[req.AuthIndex]++
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	cli, _ := client.New(server.URL, "")
	st := &state.State{}
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex", FiveHourThreshold: 90}, cli)
	sw.SetState(st)
	if _, err := sw.RunProvider(context.Background(), "codex", false, "default"); err == nil {
		t.Fatal("management failure should be propagated")
	}
	if calls["a"] != 1 || calls["r"] != 0 {
		t.Fatalf("quota calls active/reserve = %+v", calls)
	}
}

func TestRunProviderFetchesReservesOnlyAfterActiveThreshold(t *testing.T) {
	prefixes := map[string]string{"active.json": "codex", "reserve.json": "codex_1", "other-profile.json": "codex_p1_1", "cross.json": "agy"}
	calls := map[string]int{}
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"active","name":"active.json","auth_index":"a","provider":"codex"},{"id":"reserve","name":"reserve.json","auth_index":"r","provider":"codex"},{"id":"other","name":"other-profile.json","auth_index":"o","provider":"codex"},{"id":"cross","name":"cross.json","auth_index":"x","provider":"antigravity"}]}`))
		case "/v0/management/auth-files/download":
			_, _ = w.Write([]byte(`{"prefix":"` + prefixes[r.URL.Query().Get("name")] + `"}`))
		case "/v0/management/api-call":
			var req client.APICallRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			calls[req.AuthIndex]++
			mu.Unlock()
			used := 10
			if req.AuthIndex == "a" {
				used = 20
			}
			if req.AuthIndex == "o" {
				used = 95
			}
			if req.AuthIndex == "x" {
				used = 95
			}
			if req.AuthIndex == "r" {
				used = 10
			}
			_, _ = fmt.Fprintf(w, `{"rate_limit":{"primary_window":{"used_percent":%d}}}`, used)
		case "/v0/management/auth-files/fields":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			prefixes[req["name"]] = req["prefix"]
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()
	cli, _ := client.New(server.URL, "")
	cfg := &config.Config{Endpoint: server.URL, Provider: "codex", FiveHourThreshold: 90, WeeklyThreshold: 95}
	st := &state.State{}
	sw := New(cfg, cli)
	sw.SetState(st)
	if _, err := sw.RunProvider(context.Background(), "codex", false, "default"); err != nil {
		t.Fatal(err)
	}
	if calls["a"] != 1 || calls["r"] != 0 || calls["o"] != 0 {
		t.Fatalf("below threshold quota calls = %+v", calls)
	}
	// External metadata change is authoritative on the next run; threshold now triggers reserve polling.
	prefixes["active.json"] = "codex_2"
	prefixes["reserve.json"] = "codex"
	if _, err := sw.RunProvider(context.Background(), "codex", false, "default"); err != nil {
		t.Fatal(err)
	}
	if st.Active("codex", "") != "reserve" {
		t.Fatalf("cache did not follow externally observed active: %q", st.Active("codex", ""))
	}
	if calls["r"] != 1 {
		t.Fatalf("new active quota not queried: %+v", calls)
	}
}

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
	cache := &state.State{}
	sw.SetState(cache)

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
	if got := cache.Active("antigravity", ""); got != "acc-2" {
		t.Fatalf("manual switch cache = %q", got)
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
	if got := cache.Active("antigravity", ""); got != "acc-1" {
		t.Fatalf("dry-run recorded speculative active %q", got)
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
		Endpoint:          server.URL,
		ManagementKey:     "dummy",
		Provider:          "codex",
		FiveHourThreshold: 90.0,
		WeeklyThreshold:   95.0,
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
		Endpoint:          server.URL,
		ManagementKey:     "dummy",
		Provider:          "codex",
		FiveHourThreshold: 90.0,
		WeeklyThreshold:   95.0,
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
		"def-act.json": "agy",
		"def-res.json": "agy_1",
		"p1-act.json":  "agy_p1",
		"p1-res.json":  "agy_p1_1",
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

func TestListAccountsSyncsVanishedActivesPerProvider(t *testing.T) {
	st := &state.State{}
	st.SetActive("codex", "", "vanished")
	st.SetActive("antigravity", "", "unrelated")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"reserve","name":"reserve.json","auth_index":"r","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			_, _ = w.Write([]byte(`{"prefix":"codex_1"}`))
		case "/v0/management/api-call":
			_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":10}}}`))
		}
	}))
	defer server.Close()
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex"}, cli)
	sw.SetState(st)
	if _, err := sw.ListAccounts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := st.Active("codex", ""); got != "" {
		t.Fatalf("vanished active remained cached: %q", got)
	}
	if got := st.Active("antigravity", ""); got != "unrelated" {
		t.Fatalf("listing codex cleared unrelated provider cache: %q", got)
	}
}

func TestManualPromotionRejectsProblemWithError(t *testing.T) {
	st := &state.State{}
	st.RecordProblem("codex", "reserve", "quota failed")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"active","name":"active.json","provider":"codex"},{"id":"reserve","name":"reserve.json","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			prefix := "codex"
			if r.URL.Query().Get("name") == "reserve.json" {
				prefix = "codex_1"
			}
			_, _ = fmt.Fprintf(w, `{"prefix":%q}`, prefix)
		}
	}))
	defer server.Close()
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex"}, cli)
	sw.SetState(st)
	res, err := sw.SwitchToAccount(context.Background(), "reserve", false)
	if err == nil || res != nil {
		t.Fatalf("problem-account manual promotion must safely fail: result=%+v err=%v", res, err)
	}
}

func TestQuarantinedActiveDryRunDoesNotSpeculateOrPatch(t *testing.T) {
	patches := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"active","name":"active.json","auth_index":"a","provider":"codex"},{"id":"reserve","name":"reserve.json","auth_index":"r","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			prefix := "codex"
			if r.URL.Query().Get("name") == "reserve.json" {
				prefix = "codex_1"
			}
			_, _ = fmt.Fprintf(w, `{"prefix":%q}`, prefix)
		case "/v0/management/api-call":
			_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":10}}}`))
		case "/v0/management/auth-files/fields":
			patches++
		}
	}))
	defer server.Close()
	st := &state.State{}
	st.RecordProblem("codex", "active", "quota failed")
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex"}, cli)
	sw.SetState(st)
	res, err := sw.RunProvider(context.Background(), "codex", true)
	if err != nil || res.Rotated {
		t.Fatalf("quarantined dry-run must not claim promotion: result=%+v err=%v", res, err)
	}
	if patches != 0 || st.Active("codex", "") != "" {
		t.Fatalf("dry-run patched or cached speculative active: patches=%d active=%q", patches, st.Active("codex", ""))
	}
}

func TestQuarantinedActiveWithoutUsableReserveStaysUncached(t *testing.T) {
	for _, candidate := range []struct {
		name     string
		files    string
		quota    string
		disabled bool
	}{
		{name: "no reserve", files: `{"files":[{"id":"active","name":"active.json","auth_index":"a","provider":"codex"}]}`},
		{name: "all unhealthy", files: `{"files":[{"id":"active","name":"active.json","auth_index":"a","provider":"codex"},{"id":"reserve","name":"reserve.json","auth_index":"r","provider":"codex"}]}`, quota: "bad"},
		{name: "disabled candidate", files: `{"files":[{"id":"active","name":"active.json","auth_index":"a","provider":"codex"},{"id":"reserve","name":"reserve.json","auth_index":"r","provider":"codex","disabled":true}]}`},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v0/management/auth-files":
					_, _ = w.Write([]byte(candidate.files))
				case "/v0/management/auth-files/download":
					prefix := "codex"
					if r.URL.Query().Get("name") == "reserve.json" {
						prefix = "codex_1"
					}
					_, _ = fmt.Fprintf(w, `{"prefix":%q}`, prefix)
				case "/v0/management/api-call":
					if candidate.quota == "bad" {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":10}}}`))
				}
			}))
			defer server.Close()
			st := &state.State{}
			st.RecordProblem("codex", "active", "quota failed")
			cli, _ := client.New(server.URL, "")
			sw := New(&config.Config{Endpoint: server.URL, Provider: "codex"}, cli)
			sw.SetState(st)
			res, err := sw.RunProvider(context.Background(), "codex", false)
			if err != nil || res.Rotated || res.ActiveAccount != "" || st.Active("codex", "") != "" {
				t.Fatalf("unsafe no-candidate result=%+v err=%v cached=%q", res, err, st.Active("codex", ""))
			}
		})
	}
}

func TestQuarantinedActiveCannotUseAnotherProfileReserve(t *testing.T) {
	patches := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_, _ = w.Write([]byte(`{"files":[{"id":"active","name":"active.json","auth_index":"a","provider":"codex"},{"id":"other-profile","name":"other.json","auth_index":"r","provider":"codex"}]}`))
		case "/v0/management/auth-files/download":
			prefix := "codex"
			if r.URL.Query().Get("name") == "other.json" {
				prefix = "codex_p1_1"
			}
			_, _ = fmt.Fprintf(w, `{"prefix":%q}`, prefix)
		case "/v0/management/api-call":
			_, _ = w.Write([]byte(`{"groups":[{"buckets":[{"displayName":"5-Hour Limit","remainingFraction":0.9}]}]}`))
		case "/v0/management/auth-files/fields":
			patches++
		}
	}))
	defer server.Close()
	st := &state.State{}
	st.RecordProblem("codex", "active", "quota failed")
	cli, _ := client.New(server.URL, "")
	sw := New(&config.Config{Endpoint: server.URL, Provider: "codex"}, cli)
	sw.SetState(st)
	res, err := sw.RunProvider(context.Background(), "codex", false)
	if err != nil || res.Rotated || res.ActiveAccount != "" || patches != 0 {
		t.Fatalf("cross-profile reserve was used: result=%+v err=%v patches=%d", res, err, patches)
	}
}

func TestQuarantinedActiveSwapFailuresKeepCacheEmpty(t *testing.T) {
	for _, failName := range []string{"reserve.json", "active.json"} {
		t.Run(failName, func(t *testing.T) {
			prefixes := map[string]string{"active.json": "agy", "reserve.json": "agy_1"}
			var patches []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v0/management/auth-files":
					_, _ = w.Write([]byte(`{"files":[{"id":"active","name":"active.json","auth_index":"a","provider":"antigravity"},{"id":"reserve","name":"reserve.json","auth_index":"r","provider":"antigravity"}]}`))
				case "/v0/management/auth-files/download":
					_, _ = fmt.Fprintf(w, `{"prefix":%q}`, prefixes[r.URL.Query().Get("name")])
				case "/v0/management/api-call":
					_, _ = w.Write([]byte(`{"groups":[{"buckets":[{"displayName":"5-Hour Limit","remainingFraction":0.9}]}]}`))
				case "/v0/management/auth-files/fields":
					if r.Method != http.MethodPatch {
						t.Errorf("patch method = %s", r.Method)
					}
					var req map[string]string
					_ = json.NewDecoder(r.Body).Decode(&req)
					name := req["name"]
					patches = append(patches, name+":"+req["prefix"])
					if name == failName {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					prefixes[name] = req["prefix"]
					_, _ = w.Write([]byte(`{}`))
				}
			}))
			defer server.Close()
			st := &state.State{}
			st.RecordProblem("antigravity", "active", "quota failed")
			cli, _ := client.New(server.URL, "")
			sw := New(&config.Config{Endpoint: server.URL, Provider: "antigravity", FiveHourThreshold: 90, WeeklyThreshold: 95}, cli)
			sw.SetState(st)
			if res, err := sw.RunProvider(context.Background(), "antigravity", false); err == nil {
				t.Fatalf("quarantined active swap failure should propagate: result=%+v patches=%v", res, patches)
			}
			if got := st.Active("antigravity", ""); got != "" {
				t.Fatalf("failed swap cached speculative replacement %q", got)
			}
			if failName == "reserve.json" && len(patches) != 1 {
				t.Fatalf("promotion failure should not demote active: patches=%v", patches)
			}
			if failName == "active.json" {
				want := []string{"reserve.json:agy", "active.json:agy_1", "reserve.json:agy_1"}
				if strings.Join(patches, "|") != strings.Join(want, "|") {
					t.Fatalf("failed demotion rollback patches=%v want=%v", patches, want)
				}
				if prefixes["reserve.json"] != "agy_1" {
					t.Fatalf("failed swap was not rolled back: %+v", prefixes)
				}
			}
		})
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
