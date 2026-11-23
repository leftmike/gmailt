package main

import "testing"

func TestFormatLabels(t *testing.T) {
	names := map[string]string{"INBOX": "INBOX", "UNREAD": "UNREAD", "Label_1": "receipts"}
	got := formatLabels([]string{"UNREAD", "Label_1", "INBOX", "Label_9"}, names)
	if want := "INBOX, Label_9, UNREAD, receipts"; got != want {
		t.Errorf("formatLabels: got %q, want %q", got, want)
	}
}
