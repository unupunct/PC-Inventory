package store

// Software diff between two inventory snapshots. Only applications present in
// the stored snapshots are compared; nothing is inferred.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/shared/models"
)

// SoftwareChange is one added, removed or version-changed application.
type SoftwareChange struct {
	Name       string `json:"name"`
	Publisher  string `json:"publisher,omitempty"`
	Change     string `json:"change"` // added | removed | upgraded | downgraded
	FromVersion string `json:"fromVersion,omitempty"`
	ToVersion  string `json:"toVersion,omitempty"`
}

// SoftwareDiff is the result of comparing two snapshots.
type SoftwareDiff struct {
	AgentID       string           `json:"agentId"`
	FromSnapshot  int64            `json:"fromSnapshot"`
	ToSnapshot    int64            `json:"toSnapshot"`
	FromCollected time.Time        `json:"fromCollectedAt"`
	ToCollected   time.Time        `json:"toCollectedAt"`
	Added         int              `json:"added"`
	Removed       int              `json:"removed"`
	Upgraded      int              `json:"upgraded"`
	Downgraded    int              `json:"downgraded"`
	Changes       []SoftwareChange `json:"changes"`
	Available     bool             `json:"available"`
	Message       string           `json:"message,omitempty"`
}

func loadSnapshotInventory(ctx context.Context, db *sql.DB, agentID string, snapshotID int64) (models.Inventory, time.Time, error) {
	var raw, collected string
	err := db.QueryRowContext(ctx, `SELECT inventory_json,collected_at FROM inventory_snapshots WHERE agent_id=? AND id=?`, agentID, snapshotID).Scan(&raw, &collected)
	if err != nil {
		return models.Inventory{}, time.Time{}, err
	}
	var inv models.Inventory
	if err := json.Unmarshal([]byte(raw), &inv); err != nil {
		return models.Inventory{}, time.Time{}, err
	}
	return inv, parseTS(collected), nil
}

// LatestSnapshotID returns the newest snapshot id for an agent.
func (s *Store) LatestSnapshotID(ctx context.Context, agentID string) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM inventory_snapshots WHERE agent_id=? ORDER BY collected_at DESC, id DESC LIMIT 1`, agentID).Scan(&id)
	return id, err
}

// PreviousSnapshotID returns the snapshot before the given one.
func (s *Store) PreviousSnapshotID(ctx context.Context, agentID string, before int64) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM inventory_snapshots WHERE agent_id=? AND id<? ORDER BY id DESC LIMIT 1`, agentID, before).Scan(&id)
	return id, err
}

// SoftwareDiff compares the software lists of two snapshots of one agent.
func (s *Store) SoftwareDiff(ctx context.Context, agentID string, fromID, toID int64) (*SoftwareDiff, error) {
	out := &SoftwareDiff{AgentID: agentID}
	if toID == 0 {
		var err error
		toID, err = s.LatestSnapshotID(ctx, agentID)
		if err != nil {
			return nil, err
		}
	}
	if fromID == 0 {
		var err error
		fromID, err = s.PreviousSnapshotID(ctx, agentID, toID)
		if errors.Is(err, sql.ErrNoRows) {
			out.ToSnapshot = toID
			out.Message = "Only one inventory snapshot exists, so there is nothing to compare yet."
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
	toInv, toAt, err := loadSnapshotInventory(ctx, s.DB, agentID, toID)
	if err != nil {
		return nil, err
	}
	fromInv, fromAt, err := loadSnapshotInventory(ctx, s.DB, agentID, fromID)
	if err != nil {
		return nil, err
	}
	out.FromSnapshot, out.ToSnapshot = fromID, toID
	out.FromCollected, out.ToCollected = fromAt, toAt
	out.Available = true

	key := func(a models.Application) string {
		return strings.ToLower(strings.TrimSpace(a.Name)) + "|" + strings.ToLower(strings.TrimSpace(a.Publisher))
	}
	from := map[string]models.Application{}
	for _, a := range fromInv.Software {
		from[key(a)] = a
	}
	to := map[string]models.Application{}
	for _, a := range toInv.Software {
		to[key(a)] = a
	}

	for k, a := range to {
		prev, ok := from[k]
		if !ok {
			out.Changes = append(out.Changes, SoftwareChange{Name: a.Name, Publisher: a.Publisher, Change: "added", ToVersion: a.Version})
			out.Added++
			continue
		}
		if normalizeVersion(prev.Version) != normalizeVersion(a.Version) {
			kind := "upgraded"
			if versionLess(a.Version, prev.Version) {
				kind = "downgraded"
				out.Downgraded++
			} else {
				out.Upgraded++
			}
			out.Changes = append(out.Changes, SoftwareChange{Name: a.Name, Publisher: a.Publisher, Change: kind, FromVersion: prev.Version, ToVersion: a.Version})
		}
	}
	for k, a := range from {
		if _, ok := to[k]; !ok {
			out.Changes = append(out.Changes, SoftwareChange{Name: a.Name, Publisher: a.Publisher, Change: "removed", FromVersion: a.Version})
			out.Removed++
		}
	}
	sort.Slice(out.Changes, func(i, j int) bool {
		if out.Changes[i].Change != out.Changes[j].Change {
			return out.Changes[i].Change < out.Changes[j].Change
		}
		return strings.ToLower(out.Changes[i].Name) < strings.ToLower(out.Changes[j].Name)
	})
	return out, nil
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	return v
}

// versionLess compares two dotted version strings numerically when possible.
func versionLess(a, b string) bool {
	pa, pb := splitVersion(a), splitVersion(b)
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return len(pa) < len(pb)
}

func splitVersion(v string) []int {
	v = normalizeVersion(v)
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == '.' || r == '-' || r == '_' })
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n := 0
		seen := false
		for _, r := range p {
			if r < '0' || r > '9' {
				break
			}
			seen = true
			n = n*10 + int(r-'0')
		}
		if !seen {
			break
		}
		out = append(out, n)
	}
	return out
}
