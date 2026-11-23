package main

import (
	"cmp"
	"context"
	"fmt"
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

func resolveLabels(all []*gmail.Label, names []string) ([]*gmail.Label, []string) {
	byName := map[string]*gmail.Label{}
	for _, l := range all {
		byName[strings.ToLower(l.Name)] = l
	}
	var found []*gmail.Label
	var missing []string
	for _, name := range names {
		if l, ok := byName[strings.ToLower(name)]; ok {
			found = append(found, l)
		} else {
			missing = append(missing, name)
		}
	}
	return found, missing
}

func createLabel(ctx context.Context, svc *gmail.Service, name string) (*gmail.Label, error) {
	return retry(ctx, func() (*gmail.Label, error) {
		return svc.Users.Labels.Create("me", &gmail.Label{
			Name:                  name,
			LabelListVisibility:   "labelShow",
			MessageListVisibility: "show",
		}).Context(ctx).Do()
	})
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

func labels(ctx context.Context, cfg *config, args []string) error {
	if len(args) != 0 {
		return usageError("")
	}

	svc, err := cfg.newService(ctx)
	if err != nil {
		return err
	}

	all, err := listLabels(ctx, svc)
	if err != nil {
		return err
	}
	sortLabels(all)
	var ids []string
	for _, l := range all {
		ids = append(ids, l.Id)
	}

	fmt.Fprintf(cfg.out, "%8s %8s  %s\n", "messages", "unread", "label")
	return each(ctx, cfg.errOut, ids,
		func(ctx context.Context, id string) (*gmail.Label, error) {
			return retry(ctx, func() (*gmail.Label, error) {
				return svc.Users.Labels.Get("me", id).Context(ctx).Do()
			})
		},
		func(id string, l *gmail.Label) error {
			fmt.Fprintf(cfg.out, "%8d %8d  %s\n", l.MessagesTotal, l.MessagesUnread, l.Name)
			return nil
		})
}
