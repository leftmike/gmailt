package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"google.golang.org/api/gmail/v1"
)

func isID(s string) bool {
	if len(s) != 16 {
		return false
	}
	return strings.Trim(strings.ToLower(s), "0123456789abcdef") == ""
}

func parseIDArgs(args []string) ([]string, string, error) {
	var ids []string
	for _, arg := range args {
		if isID(arg) {
			ids = append(ids, strings.ToLower(arg))
		}
	}
	switch len(ids) {
	case len(args):
		return ids, "", nil
	case 0:
		return nil, strings.Join(args, " "), nil
	default:
		return nil, "", errors.New("arguments must be all message ids or a query, not both")
	}
}

func selectIDs(ctx context.Context, cfg *config, args []string,
	usage string) (*gmail.Service, []string) {

	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "usage: %s %s %s\n", os.Args[0], os.Args[1], usage)
		os.Exit(1)
	}
	ids, query, err := parseIDArgs(args)
	if err != nil {
		fatal(err)
	}

	svc, err := cfg.newService(ctx)
	if err != nil {
		fatal(err)
	}
	if query != "" {
		ids, _, err = listIDs(ctx, svc, query, 0)
		if err != nil {
			fatal(err)
		}
	}
	return svc, ids
}
