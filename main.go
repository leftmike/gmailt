package main

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
)

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "%s %s: %s\n", os.Args[0], os.Args[1], err)
	os.Exit(1)
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s <cmd> [flags] [args]\n", os.Args[0])
	for _, name := range slices.Sorted(maps.Keys(cmds)) {
		fmt.Fprintf(os.Stderr, "    %s: %s\n", name, cmds[name].help)
	}
}

type cmd struct {
	flags func(fs *flag.FlagSet)
	run   func(cfg *config, args []string)
	help  string
}

var (
	verbose bool

	cmds = map[string]cmd{
		"list":    {flags: listFlags, run: list, help: "list messages matching a Gmail search query (default in:inbox)"},
		"auth":    {flags: authFlags, run: auth, help: "authorize gmailt with Google, or revoke the saved token"},
		"config":  {run: showConfig, help: "show the effective configuration"},
		"get":     {flags: getFlags, run: get, help: "fetch and display messages by id or Gmail search query"},
		"labels":  {run: labels, help: "list labels with message and unread counts"},
		"help":    {help: "show this help"},
		"archive": {run: archive, help: "remove messages from the inbox by id or Gmail search query"},
		"delete":  {run: trash, help: "move messages to the trash by id or Gmail search query"},
		"trash":   {run: trash, help: "same as delete"},
	}
)

func main() {
	if len(os.Args) >= 2 {
		cmd, ok := cmds[os.Args[1]]
		if ok {
			if os.Args[1] == "help" {
				usage()
				return
			}

			fs := flag.NewFlagSet(fmt.Sprintf("%s %s", os.Args[0], os.Args[1]), flag.ExitOnError)
			if cmd.flags != nil {
				cmd.flags(fs)
			}
			cfg, args := loadConfig(fs, os.Args[2:])

			cmd.run(cfg, args)
			return
		}
	}

	usage()
	os.Exit(1)
}
