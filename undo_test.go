package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/api/gmail/v1"
)

func TestNetChanges(t *testing.T) {
	msg := func(id string) *gmail.Message { return &gmail.Message{Id: id} }
	records := []*gmail.History{
		{LabelsRemoved: []*gmail.HistoryLabelRemoved{
			{Message: msg("a"), LabelIds: []string{"INBOX"}},
			{Message: msg("b"), LabelIds: []string{"INBOX"}},
			{Message: msg("other"), LabelIds: []string{"INBOX"}},
		}},
		{LabelsAdded: []*gmail.HistoryLabelAdded{
			{Message: msg("c"), LabelIds: []string{"SPAM"}},
			{Message: msg("d"), LabelIds: []string{"STARRED"}},
		}},
		{LabelsRemoved: []*gmail.HistoryLabelRemoved{
			{Message: msg("c"), LabelIds: []string{"INBOX"}},
			{Message: msg("d"), LabelIds: []string{"STARRED"}},
		}},
	}
	got := netChanges(records, []string{"b", "a", "c", "d"})
	want := []labelChange{
		{IDs: []string{"c"}, Added: []string{"SPAM"}, Removed: []string{"INBOX"}},
		{IDs: []string{"a", "b"}, Removed: []string{"INBOX"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("netChanges: got %+v, want %+v", got, want)
	}
}

func TestHistoryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "history.jsonl")
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	change := []labelChange{{IDs: []string{"a"}, Removed: []string{"INBOX"}}}

	for i, when := range []time.Time{now.AddDate(0, 0, -40), now.AddDate(0, 0, -2), now} {
		n, err := appendHistory(path, historyEntry{Command: "archive x", Changes: change}, when)
		if err != nil {
			t.Fatal(err)
		}
		if n != i+1 {
			t.Errorf("appendHistory: got n %d, want %d", n, i+1)
		}
		if i == 1 {
			err := writeHistory(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, []historyEntry{{Undone: 2}})
			if err != nil {
				t.Fatal(err)
			}
		}
	}

	entries, undone, err := readHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	var ns []int
	for _, e := range entries {
		ns = append(ns, e.N)
	}
	if !slices.Equal(ns, []int{2, 3}) || !reflect.DeepEqual(undone, map[int]bool{2: true}) {
		t.Errorf("readHistory: got %v, %v", ns, undone)
	}

	e, err := findUndo(entries, undone, nil)
	if err != nil || e.N != 3 {
		t.Errorf("findUndo latest: got %v, %v", e, err)
	}
	for _, args := range [][]string{{"2"}, {"1"}, {"x"}} {
		if _, err := findUndo(entries, undone, args); err == nil {
			t.Errorf("findUndo(%v): expected error", args)
		}
	}
	if _, err := findUndo(entries, map[int]bool{2: true, 3: true}, nil); err == nil {
		t.Error("findUndo all undone: expected error")
	}

	if entries, _, err := readHistory(filepath.Join(t.TempDir(), "none")); err != nil || entries != nil {
		t.Errorf("readHistory missing: got %v, %v", entries, err)
	}
}

func TestRevertChange(t *testing.T) {
	var mu sync.Mutex
	var untrashed []string
	var batch gmail.BatchModifyMessagesRequest
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/untrash"):
			id := strings.Split(r.URL.Path, "/")[6]
			untrashed = append(untrashed, id)
			json.NewEncoder(w).Encode(gmail.Message{Id: id})
		case r.URL.Path == "/gmail/v1/users/me/messages/batchModify":
			json.NewDecoder(r.Body).Decode(&batch)
		default:
			http.NotFound(w, r)
		}
	})

	c := labelChange{IDs: []string{"a", "b"}, Added: []string{"TRASH", "STARRED"},
		Removed: []string{"INBOX"}}
	if err := revertChange(context.Background(), svc, c); err != nil {
		t.Fatal(err)
	}
	slices.Sort(untrashed)
	if !slices.Equal(untrashed, []string{"a", "b"}) || !slices.Equal(batch.Ids, []string{"a", "b"}) ||
		!slices.Equal(batch.AddLabelIds, []string{"INBOX"}) ||
		!slices.Equal(batch.RemoveLabelIds, []string{"STARRED"}) {
		t.Errorf("revertChange: untrashed %v, batch %+v", untrashed, batch)
	}
}

func TestCommandLine(t *testing.T) {
	got := commandLine([]string{"label", "+Assisted Living", "^unread", "from:alice"})
	if want := `label "+Assisted Living" ^unread from:alice`; got != want {
		t.Errorf("commandLine: got %q, want %q", got, want)
	}
}
