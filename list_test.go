package main

import (
	"testing"
	"time"

	"google.golang.org/api/gmail/v1"
)

func TestFit(t *testing.T) {
	cases := []struct {
		s    string
		n    int
		want string
	}{
		{"abc", 5, "abc  "},
		{"abcde", 5, "abcde"},
		{"abcdef", 5, "abcd…"},
		{"日本語テキスト", 4, "日本語…"},
	}
	for _, c := range cases {
		if got := fit(c.s, c.n); got != c.want {
			t.Errorf("fit(%q, %d): got %q, want %q", c.s, c.n, got, c.want)
		}
	}
}

func TestSender(t *testing.T) {
	cases := map[string]string{
		`Alice Smith <alice@example.com>`:          "Alice Smith",
		`"Smith, Alice" <alice@example.com>`:       "Smith, Alice",
		`alice@example.com`:                        "alice@example.com",
		`=?UTF-8?B?SsO8cmdlbg==?= <j@example.com>`: "Jürgen",
		`not an address`:                           "not an address",
		``:                                         "",
	}
	for from, want := range cases {
		if got := sender(from); got != want {
			t.Errorf("sender(%q): got %q, want %q", from, got, want)
		}
	}
}

func TestFormatListLine(t *testing.T) {
	saved := time.Local
	time.Local = time.UTC
	defer func() { time.Local = saved }()

	m := &gmail.Message{
		Id:           "18f3a2b4c5d6e7f8",
		InternalDate: time.Date(2026, 11, 21, 14, 2, 0, 0, time.UTC).UnixMilli(),
		LabelIds:     []string{"INBOX", "UNREAD"},
		Payload: &gmail.MessagePart{Headers: []*gmail.MessagePartHeader{
			{Name: "From", Value: "Alice Smith <alice@example.com>"},
			{Name: "Subject", Value: "=?UTF-8?Q?Re:_dinner_Saturday?=\r\n\tagain"},
		}},
	}
	want := "* 18f3a2b4c5d6e7f8  2026-11-21 14:02  Alice Smith           Re: dinner Saturday again"
	if got := formatListLine(m); got != want {
		t.Errorf("formatListLine:\n got %q\nwant %q", got, want)
	}

	m.LabelIds = []string{"INBOX"}
	if got := formatListLine(m); got[0] != ' ' {
		t.Errorf("formatListLine read: got %q", got)
	}
}

func TestListQuery(t *testing.T) {
	for _, args := range [][]string{nil, {""}, {" ", "\t"}} {
		if got := listQuery(args); got != "in:inbox" {
			t.Errorf("listQuery(%q): got %q", args, got)
		}
	}
	if got := listQuery([]string{"-in:inbox", "from:alice"}); got != "-in:inbox from:alice" {
		t.Errorf("listQuery: got %q", got)
	}
}
