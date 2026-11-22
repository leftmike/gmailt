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

var (
	listMax int
)

func listFlags(fs *flag.FlagSet) {
	fs.IntVar(&listMax, "n", 50, "maximum number of messages to list; 0 for no limit")
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

func list(cfg *config, args []string) {
	if listMax < 0 {
		fatal(errors.New("-n must be 0 or more"))
	}
	q := strings.Join(args, " ")
	if q == "" {
		q = "in:inbox"
	}

	ctx := context.Background()
	svc, err := cfg.newService(ctx)
	if err != nil {
		fatal(err)
	}

	ids, more, err := listIDs(ctx, svc, q, listMax)
	if err != nil {
		fatal(err)
	}

	cnt := 0
	err = each(ctx, ids,
		func(ctx context.Context, id string) (*gmail.Message, error) {
			return getMetadata(ctx, svc, id)
		},
		func(id string, m *gmail.Message) error {
			fmt.Println(formatListLine(m))
			cnt += 1
			return nil
		})
	if err != nil {
		fatal(err)
	}

	fmt.Printf("%d messages", cnt)
	if more {
		fmt.Print(" (more match; use -n to see more)")
	}
	fmt.Println()
}
