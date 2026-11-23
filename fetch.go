package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	msgformat "github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
)

const (
	fetchWorkers = 10
	fetchChunk   = 100
	maxRetries   = 5
	maxPageSize  = 500
)

var (
	retryDelay      = time.Second
	limiter         = rate.NewLimiter(40, 10)
	metadataHeaders = []string{"From", "Subject", "Date"}
)

func retryable(err error) bool {
	var gerr *googleapi.Error
	if !errors.As(err, &gerr) {
		return false
	}
	switch gerr.Code {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	case http.StatusForbidden:
		for _, e := range gerr.Errors {
			if e.Reason == "rateLimitExceeded" || e.Reason == "userRateLimitExceeded" {
				return true
			}
		}
	}
	return false
}

func retry[T any](ctx context.Context, call func() (T, error)) (T, error) {
	delay := retryDelay
	for attempt := 1; ; attempt++ {
		if err := limiter.Wait(ctx); err != nil {
			var zero T
			return zero, err
		}
		v, err := call()
		if err == nil || attempt == maxRetries || !retryable(err) {
			return v, err
		}
		if verbose {
			fmt.Printf("retrying in %s: %s\n", delay, err)
		}
		select {
		case <-ctx.Done():
			return v, ctx.Err()
		case <-time.After(delay + rand.N(delay/2+1)):
		}
		delay *= 2
	}
}

func listIDs(ctx context.Context, svc *gmail.Service, q string, limit int) ([]string, bool, error) {
	var ids []string
	var pageToken string
	for {
		call := svc.Users.Messages.List("me").Context(ctx)
		if q != "" {
			call = call.Q(q)
		}
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		n := maxPageSize
		if limit > 0 {
			n = min(n, limit-len(ids))
		}
		call = call.MaxResults(int64(n))

		resp, err := retry(ctx, func() (*gmail.ListMessagesResponse, error) { return call.Do() })
		if err != nil {
			return nil, false, err
		}
		for _, m := range resp.Messages {
			ids = append(ids, m.Id)
		}
		if resp.NextPageToken == "" || (limit > 0 && len(ids) >= limit) {
			return ids, resp.NextPageToken != "", nil
		}
		pageToken = resp.NextPageToken
	}
}

func decodeRaw(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

func getRaw(ctx context.Context, svc *gmail.Service, id string) ([]byte, error) {
	m, err := retry(ctx, func() (*gmail.Message, error) {
		return svc.Users.Messages.Get("me", id).Format("raw").Context(ctx).Do()
	})
	if err != nil {
		return nil, err
	}
	return decodeRaw(m.Raw)
}

func getMetadata(ctx context.Context, svc *gmail.Service, id string) (*gmail.Message, error) {
	return retry(ctx, func() (*gmail.Message, error) {
		return svc.Users.Messages.Get("me", id).Format("metadata").
			MetadataHeaders(metadataHeaders...).Context(ctx).Do()
	})
}

func header(m *gmail.Message, name string) string {
	if m.Payload == nil {
		return ""
	}
	for _, h := range m.Payload.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func parseMessage(raw []byte) (*msgformat.Entity, error) {
	m, err := msgformat.Read(bytes.NewReader(raw))
	if err != nil && !msgformat.IsUnknownCharset(err) && !msgformat.IsUnknownEncoding(err) {
		return nil, err
	}
	return m, nil
}

func each[T any](ctx context.Context, out io.Writer, ids []string,
	fetch func(ctx context.Context, id string) (T, error), fn func(id string, v T) error) error {

	for start := 0; start < len(ids); start += fetchChunk {
		chunk := ids[start:min(start+fetchChunk, len(ids))]
		vals := make([]T, len(chunk))
		errs := make([]error, len(chunk))

		var g errgroup.Group
		g.SetLimit(fetchWorkers)
		for i, id := range chunk {
			g.Go(func() error {
				vals[i], errs[i] = fetch(ctx, id)
				return nil
			})
		}
		g.Wait()
		if err := ctx.Err(); err != nil {
			return err
		}

		for i, id := range chunk {
			if errs[i] != nil {
				fmt.Fprintf(out, "skipping: %s: %s\n", id, errs[i])
				continue
			}
			if err := fn(id, vals[i]); err != nil {
				return err
			}
		}
	}
	return nil
}
