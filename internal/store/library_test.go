package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestRemoteLibraryUpsertPreservesHiddenAndSavedFiles(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	added, updated, err := st.UpsertGenerationLogFromRemote(
		"remote-1", 8, "model-a", "16:9", "prompt",
		[]string{"https://cdn.example/a.mp4"}, "success", 100,
	)
	if err != nil || !added || updated {
		t.Fatalf("first upsert = added:%v updated:%v err:%v", added, updated, err)
	}
	rows, err := st.ListGenerationLogs(10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list = %d, err=%v", len(rows), err)
	}
	id := rows[0].ID
	if err := st.SetGenerationSavedFiles(id, []string{"C:/saved/a.mp4"}); err != nil {
		t.Fatal(err)
	}
	if err := st.HideGenerationLog(id); err != nil {
		t.Fatal(err)
	}

	added, updated, err = st.UpsertGenerationLogFromRemote(
		"remote-1", 9, "model-b", "16:9", "new prompt",
		[]string{"https://cdn.example/b.mp4"}, "success", 101,
	)
	if err != nil || added || !updated {
		t.Fatalf("second upsert = added:%v updated:%v err:%v", added, updated, err)
	}
	rows, err = st.ListGenerationLogs(10)
	if err != nil || len(rows) != 0 {
		t.Fatalf("hidden list = %d, err=%v", len(rows), err)
	}
	row, err := st.GetGenerationLog(id)
	if err != nil {
		t.Fatal(err)
	}
	if row.Hidden != 1 {
		t.Fatalf("hidden = %d", row.Hidden)
	}
	var saved []string
	if err := json.Unmarshal([]byte(row.SavedFilesJSON), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0] != "C:/saved/a.mp4" {
		t.Fatalf("saved = %#v", saved)
	}
}

func TestPreferredDBPathUsesLegacyWhenRenamedDBIsEmpty(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "current", "app.db")
	legacy := filepath.Join(dir, "legacy", "app.db")
	currentStore, err := Open(current)
	if err != nil {
		t.Fatal(err)
	}
	_ = currentStore.Close()
	legacyStore, err := Open(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacyStore.AddGenerationLog("old-1", 1, "model", "1:1", "prompt", nil, nil, false, "success", ""); err != nil {
		t.Fatal(err)
	}
	_ = legacyStore.Close()

	if got := PreferredDBPath(current, legacy); got != legacy {
		t.Fatalf("PreferredDBPath = %q, want %q", got, legacy)
	}
}
