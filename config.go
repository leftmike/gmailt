package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/hashicorp/hcl/v2/hclsimple"
)

type jevConfig struct {
	APIKey  string `hcl:"api_key,optional"`
	BaseURL string `hcl:"base_url,optional"`
	Model   string `hcl:"model,optional"`
}

type config struct {
	Credentials string     `hcl:"credentials,optional"`
	Token       string     `hcl:"token,optional"`
	Backup      string     `hcl:"backup,optional"`
	Jev         *jevConfig `hcl:"jev,block"`
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

func loadConfig(fs *flag.FlagSet, args []string) (*config, []string) {
	var configFile string

	fs.BoolVar(&verbose, "verbose", false, "enable verbose output")
	fs.BoolVar(&verbose, "v", false, "enable verbose output")
	fs.StringVar(&configFile, "config", "", "HCL config file")
	fs.String("credentials", "", "OAuth client credentials file")
	fs.String("token", "", "OAuth token file")
	fs.String("backup", "", "directory to save deleted emails")
	fs.String("jev-api-key", "", "Jev (OpenRouter) API key (or $GMAILT_JEV_API_KEY)")
	fs.String("jev-base-url", "", "Jev API base URL")
	fs.String("jev-model", "", "Jev model")
	fs.Parse(args)

	var cfg *config
	var err error
	if configFile != "" {
		cfg, err = readConfig([]string{configFile})
		if err != nil {
			fatal(err)
		} else if cfg == nil {
			fatal(fmt.Errorf("config file not found: %s", configFile))
		}
	} else {
		cfg, err = readConfig(configFilenames())
		if err != nil {
			fatal(err)
		} else if cfg == nil {
			cfg = &config{}
		}
	}
	if cfg.Jev == nil {
		cfg.Jev = &jevConfig{}
	}

	if v, ok := os.LookupEnv("GMAILT_JEV_API_KEY"); ok {
		cfg.Jev.APIKey = v
	}

	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "credentials":
			cfg.Credentials = f.Value.String()
		case "token":
			cfg.Token = f.Value.String()
		case "backup":
			cfg.Backup = f.Value.String()
		case "jev-api-key":
			cfg.Jev.APIKey = f.Value.String()
		case "jev-base-url":
			cfg.Jev.BaseURL = f.Value.String()
		case "jev-model":
			cfg.Jev.Model = f.Value.String()
		}
	})

	if cfg.Credentials == "" {
		cfg.Credentials = filepath.Join(configDir(), "credentials.json")
	}
	if cfg.Token == "" {
		cfg.Token = filepath.Join(configDir(), "token.json")
	}
	cfg.Credentials = expandHome(cfg.Credentials)
	cfg.Token = expandHome(cfg.Token)
	cfg.Backup = expandHome(cfg.Backup)

	return cfg, fs.Args()
}

func showConfig(cfg *config, args []string) {
	apiKey := ""
	if cfg.Jev.APIKey != "" {
		apiKey = "(set)"
	}
	fmt.Printf("credentials:  %s\n", cfg.Credentials)
	fmt.Printf("token:        %s\n", cfg.Token)
	fmt.Printf("backup:       %s\n", cfg.Backup)
	fmt.Printf("jev api_key:  %s\n", apiKey)
	fmt.Printf("jev base_url: %s\n", cfg.Jev.BaseURL)
	fmt.Printf("jev model:    %s\n", cfg.Jev.Model)
}
