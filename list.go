package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"mime"
	"net/mail"
	"slices"
	"strings"
	"time"

	msgformat "github.com/emersion/go-message"
	"google.golang.org/api/gmail/v1"
)

type listOptions struct {
	max int
}

func listCmd(fs *flag.FlagSet) runFunc {
	var opts listOptions
	fs.IntVar(&opts.max, "n", 50, "maximum number of messages to list; 0 for no limit")
	return func(ctx context.Context, cfg *config, args []string) error {
		return list(ctx, cfg, opts, args)
	}
}

var wordDecoder = &mime.WordDecoder{CharsetReader: msgformat.CharsetReader}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func fit(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s + strings.Repeat(" ", n-len(r))
}

func decodeHeader(s string) string {
	if d, err := wordDecoder.DecodeHeader(s); err == nil {
		s = d
	}
	return oneLine(s)
}

func sender(from string) string {
	addr, err := (&mail.AddressParser{WordDecoder: wordDecoder}).Parse(from)
	if err != nil {
		return decodeHeader(from)
	}
	if addr.Name != "" {
		return oneLine(addr.Name)
	}
	return addr.Address
}

func formatListLine(m *gmail.Message) string {
	mark := " "
	if slices.Contains(m.LabelIds, "UNREAD") {
		mark = "*"
	}
	date := time.UnixMilli(m.InternalDate).Local().Format("2006-01-02 15:04")
	return fmt.Sprintf("%s %-16s  %s  %s  %s", mark, m.Id, date, fit(sender(header(m, "From")), 20),
		decodeHeader(header(m, "Subject")))
}

func listQuery(args []string) string {
	query := strings.TrimSpace(strings.Join(args, " "))
	if query == "" {
		return "in:inbox"
	}
	return query
}

func list(ctx context.Context, cfg *config, opts listOptions, args []string) error {
	if opts.max < 0 {
		return errors.New("-n must be 0 or more")
	}
	svc, err := cfg.newService(ctx)
	if err != nil {
		return err
	}

	ids, more, err := listIDs(ctx, svc, listQuery(args), opts.max)
	if err != nil {
		return err
	}

	cnt := 0
	err = each(ctx, cfg.errOut, ids,
		func(ctx context.Context, id string) (*gmail.Message, error) {
			return getMetadata(ctx, svc, id)
		},
		func(id string, m *gmail.Message) error {
			fmt.Fprintln(cfg.out, formatListLine(m))
			cnt += 1
			return nil
		})

	fmt.Fprintf(cfg.out, "%d messages", cnt)
	if more {
		fmt.Fprint(cfg.out, " (more match; use -n to see more)")
	}
	fmt.Fprintln(cfg.out)
	return err
}
