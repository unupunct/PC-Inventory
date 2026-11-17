// Package buffer stores reports on disk when the server is unreachable.
package buffer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Buffer is a bounded on-disk queue of pending reports.
type Buffer struct {
	dir     string
	maxItems int
	mu      sync.Mutex
}

type Item struct {
	Kind     string          `json:"kind"` // metrics | inventory | logs
	QueuedAt time.Time       `json:"queuedAt"`
	Payload  json.RawMessage `json:"payload"`
	File     string          `json:"-"`
}

func New(dir string, maxItems int) (*Buffer, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if maxItems <= 0 {
		maxItems = 5000
	}
	return &Buffer{dir: dir, maxItems: maxItems}, nil
}

// Enqueue writes an item atomically.
func (b *Buffer) Enqueue(kind string, payload any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	item := Item{Kind: kind, QueuedAt: time.Now().UTC(), Payload: raw}
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	name := fmt.Sprintf("%d-%s-%s.json", time.Now().UnixNano(), kind, hex.EncodeToString(sum[:4]))
	tmp := filepath.Join(b.dir, name+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(b.dir, name)); err != nil {
		return err
	}
	// Enforce the cap by dropping the oldest metrics entries first.
	items := b.listLocked()
	if len(items) > b.maxItems {
		excess := len(items) - b.maxItems
		// Drop oldest metrics before inventory so hardware data is preserved.
		var metrics []Item
		for _, it := range items {
			if it.Kind == "metrics" {
				metrics = append(metrics, it)
			}
		}
		for i := 0; i < excess && i < len(metrics); i++ {
			_ = os.Remove(metrics[i].File)
		}
	}
	return nil
}

func (b *Buffer) listLocked() []Item {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return nil
	}
	var items []Item
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(b.dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var it Item
		if err := json.Unmarshal(raw, &it); err != nil {
			// Corrupt entry: drop it.
			_ = os.Remove(path)
			continue
		}
		it.File = path
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].QueuedAt.Before(items[j].QueuedAt) })
	return items
}

// List returns queued items oldest first.
func (b *Buffer) List() []Item {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.listLocked()
}

// Len returns the number of queued items.
func (b *Buffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.listLocked())
}

// Remove deletes a flushed item.
func (b *Buffer) Remove(it Item) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return os.Remove(it.File)
}

// Clear drops everything.
func (b *Buffer) Clear() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	items := b.listLocked()
	for _, it := range items {
		_ = os.Remove(it.File)
	}
	return len(items)
}
