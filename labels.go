package main

import (
	"context"

	"google.golang.org/api/gmail/v1"
)

func labelNames(ctx context.Context, svc *gmail.Service) (map[string]string, error) {
	resp, err := retry(ctx, func() (*gmail.ListLabelsResponse, error) {
		return svc.Users.Labels.List("me").Context(ctx).Do()
	})
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, l := range resp.Labels {
		names[l.Id] = l.Name
	}
	return names, nil
}
