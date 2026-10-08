package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func codexHome() string {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return home
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

// A saved fork may retain counters but omit the inherited usage records.
// Accept that baseline only if its parent recorded it before the fork existed.
func verifiedForkBaseline(parent string, total Tokens, before string) (bool, error) {
	if _, err := statePath(parent); err != nil {
		return false, err
	}
	cutoff, err := time.Parse(time.RFC3339Nano, before)
	if err != nil {
		return false, err
	}
	patterns := []string{
		filepath.Join(codexHome(), "sessions", "*", "*", "*", "*-"+parent+".jsonl"),
		filepath.Join(codexHome(), "archived_sessions", "*-"+parent+".jsonl"),
	}
	for _, pattern := range patterns {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			return false, err
		}
		for _, path := range paths {
			f, err := os.Open(path)
			if err != nil {
				return false, err
			}
			ok, err := parentHasTotal(f, parent, total, cutoff)
			f.Close()
			if err != nil || ok {
				return ok, err
			}
		}
	}
	return false, nil
}

func parentHasTotal(f *os.File, parent string, total Tokens, cutoff time.Time) (bool, error) {
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 16*1024*1024)
	identity := false
	for scan.Scan() {
		var row Record
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			return false, err
		}
		if row.Type != "session_meta" && row.Type != "token_usage_record" {
			continue
		}
		var p Payload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return false, err
		}
		if row.Type == "session_meta" {
			identity = p.ID == parent
			continue
		}
		if !identity || p.ThreadID != parent {
			continue
		}
		stamp, err := time.Parse(time.RFC3339Nano, row.Timestamp)
		if err != nil || stamp.After(cutoff) {
			continue
		}
		usage, err := counts(p.Total)
		if err != nil {
			return false, err
		}
		if usage == total {
			return true, nil
		}
	}
	return false, scan.Err()
}

func parentID(t Thread) string {
	if t.Parent != nil {
		return *t.Parent
	}
	if t.Forked != nil {
		return *t.Forked
	}
	return ""
}

// Add each descendant's own usage once, never a previously saved tree total.
func estimateTree(client Metadata, id, path string, cfg Config, seen map[string]bool) Report {
	fail := func(err error) Report { return Report{Currency: "USD", Notes: []string{err.Error()}} }
	if _, err := statePath(id); err != nil {
		return fail(err)
	}
	if seen[id] {
		return fail(fmt.Errorf("duplicate or cyclic child relationship: %s", id))
	}
	seen[id] = true
	r := estimate(path, id, cfg)
	if r.Amount == nil {
		return r
	}
	children, err := client.Children(id)
	if err != nil {
		return fail(err)
	}
	total, _ := new(big.Rat).SetString(*r.Amount)
	for _, child := range children {
		if parentID(child) != id || child.Ephemeral || child.Path == "" {
			return fail(fmt.Errorf("child usage is unavailable: %s", child.ID))
		}
		part := estimateTree(client, child.ID, child.Path, cfg, seen)
		if part.Amount == nil {
			return fail(fmt.Errorf("child %s: %s", child.ID, strings.Join(part.Notes, "; ")))
		}
		amount, _ := new(big.Rat).SetString(*part.Amount)
		total.Add(total, amount)
		r.Requests += part.Requests
		for model, cost := range part.ByModel {
			value, _ := new(big.Rat).SetString(cost)
			if old, ok := new(big.Rat).SetString(r.ByModel[model]); ok {
				value.Add(value, old)
			}
			r.ByModel[model] = amountString(value)
		}
		r.Notes = append(r.Notes, part.Notes...)
	}
	amount := amountString(total)
	r.Amount = &amount
	if len(children) > 0 {
		r.Notes = append(r.Notes, "Includes saved child chats")
	}
	unique := map[string]bool{}
	notes := r.Notes[:0]
	for _, note := range r.Notes {
		if !unique[note] {
			notes = append(notes, note)
			unique[note] = true
		}
	}
	r.Notes = notes
	return r
}

// The thread list API indexes spawned children. Saved forks additionally carry
// their parent in session_meta; read only that header to discover those links.
func savedForks() (map[string][]Thread, error) {
	home := codexHome()
	result := map[string][]Thread{}
	for _, dir := range []string{"sessions", "archived_sessions"} {
		err := filepath.WalkDir(filepath.Join(home, dir), func(path string, entry fs.DirEntry, err error) error {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			scan := bufio.NewScanner(f)
			scan.Buffer(make([]byte, 4096), 1024*1024)
			ok := scan.Scan()
			var header struct {
				Type    string `json:"type"`
				Payload struct {
					ID     string          `json:"id"`
					Forked *string         `json:"forked_from_id"`
					Source json.RawMessage `json:"source"`
				} `json:"payload"`
			}
			parseErr := json.Unmarshal(scan.Bytes(), &header)
			readErr := scan.Err()
			f.Close()
			if readErr != nil {
				return readErr
			}
			// A newly created transcript may not have a complete header yet.
			if !ok || parseErr != nil || header.Type != "session_meta" || header.Payload.Forked == nil {
				return nil
			}
			var source map[string]json.RawMessage
			if json.Unmarshal(header.Payload.Source, &source) == nil {
				if _, ok := source["subagent"]; ok {
					return nil
				}
				if _, ok := source["internal"]; ok {
					return nil
				}
			}
			t := Thread{ID: header.Payload.ID, Path: path, Forked: header.Payload.Forked}
			result[*t.Forked] = append(result[*t.Forked], t)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}
