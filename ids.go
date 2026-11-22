package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"google.golang.org/api/gmail/v1"
)

var (
	idQuery string
)

func idFlags(fs *flag.FlagSet) {
	fs.StringVar(&idQuery, "q", "", "select messages with a Gmail search query instead of ids")
}

func parseIDs(args []string) ([]string, error) {
	var ids []string
	for _, arg := range args {
		if arg == "" || strings.Trim(strings.ToLower(arg), "0123456789abcdef") != "" {
			return nil, fmt.Errorf("invalid message id: %s", arg)
		}
		ids = append(ids, strings.ToLower(arg))
	}
	return ids, nil
}

func selectIDs(ctx context.Context, cfg *config, args []string,
	usage string) (*gmail.Service, []string) {

	if idQuery != "" && len(args) > 0 {
		fatal(errors.New("give message ids or -q, not both"))
	}
	if idQuery == "" && len(args) == 0 {
		fmt.Fprintf(os.Stderr, "usage: %s %s %s\n", os.Args[0], os.Args[1], usage)
		os.Exit(1)
	}
	ids, err := parseIDs(args)
	if err != nil {
		fatal(err)
	}

	svc, err := cfg.newService(ctx)
	if err != nil {
		fatal(err)
	}
	if idQuery != "" {
		ids, _, err = listIDs(ctx, svc, idQuery, 0)
		if err != nil {
			fatal(err)
		}
	}
	return svc, ids
}
