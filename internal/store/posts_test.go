package store

import (
	"errors"
	"testing"
	"time"
)

func TestNormalizePostLifecycleRequiresFutureSchedule(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)

	if err := normalizePostLifecycle(&Post{Status: "scheduled"}, now); !errors.Is(err, ErrInvalidPostSchedule) {
		t.Fatalf("expected missing schedule to be invalid, got %v", err)
	}

	past := now.Add(-time.Minute)
	if err := normalizePostLifecycle(&Post{Status: "scheduled", ScheduledAt: &past}, now); !errors.Is(err, ErrInvalidPostSchedule) {
		t.Fatalf("expected past schedule to be invalid, got %v", err)
	}
}

func TestNormalizePostLifecycleFutureSchedule(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	p := &Post{
		Status:      "scheduled",
		ScheduledAt: &future,
	}

	if err := normalizePostLifecycle(p, now); err != nil {
		t.Fatalf("expected future schedule to be valid, got %v", err)
	}
	if p.PublishedAt != nil {
		t.Fatal("scheduled post should not have publishedAt set")
	}
	if p.ScheduledAt == nil || !p.ScheduledAt.Equal(future) {
		t.Fatalf("scheduledAt not preserved: %#v", p.ScheduledAt)
	}
}

func TestNormalizePostLifecyclePublishedAndDraft(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)

	published := &Post{Status: "published", ScheduledAt: &future}
	if err := normalizePostLifecycle(published, now); err != nil {
		t.Fatalf("expected published post to be valid, got %v", err)
	}
	if published.PublishedAt == nil || !published.PublishedAt.Equal(now) {
		t.Fatalf("publishedAt should be set to now, got %#v", published.PublishedAt)
	}
	if published.ScheduledAt != nil {
		t.Fatal("published post should clear scheduledAt")
	}

	draft := &Post{Status: "draft", PublishedAt: &now, ScheduledAt: &future}
	if err := normalizePostLifecycle(draft, now); err != nil {
		t.Fatalf("expected draft post to be valid, got %v", err)
	}
	if draft.PublishedAt != nil || draft.ScheduledAt != nil {
		t.Fatal("draft post should clear publishing timestamps")
	}
}
