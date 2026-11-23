package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/hashicorp/hcl/v2/hclsimple"
	"google.golang.org/api/gmail/v1"
)

type config struct {
	Credentials string `hcl:"credentials,optional"`
	Token       string `hcl:"token,optional"`
	History     string `hcl:"history,optional"`

	service *gmail.Service
	out     io.Writer
	errOut  io.Writer
	name    string
	command string
}

func configDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(home, "gmailt")
	}
	return filepath.Join(home, ".gmailt")
}

func configFilenames() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return []string{"./gmailt.hcl"}
	}
	return []string{filepath.Join(configDir(), "gmailt.hcl"), filepath.Join(home, ".gmailt.hcl"),
		"./gmailt.hcl"}
}

func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}

func readConfig(filenames []string) (*config, error) {
	for _, name := range filenames {
		buf, err := os.ReadFile(name)
		if err == nil {
			var cfg config
			err := hclsimple.Decode(name, buf, nil, &cfg)
			return &cfg, err
		}
	}

	return nil, nil
}

func loadConfig(fs *flag.FlagSet, args []string) (*config, []string, error) {
	var configFile string

	fs.BoolVar(&verbose, "verbose", false, "enable verbose output")
	fs.BoolVar(&verbose, "v", false, "enable verbose output")
	fs.StringVar(&configFile, "config", "", "HCL config file")
	fs.String("credentials", "", "OAuth client credentials file")
	fs.String("token", "", "OAuth token file")
	fs.String("history", "", "history file used by undo")
	fs.Parse(args)

	var cfg *config
	var err error
	if configFile != "" {
		cfg, err = readConfig([]string{configFile})
		if err != nil {
			return nil, nil, err
		} else if cfg == nil {
			return nil, nil, fmt.Errorf("config file not found: %s", configFile)
		}
	} else {
		cfg, err = readConfig(configFilenames())
		if err != nil {
			return nil, nil, err
		} else if cfg == nil {
			cfg = &config{}
		}
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "credentials":
			cfg.Credentials = f.Value.String()
		case "token":
			cfg.Token = f.Value.String()
		case "history":
			cfg.History = f.Value.String()
		}
	})

	if cfg.Credentials == "" {
		cfg.Credentials = filepath.Join(configDir(), "credentials.json")
	}
	if cfg.Token == "" {
		cfg.Token = filepath.Join(configDir(), "token.json")
	}
	if cfg.History == "" {
		cfg.History = filepath.Join(configDir(), "history.jsonl")
	}
	cfg.Credentials = expandHome(cfg.Credentials)
	cfg.Token = expandHome(cfg.Token)
	cfg.History = expandHome(cfg.History)

	return cfg, fs.Args(), nil
}

func showConfig(ctx context.Context, cfg *config, args []string) error {
	if len(args) != 0 {
		return usageError("")
	}
	fmt.Fprintf(cfg.out, "credentials: %s\n", cfg.Credentials)
	fmt.Fprintf(cfg.out, "token:       %s\n", cfg.Token)
	fmt.Fprintf(cfg.out, "history:     %s\n", cfg.History)
	return nil
}

func warn(cfg *config, err error) {
	fmt.Fprintf(cfg.errOut, "%s: warning: %s\n", cfg.name, err)
}
