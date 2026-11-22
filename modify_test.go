package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"google.golang.org/api/gmail/v1"
)

func TestModifyLabels(t *testing.T) {
	var reqs []gmail.BatchModifyMessagesRequest
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/gmail/v1/users/me/messages/batchModify" {
			http.NotFound(w, r)
			return
		}
		var req gmail.BatchModifyMessagesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reqs = append(reqs, req)
		w.WriteHeader(http.StatusNoContent)
	})

	var ids []string
	for i := range 2500 {
		ids = append(ids, fmt.Sprintf("%016x", i))
	}
	err := modifyLabels(context.Background(), svc, ids, []string{"SPAM"}, []string{"INBOX"})
	if err != nil {
		t.Fatal(err)
	}

	if len(reqs) != 3 {
		t.Fatalf("modifyLabels: got %d requests, want 3", len(reqs))
	}
	for i, n := range []int{1000, 1000, 500} {
		req := reqs[i]
		if len(req.Ids) != n || req.Ids[0] != ids[i*1000] ||
			!slices.Equal(req.AddLabelIds, []string{"SPAM"}) ||
			!slices.Equal(req.RemoveLabelIds, []string{"INBOX"}) {
			t.Errorf("modifyLabels request %d: got %d ids, add %v, remove %v", i, len(req.Ids),
				req.AddLabelIds, req.RemoveLabelIds)
		}
	}

	reqs = nil
	if err := modifyLabels(context.Background(), svc, nil, nil, []string{"INBOX"}); err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 0 {
		t.Errorf("modifyLabels no ids: got %d requests", len(reqs))
	}
}
