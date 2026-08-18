package node

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoad(t *testing.T) {
	// Nested path: Save must create missing parent directories.
	path := filepath.Join(t.TempDir(), "antsd", "state.json")

	if _, err := Load(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Load before Save: got %v, want an fs.ErrNotExist", err)
	}

	state := PersistedState{
		NodeName:             "ants-01",
		Role:                 RoleServer,
		BootCount:            1,
		FirstBootCompletedAt: time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC),
		RoleChangedAt:        time.Date(2026, 8, 5, 9, 30, 0, 0, time.UTC),
	}
	if err := state.Save(path); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save returned error: %v", err)
	}
	if loaded != state {
		t.Errorf("round-trip mismatch: got %+v, want %+v", loaded, state)
	}
}

// TestLoadRejectsDamagedState checks that a state file antsd cannot trust is
// reported as an error distinct from "no file". Mistaking one for the other
// would send an already-installed node back through the first-boot protocol.
func TestLoadRejectsDamagedState(t *testing.T) {
	cases := map[string]string{
		"not json":       "{definitely not json",
		"empty file":     "",
		"missing name":   `{"role":"server","boot_count":1}`,
		"unknown role":   `{"node_name":"ants-01","role":"overlord","boot_count":1}`,
		"role omitted":   `{"node_name":"ants-01","boot_count":1}`,
		"empty json obj": `{}`,
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write state file: %v", err)
			}

			_, err := Load(path)
			if err == nil {
				t.Fatal("Load accepted a damaged state file")
			}
			if errors.Is(err, fs.ErrNotExist) {
				t.Errorf("damaged state file reported as missing: %v", err)
			}
		})
	}
}

func TestSaveOverwritesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	first := PersistedState{NodeName: "ants-01", Role: RoleServer, BootCount: 1}
	if err := first.Save(path); err != nil {
		t.Fatalf("first Save returned error: %v", err)
	}

	second := PersistedState{NodeName: "ants-01", Role: RoleAgent, BootCount: 2}
	if err := second.Save(path); err != nil {
		t.Fatalf("second Save returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var loaded PersistedState
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal state file: %v", err)
	}
	if loaded.Role != RoleAgent || loaded.BootCount != 2 {
		t.Errorf("file not overwritten: got %+v", loaded)
	}

	// No leftover temp files from the atomic write.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected only the state file in dir, found %d entries", len(entries))
	}
}
