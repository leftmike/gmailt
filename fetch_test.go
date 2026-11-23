package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

func newTestService(t *testing.T, h http.HandlerFunc) *gmail.Service {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	svc, err := gmail.NewService(context.Background(), option.WithEndpoint(srv.URL+"/"),
		option.WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestListIDs(t *testing.T) {
	pages := map[string]string{
		"":   `{"messages":[{"id":"a"},{"id":"b"}],"nextPageToken":"p2"}`,
		"p2": `{"messages":[{"id":"c"}]}`,
	}
	var maxResults []string
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gmail/v1/users/me/messages" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("q") != "from:x" {
			http.Error(w, "bad query: "+r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		maxResults = append(maxResults, q.Get("maxResults"))
		io.WriteString(w, pages[q.Get("pageToken")])
	})

	ctx := context.Background()
	ids, more, err := listIDs(ctx, svc, "from:x", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids, []string{"a", "b", "c"}) || more {
		t.Errorf("listIDs: got %v, %v", ids, more)
	}
	if !slices.Equal(maxResults, []string{"500", "500"}) {
		t.Errorf("maxResults: got %v", maxResults)
	}

	maxResults = nil
	ids, more, err = listIDs(ctx, svc, "from:x", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids, []string{"a", "b"}) || !more {
		t.Errorf("listIDs max 2: got %v, %v", ids, more)
	}
	if !slices.Equal(maxResults, []string{"2"}) {
		t.Errorf("maxResults max 2: got %v", maxResults)
	}
}

func TestGetRaw(t *testing.T) {
	retryDelay = time.Millisecond
	raw := "Subject: hi\r\n\r\nbody\r\n"

	var calls atomic.Int32
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "raw" {
			http.Error(w, "bad format", http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/gmail/v1/users/me/messages/a":
			if calls.Add(1) == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				io.WriteString(w, `{"error":{"code":429,"message":"slow down"}}`)
				return
			}
			fmt.Fprintf(w, `{"id":"a","raw":%q}`, base64.URLEncoding.EncodeToString([]byte(raw)))
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":{"code":404,"message":"not found"}}`)
		}
	})

	ctx := context.Background()
	b, err := getRaw(ctx, svc, "a")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != raw {
		t.Errorf("getRaw: got %q", b)
	}
	if calls.Load() != 2 {
		t.Errorf("getRaw: got %d calls, want 2", calls.Load())
	}

	if _, err := getRaw(ctx, svc, "missing"); err == nil {
		t.Error("getRaw missing: expected error")
	}

	entity, err := parseMessage(b)
	if err != nil {
		t.Fatal(err)
	}
	if s := entity.Header.Get("Subject"); s != "hi" {
		t.Errorf("parseMessage: got subject %q", s)
	}
}

func TestGetMetadata(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("format") != "metadata" ||
			!slices.Equal(q["metadataHeaders"], []string{"From", "Subject", "Date"}) {
			http.Error(w, "bad query: "+r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		io.WriteString(w,
			`{"id":"a","payload":{"headers":[{"name":"subject","value":"hi"},{"name":"From","value":"me"}]}}`)
	})

	m, err := getMetadata(context.Background(), svc, "a")
	if err != nil {
		t.Fatal(err)
	}
	if header(m, "Subject") != "hi" || header(m, "from") != "me" || header(m, "Date") != "" {
		t.Errorf("header: got %+v", m.Payload.Headers)
	}
	if header(&gmail.Message{}, "Subject") != "" {
		t.Error("header: expected empty for nil payload")
	}
}

func TestDecodeRaw(t *testing.T) {
	for _, s := range []string{"aGk_", "aGk-Pw", "aGk-Pw=="} {
		if _, err := decodeRaw(s); err != nil {
			t.Errorf("decodeRaw(%q): %s", s, err)
		}
	}
}

func TestEach(t *testing.T) {
	var ids, want []string
	bad := map[string]bool{}
	for i := range 250 {
		id := fmt.Sprintf("m%d", i)
		ids = append(ids, id)
		if i%50 == 7 {
			bad[id] = true
		} else {
			want = append(want, id)
		}
	}

	var skipped strings.Builder
	skipOutput = &skipped
	t.Cleanup(func() { skipOutput = os.Stdout })

	var got []string
	err := each(context.Background(), ids,
		func(ctx context.Context, id string) (string, error) {
			if bad[id] {
				return "", errors.New("fail")
			}
			return id, nil
		},
		func(id string, v string) error {
			got = append(got, v)
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("each: got %d results, want %d in order", len(got), len(want))
	}
	wantSkipped := "skipping: m7: fail\nskipping: m57: fail\nskipping: m107: fail\nskipping: m157: fail\n" +
		"skipping: m207: fail\n"
	if skipped.String() != wantSkipped {
		t.Errorf("each: skipped %q, want %q", skipped.String(), wantSkipped)
	}

	stop := errors.New("stop")
	n := 0
	err = each(context.Background(), ids,
		func(ctx context.Context, id string) (string, error) { return id, nil },
		func(id string, v string) error {
			n++
			if n == 3 {
				return stop
			}
			return nil
		})
	if !errors.Is(err, stop) || n != 3 {
		t.Errorf("each stop: got err %v after %d", err, n)
	}
}

func TestRetryable(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&googleapi.Error{Code: 429}, true},
		{&googleapi.Error{Code: 503}, true},
		{fmt.Errorf("wrapped: %w", &googleapi.Error{Code: 500}), true},
		{&googleapi.Error{Code: 403, Errors: []googleapi.ErrorItem{{Reason: "userRateLimitExceeded"}}}, true},
		{&googleapi.Error{Code: 403, Errors: []googleapi.ErrorItem{{Reason: "insufficientPermissions"}}}, false},
		{&googleapi.Error{Code: 404}, false},
		{&googleapi.Error{Code: 400}, false},
		{errors.New("network"), false},
	}
	for _, c := range cases {
		if got := retryable(c.err); got != c.want {
			t.Errorf("retryable(%v): got %v, want %v", c.err, got, c.want)
		}
	}
}
