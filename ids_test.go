package main

import (
	"slices"
	"testing"
)

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

	if _, query, err := parseIDArgs([]string{"", " from:alice "}); err != nil || query != "from:alice" {
		t.Errorf("parseIDArgs trim: got %q, %v", query, err)
	}
	for _, args := range [][]string{{""}, {" "}, {"", "\t"}} {
		if _, _, err := parseIDArgs(args); err == nil || err.Error() != "empty query" {
			t.Errorf("parseIDArgs(%q): got %v, want empty query", args, err)
		}
	}
}
