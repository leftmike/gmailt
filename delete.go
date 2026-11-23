package main

import (
	"context"
	"fmt"

	"google.golang.org/api/gmail/v1"
)

func trashMessage(ctx context.Context, svc *gmail.Service, id string) (*gmail.Message, error) {
	return retry(ctx, func() (*gmail.Message, error) {
		return svc.Users.Messages.Trash("me", id).Context(ctx).Do()
	})
}

func trash(ctx context.Context, cfg *config, args []string) error {
	svc, ids, err := selectIDs(ctx, cfg, args, "<id>... | <query>...")
	if err != nil {
		return err
	}

	cnt := 0
	err = recordChanges(ctx, cfg, svc, ids, nil, func() error {
		return each(ctx, cfg.out, ids,
			func(ctx context.Context, id string) (*gmail.Message, error) {
				return trashMessage(ctx, svc, id)
			},
			func(id string, m *gmail.Message) error {
				fmt.Fprintf(cfg.out, "deleted %s\n", id)
				cnt += 1
				return nil
			})
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(cfg.out, "%d messages moved to the trash\n", cnt)
	return nil
}
