package buffer

import (
	"encoding/json"
	"testing"
)

func TestEnqueueListRemove(t *testing.T) {
	b, err := New(t.TempDir(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Enqueue("metrics", map[string]any{"value": 1}); err != nil {
		t.Fatal(err)
	}
	if err := b.Enqueue("inventory", map[string]any{"hash": "abc"}); err != nil {
		t.Fatal(err)
	}
	items := b.List()
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	// Oldest first.
	if items[0].Kind != "metrics" || items[1].Kind != "inventory" {
		t.Fatalf("unexpected order: %s, %s", items[0].Kind, items[1].Kind)
	}
	var payload map[string]any
	if err := json.Unmarshal(items[0].Payload, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload["value"].(float64) != 1 {
		t.Fatalf("payload lost: %v", payload)
	}
	if err := b.Remove(items[0]); err != nil {
		t.Fatal(err)
	}
	if got := b.Len(); got != 1 {
		t.Fatalf("want 1 item left, got %d", got)
	}
}

func TestCapDropsOldestMetricsFirst(t *testing.T) {
	dir := t.TempDir()
	b, err := New(dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	// An inventory report is queued first, then enough metrics to overflow.
	if err := b.Enqueue("inventory", map[string]int{"n": 0}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := b.Enqueue("metrics", map[string]int{"n": i}); err != nil {
			t.Fatal(err)
		}
	}
	items := b.List()
	kept := map[string]int{}
	for _, it := range items {
		kept[it.Kind]++
	}
	// Hardware data is never dropped in favour of metrics.
	if kept["inventory"] != 1 {
		t.Fatalf("inventory was dropped: %v", kept)
	}
	if len(items) > 3 {
		t.Fatalf("buffer exceeded its cap: %d items", len(items))
	}
}

func TestClear(t *testing.T) {
	b, _ := New(t.TempDir(), 10)
	_ = b.Enqueue("metrics", map[string]int{"n": 1})
	_ = b.Enqueue("metrics", map[string]int{"n": 2})
	if n := b.Clear(); n != 2 {
		t.Fatalf("Clear removed %d, want 2", n)
	}
	if b.Len() != 0 {
		t.Fatal("buffer not empty after Clear")
	}
}
