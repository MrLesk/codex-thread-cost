package main

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testID = "01900000-0000-7000-8000-000000000001"
const otherID = "01900000-0000-7000-8000-000000000002"

func rates(t *testing.T) map[string]Rate {
	t.Helper()
	var c struct {
		Models map[string]Rate `json:"models"`
	}
	if err := json.Unmarshal(bundledRates, &c); err != nil {
		t.Fatal(err)
	}
	if err := validateRates(c.Models); err != nil {
		t.Fatal(err)
	}
	return c.Models
}
func TestPrices(t *testing.T) {
	catalog := rates(t)
	for _, tc := range []struct {
		name, model, tier, want string
		usage                   Tokens
	}{
		{"caching and output", "gpt-6.1-sol", "standard", "0.00217", Tokens{1000, 200, 100, 50}},
		{"long fast", "gpt-6.1-sol", "fast", "1.69", Tokens{300000, 100000, 10000, 1000}},
		{"threshold", "gpt-6.1-sol", "standard", "0.554", Tokens{272000, 0, 0, 1000}},
		{"dated model", "gpt-6.1-sol-2026-10-01", "standard", "0.00217", Tokens{1000, 200, 100, 50}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := requestCost(Request{Usage: tc.usage, Model: tc.model, Tier: tc.tier}, catalog)
			want, _ := new(big.Rat).SetString(tc.want)
			if err != nil || got.Cmp(want) != 0 {
				t.Fatalf("got %v, %v; want %s", got, err, tc.want)
			}
		})
	}
	for _, q := range []Request{{Model: "unknown", Tier: "standard"}, {Model: "gpt-6.1-sol", Tier: "ultrafast"}, {Model: "gpt-5.3-codex", Tier: "standard", Usage: Tokens{1, 0, 1, 0}}} {
		if _, err := requestCost(q, catalog); err == nil {
			t.Fatal("unknown price accepted")
		}
	}
}
func TestMoneyLabels(t *testing.T) {
	for _, tc := range []struct {
		amount    string
		precision int
		want      string
	}{{"16.15", 2, "[~$16.15]"}, {"1.245", 2, "[~$1.25]"}, {"0.00217", 2, "[~$0.00]"}, {"1.5", 0, "[~$2]"}, {"0", 2, "[~$0.00]"}} {
		if got := label(tc.amount, tc.precision); got != tc.want {
			t.Errorf("got %s want %s", got, tc.want)
		}
	}
}
func TestRecordedTiers(t *testing.T) {
	c := defaults()
	c.DefaultTier = "fast"
	got := estimate("testdata/tiers.jsonl", testID, c)
	if got.Amount == nil || *got.Amount != "0.008" || len(got.Notes) != 0 {
		t.Fatalf("unexpected report %+v", got)
	}
	for name, snapshot := range map[string]map[string]any{
		"foreign":        {"thread_id": otherID, "thread_settings": map[string]any{}},
		"ownerless":      {"thread_settings": map[string]any{}},
		"explicit reset": {"thread_id": testID, "thread_settings": map[string]any{"service_tier": nil}},
	} {
		t.Run(name, func(t *testing.T) {
			snapshot["type"] = "thread_settings_applied"
			rows := base("priority")
			rows = append(rows, event("event_msg", snapshot), event("turn_context", map[string]any{"model": "gpt-6.1-sol"}), record("r1", 1000, 1000))
			r := estimate(transcript(t, rows), testID, c)
			want := "0.004"
			if name == "explicit reset" {
				want = "0.002"
			}
			if r.Amount == nil || *r.Amount != want {
				t.Fatalf("got %+v want %s", r, want)
			}
		})
	}
	rows := base("")
	rows = append(rows[:1], rows[2:]...)
	rows = append(rows, record("r1", 1000, 1000))
	r := estimate(transcript(t, rows), testID, c)
	if r.Amount == nil || *r.Amount != "0.004" || len(r.Notes) == 0 {
		t.Fatalf("fallback failed: %+v", r)
	}
}
func event(kind string, p map[string]any) map[string]any {
	return map[string]any{"type": kind, "payload": p}
}
func tokens(n int64) map[string]any {
	return map[string]any{"input_tokens": n, "cached_input_tokens": 0, "cache_write_input_tokens": 0, "output_tokens": 0, "reasoning_output_tokens": 0}
}
func base(tier string) []map[string]any {
	settings := map[string]any{"model": "gpt-6.1-sol", "model_provider_id": "openai"}
	if tier != "" {
		settings["service_tier"] = tier
	}
	return []map[string]any{event("session_meta", map[string]any{"id": testID, "model_provider": "openai"}), event("event_msg", map[string]any{"type": "thread_settings_applied", "thread_id": testID, "thread_settings": settings}), event("turn_context", map[string]any{"model": "gpt-6.1-sol"})}
}
func record(id string, n, total int64) map[string]any {
	return event("token_usage_record", map[string]any{"thread_id": testID, "response_id": id, "usage": tokens(n), "thread_token_usage": tokens(total)})
}
func legacy(n, last int64) map[string]any {
	return event("event_msg", map[string]any{"type": "token_count", "info": map[string]any{"total_token_usage": tokens(n), "last_token_usage": tokens(last)}})
}
func transcript(t *testing.T, rows []map[string]any) string {
	t.Helper()
	var b strings.Builder
	for _, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestUsageTotals(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows []map[string]any
		want string
	}{
		{"one response", []map[string]any{record("r1", 1000, 1000)}, "0.002"},
		{"duplicates", []map[string]any{record("r1", 1000, 1000), record("r1", 1000, 1000)}, "0.002"},
		{"old replay", []map[string]any{record("r1", 1000, 1000), record("r2", 1000, 2000), record("r1", 1000, 1000)}, "0.004"},
		{"duplicate conflict", []map[string]any{record("r1", 1000, 1000), record("r1", 1000, 2000)}, ""},
		{"missing history", []map[string]any{record("r1", 1000, 2000)}, ""},
		{"intermediate conflict", []map[string]any{record("r1", 1000, 3000), record("r2", 1000, 2000)}, ""},
		{"modern and legacy", []map[string]any{record("r1", 1000, 1000), legacy(1000, 1000), legacy(1000, 1000)}, "0.002"},
		{"later legacy", []map[string]any{record("r1", 1000, 1000), legacy(2000, 1000)}, ""},
		{"legacy reset cannot hide usage", []map[string]any{record("r1", 1000, 1000), legacy(2000, 1000), legacy(1000, 1000)}, ""},
		{"legacy duplicates", []map[string]any{legacy(1000, 1000), legacy(1000, 1000)}, "0.002"},
		{"legacy reset", []map[string]any{legacy(1000, 1000), legacy(900, 900)}, ""},
		{"legacy missing request", []map[string]any{legacy(2000, 1000)}, ""},
		{"no usage", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := append(base(""), tc.rows...)
			r := estimate(transcript(t, rows), testID, defaults())
			if tc.want == "" {
				if r.Amount != nil {
					t.Fatalf("accepted incomplete usage: %+v", r)
				}
			} else if r.Amount == nil || *r.Amount != tc.want {
				t.Fatalf("got %+v want %s", r, tc.want)
			}
		})
	}
}
func TestInvalidUsage(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"input_tokens":true}`, `{"input_tokens":-1,"cached_input_tokens":0,"output_tokens":0}`, `{"input_tokens":10,"cached_input_tokens":9,"cache_write_input_tokens":9,"output_tokens":0}`} {
		if _, err := counts(json.RawMessage(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	path := transcript(t, append(base(""), record("r1", 1000, 1000)))
	if r := estimate(path, otherID, defaults()); r.Amount != nil {
		t.Fatal("accepted another chat")
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString(`{"type":`)
	f.Close()
	if r := estimate(path, testID, defaults()); r.Amount != nil {
		t.Fatal("accepted partial record")
	}
}
func TestTitleBehavior(t *testing.T) {
	for _, position := range []string{"prefix", "suffix"} {
		t.Run(position, func(t *testing.T) {
			c := defaults()
			c.Position = position
			amount := "2.00"
			baseTitle := "修正 café"
			if position == "suffix" {
				baseTitle = "[~$2.00] " + baseTitle
			} else {
				baseTitle += " [~$2.00]"
			}
			old, owned := planTitle(baseTitle, State{}, Report{Amount: &amount}, c)
			state := State{Owned: []Owned{owned}}
			changed := "3.00"
			next, _ := planTitle(old, state, Report{Amount: &changed}, c)
			want := baseTitle + " [~$3.00]"
			if position == "prefix" {
				want = "[~$3.00] " + baseTitle
			}
			if next != want {
				t.Fatalf("got %s want %s", next, want)
			}
			if got, _ := planTitle(old, state, Report{}, c); got != old {
				t.Fatal("unavailable estimate changed price")
			}
			if got, _ := planTitle(baseTitle, State{}, Report{}, c); got != baseTitle {
				t.Fatal("unavailable estimate added a label")
			}
			if got := removeOwned(strings.ReplaceAll(old, "café", "budget"), state); got != strings.ReplaceAll(baseTitle, "café", "budget") {
				t.Fatal("user title text changed")
			}
		})
	}
}

type fakeMetadata struct {
	thread Thread
	writes int
}

func (f *fakeMetadata) Read(string) (Thread, error)       { return f.thread, nil }
func (f *fakeMetadata) Children(string) ([]Thread, error) { return nil, nil }
func (f *fakeMetadata) Rename(_ string, name string) error {
	f.writes++
	f.thread.Name = name
	return nil
}
func TestUnavailablePriceAndRecovery(t *testing.T) {
	t.Setenv("THREAD_COST_HOME", t.TempDir())
	c := defaults()
	client := &fakeMetadata{thread: Thread{Name: "Budget [$20]"}}
	valid := transcript(t, append(base(""), record("r1", 100000, 100000)))
	first, err := syncWith(client, testID, valid, c, false)
	if err != nil {
		t.Fatal(err)
	}
	missing := transcript(t, base(""))
	before := client.writes
	second, err := syncWith(client, testID, missing, c, false)
	if err != nil {
		t.Fatal(err)
	}
	if second.Title != first.Title || client.writes != before {
		t.Fatal("unavailable estimate changed title")
	}
	c.Position = "prefix"
	third, err := syncWith(client, testID, valid, c, false)
	if err != nil {
		t.Fatal(err)
	}
	if third.Title != "[~$0.20] Budget [$20]" {
		t.Fatalf("got %s", third.Title)
	}
}
func TestOldTitleState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	raw := `{"base_title":"~$2.00 Budget","last_label":"~$2.00","owned_labels":["~$2.00"],"last_title":"~$2.00 Budget ~$2.00"}`
	os.WriteFile(path, []byte(raw), 0600)
	state, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := removeOwned("~$2.00 Budget ~$2.00", state); got != "~$2.00 Budget" {
		t.Fatalf("got %s", got)
	}
}

func TestTierChangesTakeEffectOnNextTurn(t *testing.T) {
	for _, tc := range []struct{ before, after, want string }{
		{"standard", "priority", "0.010"},
		{"priority", "standard", "0.014"},
	} {
		t.Run(tc.before+" to "+tc.after, func(t *testing.T) {
			rows := base(tc.before)
			rows = rows[:2]
			context := func(turn string) map[string]any {
				return event("turn_context", map[string]any{"turn_id": turn, "model": "gpt-6.1-sol"})
			}
			usage := func(turn, response string, total int64) map[string]any {
				r := record(response, 1000, total)
				r["payload"].(map[string]any)["turn_id"] = turn
				return r
			}
			rows = append(rows, event("event_msg", map[string]any{"type": "task_started", "turn_id": "A"}))
			// Change the persistent tier before the first request finishes (and
			// even before its context is written). The active turn keeps its tier.
			rows = append(rows, base(tc.after)[1], context("A"), usage("A", "r1", 1000), legacy(1000, 1000))
			rows = append(rows, context("A"), usage("A", "r2", 2000), legacy(2000, 1000))
			rows = append(rows, context("B"), usage("B", "r3", 3000), legacy(3000, 1000))
			// A delayed response and replay for A must retain A's attribution.
			rows = append(rows, usage("A", "r4", 4000), usage("A", "r1", 1000))
			r := estimate(transcript(t, rows), testID, defaults())
			want, _ := new(big.Rat).SetString(tc.want)
			if r.Amount == nil {
				t.Fatalf("unavailable: %+v", r)
			}
			got, _ := new(big.Rat).SetString(*r.Amount)
			if got.Cmp(want) != 0 || r.Requests != 4 {
				t.Fatalf("got %+v, want %s for four requests", r, tc.want)
			}
			// Legacy-only histories use the same frozen turn settings.
			legacyRows := rows[:0:0]
			for _, row := range rows {
				if row["type"] != "token_usage_record" {
					legacyRows = append(legacyRows, row)
				}
			}
			r = estimate(transcript(t, legacyRows), testID, defaults())
			legacyWant := "0.008"
			if tc.before == "priority" {
				legacyWant = "0.01"
			}
			if r.Amount == nil || *r.Amount != legacyWant {
				t.Fatalf("wrong legacy tier attribution: %+v", r)
			}
		})
	}
}

func TestLegacyPrefixAndModernUsage(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rows     []map[string]any
		want     string
		requests int
	}{
		{"upgrade", []map[string]any{legacy(1000, 1000), record("r1", 1000, 1000), legacy(2000, 1000)}, "0.004", 2},
		{"later turns", []map[string]any{legacy(1000, 1000), record("r1", 1000, 1000), legacy(2000, 1000), record("r2", 2000, 3000), legacy(4000, 2000)}, "0.008", 3},
		{"pending legacy checkpoint", []map[string]any{legacy(1000, 1000), record("r1", 1000, 1000)}, "", 0},
		{"stale initial checkpoint", []map[string]any{legacy(1000, 1000), record("r1", 1000, 1000), legacy(1000, 1000)}, "", 0},
		{"delayed initial checkpoint", []map[string]any{legacy(1000, 1000), record("r1", 1000, 1000), record("r2", 1000, 2000), legacy(2000, 1000)}, "", 0},
		{"modern ahead after verified transition", []map[string]any{legacy(1000, 1000), record("r1", 1000, 1000), legacy(2000, 1000), record("r2", 1000, 2000)}, "0.006", 3},
		{"stale repeated checkpoint", []map[string]any{legacy(1000, 1000), record("r1", 1000, 1000), legacy(2000, 1000), record("r2", 1000, 2000), legacy(2000, 1000)}, "0.006", 3},
		{"duplicate mirrors", []map[string]any{legacy(1000, 1000), record("r1", 1000, 1000), legacy(2000, 1000), record("r1", 1000, 1000), legacy(2000, 1000)}, "0.004", 2},
		{"missing legacy prefix", []map[string]any{legacy(2000, 1000), record("r1", 1000, 1000), legacy(3000, 1000)}, "", 0},
		{"unmatched offset", []map[string]any{legacy(1000, 1000), record("r1", 1000, 1000), legacy(2500, 1500)}, "", 0},
		{"missing modern request", []map[string]any{legacy(1000, 1000), record("r1", 1000, 2000), legacy(3000, 1000)}, "", 0},
		{"different request boundaries", []map[string]any{legacy(1000, 1000), record("r1", 1000, 1000), legacy(1500, 500), record("r2", 1000, 2000), legacy(3000, 1500)}, "", 0},
		{"legacy reset", []map[string]any{legacy(1000, 1000), record("r1", 1000, 1000), legacy(2000, 1000), legacy(1000, 1000)}, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := estimate(transcript(t, append(base("standard"), tc.rows...)), testID, defaults())
			if tc.want == "" {
				if r.Amount != nil {
					t.Fatalf("accepted incomplete history: %+v", r)
				}
			} else if r.Amount == nil || *r.Amount != tc.want || r.Requests != tc.requests {
				t.Fatalf("got %+v; want %s, %d requests", r, tc.want, tc.requests)
			}
		})
	}
	// Preserve the model and tier of the old segment, without charging its
	// mirrored modern usage twice or treating its cumulative total as a request.
	rows := append(base("standard"), legacy(1000, 1000), legacy(2000, 1000))
	rows = append(rows, base("priority")[1], event("turn_context", map[string]any{"model": "gpt-6-astra"}), record("r1", 1000, 1000), legacy(3000, 1000))
	r := estimate(transcript(t, rows), testID, defaults())
	if r.Amount == nil || *r.Amount != "0.024" || r.Requests != 3 {
		t.Fatalf("lost historical rates: %+v", r)
	}
	// Reconcile all four token categories, not only input totals.
	old, mirror, modern := legacy(1000, 1000), legacy(2000, 1000), record("r1", 1000, 1000)
	usage := map[string]any{"input_tokens": 1000, "cached_input_tokens": 200, "cache_write_input_tokens": 100, "output_tokens": 50}
	old["payload"].(map[string]any)["info"] = map[string]any{"total_token_usage": usage, "last_token_usage": usage}
	modern["payload"].(map[string]any)["usage"] = usage
	modern["payload"].(map[string]any)["thread_token_usage"] = usage
	mirror["payload"].(map[string]any)["info"] = map[string]any{"total_token_usage": map[string]any{"input_tokens": 2000, "cached_input_tokens": 400, "cache_write_input_tokens": 200, "output_tokens": 100}, "last_token_usage": usage}
	r = estimate(transcript(t, append(base("standard"), old, modern, mirror)), testID, defaults())
	if r.Amount == nil || *r.Amount != "0.00434" || r.Requests != 2 {
		t.Fatalf("lost cached, written, or output tokens: %+v", r)
	}
}

func TestCustomRatesRoundOnlyAtDisplay(t *testing.T) {
	for _, tc := range []struct {
		rate      string
		precision int
		want      string
	}{
		{"0.0049999999996", 2, "[~$0.00]"},
		{"0.005", 2, "[~$0.01]"},
		{"0.000000499999999999999999", 6, "[~$0.000000]"},
		{"0.4999999999996", 0, "[~$0]"},
	} {
		c := defaults()
		c.Precision = tc.precision
		c.Rates["gpt-6.1-sol"] = Rate{Input: json.Number(tc.rate), Cached: "0", Output: "0", Tiers: map[string]json.Number{"standard": "1"}}
		r := estimate(transcript(t, append(base("standard"), record("r1", 1000000, 1000000))), testID, c)
		if r.Amount == nil || *r.Amount != tc.rate || r.ByModel["gpt-6.1-sol/standard"] != tc.rate {
			t.Fatalf("lost exact amount: %+v", r)
		}
		// The saved report must retain precision too.
		data, _ := json.Marshal(r)
		var saved Report
		if err := json.Unmarshal(data, &saved); err != nil {
			t.Fatal(err)
		}
		got, _ := planTitle("Budget", State{}, saved, c)
		if got != "Budget "+tc.want {
			t.Fatalf("got %s, want %s", got, tc.want)
		}
	}
}
