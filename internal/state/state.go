package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// State is persisted to disk (state.json) and committed back to the repo
// by the GitHub Actions workflow after every run, so progress survives
// between ephemeral runner instances.
type State struct {
	// LastBatchStart is when the current/most recent full sweep of all
	// companies began. A new batch only starts once BatchIntervalDays
	// have passed since this timestamp.
	LastBatchStart time.Time `json:"last_batch_start"`

	// Pending holds company names not yet processed in the current batch.
	// Non-empty means a previous run was cut short (usually by Gemini's
	// daily quota) and the next run should resume from here instead of
	// waiting for the next scheduled batch.
	Pending []string `json:"pending"`

	// SeenJobs maps a stable hash of (company, title, url) -> true, so
	// we only ever notify Discord about genuinely new postings.
	SeenJobs map[string]bool `json:"seen_jobs"`
}

// Load reads state.json, returning a fresh empty State if the file doesn't
// exist yet (e.g. first ever run).
func Load(path string) (*State, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &State{SeenJobs: map[string]bool{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading state file %q: %w", path, err)
	}

	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parsing state file %q: %w", path, err)
	}
	if s.SeenJobs == nil {
		s.SeenJobs = map[string]bool{}
	}
	return &s, nil
}

// Save writes the state back to disk as indented JSON.
func (s *State) Save(path string) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("writing state file %q: %w", path, err)
	}
	return nil
}

// JobKey produces a stable identifier for a (company, title, url) triple so
// we can tell whether a posting has already been sent to Discord before.
func JobKey(company, title, url string) string {
	h := sha256.Sum256([]byte(company + "|" + title + "|" + url))
	return hex.EncodeToString(h[:])
}
