package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func ownRows(id string, n int64) []map[string]any {
	rows := base("standard")
	rows[0]["payload"].(map[string]any)["id"] = id
	rows[1]["payload"].(map[string]any)["thread_id"] = id
	r := record("response-"+id, n, n)
	r["payload"].(map[string]any)["thread_id"] = id
	return append(rows, r)
}

func TestForkCounterBaselineWithoutCopiedRecords(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	dir := filepath.Join(home, "sessions", "2026", "10", "15")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	parent := ownRows(testID, 100000)
	parent[3]["timestamp"] = "2026-10-15T08:00:00Z"
	path := filepath.Join(dir, "rollout-"+testID+".jsonl")
	saveParent := func() {
		data, err := os.ReadFile(transcript(t, parent))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	saveParent()
	rows := ownRows(otherID, 1000)
	meta := rows[0]["payload"].(map[string]any)
	meta["forked_from_id"], meta["timestamp"] = testID, "2026-10-15T09:00:00Z"
	rows[3]["payload"].(map[string]any)["thread_token_usage"] = tokens(101000)
	r := estimate(transcript(t, rows), otherID, defaults())
	if r.Amount == nil || *r.Amount != "0.002" || r.Requests != 1 {
		t.Fatalf("wrong fork delta: %+v", r)
	}
	parent[3]["timestamp"] = "2026-10-15T10:00:00Z"
	saveParent()
	if r := estimate(transcript(t, rows), otherID, defaults()); r.Amount != nil {
		t.Fatal("accepted a counter recorded after the fork")
	}
}

func TestChildUsageExcludesInheritedHistory(t *testing.T) {
	for _, carry := range []bool{false, true} {
		for _, spawned := range []bool{false, true} {
			rows := ownRows(otherID, 1000)
			meta := rows[0]["payload"].(map[string]any)
			meta["forked_from_id"] = testID
			if spawned {
				meta["source"] = map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{"parent_thread_id": testID}}}
			}
			if carry {
				rows[3]["payload"].(map[string]any)["thread_token_usage"] = tokens(101000)
			}
			// Inherited legacy events and foreign modern records are context, not
			// newly incurred child costs. Both reset and carried totals exist.
			inherited := []map[string]any{record("parent-response", 100000, 100000), legacy(100000, 100000)}
			rows = append(append(rows[:1:1], inherited...), rows[1:]...)
			r := estimate(transcript(t, rows), otherID, defaults())
			if r.Amount == nil || *r.Amount != "0.002" || r.Requests != 1 {
				t.Fatalf("carry=%v spawned=%v: %+v", carry, spawned, r)
			}
			// A gap in the child's own modern counter must still be rejected.
			extra := record("missing-child-prefix", 1000, 500000)
			extra["payload"].(map[string]any)["thread_id"] = otherID
			if r := estimate(transcript(t, append(rows, extra)), otherID, defaults()); r.Amount != nil {
				t.Fatal("accepted incomplete child usage")
			}
		}
	}
	rows := ownRows(otherID, 1000)
	rows[0]["payload"].(map[string]any)["forked_from_id"] = testID
	rows = append(rows[:3], legacy(1000, 1000))
	if r := estimate(transcript(t, rows), otherID, defaults()); r.Amount != nil {
		t.Fatal("guessed ownership of legacy-only fork usage")
	}
}

type treeMetadata struct {
	threads  map[string]Thread
	children map[string][]Thread
	listErr  error
	writes   int
}

func (m *treeMetadata) Read(id string) (Thread, error) {
	t, ok := m.threads[id]
	if !ok {
		return Thread{}, fmt.Errorf("missing chat")
	}
	return t, nil
}
func (m *treeMetadata) Children(id string) ([]Thread, error) { return m.children[id], m.listErr }
func (m *treeMetadata) Rename(id, name string) error {
	t := m.threads[id]
	t.Name = name
	m.threads[id] = t
	m.writes++
	return nil
}

func TestTreeCostsAndUnavailableChildren(t *testing.T) {
	t.Setenv("THREAD_COST_HOME", t.TempDir())
	grandID := "01900000-0000-7000-8000-000000000003"
	forkID := "01900000-0000-7000-8000-000000000004"
	root := Thread{ID: testID, Name: "Budget", Path: transcript(t, ownRows(testID, 1000))}
	child := Thread{ID: otherID, Nickname: "Reviewer", Parent: &root.ID, Path: transcript(t, ownRows(otherID, 2000))}
	grand := Thread{ID: grandID, Name: "Nested", Parent: &child.ID, Path: transcript(t, ownRows(grandID, 3000))}
	fork := Thread{ID: forkID, Name: "Side chat", Forked: &root.ID, Path: transcript(t, ownRows(forkID, 4000))}
	m := &treeMetadata{threads: map[string]Thread{root.ID: root, child.ID: child, grand.ID: grand, fork.ID: fork}, children: map[string][]Thread{root.ID: {child, fork}, child.ID: {grand}}}
	c := defaults()
	r, err := syncWith(m, root.ID, root.Path, c, false)
	if err != nil || r.Title != "Budget [~$0.02]" || r.Estimate.Requests != 4 || r.Estimate.ByModel["gpt-6.1-sol/standard"] != "0.02" {
		t.Fatalf("wrong tree total: %+v, %v", r, err)
	}
	r, err = syncWith(m, child.ID, child.Path, c, false)
	if err != nil || r.Title != "Reviewer [~$0.01]" || r.Estimate.Requests != 2 {
		t.Fatalf("wrong child title/total: %+v, %v", r, err)
	}
	// A completed nested child refreshes its own label and every ancestor.
	m.threads[root.ID] = root
	m.threads[child.ID] = child
	_, err = syncAncestors(m, grand.ID, grand.Path, c, false)
	if err != nil || m.threads[root.ID].Name != "Budget [~$0.02]" || m.threads[child.ID].Name != "Reviewer [~$0.01]" || m.threads[grand.ID].Name != "Nested [~$0.01]" {
		t.Fatalf("ancestor refresh failed: %+v %v", m.threads, err)
	}
	// Repeated parent updates must not re-add saved child subtree totals.
	r, err = syncWith(m, root.ID, root.Path, c, false)
	if err != nil || *r.Estimate.Amount != "0.02" {
		t.Fatalf("double-counted a child: %+v %v", r, err)
	}
	grand.Path = transcript(t, base("standard"))
	m.children[child.ID] = []Thread{grand}
	before := m.writes
	r, err = syncWith(m, root.ID, root.Path, c, false)
	if err != nil || r.Estimate.Amount != nil || r.Title != "Budget [~$0.02]" || m.writes != before {
		t.Fatalf("incomplete child changed parent price: %+v %v", r, err)
	}
	m.listErr = fmt.Errorf("child listing failed")
	if got := estimateTree(m, root.ID, root.Path, c, map[string]bool{}); got.Amount != nil {
		t.Fatal("priced an unknown child list")
	}
	m.listErr = nil
	root.Parent = &child.ID
	m.children[child.ID] = []Thread{root}
	if got := estimateTree(m, root.ID, root.Path, c, map[string]bool{}); got.Amount != nil {
		t.Fatal("accepted cyclic ancestry")
	}
}
