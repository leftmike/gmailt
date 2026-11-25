package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/api/gmail/v1"
)

type fakeMessage struct {
	id      string
	from    string
	subject string
	date    time.Time
	labels  []string
}

type fakeGmail struct {
	mu        sync.Mutex
	messages  []*fakeMessage
	labels    []*gmail.Label
	historyID uint64
	history   []*gmail.History
	failNext  int
	svc       *gmail.Service
}

func newFakeGmail(t *testing.T) *fakeGmail {
	t.Helper()
	day := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	f := &fakeGmail{
		historyID: 1000,
		messages: []*fakeMessage{
			{id: "000000000000000c", from: "Alice Smith <alice@example.com>", subject: "dinner Saturday",
				date: day.Add(2 * time.Hour), labels: []string{"INBOX", "UNREAD"}},
			{id: "000000000000000b", from: "GitHub <noreply@github.com>", subject: "New issue",
				date: day.Add(time.Hour), labels: []string{"INBOX", "STARRED"}},
			{id: "000000000000000a", from: "bob@example.com", subject: "receipt", date: day,
				labels: []string{"Label_1"}},
		},
		labels: []*gmail.Label{
			{Id: "INBOX", Name: "INBOX", Type: "system"},
			{Id: "UNREAD", Name: "UNREAD", Type: "system"},
			{Id: "STARRED", Name: "STARRED", Type: "system"},
			{Id: "TRASH", Name: "TRASH", Type: "system"},
			{Id: "Label_1", Name: "Receipts", Type: "user"},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /gmail/v1/users/me/profile", f.profile)
	mux.HandleFunc("GET /gmail/v1/users/me/messages", f.list)
	mux.HandleFunc("GET /gmail/v1/users/me/messages/{id}", f.get)
	mux.HandleFunc("POST /gmail/v1/users/me/messages/{id}/trash", f.trash)
	mux.HandleFunc("POST /gmail/v1/users/me/messages/{id}/untrash", f.trash)
	mux.HandleFunc("POST /gmail/v1/users/me/messages/batchModify", f.batchModify)
	mux.HandleFunc("GET /gmail/v1/users/me/labels", f.listLabels)
	mux.HandleFunc("GET /gmail/v1/users/me/labels/{id}", f.getLabel)
	mux.HandleFunc("POST /gmail/v1/users/me/labels", f.createLabel)
	mux.HandleFunc("GET /gmail/v1/users/me/history", f.listHistory)
	f.svc = newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.failNext > 0 {
			f.failNext--
			http.Error(w, `{"error":{"code":503,"message":"unavailable"}}`, http.StatusServiceUnavailable)
			return
		}
		mux.ServeHTTP(w, r)
	})
	return f
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (f *fakeGmail) message(id string) *fakeMessage {
	for _, m := range f.messages {
		if m.id == id {
			return m
		}
	}
	return nil
}

func (f *fakeGmail) labelsOf(id string) []string {
	return slices.Sorted(slices.Values(f.message(id).labels))
}

func (f *fakeGmail) labelID(name string) string {
	for _, l := range f.labels {
		if strings.EqualFold(l.Name, name) {
			return l.Id
		}
	}
	return ""
}

func (f *fakeGmail) matches(m *fakeMessage, q string) bool {
	if slices.Contains(m.labels, "TRASH") && !strings.Contains(q, "in:trash") {
		return false
	}
	for _, term := range strings.Fields(q) {
		key, val, _ := strings.Cut(term, ":")
		switch key {
		case "in", "label":
			if !slices.Contains(m.labels, f.labelID(val)) {
				return false
			}
		case "is":
			if !slices.Contains(m.labels, strings.ToUpper(val)) {
				return false
			}
		case "from":
			if !strings.Contains(m.from, val) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (f *fakeGmail) change(m *fakeMessage, add, remove []string) {
	var added, removed []string
	for _, l := range add {
		if !slices.Contains(m.labels, l) {
			m.labels = append(m.labels, l)
			added = append(added, l)
		}
	}
	for _, l := range remove {
		if i := slices.Index(m.labels, l); i >= 0 {
			m.labels = slices.Delete(m.labels, i, i+1)
			removed = append(removed, l)
		}
	}
	msg := &gmail.Message{Id: m.id}
	if len(added) > 0 {
		f.historyID++
		f.history = append(f.history, &gmail.History{Id: f.historyID,
			LabelsAdded: []*gmail.HistoryLabelAdded{{Message: msg, LabelIds: added}}})
	}
	if len(removed) > 0 {
		f.historyID++
		f.history = append(f.history, &gmail.History{Id: f.historyID,
			LabelsRemoved: []*gmail.HistoryLabelRemoved{{Message: msg, LabelIds: removed}}})
	}
}

func (f *fakeGmail) profile(w http.ResponseWriter, r *http.Request) {
	reply(w, gmail.Profile{EmailAddress: "me@example.com", HistoryId: f.historyID})
}

func (f *fakeGmail) list(w http.ResponseWriter, r *http.Request) {
	var ids []*gmail.Message
	for _, m := range f.messages {
		if f.matches(m, r.URL.Query().Get("q")) {
			ids = append(ids, &gmail.Message{Id: m.id})
		}
	}
	start, _ := strconv.Atoi(r.URL.Query().Get("pageToken"))
	size, _ := strconv.Atoi(r.URL.Query().Get("maxResults"))
	if size == 0 {
		size = 100
	}
	resp := gmail.ListMessagesResponse{Messages: ids[start:min(start+size, len(ids))]}
	if start+size < len(ids) {
		resp.NextPageToken = strconv.Itoa(start + size)
	}
	reply(w, resp)
}

func (f *fakeGmail) get(w http.ResponseWriter, r *http.Request) {
	m := f.message(r.PathValue("id"))
	if m == nil {
		http.Error(w, `{"error":{"code":404,"message":"Requested entity was not found."}}`,
			http.StatusNotFound)
		return
	}
	date := m.date.Format(time.RFC1123Z)
	resp := gmail.Message{Id: m.id, LabelIds: m.labels, InternalDate: m.date.UnixMilli()}
	if r.URL.Query().Get("format") == "raw" {
		raw := fmt.Sprintf("From: %s\r\nTo: me@example.com\r\nSubject: %s\r\nDate: %s\r\n"+
			"Content-Type: text/plain\r\n\r\nbody of %s\r\n", m.from, m.subject, date, m.id)
		resp.Raw = base64.URLEncoding.EncodeToString([]byte(raw))
	} else {
		resp.Payload = &gmail.MessagePart{Headers: []*gmail.MessagePartHeader{
			{Name: "From", Value: m.from}, {Name: "Subject", Value: m.subject}, {Name: "Date", Value: date},
		}}
	}
	reply(w, resp)
}

func (f *fakeGmail) trash(w http.ResponseWriter, r *http.Request) {
	m := f.message(r.PathValue("id"))
	if m == nil {
		http.Error(w, `{"error":{"code":404,"message":"Requested entity was not found."}}`,
			http.StatusNotFound)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/untrash") {
		f.change(m, nil, []string{"TRASH"})
	} else {
		f.change(m, []string{"TRASH"}, nil)
	}
	reply(w, gmail.Message{Id: m.id, LabelIds: m.labels})
}

func (f *fakeGmail) batchModify(w http.ResponseWriter, r *http.Request) {
	var req gmail.BatchModifyMessagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, id := range req.Ids {
		if m := f.message(id); m != nil {
			f.change(m, req.AddLabelIds, req.RemoveLabelIds)
		}
	}
}

func (f *fakeGmail) listLabels(w http.ResponseWriter, r *http.Request) {
	reply(w, gmail.ListLabelsResponse{Labels: f.labels})
}

func (f *fakeGmail) getLabel(w http.ResponseWriter, r *http.Request) {
	for _, l := range f.labels {
		if l.Id != r.PathValue("id") {
			continue
		}
		c := *l
		for _, m := range f.messages {
			if slices.Contains(m.labels, l.Id) {
				c.MessagesTotal++
				if slices.Contains(m.labels, "UNREAD") {
					c.MessagesUnread++
				}
			}
		}
		reply(w, c)
		return
	}
	http.NotFound(w, r)
}

func (f *fakeGmail) createLabel(w http.ResponseWriter, r *http.Request) {
	var l gmail.Label
	if err := json.NewDecoder(r.Body).Decode(&l); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	l.Id = fmt.Sprintf("Label_%d", len(f.labels)+1)
	l.Type = "user"
	f.labels = append(f.labels, &l)
	reply(w, l)
}

func (f *fakeGmail) listHistory(w http.ResponseWriter, r *http.Request) {
	start, _ := strconv.ParseUint(r.URL.Query().Get("startHistoryId"), 10, 64)
	var records []*gmail.History
	for _, h := range f.history {
		if h.Id > start {
			records = append(records, h)
		}
	}
	reply(w, gmail.ListHistoryResponse{History: records, HistoryId: f.historyID})
}

type testRun struct {
	t       *testing.T
	f       *fakeGmail
	history string
}

func TestMain(m *testing.M) {
	limiter = rate.NewLimiter(rate.Inf, 0)
	os.Exit(m.Run())
}

func newTestRun(t *testing.T) *testRun {
	return &testRun{t: t, f: newFakeGmail(t), history: filepath.Join(t.TempDir(), "history.jsonl")}
}

func (tr *testRun) run(args ...string) (string, string, error) {
	tr.t.Helper()
	var out, errOut bytes.Buffer
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(&errOut)
	run := cmds[args[0]].setup(fs)
	if err := fs.Parse(args[1:]); err != nil {
		return out.String(), errOut.String(), err
	}
	cfg := &config{
		History: tr.history,
		service: tr.f.svc,
		out:     &out,
		errOut:  &errOut,
		name:    "gmail " + args[0],
		program: "gmail",
		command: commandLine(args),
	}
	err := run(tr.t.Context(), cfg, fs.Args())
	return out.String(), errOut.String(), err
}

func (tr *testRun) ok(args ...string) string {
	tr.t.Helper()
	out, errOut, err := tr.run(args...)
	if err != nil || errOut != "" {
		tr.t.Fatalf("%v: %v, stderr %q", args, err, errOut)
	}
	return out
}

func (tr *testRun) fails(want string, args ...string) {
	tr.t.Helper()
	_, _, err := tr.run(args...)
	if err == nil || !strings.Contains(err.Error(), want) {
		tr.t.Errorf("%v: got error %v, want %q", args, err, want)
	}
}

func (tr *testRun) usage(args ...string) {
	tr.t.Helper()
	_, _, err := tr.run(args...)
	var u usageError
	if !errors.As(err, &u) {
		tr.t.Errorf("%v: got %v, want usage error", args, err)
	}
}

func lines(s ...string) string {
	return strings.Join(s, "\n") + "\n"
}

func TestListCommand(t *testing.T) {
	tr := newTestRun(t)
	when := func(h int) string {
		return time.Date(2026, 11, 20, 9+h, 0, 0, 0, time.UTC).Local().Format("2006-01-02 15:04")
	}

	got := tr.ok("list")
	want := lines(
		"* 000000000000000c  "+when(2)+"  Alice Smith           dinner Saturday",
		"  000000000000000b  "+when(1)+"  GitHub                New issue",
		"2 messages")
	if got != want {
		t.Errorf("list: got\n%s\nwant\n%s", got, want)
	}

	if got := tr.ok("list", "-n", "1", "label:receipts"); !strings.Contains(got, "000000000000000a") ||
		!strings.HasSuffix(got, "1 messages\n") {
		t.Errorf("list label:receipts: got %q", got)
	}
	got = tr.ok("list", "-n", "1")
	if !strings.HasSuffix(got, "1 messages (more match; use -n to see more)\n") {
		t.Errorf("list -n 1: got %q", got)
	}
	if got := tr.ok("list", "from:nobody"); got != "0 messages\n" {
		t.Errorf("list no match: got %q", got)
	}
	tr.fails("-n must be 0 or more", "list", "-n", "-1")
}

func TestGetCommand(t *testing.T) {
	tr := newTestRun(t)

	got := tr.ok("get", "-format", "brief", "000000000000000a")
	if !strings.HasPrefix(got, "Date: ") ||
		!strings.Contains(got, "From: bob@example.com\nSubject: receipt\n\n") ||
		!strings.HasSuffix(got, "body of 000000000000000a\n") {
		t.Errorf("get brief: got %q", got)
	}

	got = tr.ok("get", "-format", "labels", "in:inbox")
	if !strings.Contains(got, "[000000000000000c]") || !strings.Contains(got, "[000000000000000b]") ||
		!strings.Contains(got, "Labels: INBOX, UNREAD\n") || !strings.Contains(got, "Labels: INBOX, STARRED\n") {
		t.Errorf("get labels: got %q", got)
	}

	got = tr.ok("get", "-format", "raw", "000000000000000a")
	if !strings.HasPrefix(got, "From: bob@example.com\r\n") {
		t.Errorf("get raw: got %q", got)
	}

	out, errOut, err := tr.run("get", "000000000000000a", "0000000000000fff")
	if err == nil || err.Error() != "1 of 2 failed" || !strings.Contains(out, "body of 000000000000000a") ||
		strings.Contains(out, "skipping") || !strings.HasPrefix(errOut, "skipping: 0000000000000fff: ") {
		t.Errorf("get missing: got %v, stdout %q, stderr %q", err, out, errOut)
	}

	tr.usage("get")
	tr.fails("expected brief", "get", "-format", "nope", "000000000000000a")
	tr.fails("not both", "get", "000000000000000a", "in:inbox")
}

func TestArchiveUndo(t *testing.T) {
	tr := newTestRun(t)

	if got := tr.ok("archive", "in:inbox"); got != "2 messages archived\n" {
		t.Errorf("archive: got %q", got)
	}
	if got := tr.f.labelsOf("000000000000000c"); !slices.Equal(got, []string{"UNREAD"}) {
		t.Errorf("archived labels: got %v", got)
	}
	if got := tr.ok("history"); !strings.Contains(got, "     2  archive in:inbox\n") ||
		strings.Contains(got, "undone") {
		t.Errorf("history: got %q", got)
	}

	want := lines("2 messages restored: +INBOX", "undid 1: archive in:inbox")
	if got := tr.ok("undo"); got != want {
		t.Errorf("undo: got %q, want %q", got, want)
	}
	if got := tr.f.labelsOf("000000000000000c"); !slices.Equal(got, []string{"INBOX", "UNREAD"}) {
		t.Errorf("restored labels: got %v", got)
	}
	if got := tr.ok("history"); !strings.HasSuffix(got, "archive in:inbox  (undone)\n") {
		t.Errorf("history after undo: got %q", got)
	}

	tr.fails("nothing to undo", "undo")
	tr.fails("1 is already undone", "undo", "1")
	tr.fails("no such history entry: 9", "undo", "9")
	tr.usage("undo", "1", "2")
	tr.usage("archive")
	tr.fails("empty query", "archive", " ")
	tr.fails("empty query", "delete", "")
	if got := tr.f.labelsOf("000000000000000c"); !slices.Equal(got, []string{"INBOX", "UNREAD"}) {
		t.Errorf("labels changed by empty query: %v", got)
	}
}

func TestLabelUndo(t *testing.T) {
	tr := newTestRun(t)

	got := tr.ok("label", "+starred", "^unread", "in:inbox")
	if got != "1 messages updated: +STARRED ^UNREAD\n" {
		t.Errorf("label: got %q", got)
	}
	if got := tr.ok("history"); !strings.Contains(got, "     1  label +starred ^unread in:inbox\n") {
		t.Errorf("history: got %q", got)
	}

	got = tr.ok("undo")
	want := lines("1 messages restored: +UNREAD ^STARRED", "undid 1: label +starred ^unread in:inbox")
	if got != want {
		t.Errorf("undo: got %q, want %q", got, want)
	}
	if got := tr.f.labelsOf("000000000000000b"); !slices.Equal(got, []string{"INBOX", "STARRED"}) {
		t.Errorf("already-starred message changed by undo: %v", got)
	}
	if got := tr.f.labelsOf("000000000000000c"); !slices.Equal(got, []string{"INBOX", "UNREAD"}) {
		t.Errorf("undo didn't restore: %v", got)
	}

	tr.fails("no such label: Work (use -create to create it)", "label", "+Work", "000000000000000a")
	tr.fails("no such label: Work", "label", "^Work", "000000000000000a")
	tr.fails("label both added and removed: inbox", "label", "+INBOX", "^inbox", "000000000000000a")
	tr.usage("label", "in:inbox")
	tr.usage("label", "+starred")

	got = tr.ok("label", "-create", "+Work", "000000000000000a")
	if got != lines("created label Work", "1 messages updated: +Work") {
		t.Errorf("label -create: got %q", got)
	}
	got = tr.ok("undo")
	if !strings.Contains(got, "1 messages restored: ^Work\n") ||
		!strings.Contains(got, "label Work was created by this command and is left in place\n") {
		t.Errorf("undo -create: got %q", got)
	}
}

func TestDeleteUndo(t *testing.T) {
	tr := newTestRun(t)

	out, errOut, err := tr.run("delete", "000000000000000a", "0000000000000fff")
	if err == nil || err.Error() != "1 of 2 failed" ||
		out != "deleted 000000000000000a\n1 messages moved to the trash\n" ||
		!strings.HasPrefix(errOut, "skipping: 0000000000000fff: ") {
		t.Errorf("delete: got %v, stdout %q, stderr %q", err, out, errOut)
	}
	if got := tr.ok("list", "label:receipts"); got != "0 messages\n" {
		t.Errorf("list after delete: got %q", got)
	}

	if got := tr.ok("undo"); !strings.HasPrefix(got, "1 messages restored: ^TRASH\n") {
		t.Errorf("undo: got %q", got)
	}
	if got := tr.f.labelsOf("000000000000000a"); !slices.Equal(got, []string{"Label_1"}) {
		t.Errorf("untrashed labels: got %v", got)
	}
}

func TestUndoMissingMessage(t *testing.T) {
	tr := newTestRun(t)

	tr.ok("delete", "000000000000000a", "000000000000000b")
	tr.f.messages = slices.DeleteFunc(tr.f.messages, func(m *fakeMessage) bool {
		return m.id == "000000000000000a"
	})

	out, errOut, err := tr.run("undo")
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if errOut != "gmail undo: warning: 000000000000000a no longer exists\n" {
		t.Errorf("undo stderr: got %q", errOut)
	}
	if !strings.HasPrefix(out, "1 messages restored: ^TRASH\n") {
		t.Errorf("undo: got %q", out)
	}
	if got := tr.f.labelsOf("000000000000000b"); !slices.Equal(got, []string{"INBOX", "STARRED"}) {
		t.Errorf("untrashed labels: got %v", got)
	}
	if got := tr.ok("history"); !strings.HasSuffix(got, "(undone)\n") {
		t.Errorf("history after undo: got %q", got)
	}
}

func TestVerboseRetry(t *testing.T) {
	tr := newTestRun(t)
	verbose, retryDelay = true, time.Millisecond
	t.Cleanup(func() { verbose, retryDelay = false, time.Second })

	tr.f.failNext = 1
	out, errOut, err := tr.run("labels")
	if err != nil || !strings.Contains(out, "Receipts") {
		t.Fatalf("labels: %v, stdout %q", err, out)
	}
	if !strings.HasPrefix(errOut, "retrying in ") || !strings.Contains(errOut, "503") {
		t.Errorf("labels stderr: got %q", errOut)
	}
}

func TestUndoDamagedHistory(t *testing.T) {
	tr := newTestRun(t)

	tr.ok("archive", "in:inbox")
	f, err := os.OpenFile(tr.history, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"n":2,"ti`)
	f.Close()

	warning := "gmail history: warning: " + tr.history + ": skipped 1 unreadable lines\n"
	out, errOut, err := tr.run("history")
	if err != nil || errOut != warning || !strings.HasSuffix(out, "archive in:inbox\n") {
		t.Errorf("history: got %v, stdout %q, stderr %q", err, out, errOut)
	}

	out, errOut, err = tr.run("undo")
	if err != nil || !strings.Contains(errOut, "skipped 1 unreadable lines") ||
		!strings.HasSuffix(out, "undid 1: archive in:inbox\n") {
		t.Errorf("undo: got %v, stdout %q, stderr %q", err, out, errOut)
	}
	if got := tr.ok("history"); !strings.HasSuffix(got, "archive in:inbox  (undone)\n") {
		t.Errorf("history after undo: got %q", got)
	}
}

func TestChangedCounts(t *testing.T) {
	tr := newTestRun(t)

	if got := tr.ok("label", "+starred", "000000000000000c", "0000000000000fff"); got !=
		"1 messages updated: +STARRED\n" {
		t.Errorf("label: got %q", got)
	}
	if got := tr.ok("label", "+starred", "000000000000000c"); got != "0 messages updated: +STARRED\n" {
		t.Errorf("label again: got %q", got)
	}
	if got := tr.ok("archive", "000000000000000a", "000000000000000b", "0000000000000fff"); got !=
		"1 messages archived\n" {
		t.Errorf("archive: got %q", got)
	}
}

func TestLabelsCommand(t *testing.T) {
	tr := newTestRun(t)
	want := lines(
		"messages   unread  label",
		"       2        1  INBOX",
		"       1        0  STARRED",
		"       0        0  TRASH",
		"       1        1  UNREAD",
		"       1        0  Receipts")
	if got := tr.ok("labels"); got != want {
		t.Errorf("labels: got\n%s\nwant\n%s", got, want)
	}
	tr.usage("labels", "x")
}

func TestHistoryEmpty(t *testing.T) {
	tr := newTestRun(t)
	if got := tr.ok("history"); got != "" {
		t.Errorf("history: got %q", got)
	}
	tr.usage("history", "x")
	tr.fails("nothing to undo", "undo")
}

func TestHistoryWarning(t *testing.T) {
	tr := newTestRun(t)
	tr.history = filepath.Join(t.TempDir(), "missing", "dir", "\x00bad")
	out, errOut, err := tr.run("archive", "000000000000000c")
	if err != nil || out != "1 messages archived\n" ||
		!strings.HasPrefix(errOut, "gmail archive: warning: not saved to history: ") {
		t.Errorf("archive: got %q, %q, %v", out, errOut, err)
	}
}

func TestConfigCommand(t *testing.T) {
	tr := newTestRun(t)
	if got := tr.ok("config"); !strings.Contains(got, "history:     "+tr.history+"\n") {
		t.Errorf("config: got %q", got)
	}
	tr.usage("config", "x")
	tr.usage("auth", "x")
}
