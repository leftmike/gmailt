package main

import (
	"slices"
	"testing"

	"google.golang.org/api/gmail/v1"
)

func TestSortLabels(t *testing.T) {
	all := []*gmail.Label{
		{Name: "work", Type: "user"},
		{Name: "UNREAD", Type: "system"},
		{Name: "Receipts", Type: "user"},
		{Name: "INBOX", Type: "system"},
		{Name: "bills", Type: "user"},
	}
	sortLabels(all)

	var got []string
	for _, l := range all {
		got = append(got, l.Name)
	}
	if want := []string{"INBOX", "UNREAD", "bills", "Receipts", "work"}; !slices.Equal(got, want) {
		t.Errorf("sortLabels: got %v, want %v", got, want)
	}
}
