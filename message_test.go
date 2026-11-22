package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestTruncate(t *testing.T) {
	cases := []struct {
		s    string
		n    int
		want string
	}{
		{"hello", 90, "hello"},
		{"  hello   world  ", 90, "hello world"},
		{"line one\r\n\r\n\r\nline two", 90, "line one\nline two"},
		{"a\tb\x00c", 90, "a b c"},
		{"abcdefghij", 5, "abcde..."},
	}
	for _, c := range cases {
		if got := truncate(c.s, c.n); got != c.want {
			t.Errorf("truncate(%q, %d): got %q, want %q", c.s, c.n, got, c.want)
		}
	}
}

func TestFormatBody(t *testing.T) {
	cases := []struct {
		file    string
		subject string
		body    string
	}{
		{"base64.eml", "base64",
			"Hello world, this is a test of the emergency broadcast system."},
		{"qp.eml", "quoted printable",
			"Café costs 5=5 dollars, a very long line that is soft wrapped right here.\r\n"},
		{"multipart.eml", "multipart",
			"Café crème brûlée.\n[text/html; charset=utf-8, base64, 12 bytes]"},
		{"html-only.eml", "html only",
			"[text/html; charset=shift_jis, base64, 16 bytes]\n[application/pdf, base64, 5 bytes]"},
		{"unknown-encoding.eml", "unknown encoding", "hello there\r\n"},
	}
	for _, c := range cases {
		raw, err := os.ReadFile(filepath.Join("testdata", c.file))
		if err != nil {
			t.Fatal(err)
		}
		entity, err := parseMessage(raw)
		if err != nil {
			t.Errorf("%s: parseMessage: %s", c.file, err)
			continue
		}
		msg, err := messageFromEntity(entity)
		if err != nil {
			t.Errorf("%s: messageFromEntity: %s", c.file, err)
			continue
		}
		if msg.subject != c.subject {
			t.Errorf("%s: subject: got %q, want %q", c.file, msg.subject, c.subject)
		}
		if got := msg.formatBody(false); got != c.body {
			t.Errorf("%s: formatBody: got %q, want %q", c.file, got, c.body)
		}
	}
}

func TestParseIDArgs(t *testing.T) {
	ids, query, err := parseIDArgs([]string{"1a2046b535d465fe", "1A2044F4048D3679"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids, []string{"1a2046b535d465fe", "1a2044f4048d3679"}) || query != "" {
		t.Errorf("parseIDArgs ids: got %v, %q", ids, query)
	}

	ids, query, err = parseIDArgs([]string{"from:alice", "is:unread", "1a2046b535d465f",
		"1a2046b535d465fe0", "1a2046b535d465fg"})
	if err != nil {
		t.Fatal(err)
	}
	if ids != nil || query != "from:alice is:unread 1a2046b535d465f 1a2046b535d465fe0 1a2046b535d465fg" {
		t.Errorf("parseIDArgs query: got %v, %q", ids, query)
	}

	if _, _, err := parseIDArgs([]string{"1a2046b535d465fe", "from:alice"}); err == nil {
		t.Error("parseIDArgs mixed: expected error")
	}
}

func TestFormatLabels(t *testing.T) {
	names := map[string]string{"INBOX": "INBOX", "UNREAD": "UNREAD", "Label_1": "receipts"}
	got := formatLabels([]string{"UNREAD", "Label_1", "INBOX", "Label_9"}, names)
	if want := "INBOX, Label_9, UNREAD, receipts"; got != want {
		t.Errorf("formatLabels: got %q, want %q", got, want)
	}
}
