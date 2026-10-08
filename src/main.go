package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const version = "0.3.2"

type Config struct {
	Enabled     bool            `json:"enabled"`
	Position    string          `json:"position"`
	Precision   int             `json:"precision"`
	DefaultTier string          `json:"default_tier"`
	Binary      string          `json:"codex_binary"`
	Transport   string          `json:"transport"`
	Socket      string          `json:"socket"`
	Rates       map[string]Rate `json:"rates"`
}

func defaults() Config {
	return Config{true, "suffix", 2, "standard", "codex", "auto", "", map[string]Rate{}}
}
func dataHome() string {
	home, _ := os.UserHomeDir()
	base := os.Getenv("CODEX_HOME")
	if base == "" {
		base = filepath.Join(home, ".codex")
	}
	dir := os.Getenv("THREAD_COST_HOME")
	if dir == "" {
		dir = filepath.Join(base, "thread-cost")
	}
	if strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`) {
		dir = filepath.Join(home, dir[2:])
	}
	return dir
}
func loadJSON(path string, dst any) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}
func config() (Config, error) {
	c := defaults()
	if err := loadJSON(filepath.Join(dataHome(), "config.json"), &c); err != nil {
		return c, err
	}
	if c.Position != "prefix" && c.Position != "suffix" {
		return c, fmt.Errorf("position must be prefix or suffix")
	}
	if c.Precision < 0 || c.Precision > 6 {
		return c, fmt.Errorf("precision must be 0–6")
	}
	if c.Binary == "" {
		return c, fmt.Errorf("codex_binary must name an executable")
	}
	if c.Transport != "auto" && c.Transport != "proxy" && c.Transport != "stdio" {
		return c, fmt.Errorf("invalid transport")
	}
	switch c.DefaultTier {
	case "standard", "fast", "flex", "batch", "ultrafast":
	default:
		return c, fmt.Errorf("invalid default_tier")
	}
	return c, validateRates(c.Rates)
}
func saveJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".thread-cost-")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err = f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func statePath(id string) (string, error) {
	if !uuidPattern.MatchString(id) {
		return "", fmt.Errorf("expected a Codex chat UUID")
	}
	return filepath.Join(dataHome(), "threads", strings.ToLower(id)+".json"), nil
}

type Owned struct {
	Label    string `json:"label"`
	Position string `json:"position"`
	Base     string `json:"base_title"`
	Title    string `json:"title"`
}
type State struct {
	Owned    []Owned `json:"owned_labels"`
	Estimate Report  `json:"estimate"`
}

func loadState(path string) (State, error) {
	var raw struct {
		Owned    []json.RawMessage `json:"owned_labels"`
		Label    string            `json:"last_label"`
		Base     string            `json:"base_title"`
		Title    string            `json:"last_title"`
		Estimate Report            `json:"estimate"`
	}
	if err := loadJSON(path, &raw); err != nil {
		return State{}, err
	}
	result := State{Estimate: raw.Estimate}
	if len(raw.Owned) == 0 && raw.Label != "" {
		b, _ := json.Marshal(raw.Label)
		raw.Owned = append(raw.Owned, b)
	}
	for _, b := range raw.Owned {
		var entry Owned
		if len(b) > 0 && b[0] == '"' {
			if err := json.Unmarshal(b, &entry.Label); err != nil {
				return result, err
			}
			entry.Base = raw.Base
			if raw.Title == raw.Base+" "+entry.Label {
				entry.Position = "suffix"
				entry.Title = raw.Title
			} else if raw.Title == entry.Label+" "+raw.Base {
				entry.Position = "prefix"
				entry.Title = raw.Title
			}
		} else if err := json.Unmarshal(b, &entry); err != nil {
			return result, err
		}
		if entry.Label != "" {
			result.Owned = append(result.Owned, entry)
		}
	}
	return result, nil
}
func removeOwned(title string, state State) string {
	for _, e := range state.Owned {
		if e.Title != "" && e.Title == title {
			return e.Base
		}
	}
	for _, e := range state.Owned {
		pre, suf := strings.HasPrefix(title, e.Label+" "), strings.HasSuffix(title, " "+e.Label)
		if pre && (e.Position == "prefix" || e.Position == "" && !suf) {
			return strings.TrimPrefix(title, e.Label+" ")
		}
		if suf && (e.Position == "suffix" || e.Position == "" && !pre) {
			return strings.TrimSuffix(title, " "+e.Label)
		}
	}
	return title
}
func label(amount string, precision int) string {
	r, _ := new(big.Rat).SetString(amount)
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(precision)), nil)
	n := new(big.Int).Mul(r.Num(), scale)
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(n, r.Denom(), rem)
	if new(big.Int).Lsh(rem, 1).Cmp(r.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	digits := q.String()
	if precision > 0 {
		for len(digits) <= precision {
			digits = "0" + digits
		}
		at := len(digits) - precision
		digits = digits[:at] + "." + digits[at:]
	}
	return "[~$" + digits + "]"
}
func planTitle(current string, state State, report Report, c Config) (string, Owned) {
	if report.Amount == nil {
		return current, Owned{}
	}
	base := removeOwned(current, state)
	tag := label(*report.Amount, c.Precision)
	title := base + " " + tag
	if c.Position == "prefix" {
		title = tag + " " + base
	}
	return title, Owned{tag, c.Position, base, title}
}

type Thread struct {
	Name   string  `json:"name"`
	Path   string  `json:"path"`
	Parent *string `json:"parentThreadId"`
}
type Metadata interface {
	Read(string) (Thread, error)
	Rename(string, string) error
}

func writeTitle(client Metadata, id, current, next string, before func() error) (bool, error) {
	fresh, err := client.Read(id)
	if err != nil {
		return false, err
	}
	if fresh.Name != current {
		return false, nil
	}
	if before != nil {
		if err = before(); err != nil {
			return false, err
		}
	}
	if err = client.Rename(id, next); err != nil {
		return false, err
	}
	after, err := client.Read(id)
	if err != nil {
		return false, err
	}
	if after.Name != next {
		return false, fmt.Errorf("title changed during verification")
	}
	return true, nil
}

type Result struct {
	Status   string  `json:"status"`
	Title    string  `json:"title,omitempty"`
	Estimate *Report `json:"estimate,omitempty"`
}

func syncWith(client Metadata, id, path string, c Config, dry bool) (Result, error) {
	stateFile, err := statePath(id)
	if err != nil {
		return Result{}, err
	}
	if !c.Enabled {
		return Result{Status: "disabled"}, nil
	}
	release, err := lockChat(id)
	if err != nil {
		return Result{}, err
	}
	defer release()
	thread, err := client.Read(id)
	if err != nil {
		return Result{}, err
	}
	if thread.Parent != nil || thread.Name == "" {
		return Result{Status: "skipped"}, nil
	}
	if path == "" {
		path = thread.Path
	}
	if path == "" {
		return Result{}, fmt.Errorf("no local transcript")
	}
	report := estimate(path, id, c)
	previous, err := loadState(stateFile)
	if err != nil {
		return Result{}, err
	}
	title, entry := planTitle(thread.Name, previous, report, c)
	result := Result{Status: "unchanged", Title: title, Estimate: &report}
	if dry {
		result.Status = "preview"
		return result, nil
	}
	if report.Amount == nil {
		previous.Estimate = report
		return result, saveJSON(stateFile, previous)
	}
	saved := State{[]Owned{entry}, report}
	if title != thread.Name {
		pending := previous
		pending.Estimate = report
		exists := false
		for _, old := range pending.Owned {
			exists = exists || old == entry
		}
		if !exists {
			pending.Owned = append(pending.Owned, entry)
		}
		ok, err := writeTitle(client, id, thread.Name, title, func() error { return saveJSON(stateFile, pending) })
		if err != nil {
			return result, err
		}
		if !ok {
			return Result{Status: "skipped"}, nil
		}
		result.Status = "updated"
	}
	return result, saveJSON(stateFile, saved)
}
func syncChat(id, path string, dry bool) (Result, error) {
	if _, err := statePath(id); err != nil {
		return Result{}, err
	}
	c, err := config()
	if err != nil {
		return Result{}, err
	}
	if !c.Enabled {
		return Result{Status: "disabled"}, nil
	}
	client, err := connect(c)
	if err != nil {
		return Result{}, err
	}
	defer client.Close()
	return syncWith(client, id, path, c, dry)
}
func emit(v any) { _ = json.NewEncoder(os.Stdout).Encode(v) }
func hook() {
	var e struct {
		Event  string `json:"hook_event_name"`
		Source string `json:"source"`
		ID     string `json:"session_id"`
		Path   string `json:"transcript_path"`
	}
	err := json.NewDecoder(io.LimitReader(os.Stdin, 2000000)).Decode(&e)
	supported := e.Event == "Stop" || e.Event == "Interrupt" ||
		(e.Event == "SessionStart" && (e.Source == "startup" || e.Source == "resume"))
	if err == nil && !supported {
		emit(map[string]any{})
		return
	}
	var result Result
	if err == nil {
		result, err = syncChat(e.ID, e.Path, false)
	}
	if err != nil {
		emit(map[string]string{"systemMessage": "Thread Cost: update skipped; " + err.Error()})
		return
	}
	if result.Estimate != nil && result.Estimate.Amount == nil {
		emit(map[string]string{"systemMessage": "Thread Cost: estimate unavailable; title unchanged."})
		return
	}
	emit(map[string]any{})
}
func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: thread-cost hook | sync | estimate | status | version")
	}
	command := os.Args[1]
	if command == "hook" {
		hook()
		return nil
	}
	if command == "version" || command == "--version" {
		fmt.Println(version)
		return nil
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	id := flags.String("thread", os.Getenv("CODEX_THREAD_ID"), "chat UUID")
	path := flags.String("transcript", "", "local transcript")
	dry := flags.Bool("dry-run", false, "preview without updating the title")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	switch command {
	case "sync":
		r, err := syncChat(*id, *path, *dry)
		if err != nil {
			return err
		}
		emit(r)
	case "estimate":
		if _, err := statePath(*id); err != nil {
			return err
		}
		c, err := config()
		if err != nil {
			return err
		}
		emit(estimate(*path, *id, c))
	case "status":
		if *id == "" {
			c, err := config()
			if err != nil {
				return err
			}
			emit(c)
		} else {
			p, err := statePath(*id)
			if err != nil {
				return err
			}
			s, err := loadState(p)
			if err != nil {
				return err
			}
			emit(s)
		}
	default:
		return fmt.Errorf("unknown command %s", command)
	}
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Thread Cost:", err)
		os.Exit(1)
	}
}
