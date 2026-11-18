package repos

import (
	"strings"
	"testing"

	"github.com/hanamisakip/udev/internal/config"
)

func testConfig() *config.Config {
	c := config.Defaults("/tmp/udev")
	c.Repos.Dir = "/tmp/udev-repos"
	return c
}

func TestRepoName(t *testing.T) {
	cases := map[string]string{
		"dani-garcia/vaultwarden":                "vaultwarden",
		"https://github.com/localsend/localsend": "localsend",
		"https://github.com/foo/bar.git":         "bar",
		"git@github.com:HanamiSakiP/udev.git":    "udev",
		"HanamiSakiP/u-ani/":                     "u-ani",
	}
	for in, want := range cases {
		if got := RepoName(in); got != want {
			t.Errorf("RepoName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCloneURL(t *testing.T) {
	if got := cloneURL("dani-garcia/vaultwarden", "https"); got != "https://github.com/dani-garcia/vaultwarden.git" {
		t.Fatalf("https url = %q", got)
	}
	if got := cloneURL("dani-garcia/vaultwarden", "ssh"); got != "git@github.com:dani-garcia/vaultwarden.git" {
		t.Fatalf("ssh url = %q", got)
	}
	if got := cloneURL("https://gitlab.com/a/b.git", "https"); got != "https://gitlab.com/a/b.git" {
		t.Fatalf("full url = %q", got)
	}
	if got := cloneURL("gitlab.com/a/b", "https"); got != "https://gitlab.com/a/b.git" {
		t.Fatalf("hosted url = %q", got)
	}
	if got := cloneURL("gitlab.com/a/b", "ssh"); got != "git@gitlab.com:a/b.git" {
		t.Fatalf("hosted ssh url = %q", got)
	}
}

func TestResolveAll(t *testing.T) {
	cfg := testConfig()
	list, err := Resolve(cfg, Options{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(list) != len(cfg.Repos.List) {
		t.Fatalf("len = %d, want %d", len(list), len(cfg.Repos.List))
	}
	if list[0].Name != "vaultwarden" || list[0].Dest != "/tmp/udev-repos/vaultwarden" {
		t.Fatalf("first repo = %+v", list[0])
	}
}

func TestResolveFilter(t *testing.T) {
	cfg := testConfig()
	list, err := Resolve(cfg, Options{Args: []string{"vaultwarden", "u-ani"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
	if list[0].Name != "vaultwarden" || list[1].Name != "u-ani" {
		t.Fatalf("repos = %+v", list)
	}

	if _, err := Resolve(cfg, Options{Args: []string{"not-configured"}}); err == nil {
		t.Fatal("expected error for unknown repo name")
	}
}

func TestResolveExplicitRepo(t *testing.T) {
	cfg := testConfig()
	list, err := Resolve(cfg, Options{Args: []string{"someone/newrepo"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(list) != 1 || list[0].URL != "https://github.com/someone/newrepo.git" {
		t.Fatalf("repos = %+v", list)
	}
}

func TestResolveFromConfigAddition(t *testing.T) {
	cfg := testConfig()
	cfg.Repos.List = append(cfg.Repos.List, "someone/added-in-config")
	list, err := Resolve(cfg, Options{Args: []string{"added-in-config"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(list) != 1 || list[0].Dest != "/tmp/udev-repos/added-in-config" {
		t.Fatalf("repos = %+v", list)
	}
}

func TestResolveDedupes(t *testing.T) {
	cfg := testConfig()
	list, err := Resolve(cfg, Options{Args: []string{"udev", "HanamiSakiP/udev", "https://github.com/HanamiSakiP/udev"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(list), list)
	}
}

func TestParseOptions(t *testing.T) {
	opts, err := ParseOptions([]string{"--tool", "git", "--dir", "~/code", "--list", "--check", "udev"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.Tool != "git" || opts.Dir != "~/code" || !opts.List || !opts.Check || len(opts.Args) != 1 {
		t.Fatalf("opts = %+v", opts)
	}
	if _, err := ParseOptions([]string{"--tool=jj"}); err != nil {
		t.Fatalf("parse equals form: %v", err)
	}
	if _, err := ParseOptions([]string{"--unknown"}); err == nil {
		t.Fatal("expected error for unknown flag")
	}
	if _, err := ParseOptions([]string{"--tool"}); err == nil || !strings.Contains(err.Error(), "需要一个值") {
		t.Fatalf("expected missing value error, got %v", err)
	}
}
