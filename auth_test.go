package main

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/oauth2"
)

func TestTokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "token.json")
	tok := &oauth2.Token{AccessToken: "access", RefreshToken: "refresh", TokenType: "Bearer"}
	if err := writeToken(path, tok); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("writeToken: mode %v, want 0600", fi.Mode().Perm())
	}
	got, err := readToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "access" || got.RefreshToken != "refresh" {
		t.Errorf("readToken: got %+v", got)
	}

	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readToken(path); err == nil {
		t.Error("readToken bad json: expected error")
	}
}

type tokenSequence []*oauth2.Token

func (ts *tokenSequence) Token() (*oauth2.Token, error) {
	tok := (*ts)[0]
	*ts = (*ts)[1:]
	return tok, nil
}

func TestSavingTokenSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.json")
	seq := tokenSequence{{AccessToken: "one"}, {AccessToken: "two"}}
	s := &savingTokenSource{src: &seq, path: path, last: "one"}

	if _, err := s.Token(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("unchanged token was written: %v", err)
	}

	if _, err := s.Token(); err != nil {
		t.Fatal(err)
	}
	got, err := readToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "two" || s.last != "two" {
		t.Errorf("refreshed token: got %q, last %q", got.AccessToken, s.last)
	}
}
