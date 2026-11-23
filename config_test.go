package main

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cases := map[string]string{
		"~":            home,
		"~/token.json": filepath.Join(home, "token.json"),
		"~bob/x":       "~bob/x",
		"/abs/x":       "/abs/x",
		"rel/x":        "rel/x",
	}
	for in, want := range cases {
		if got := expandHome(in); got != want {
			t.Errorf("expandHome(%q): got %q, want %q", in, got, want)
		}
	}
}

func TestLoadConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	cfg, args := loadConfig(flag.NewFlagSet("test", flag.ContinueOnError), []string{"a", "b"})
	dir := filepath.Join(home, ".gmailt")
	if cfg.Credentials != filepath.Join(dir, "credentials.json") ||
		cfg.Token != filepath.Join(dir, "token.json") ||
		cfg.History != filepath.Join(dir, "history.jsonl") || !slices.Equal(args, []string{"a", "b"}) {
		t.Errorf("defaults: got %+v, %v", cfg, args)
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	hcl := "credentials = \"~/creds.json\"\ntoken = \"/tmp/tok.json\"\nhistory = \"~/h.jsonl\"\n"
	if err := os.WriteFile(filepath.Join(dir, "gmailt.hcl"), []byte(hcl), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, _ = loadConfig(flag.NewFlagSet("test", flag.ContinueOnError), []string{"-token", "flag.json"})
	if cfg.Credentials != filepath.Join(home, "creds.json") || cfg.Token != "flag.json" ||
		cfg.History != filepath.Join(home, "h.jsonl") {
		t.Errorf("file and flags: got %+v", cfg)
	}

	other := filepath.Join(t.TempDir(), "other.hcl")
	if err := os.WriteFile(other, []byte("token = \"other.json\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, _ = loadConfig(flag.NewFlagSet("test", flag.ContinueOnError), []string{"-config", other})
	if cfg.Token != "other.json" || cfg.Credentials != filepath.Join(dir, "credentials.json") {
		t.Errorf("-config: got %+v", cfg)
	}
}

func TestReadConfigError(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.hcl")
	if err := os.WriteFile(bad, []byte("nope = \n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfig([]string{bad}); err == nil {
		t.Error("readConfig: expected error")
	}
	if cfg, err := readConfig([]string{filepath.Join(t.TempDir(), "none.hcl")}); cfg != nil || err != nil {
		t.Errorf("readConfig missing: got %v, %v", cfg, err)
	}
}
