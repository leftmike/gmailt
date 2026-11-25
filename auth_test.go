package main

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

func TestLogTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	var buf strings.Builder
	client := &http.Client{Transport: &logTransport{base: http.DefaultTransport, w: &buf}}
	resp, err := client.Get(srv.URL +
		"/gmail/v1/users/me/messages?alt=json&prettyPrint=false&q=in%3Ainbox+is%3Aunread")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	got := buf.String()
	want := "GET /gmail/v1/users/me/messages?q=in:inbox is:unread: 404 Not Found ("
	if !strings.HasPrefix(got, want) || !strings.HasSuffix(got, "ms)\n") {
		t.Errorf("logTransport: got %q, want prefix %q", got, want)
	}
}

func TestAuthorizeOutput(t *testing.T) {
	oc := &oauth2.Config{ClientID: "id", Endpoint: oauth2.Endpoint{AuthURL: "https://auth.example.com/"}}
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := authorize(t.Context(), oc, pw)
		done <- err
	}()

	r := bufio.NewReader(pr)
	var lines []string
	for range 3 {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, strings.TrimSuffix(line, "\n"))
	}
	go io.Copy(io.Discard, r)
	if lines[0] != "open this URL in a browser to authorize gmail:" || lines[1] != "" {
		t.Fatalf("authorize: got %q", lines)
	}

	u, err := url.Parse(lines[2])
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	resp, err := http.Get(q.Get("redirect_uri") + "?state=" + url.QueryEscape(q.Get("state")) +
		"&error=access_denied")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if err := <-done; err == nil || err.Error() != "authorization failed: access_denied" {
		t.Errorf("authorize: got %v", err)
	}
}
