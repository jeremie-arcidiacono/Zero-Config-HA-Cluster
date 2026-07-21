package node

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PersistedState is the local node state written to disk once the first boot completes.
// Its presence distinguishes a first boot from a reboot: on later startups antsd will read it back and
// take the rejoin-cluster path instead of the first-boot protocol.
type PersistedState struct {
	NodeName             string    `json:"node_name"`
	Role                 Role      `json:"role"`
	BootCount            int       `json:"boot_count"`
	FirstBootCompletedAt time.Time `json:"first_boot_completed_at"`
}

// Save writes the state as JSON to a path.
// The writing is atomic (temp file + rename).
func (s PersistedState) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal persisted state: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp state file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename temp state file to %s: %w", path, err)
	}
	return nil
}

// Exists reports whether a state file is present at path.
func Exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
