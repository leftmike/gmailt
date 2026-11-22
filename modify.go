package main

import (
	"context"
	"fmt"

	"google.golang.org/api/gmail/v1"
)

const maxBatchModify = 1000

func modifyLabels(ctx context.Context, svc *gmail.Service, ids []string, add, remove []string) error {
	for start := 0; start < len(ids); start += maxBatchModify {
		req := &gmail.BatchModifyMessagesRequest{
			Ids:            ids[start:min(start+maxBatchModify, len(ids))],
			AddLabelIds:    add,
			RemoveLabelIds: remove,
		}
		_, err := retry(ctx, func() (struct{}, error) {
			return struct{}{}, svc.Users.Messages.BatchModify("me", req).Context(ctx).Do()
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func archive(cfg *config, args []string) {
	ctx := context.Background()
	svc, ids := selectIDs(ctx, cfg, args, "<id>... | <query>...")
	if err := modifyLabels(ctx, svc, ids, nil, []string{"INBOX"}); err != nil {
		fatal(err)
	}
	fmt.Printf("%d messages archived\n", len(ids))
}
