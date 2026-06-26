package api

import (
	"testing"
	"time"
)

func TestParseTimeUsesBeijing(t *testing.T) {
	parsed, err := parseTime("2026-06-22T08:00:00+08:00")
	if err != nil {
		t.Fatal(err)
	}
	_, offset := parsed.Zone()
	if offset != 8*60*60 {
		t.Fatalf("expected Beijing offset, got %d", offset)
	}
	if parsed.Format(time.RFC3339) != "2026-06-22T08:00:00+08:00" {
		t.Fatalf("unexpected time: %s", parsed.Format(time.RFC3339))
	}
}

func TestParseTimeTreatsZoneLessInputAsBeijing(t *testing.T) {
	parsed, err := parseTime("2026-06-22T08:00:00")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Format(time.RFC3339) != "2026-06-22T08:00:00+08:00" {
		t.Fatalf("unexpected time: %s", parsed.Format(time.RFC3339))
	}
}
