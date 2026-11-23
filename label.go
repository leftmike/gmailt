package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"google.golang.org/api/gmail/v1"
)

const labelUsage = "[-create] +<label>|^<label>... <id>... | <query>..."

type labelOptions struct {
	create bool
}

func labelCmd(fs *flag.FlagSet) runFunc {
	var opts labelOptions
	fs.BoolVar(&opts.create, "create", false, "create labels added with + that don't exist")
	return func(ctx context.Context, cfg *config, args []string) error {
		return label(ctx, cfg, opts, args)
	}
}

func parseLabelArgs(args []string) ([]string, []string, []string, error) {
	var add, remove []string
	seen := map[string]byte{}
	i := 0
	for ; i < len(args); i++ {
		arg := args[i]
		if arg == "" || (arg[0] != '+' && arg[0] != '^') {
			break
		}
		name := arg[1:]
		if name == "" {
			return nil, nil, nil, fmt.Errorf("missing label name: %s", arg)
		}

		key := strings.ToLower(name)
		if op, ok := seen[key]; ok {
			if op != arg[0] {
				return nil, nil, nil, fmt.Errorf("label both added and removed: %s", name)
			}
			continue
		}
		seen[key] = arg[0]
		if arg[0] == '+' {
			add = append(add, name)
		} else {
			remove = append(remove, name)
		}
	}
	return add, remove, args[i:], nil
}

func labelIDs(labels []*gmail.Label) []string {
	var ids []string
	for _, l := range labels {
		ids = append(ids, l.Id)
	}
	return ids
}

func describeChanges(add, remove []*gmail.Label) string {
	var changes []string
	for _, l := range add {
		changes = append(changes, "+"+l.Name)
	}
	for _, l := range remove {
		changes = append(changes, "^"+l.Name)
	}
	return strings.Join(changes, " ")
}

func label(ctx context.Context, cfg *config, opts labelOptions, args []string) error {
	add, remove, rest, err := parseLabelArgs(args)
	if err != nil {
		return err
	}
	if len(add) == 0 && len(remove) == 0 {
		return usageError(labelUsage)
	}

	svc, ids, err := selectIDs(ctx, cfg, rest, labelUsage)
	if err != nil {
		return err
	}

	all, err := listLabels(ctx, svc)
	if err != nil {
		return err
	}
	addLabels, missing := resolveLabels(all, add)
	removeLabels, missingRemove := resolveLabels(all, remove)
	if len(missingRemove) > 0 {
		return fmt.Errorf("no such label: %s", strings.Join(missingRemove, ", "))
	}
	if len(missing) > 0 && !opts.create {
		return fmt.Errorf("no such label: %s (use -create to create it)", strings.Join(missing, ", "))
	}
	var created []string
	for _, name := range missing {
		l, err := createLabel(ctx, svc, name)
		if err != nil {
			return err
		}
		fmt.Fprintf(cfg.out, "created label %s\n", l.Name)
		addLabels = append(addLabels, l)
		created = append(created, l.Name)
	}

	err = recordChanges(ctx, cfg, svc, ids, created, func() error {
		return modifyLabels(ctx, svc, ids, labelIDs(addLabels), labelIDs(removeLabels))
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(cfg.out, "%d messages updated: %s\n", len(ids), describeChanges(addLabels, removeLabels))
	return nil
}
