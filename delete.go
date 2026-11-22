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

func trash(cfg *config, args []string) {
	ctx := context.Background()
	svc, ids := selectIDs(ctx, cfg, args, "<id>... | <query>...")

	cnt := 0
	err := each(ctx, ids,
		func(ctx context.Context, id string) (*gmail.Message, error) {
			return trashMessage(ctx, svc, id)
		},
		func(id string, m *gmail.Message) error {
			fmt.Printf("deleted %s\n", id)
			cnt += 1
			return nil
		})
	if err != nil {
		fatal(err)
	}

	fmt.Printf("%d messages moved to the trash\n", cnt)
}
