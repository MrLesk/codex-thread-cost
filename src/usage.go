package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"regexp"
)

//go:embed rates.json
var bundledRates []byte

type Rate struct {
	Input      json.Number            `json:"input"`
	Cached     json.Number            `json:"cached"`
	Write      *json.Number           `json:"cache_write"`
	Output     json.Number            `json:"output"`
	Tiers      map[string]json.Number `json:"tiers"`
	LongAbove  *int64                 `json:"long_context_above"`
	LongInput  *json.Number           `json:"long_input_multiplier"`
	LongOutput *json.Number           `json:"long_output_multiplier"`
}
type Report struct {
	Currency string            `json:"currency"`
	Amount   *string           `json:"amount"`
	Requests int               `json:"requests"`
	Notes    []string          `json:"notes"`
	ByModel  map[string]string `json:"by_model,omitempty"`
}
type Tokens [4]int64

var tokenFields = []string{"input_tokens", "cached_input_tokens", "cache_write_input_tokens", "output_tokens"}
var datedModel = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)

type Record struct {
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp string          `json:"timestamp"`
}
type Payload struct {
	Type       string          `json:"type"`
	ID         string          `json:"id"`
	ThreadID   string          `json:"thread_id"`
	ForkedFrom string          `json:"forked_from_id"`
	Timestamp  string          `json:"timestamp"`
	TurnID     string          `json:"turn_id"`
	Provider   string          `json:"model_provider"`
	Source     json.RawMessage `json:"source"`
	Model      string          `json:"model"`
	ResponseID string          `json:"response_id"`
	Usage      json.RawMessage `json:"usage"`
	Total      json.RawMessage `json:"thread_token_usage"`
	Settings   *struct {
		Model    string  `json:"model"`
		Tier     *string `json:"service_tier"`
		Provider string  `json:"model_provider_id"`
	} `json:"thread_settings"`
	Info *struct {
		Total json.RawMessage `json:"total_token_usage"`
		Last  json.RawMessage `json:"last_token_usage"`
	} `json:"info"`
}
type Request struct {
	Usage       Tokens
	Model, Tier string
	Total       Tokens
}

