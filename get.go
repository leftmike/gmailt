package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"google.golang.org/api/gmail/v1"
)

var (
	format = "normal"
)

func getFlags(fs *flag.FlagSet) {
	fs.StringVar(&format, "format", "normal", "brief|normal|full|raw|parts|labels")
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

func printMessage(raw []byte) error {
	if format == "raw" {
		os.Stdout.Write(raw)
		return nil
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
		msg.printHeaders([]string{"Date", "From"})
		fmt.Println()
		fmt.Println(msg.formatBody(true))

	case "normal":
		msg.printHeaders([]string{"Date", "From", "To", "CC", "BCC"})
		fmt.Printf("Content-Type: %s\n", msg.header.Get("Content-Type"))
		if cte := msg.header.Get("Content-Transfer-Encoding"); cte != "" {
			fmt.Printf("Content-Transfer-Encoding: %s\n", cte)
		}
		fmt.Println()
		fmt.Println(msg.formatBody(false))

	case "full":
		fields := msg.header.Fields()
		for fields.Next() {
			text, err := fields.Text()
			if err != nil {
				text = fields.Value()
			}
			fmt.Printf("%s: %s\n", fields.Key(), text)
		}
		fmt.Println()
		fmt.Println(msg.formatBody(false))

	case "parts":
		msg.printContent()
	}
	return nil
}

func get(cfg *config, args []string) {
	if !slices.Contains([]string{"brief", "normal", "full", "raw", "parts", "labels"}, format) {
		fmt.Fprintf(os.Stderr,
			"usage: %s %s: expected brief, normal, full, raw, parts, or labels for format\n",
			os.Args[0], os.Args[1])
		os.Exit(1)
	}

	ctx := context.Background()
	svc, ids := selectIDs(ctx, cfg, args, "[-format <format>] <id>... | <query>...")

	separator := func(id string) {
		if len(ids) > 1 {
			fmt.Printf("--------------------------------[%s]--------------------------------\n", id)
		}
	}

	if format == "labels" {
		names, err := labelNames(ctx, svc)
		if err != nil {
			fatal(err)
		}
		err = each(ctx, ids,
			func(ctx context.Context, id string) (*gmail.Message, error) {
				return getMetadata(ctx, svc, id)
			},
			func(id string, m *gmail.Message) error {
				separator(id)
				fmt.Printf("Date: %s\nFrom: %s\nSubject: %s\nLabels: %s\n", header(m, "Date"),
					decodeHeader(header(m, "From")), decodeHeader(header(m, "Subject")),
					formatLabels(m.LabelIds, names))
				return nil
			})
		if err != nil {
			fatal(err)
		}
		return
	}

	err := each(ctx, ids,
		func(ctx context.Context, id string) ([]byte, error) {
			return getRaw(ctx, svc, id)
		},
		func(id string, raw []byte) error {
			separator(id)
			if err := printMessage(raw); err != nil {
				fmt.Printf("skipping: %s: %s\n", id, err)
			}
			return nil
		})
	if err != nil {
		fatal(err)
	}
}
