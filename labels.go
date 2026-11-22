package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"google.golang.org/api/gmail/v1"
)

func listLabels(ctx context.Context, svc *gmail.Service) ([]*gmail.Label, error) {
	resp, err := retry(ctx, func() (*gmail.ListLabelsResponse, error) {
		return svc.Users.Labels.List("me").Context(ctx).Do()
	})
	if err != nil {
		return nil, err
	}
	return resp.Labels, nil
}

func labelNames(ctx context.Context, svc *gmail.Service) (map[string]string, error) {
	all, err := listLabels(ctx, svc)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, l := range all {
		names[l.Id] = l.Name
	}
	return names, nil
}

func sortLabels(all []*gmail.Label) {
	slices.SortFunc(all, func(a, b *gmail.Label) int {
		if (a.Type == "system") != (b.Type == "system") {
			if a.Type == "system" {
				return -1
			}
			return 1
		}
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
}

func labels(cfg *config, args []string) {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "usage: %s %s\n", os.Args[0], os.Args[1])
		os.Exit(1)
	}

	ctx := context.Background()
	svc, err := cfg.newService(ctx)
	if err != nil {
		fatal(err)
	}

	all, err := listLabels(ctx, svc)
	if err != nil {
		fatal(err)
	}
	sortLabels(all)
	var ids []string
	for _, l := range all {
		ids = append(ids, l.Id)
	}

	fmt.Printf("%8s %8s  %s\n", "messages", "unread", "label")
	err = each(ctx, ids,
		func(ctx context.Context, id string) (*gmail.Label, error) {
			return retry(ctx, func() (*gmail.Label, error) {
				return svc.Users.Labels.Get("me", id).Context(ctx).Do()
			})
		},
		func(id string, l *gmail.Label) error {
			fmt.Printf("%8d %8d  %s\n", l.MessagesTotal, l.MessagesUnread, l.Name)
			return nil
		})
	if err != nil {
		fatal(err)
	}
}