func counts(raw json.RawMessage) (Tokens, error) {
	var result Tokens
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return result, fmt.Errorf("missing token usage")
	}
	for i, name := range tokenFields {
		value, ok := fields[name]
		if !ok && i == 2 {
			continue
		}
		if !ok || bytes.Equal(value, []byte("null")) || json.Unmarshal(value, &result[i]) != nil || result[i] < 0 {
			return result, fmt.Errorf("invalid token count: %s", name)
		}
	}
	if result[1] > result[0] || result[2] > result[0]-result[1] {
		return result, fmt.Errorf("cached input exceeds input")
	}
	return result, nil
}
func plus(a, b Tokens) (Tokens, error) {
	for i := range a {
		if b[i] > int64(^uint64(0)>>1)-a[i] {
			return a, fmt.Errorf("token total overflow")
		}
		a[i] += b[i]
	}
	return a, nil
}
func price(n json.Number) (*big.Rat, error) {
	r, ok := new(big.Rat).SetString(string(n))
	if !ok || r.Sign() < 0 {
		return nil, fmt.Errorf("prices must be finite nonnegative numbers")
	}
	return r, nil
}
func validateRates(rates map[string]Rate) error {
	for model, r := range rates {
		values := []json.Number{r.Input, r.Cached, r.Output}
		for _, n := range []*json.Number{r.Write, r.LongInput, r.LongOutput} {
			if n != nil {
				values = append(values, *n)
			}
		}
		if model == "" || len(r.Tiers) == 0 || (r.LongAbove != nil && *r.LongAbove < 0) {
			return fmt.Errorf("invalid rates for %s", model)
		}
		for tier, n := range r.Tiers {
			if tier == "" {
				return fmt.Errorf("invalid tier for %s", model)
			}
			values = append(values, n)
		}
		for _, n := range values {
			if _, err := price(n); err != nil {
				return fmt.Errorf("invalid rates for %s: %w", model, err)
			}
		}
	}
	return nil
}
func requestCost(q Request, rates map[string]Rate) (*big.Rat, error) {
	r, ok := rates[q.Model]
	if !ok {
		r, ok = rates[datedModel.ReplaceAllString(q.Model, "")]
	}
	if !ok {
		return nil, fmt.Errorf("no price for model %s", q.Model)
	}
	tier, ok := r.Tiers[q.Tier]
	if !ok {
		return nil, fmt.Errorf("no price for tier %s on %s", q.Tier, q.Model)
	}
	if q.Usage[2] > 0 && r.Write == nil {
		return nil, fmt.Errorf("no cache-write price for %s", q.Model)
	}
	mul := func(n int64, p json.Number) *big.Rat {
		v, _ := price(p)
		return new(big.Rat).Mul(new(big.Rat).SetInt64(n), v)
	}
	input := mul(q.Usage[0]-q.Usage[1]-q.Usage[2], r.Input)
	input.Add(input, mul(q.Usage[1], r.Cached))
	if r.Write != nil {
		input.Add(input, mul(q.Usage[2], *r.Write))
	}
	output := mul(q.Usage[3], r.Output)
	if r.LongAbove != nil && q.Usage[0] > *r.LongAbove {
		if r.LongInput != nil {
			m, _ := price(*r.LongInput)
			input.Mul(input, m)
		}
		if r.LongOutput != nil {
			m, _ := price(*r.LongOutput)
			output.Mul(output, m)
		}
	}
	result := new(big.Rat).Add(input, output)
	m, _ := price(tier)
	return result.Quo(result.Mul(result, m), big.NewRat(1000000, 1)), nil
}
func readUsage(path, id, fallback string) ([]Request, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	reader := bufio.NewReader(f)
	var modern, legacy []Request
	seen := map[string]Request{}
	var sum, previous, legacyMax Tokens
	var inherited, baseline Tokens
	scoped, parent := false, ""
	forkTime := ""
	var pending, active struct{ Model, Tier string }
	turns := map[string]struct{ Model, Tier string }{}
	var activeID string
	hasActive := false
	prefixLimit := 0
	var anchorTotal Tokens
	anchorCount := 0
	identity, invalidLegacy, usedFallback := false, false, false
	for {
		line, err := reader.ReadBytes('\n')
		if err == io.EOF && len(line) == 0 {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("incomplete transcript; retry after the turn")
		}
		var row Record
		if json.Unmarshal(line, &row) != nil {
			return nil, nil, fmt.Errorf("invalid transcript record")
		}
		if row.Type != "session_meta" && row.Type != "turn_context" && row.Type != "token_usage_record" && row.Type != "event_msg" {
			continue
		}
		var p Payload
		if json.Unmarshal(row.Payload, &p) != nil {
			return nil, nil, fmt.Errorf("invalid usage metadata")
		}
		switch {
		case row.Type == "session_meta":
			owner := p.ID
			if owner == "" {
				owner = p.ThreadID
			}
			if owner != id {
				return nil, nil, fmt.Errorf("transcript belongs to another chat")
			}
			identity = true
			if p.ForkedFrom != "" {
				scoped, parent = true, p.ForkedFrom
				forkTime = p.Timestamp
			}
			var source map[string]json.RawMessage
			if json.Unmarshal(p.Source, &source) == nil {
				if raw, ok := source["subagent"]; ok {
					var child struct {
						Spawn *struct {
							Parent string `json:"parent_thread_id"`
						} `json:"thread_spawn"`
					}
					if json.Unmarshal(raw, &child) != nil || child.Spawn == nil || child.Spawn.Parent == "" {
						return nil, nil, fmt.Errorf("internal subagent chat skipped")
					}
					scoped, parent = true, child.Spawn.Parent
				}
				if _, ok := source["internal"]; ok {
					return nil, nil, fmt.Errorf("internal chat skipped")
				}
			}
			if p.Provider != "" && p.Provider != "openai" {
				return nil, nil, fmt.Errorf("unsupported provider")
			}
		case row.Type == "turn_context":
			if saved, ok := turns[p.TurnID]; p.TurnID != "" && ok {
				active = saved
			} else {
				active = pending
			}
			active.Model = p.Model
			activeID, hasActive = p.TurnID, true
			if activeID != "" {
				turns[activeID] = active
			}
		case row.Type == "event_msg" && p.Type == "task_started":
			active = pending
			if saved, ok := turns[p.TurnID]; p.TurnID != "" && ok {
				active = saved
			}
			activeID, hasActive = p.TurnID, true
			if activeID != "" {
				turns[activeID] = active
			}
		case row.Type == "event_msg" && p.Type == "thread_settings_applied":
			if p.ThreadID != id {
				continue
			}
			if p.Settings == nil {
				return nil, nil, fmt.Errorf("invalid settings snapshot")
			}
			if p.Settings.Provider != "" && p.Settings.Provider != "openai" {
				return nil, nil, fmt.Errorf("unsupported provider")
			}
			pending.Model = p.Settings.Model
			pending.Tier = "standard"
			if p.Settings.Tier != nil {
				pending.Tier = *p.Settings.Tier
				if pending.Tier == "" {
					return nil, nil, fmt.Errorf("invalid service tier")
				}
			}
			if pending.Tier == "priority" {
				pending.Tier = "fast"
			}
			if pending.Tier == "default" {
				pending.Tier = "standard"
			}
		case row.Type == "token_usage_record":
			if p.ThreadID != id {
				if scoped && len(modern) == 0 && p.ThreadID == parent {
					var err error
					inherited, err = counts(p.Total)
					if err != nil {
						return nil, nil, err
					}
				}
				continue
			}
			if p.ResponseID == "" {
				return nil, nil, fmt.Errorf("usage has no response ID")
			}
			u, err := counts(p.Usage)
			if err != nil {
				return nil, nil, err
			}
			total, err := counts(p.Total)
			if err != nil {
				return nil, nil, err
			}
			settings := active
			if !hasActive {
				return nil, nil, fmt.Errorf("missing turn context")
			}
			if p.TurnID != "" {
				var ok bool
				settings, ok = turns[p.TurnID]
				if !ok {
					return nil, nil, fmt.Errorf("missing context for usage turn")
				}
			}
			q := Request{u, settings.Model, settings.Tier, total}
			if p.Model != "" {
				q.Model = p.Model
			}
			if q.Tier == "" {
				q.Tier = fallback
				usedFallback = true
			}
			if old, ok := seen[p.ResponseID]; ok {
				if old != q {
					return nil, nil, fmt.Errorf("conflicting duplicate response")
				}
				continue
			}
			sum, err = plus(sum, u)
			if err != nil {
				return nil, nil, err
			}
			if scoped && len(modern) == 0 && sum != total {
				baseline = inherited
				if baseline == (Tokens{}) && forkTime != "" {
					for i := range baseline {
						baseline[i] = total[i] - sum[i]
						if baseline[i] < 0 {
							return nil, nil, fmt.Errorf("invalid inherited usage total")
						}
					}
					ok, err := verifiedForkBaseline(parent, baseline, forkTime)
					if err != nil || !ok {
						return nil, nil, fmt.Errorf("cannot verify inherited usage total")
					}
				}
			}
			want, err := plus(baseline, sum)
			if err != nil || want != total {
				return nil, nil, fmt.Errorf("incomplete recorded usage total")
			}
			seen[p.ResponseID] = q
			if len(modern) == 0 {
				prefixLimit = len(legacy)
			}
			modern = append(modern, q)
		case row.Type == "event_msg" && p.Type == "token_count" && p.Info != nil:
			// Forks can copy legacy counters without ownership information.
			// Only their own modern records can prove newly incurred usage.
			if scoped {
				continue
			}
			total, err := counts(p.Info.Total)
			if err != nil {
				return nil, nil, err
			}
			var delta Tokens
			changed, reset := false, false
			for i := range total {
				delta[i] = total[i] - previous[i]
				changed = changed || delta[i] != 0
				reset = reset || delta[i] < 0
				if total[i] > legacyMax[i] {
					legacyMax[i] = total[i]
				}
			}
			// Keep the first advancing checkpoint: repeated token_count events
			// can still describe an earlier response while a newer one is saved.
			if len(modern) > 0 && !reset && changed && anchorCount == 0 {
				anchorTotal, anchorCount = total, len(modern)
			}
			if reset {
				invalidLegacy = true
			} else if changed {
				last, err := counts(p.Info.Last)
				if err != nil {
					return nil, nil, err
				}
				if last != delta {
					invalidLegacy = true
				}
				if !hasActive {
					return nil, nil, fmt.Errorf("missing turn context")
				}
				observed := active.Tier
				if observed == "" {
					observed = fallback
					usedFallback = true
				}
				legacy = append(legacy, Request{delta, active.Model, observed, total})
			}
			previous = total
		}
	}
	if !identity {
		return nil, nil, fmt.Errorf("missing chat identity")
	}
	notes := []string{}
	if usedFallback {
		notes = append(notes, "Missing service tier: using configured "+fallback+" rates")
	}
	if len(modern) > 0 {
		// Modern totals start at zero even when a resumed chat has older usage.
		// An advancing legacy checkpoint must confirm the entire older prefix.
		var offset Tokens
		prefix := 0
		if prefixLimit > 0 {
			if invalidLegacy {
				return nil, nil, fmt.Errorf("incomplete legacy usage prefix")
			}
			if anchorCount == 0 {
				return nil, nil, fmt.Errorf("waiting for a usage checkpoint to reconcile history")
			}
			prefix = prefixLimit
			offset = legacy[prefix-1].Total
			checkpoint, err := plus(offset, modern[anchorCount-1].Total)
			if err != nil || checkpoint != anchorTotal {
				return nil, nil, fmt.Errorf("inconsistent usage checkpoint")
			}
			if prefix == len(legacy) || len(legacy)-prefix > len(modern) {
				return nil, nil, fmt.Errorf("unverified legacy usage prefix")
			}
			for i, q := range legacy[prefix:] {
				want, err := plus(offset, modern[i].Total)
				if err != nil || q.Total != want || q.Usage != modern[i].Usage {
					return nil, nil, fmt.Errorf("inconsistent legacy and modern usage")
				}
			}
		}
		fullTotal, err := plus(offset, sum)
		if err != nil {
			return nil, nil, err
		}
		for i := range sum {
			if legacyMax[i] > fullTotal[i] {
				return nil, nil, fmt.Errorf("incomplete recorded usage total")
			}
		}
		if prefix > 0 {
			notes = append(notes, "Using verified legacy usage before modern records")
		}
		return append(legacy[:prefix], modern...), notes, nil
	}
	if invalidLegacy || len(legacy) == 0 {
		return nil, nil, fmt.Errorf("no complete per-request usage")
	}
	return legacy, append(notes, "Using legacy token-count events"), nil
}
func amountString(r *big.Rat) string {
	precision, exact := r.FloatPrec()
	if !exact {
		return r.RatString()
	}
	return r.FloatString(precision)
}
func estimate(path, id string, cfg Config) Report {
	report := Report{Currency: "USD", Notes: []string{}}
	fail := func(err error) Report { report.Notes = []string{err.Error()}; return report }
	var catalog struct {
		Models map[string]Rate `json:"models"`
	}
	if err := json.Unmarshal(bundledRates, &catalog); err != nil {
		return fail(err)
	}
	for model, rate := range cfg.Rates {
		catalog.Models[model] = rate
	}
	if err := validateRates(catalog.Models); err != nil {
		return fail(err)
	}
	requests, notes, err := readUsage(path, id, cfg.DefaultTier)
	if err != nil {
		return fail(err)
	}
	total := new(big.Rat)
	breakdown := map[string]*big.Rat{}
	for _, q := range requests {
		cost, err := requestCost(q, catalog.Models)
		if err != nil {
			return fail(err)
		}
		total.Add(total, cost)
		key := q.Model + "/" + q.Tier
		if breakdown[key] == nil {
			breakdown[key] = new(big.Rat)
		}
		breakdown[key].Add(breakdown[key], cost)
	}
	amount := amountString(total)
	report.Amount = &amount
	report.Requests = len(requests)
	report.Notes = notes
	report.ByModel = map[string]string{}
	for key, cost := range breakdown {
		report.ByModel[key] = amountString(cost)
	}
	return report
}
