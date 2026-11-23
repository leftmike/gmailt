package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"google.golang.org/api/gmail/v1"
)

const labelUsage = "[-create] +<label>|^<label>... <id>... | <query>..."

var (
	labelCreate bool
)

func labelFlags(fs *flag.FlagSet) {
	fs.BoolVar(&labelCreate, "create", false, "create labels added with + that don't exist")
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

func label(cfg *config, args []string) {
	add, remove, rest, err := parseLabelArgs(args)
	if err != nil {
		fatal(err)
	}
	if len(add) == 0 && len(remove) == 0 {
		fmt.Fprintf(os.Stderr, "usage: %s %s %s\n", os.Args[0], os.Args[1], labelUsage)
		os.Exit(1)
	}

	ctx := context.Background()
	svc, ids := selectIDs(ctx, cfg, rest, labelUsage)

	all, err := listLabels(ctx, svc)
	if err != nil {
		fatal(err)
	}
	addLabels, missing := resolveLabels(all, add)
	removeLabels, missingRemove := resolveLabels(all, remove)
	if len(missingRemove) > 0 {
		fatal(fmt.Errorf("no such label: %s", strings.Join(missingRemove, ", ")))
	}
	if len(missing) > 0 && !labelCreate {
		fatal(fmt.Errorf("no such label: %s (use -create to create it)", strings.Join(missing, ", ")))
	}
	var created []string
	for _, name := range missing {
		l, err := createLabel(ctx, svc, name)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("created label %s\n", l.Name)
		addLabels = append(addLabels, l)
		created = append(created, l.Name)
	}

	err = recordChanges(ctx, cfg, svc, ids, created, func() error {
		return modifyLabels(ctx, svc, ids, labelIDs(addLabels), labelIDs(removeLabels))
	})
	if err != nil {
		fatal(err)
	}
	fmt.Printf("%d messages updated: %s\n", len(ids), describeChanges(addLabels, removeLabels))
}
