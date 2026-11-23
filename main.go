package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
)

type runFunc func(ctx context.Context, cfg *config, args []string) error

type cmd struct {
	setup func(fs *flag.FlagSet) runFunc
	help  string
}

type usageError string

func (u usageError) Error() string {
	return "usage: " + string(u)
}

func noFlags(run runFunc) func(fs *flag.FlagSet) runFunc {
	return func(fs *flag.FlagSet) runFunc {
		return run
	}
}

func fatal(name string, err error) {
	var u usageError
	if errors.As(err, &u) {
		fmt.Fprintf(os.Stderr, "usage: %s\n", strings.TrimSpace(name+" "+string(u)))
	} else {
		fmt.Fprintf(os.Stderr, "%s: %s\n", name, err)
	}
	os.Exit(1)
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s <cmd> [flags] [args]\n", os.Args[0])
	for _, name := range slices.Sorted(maps.Keys(cmds)) {
		fmt.Fprintf(os.Stderr, "    %s: %s\n", name, cmds[name].help)
	}
}

var (
	verbose bool

	cmds = map[string]cmd{
		"list":    {setup: listCmd, help: "list messages matching a Gmail search query (default in:inbox)"},
		"auth":    {setup: authCmd, help: "authorize gmailt with Google, or revoke the saved token"},
		"config":  {setup: noFlags(showConfig), help: "show the effective configuration"},
		"get":     {setup: getCmd, help: "fetch and display messages by id or Gmail search query"},
		"label":   {setup: labelCmd, help: "add (+name) and remove (^name) labels on messages by id or Gmail search query"},
		"labels":  {setup: noFlags(labels), help: "list labels with message and unread counts"},
		"help":    {help: "show this help"},
		"archive": {setup: noFlags(archive), help: "remove messages from the inbox by id or Gmail search query"},
		"delete":  {setup: noFlags(trash), help: "move messages to the trash by id or Gmail search query"},
		"trash":   {setup: noFlags(trash), help: "same as delete"},
		"undo":    {setup: noFlags(undo), help: "undo the most recent (or <n>th) delete, archive, or label"},
		"history": {setup: noFlags(history), help: "show the history of commands that undo can reverse"},
	}
)

func main() {
	if len(os.Args) >= 2 {
		if os.Args[1] == "help" {
			usage()
			return
		}
		if c, ok := cmds[os.Args[1]]; ok {
			name := os.Args[0] + " " + os.Args[1]
			fs := flag.NewFlagSet(name, flag.ExitOnError)
			run := c.setup(fs)
			cfg, args, err := loadConfig(fs, os.Args[2:])
			if err != nil {
				fatal(name, err)
			}
			cfg.out = os.Stdout
			cfg.errOut = os.Stderr
			cfg.name = name
			cfg.command = commandLine(os.Args[1:])

			if err := run(context.Background(), cfg, args); err != nil {
				fatal(name, err)
			}
			return
		}
	}

	usage()
	os.Exit(1)
}
