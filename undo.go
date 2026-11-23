package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
)

const historyDays = 30

type labelChange struct {
	IDs     []string `json:"ids"`
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

type historyEntry struct {
	N       int           `json:"n,omitempty"`
	Time    time.Time     `json:"time,omitzero"`
	Command string        `json:"command,omitempty"`
	Changes []labelChange `json:"changes,omitempty"`
	Created []string      `json:"created,omitempty"`
	Undone  int           `json:"undone,omitempty"`
}

func (e historyEntry) messageCount() int {
	ids := map[string]bool{}
	for _, c := range e.Changes {
		for _, id := range c.IDs {
			ids[id] = true
		}
	}
	return len(ids)
}

func readHistory(path string) ([]historyEntry, map[int]bool, error) {
	undone := map[int]bool{}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, undone, nil
	} else if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	var entries []historyEntry
	dec := json.NewDecoder(f)
	for {
		var e historyEntry
		err := dec.Decode(&e)
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", path, err)
		}
		if e.Undone != 0 {
			undone[e.Undone] = true
		} else {
			entries = append(entries, e)
		}
	}
	return entries, undone, nil
}

func writeHistory(path string, flags int, records []historyEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

func appendHistory(path string, e historyEntry, now time.Time) (int, error) {
	entries, undone, err := readHistory(path)
	if err != nil {
		return 0, err
	}
	for _, old := range entries {
		e.N = max(e.N, old.N)
	}
	e.N += 1
	e.Time = now

	cutoff := now.AddDate(0, 0, -historyDays)
	if !slices.ContainsFunc(entries, func(old historyEntry) bool { return old.Time.Before(cutoff) }) {
		return e.N, writeHistory(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, []historyEntry{e})
	}

	var keep []historyEntry
	for _, old := range entries {
		if old.Time.Before(cutoff) {
			continue
		}
		keep = append(keep, old)
		if undone[old.N] {
			keep = append(keep, historyEntry{Undone: old.N})
		}
	}
	tmp := path + ".tmp"
	if err := writeHistory(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, append(keep, e)); err != nil {
		return 0, err
	}
	return e.N, os.Rename(tmp, path)
}

func netChanges(records []*gmail.History, ids []string) []labelChange {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	type delta struct{ added, removed map[string]bool }
	deltas := map[string]*delta{}
	apply := func(m *gmail.Message, labels []string, add bool) {
		if m == nil || !want[m.Id] {
			return
		}
		d, ok := deltas[m.Id]
		if !ok {
			d = &delta{added: map[string]bool{}, removed: map[string]bool{}}
			deltas[m.Id] = d
		}
		to, from := d.added, d.removed
		if !add {
			to, from = from, to
		}
		for _, l := range labels {
			if from[l] {
				delete(from, l)
			} else {
				to[l] = true
			}
		}
	}
	for _, h := range records {
		for _, la := range h.LabelsAdded {
			apply(la.Message, la.LabelIds, true)
		}
		for _, lr := range h.LabelsRemoved {
			apply(lr.Message, lr.LabelIds, false)
		}
	}

	groups := map[string]*labelChange{}
	for id, d := range deltas {
		if len(d.added) == 0 && len(d.removed) == 0 {
			continue
		}
		added := slices.Sorted(maps.Keys(d.added))
		removed := slices.Sorted(maps.Keys(d.removed))
		key := strings.Join(added, ",") + "|" + strings.Join(removed, ",")
		g, ok := groups[key]
		if !ok {
			g = &labelChange{Added: added, Removed: removed}
			groups[key] = g
		}
		g.IDs = append(g.IDs, id)
	}

	var changes []labelChange
	for _, key := range slices.Sorted(maps.Keys(groups)) {
		g := groups[key]
		slices.Sort(g.IDs)
		changes = append(changes, *g)
	}
	return changes
}

func listHistory(ctx context.Context, svc *gmail.Service, start uint64) ([]*gmail.History, error) {
	var records []*gmail.History
	var pageToken string
	for {
		resp, err := retry(ctx, func() (*gmail.ListHistoryResponse, error) {
			call := svc.Users.History.List("me").StartHistoryId(start).
				HistoryTypes("labelAdded", "labelRemoved").MaxResults(maxPageSize).Context(ctx)
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}
			return call.Do()
		})
		if err != nil {
			return nil, err
		}
		records = append(records, resp.History...)
		if resp.NextPageToken == "" {
			return records, nil
		}
		pageToken = resp.NextPageToken
	}
}

func commandLine(args []string) string {
	var words []string
	for _, arg := range args {
		if arg == "" || strings.ContainsAny(arg, " \t\"'\\") {
			arg = strconv.Quote(arg)
		}
		words = append(words, arg)
	}
	return strings.Join(words, " ")
}

func recordChanges(ctx context.Context, cfg *config, svc *gmail.Service, ids, created []string,
	change func() error) error {

	profile, err := retry(ctx, func() (*gmail.Profile, error) {
		return svc.Users.GetProfile("me").Context(ctx).Do()
	})
	changeErr := change()
	if err != nil {
		warn(cfg, fmt.Errorf("not saved to history: %w", err))
		return changeErr
	}

	records, err := listHistory(ctx, svc, profile.HistoryId)
	if err == nil {
		changes := netChanges(records, ids)
		if len(changes) > 0 || len(created) > 0 {
			_, err = appendHistory(cfg.History, historyEntry{
				Command: cfg.command,
				Changes: changes,
				Created: created,
			}, time.Now())
		}
	}
	if err != nil {
		warn(cfg, fmt.Errorf("not saved to history: %w", err))
	}
	return changeErr
}

