package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"google.golang.org/api/gmail/v1"
)

const getUsage = "[-format <format>] <id>... | <query>..."

type getOptions struct {
	format string
}

func getCmd(fs *flag.FlagSet) runFunc {
	var opts getOptions
	fs.StringVar(&opts.format, "format", "normal", "brief|normal|full|raw|parts|labels")
	return func(ctx context.Context, cfg *config, args []string) error {
		return get(ctx, cfg, opts, args)
	}
}

func formatLabels(ids []string, names map[string]string) string {
	var labels []string
	for _, id := range ids {
		if name, ok := names[id]; ok {
			labels = append(labels, name)
		} else {
			labels = append(labels, id)
		}
	}
	slices.Sort(labels)
	return strings.Join(labels, ", ")
}

func printMessage(w io.Writer, format string, raw []byte) error {
	if format == "raw" {
		_, err := w.Write(raw)
		return err
	}

	entity, err := parseMessage(raw)
	if err != nil {
		return err
	}
	msg, err := messageFromEntity(entity)
	if err != nil {
		return err
	}

	switch format {
	case "brief":
		msg.printHeaders(w, []string{"Date", "From"})
		fmt.Fprintln(w)
		fmt.Fprintln(w, msg.formatBody(true))

	case "normal":
		msg.printHeaders(w, []string{"Date", "From", "To", "CC", "BCC"})
		fmt.Fprintf(w, "Content-Type: %s\n", msg.header.Get("Content-Type"))
		if cte := msg.header.Get("Content-Transfer-Encoding"); cte != "" {
			fmt.Fprintf(w, "Content-Transfer-Encoding: %s\n", cte)
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, msg.formatBody(false))

	case "full":
		fields := msg.header.Fields()
		for fields.Next() {
			text, err := fields.Text()
			if err != nil {
				text = fields.Value()
			}
			fmt.Fprintf(w, "%s: %s\n", fields.Key(), text)
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, msg.formatBody(false))

	case "parts":
		msg.printContent(w)
	}
	return nil
}

func get(ctx context.Context, cfg *config, opts getOptions, args []string) error {
	if !slices.Contains([]string{"brief", "normal", "full", "raw", "parts", "labels"}, opts.format) {
		return errors.New("expected brief, normal, full, raw, parts, or labels for -format")
	}

	svc, ids, err := selectIDs(ctx, cfg, args, getUsage)
	if err != nil {
		return err
	}

	separator := func(id string) {
		if len(ids) > 1 {
			fmt.Fprintf(cfg.out, "--------------------------------[%s]--------------------------------\n",
				id)
		}
	}

	if opts.format == "labels" {
		names, err := labelNames(ctx, svc)
		if err != nil {
			return err
		}
		return each(ctx, cfg.out, ids,
			func(ctx context.Context, id string) (*gmail.Message, error) {
				return getMetadata(ctx, svc, id)
			},
			func(id string, m *gmail.Message) error {
				separator(id)
				fmt.Fprintf(cfg.out, "Date: %s\nFrom: %s\nSubject: %s\nLabels: %s\n", header(m, "Date"),
					decodeHeader(header(m, "From")), decodeHeader(header(m, "Subject")),
					formatLabels(m.LabelIds, names))
				return nil
			})
	}

	return each(ctx, cfg.out, ids,
		func(ctx context.Context, id string) ([]byte, error) {
			return getRaw(ctx, svc, id)
		},
		func(id string, raw []byte) error {
			separator(id)
			if err := printMessage(cfg.out, opts.format, raw); err != nil {
				fmt.Fprintf(cfg.out, "skipping: %s: %s\n", id, err)
			}
			return nil
		})
}
