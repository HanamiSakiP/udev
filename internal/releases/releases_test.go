package releases

import (
	"testing"

	"github.com/hanamisakip/udev/internal/config"
)

func testConfig() *config.Config {
	return config.Defaults("/tmp/udev")
}

func TestNormalizeRepo(t *testing.T) {
	cases := map[string]string{
		"iina/iina":                                       "iina/iina",
		"https://github.com/iina/iina":                    "iina/iina",
		"https://github.com/iina/iina.git":                "iina/iina",
		"git@github.com:shinchiro/mpv-winbuild-cmake.git": "shinchiro/mpv-winbuild-cmake",
		"github.com/gauthierpiarrette/highball/":          "gauthierpiarrette/highball",
	}
	for in, want := range cases {
		got, err := NormalizeRepo(in)
		if err != nil {
			t.Fatalf("NormalizeRepo(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("NormalizeRepo(%q) = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"", "iina", "iina/"} {
		if _, err := NormalizeRepo(bad); err == nil {
			t.Errorf("NormalizeRepo(%q) expected error", bad)
		}
	}
}

func TestParseOptions(t *testing.T) {
	opts, err := ParseOptions([]string{"--tool", "gh", "--dir", "~/Downloads", "--list", "--force", "iina"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.Tool != "gh" || opts.Dir != "~/Downloads" || !opts.List || !opts.Force || len(opts.Args) != 1 {
		t.Fatalf("opts = %+v", opts)
	}

	if _, err := ParseOptions([]string{"--tool"}); err == nil {
		t.Fatal("expected missing value error")
	}
	if _, err := ParseOptions([]string{"--unknown"}); err == nil {
		t.Fatal("expected unknown flag error")
	}
	if _, err := ParseOptions([]string{"--help"}); !IsHelp(err) {
		t.Fatalf("expected help, got %v", err)
	}
}

func TestResolveAll(t *testing.T) {
	cfg := testConfig()
	list, err := Resolve(cfg, Options{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(list) != len(config.DefaultReleaseList()) {
		t.Fatalf("len = %d, want %d", len(list), len(config.DefaultReleaseList()))
	}
	if list[0].Repo != "iina/iina" || list[0].Pattern != "IINA.v*.dmg" {
		t.Fatalf("first item = %+v", list[0])
	}
	if list[2].Pattern != "*mpv-x86_64-v3-*.7z" {
		t.Fatalf("third pattern = %q", list[2].Pattern)
	}
}

func TestResolveFilter(t *testing.T) {
	cfg := testConfig()
	list, err := Resolve(cfg, Options{Args: []string{"iina", "highball"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(list) != 2 || list[0].Repo != "iina/iina" || list[1].Repo != "gauthierpiarrette/highball" {
		t.Fatalf("list = %+v", list)
	}

	if _, err := Resolve(cfg, Options{Args: []string{"not-configured"}}); err == nil {
		t.Fatal("expected error for unknown repo")
	}
}

func TestResolveAdHocPattern(t *testing.T) {
	cfg := testConfig()
	list, err := Resolve(cfg, Options{Args: []string{"someone/repo#*.zip"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(list) != 1 || list[0].Repo != "someone/repo" || list[0].Pattern != "*.zip" {
		t.Fatalf("list = %+v", list)
	}
}

func TestMatchAssetNames(t *testing.T) {
	names := []string{
		"mpv-x86_64-v3-20251118-git-abc.7z",
		"mpv-x86_64-v2-20251118-git-abc.7z",
		"mpv-i686-v3-20251118-git-abc.7z",
	}
	got, err := matchAssetNames(names, "*mpv-x86_64-v3-*.7z")
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(got) != 1 || got[0] != names[0] {
		t.Fatalf("matched = %v", got)
	}
	if _, err := matchAssetNames(names, "["); err == nil {
		t.Fatal("expected bad pattern error")
	}
}

func TestDirOfDefaultsToReleaseDir(t *testing.T) {
	cfg := testConfig()
	cfg.Releases.Dir = ""
	got := DirOf(cfg, Options{})
	want := "/tmp/udev/git/release"
	if got != want {
		t.Fatalf("DirOf() = %q, want %q", got, want)
	}

	cfg.Releases.Dir = "/tmp/udev-downloads"
	if got := DirOf(cfg, Options{}); got != "/tmp/udev-downloads" {
		t.Fatalf("DirOf(cfg) = %q", got)
	}
	if got := DirOf(cfg, Options{Dir: "/tmp/override"}); got != "/tmp/override" {
		t.Fatalf("DirOf(opts) = %q", got)
	}
}

func TestDetectToolExplicitAPI(t *testing.T) {
	if got, err := DetectTool("api", nil); err != nil || got != "api" {
		t.Fatalf("DetectTool(api) = %q, %v", got, err)
	}
	if _, err := DetectTool("unknown", nil); err == nil {
		t.Fatal("expected error for unknown tool")
	}
}
