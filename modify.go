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

func archive(ctx context.Context, cfg *config, args []string) error {
	svc, ids, err := selectIDs(ctx, cfg, args, "<id>... | <query>...")
	if err != nil {
		return err
	}
	err = recordChanges(ctx, cfg, svc, ids, nil, func() error {
		return modifyLabels(ctx, svc, ids, nil, []string{"INBOX"})
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(cfg.out, "%d messages archived\n", len(ids))
	return nil
}
