package main

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"google.golang.org/api/gmail/v1"
)

func TestParseLabelArgs(t *testing.T) {
	add, remove, rest, err := parseLabelArgs([]string{"+spam", "^INBOX", "+Assisted Living", "+SPAM",
		"from:alice", "+word", "^x"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(add, []string{"spam", "Assisted Living"}) || !slices.Equal(remove, []string{"INBOX"}) ||
		!slices.Equal(rest, []string{"from:alice", "+word", "^x"}) {
		t.Errorf("parseLabelArgs: got %v, %v, %v", add, remove, rest)
	}

	add, remove, rest, err = parseLabelArgs([]string{"in:inbox"})
	if err != nil || add != nil || remove != nil || !slices.Equal(rest, []string{"in:inbox"}) {
		t.Errorf("parseLabelArgs no ops: got %v, %v, %v, %v", add, remove, rest, err)
	}

	for _, bad := range [][]string{{"+work", "^Work", "x"}, {"+", "x"}, {"^", "x"}} {
		if _, _, _, err := parseLabelArgs(bad); err == nil {
			t.Errorf("parseLabelArgs(%q): expected error", bad)
		}
	}
}

func TestResolveLabels(t *testing.T) {
	all := []*gmail.Label{
		{Id: "INBOX", Name: "INBOX"},
		{Id: "SPAM", Name: "SPAM"},
		{Id: "Label_7", Name: "Assisted Living"},
	}
	found, missing := resolveLabels(all, []string{"spam", "assisted living", "nope"})
	if !slices.Equal(labelIDs(found), []string{"SPAM", "Label_7"}) ||
		!slices.Equal(missing, []string{"nope"}) {
		t.Errorf("resolveLabels: got %v, %v", labelIDs(found), missing)
	}

	if got := describeChanges(found, all[:1]); got != "+SPAM +Assisted Living ^INBOX" {
		t.Errorf("describeChanges: got %q", got)
	}
}

func TestCreateLabel(t *testing.T) {
	var created gmail.Label
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/gmail/v1/users/me/labels" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		created.Id = "Label_99"
		json.NewEncoder(w).Encode(created)
	})

	l, err := createLabel(context.Background(), svc, "Receipts/2026")
	if err != nil {
		t.Fatal(err)
	}
	if l.Id != "Label_99" || l.Name != "Receipts/2026" || created.LabelListVisibility != "labelShow" ||
		created.MessageListVisibility != "show" {
		t.Errorf("createLabel: got %+v, sent %+v", l, created)
	}
}
