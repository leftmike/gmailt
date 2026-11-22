package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
)

func TestTrashMessage(t *testing.T) {
	var trashed []string
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/gmail/v1/users/me/messages/1a2046b535d465fe/trash":
			trashed = append(trashed, "1a2046b535d465fe")
			fmt.Fprint(w, `{"id":"1a2046b535d465fe","labelIds":["TRASH"]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":{"code":404,"message":"not found"}}`)
		}
	})

	ctx := context.Background()
	m, err := trashMessage(ctx, svc, "1a2046b535d465fe")
	if err != nil {
		t.Fatal(err)
	}
	if len(trashed) != 1 || len(m.LabelIds) != 1 || m.LabelIds[0] != "TRASH" {
		t.Errorf("trashMessage: got %v, %+v", trashed, m)
	}

	if _, err := trashMessage(ctx, svc, "0000000000000000"); err == nil {
		t.Error("trashMessage missing: expected error")
	}
}