func notFound(err error) bool {
	var gerr *googleapi.Error
	return errors.As(err, &gerr) && gerr.Code == http.StatusNotFound
}

func perMessage(ctx context.Context, cfg *config, ids []string,
	call func(ctx context.Context, id string) (*gmail.Message, error)) ([]string, error) {

	var found []string
	gone := 0
	err := each(ctx, cfg.out, ids,
		func(ctx context.Context, id string) (*gmail.Message, error) {
			m, err := call(ctx, id)
			if notFound(err) {
				return nil, nil
			}
			return m, err
		},
		func(id string, m *gmail.Message) error {
			if m == nil {
				warn(cfg, fmt.Errorf("%s no longer exists", id))
				gone += 1
			} else {
				found = append(found, id)
			}
			return nil
		})
	if err != nil {
		return nil, err
	}
	if n := len(ids) - len(found) - gone; n > 0 {
		return nil, fmt.Errorf("%d of %d messages failed", n, len(ids))
	}
	return found, nil
}

func without(labels []string, label string) []string {
	return slices.DeleteFunc(slices.Clone(labels), func(l string) bool { return l == label })
}

func revertChange(ctx context.Context, cfg *config, svc *gmail.Service, c labelChange) ([]string, error) {
	ids := c.IDs
	add, remove := c.Removed, c.Added
	var err error
	if slices.Contains(remove, "TRASH") {
		ids, err = perMessage(ctx, cfg, ids, func(ctx context.Context, id string) (*gmail.Message, error) {
			return retry(ctx, func() (*gmail.Message, error) {
				return svc.Users.Messages.Untrash("me", id).Context(ctx).Do()
			})
		})
		if err != nil {
			return nil, err
		}
		remove = without(remove, "TRASH")
	}
	if slices.Contains(add, "TRASH") {
		ids, err = perMessage(ctx, cfg, ids, func(ctx context.Context, id string) (*gmail.Message, error) {
			return trashMessage(ctx, svc, id)
		})
		if err != nil {
			return nil, err
		}
		add = without(add, "TRASH")
	}
	if len(ids) == 0 || (len(add) == 0 && len(remove) == 0) {
		return ids, nil
	}
	return ids, modifyLabels(ctx, svc, ids, add, remove)
}

func findUndo(entries []historyEntry, undone map[int]bool, args []string) (*historyEntry, error) {
	if len(args) == 0 {
		for i := len(entries) - 1; i >= 0; i-- {
			if !undone[entries[i].N] {
				return &entries[i], nil
			}
		}
		return nil, errors.New("nothing to undo")
	}

	n, err := strconv.Atoi(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad history number: %s", args[0])
	}
	for i := range entries {
		if entries[i].N == n {
			if undone[n] {
				return nil, fmt.Errorf("%d is already undone", n)
			}
			return &entries[i], nil
		}
	}
	return nil, fmt.Errorf("no such history entry: %d", n)
}

func namedLabels(names map[string]string, ids []string) []*gmail.Label {
	var labels []*gmail.Label
	for _, id := range ids {
		name, ok := names[id]
		if !ok {
			name = id
		}
		labels = append(labels, &gmail.Label{Id: id, Name: name})
	}
	return labels
}

func undo(ctx context.Context, cfg *config, args []string) error {
	if len(args) > 1 {
		return usageError("[<n>]")
	}
	entries, undone, err := readHistory(cfg.History)
	if err != nil {
		return err
	}
	e, err := findUndo(entries, undone, args)
	if err != nil {
		return err
	}

	svc, err := cfg.newService(ctx)
	if err != nil {
		return err
	}
	names, err := labelNames(ctx, svc)
	if err != nil {
		return err
	}

	for _, c := range e.Changes {
		ids, err := revertChange(ctx, cfg, svc, c)
		if err != nil {
			return err
		}
		fmt.Fprintf(cfg.out, "%d messages restored: %s\n", len(ids),
			describeChanges(namedLabels(names, c.Removed), namedLabels(names, c.Added)))
	}
	for _, name := range e.Created {
		fmt.Fprintf(cfg.out, "label %s was created by this command and is left in place\n", name)
	}
	err = writeHistory(cfg.History, os.O_WRONLY|os.O_CREATE|os.O_APPEND,
		[]historyEntry{{Undone: e.N}})
	if err != nil {
		return err
	}
	fmt.Fprintf(cfg.out, "undid %d: %s\n", e.N, e.Command)
	return nil
}

func formatHistoryLine(e historyEntry, undone bool) string {
	s := fmt.Sprintf("%4d  %s  %6d  %s", e.N, e.Time.Local().Format("2006-01-02 15:04"),
		e.messageCount(), e.Command)
	if undone {
		s += "  (undone)"
	}
	return s
}

func history(ctx context.Context, cfg *config, args []string) error {
	if len(args) != 0 {
		return usageError("")
	}
	entries, undone, err := readHistory(cfg.History)
	if err != nil {
		return err
	}
	for _, e := range slices.Backward(entries) {
		fmt.Fprintln(cfg.out, formatHistoryLine(e, undone[e.N]))
	}
	return nil
}
