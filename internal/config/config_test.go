package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDefaultsRoundTrip(t *testing.T) {
	d := Defaults("/tmp/udev")
	data, err := yaml.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.Paths.PyenvRoot != "~/.pyenv" {
		t.Fatalf("pyenv_root = %q", c.Paths.PyenvRoot)
	}
	if len(c.DevTools.Node.Npm) != len(d.DevTools.Node.Npm) {
		t.Fatalf("npm count = %d, want %d", len(c.DevTools.Node.Npm), len(d.DevTools.Node.Npm))
	}
	if len(c.DevTools.Go.Packages) != len(d.DevTools.Go.Packages) {
		t.Fatalf("go package count mismatch")
	}
	if c.DevTools.Go.Packages[0].Path != d.DevTools.Go.Packages[0].Path {
		t.Fatalf("go package path mismatch")
	}
	if len(c.DevTools.Cargo.Packages) != len(d.DevTools.Cargo.Packages) {
		t.Fatalf("cargo package count mismatch")
	}
	if len(c.Repos.List) != len(d.Repos.List) || c.Repos.List[0] != d.Repos.List[0] {
		t.Fatalf("repos list mismatch")
	}
	if c.Repos.Dir != d.Repos.Dir {
		t.Fatalf("repos dir = %q, want %q", c.Repos.Dir, d.Repos.Dir)
	}
}

func TestEnsureReposSectionAppendsDefaults(t *testing.T) {
	home := t.TempDir()
	cfg := Defaults(home)
	cfg.Home = home
	cfg.File = filepath.Join(home, "config.yaml")
	if err := os.WriteFile(cfg.File, []byte("mirror:\n  use_cn: false\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	added, err := cfg.EnsureReposSection()
	if err != nil {
		t.Fatalf("ensure repos: %v", err)
	}
	if !added {
		t.Fatal("expected repos section to be appended")
	}

	data, err := os.ReadFile(cfg.File)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "repos:") {
		t.Fatalf("repos section missing:\n%s", data)
	}

	var parsed Config
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse appended config: %v", err)
	}
	if len(parsed.Repos.List) != len(cfg.Repos.List) {
		t.Fatalf("repos count = %d, want %d", len(parsed.Repos.List), len(cfg.Repos.List))
	}
	if parsed.Repos.Dir != cfg.Repos.Dir {
		t.Fatalf("repos dir = %q, want %q", parsed.Repos.Dir, cfg.Repos.Dir)
	}

	added, err = cfg.EnsureReposSection()
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if added {
		t.Fatal("expected second call to be a no-op")
	}
}

func TestEnsureGoPackagesAppendsDefaults(t *testing.T) {
	home := t.TempDir()
	cfg := Defaults(home)
	cfg.Home = home
	cfg.File = filepath.Join(home, "config.yaml")
	content := "# 用户注释\ndev_tools:\n  go:\n    packages:\n      - github.com/junegunn/fzf@latest  # 保留这条注释\n    cleanup:\n      - g clean\n"
	if err := os.WriteFile(cfg.File, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg.DevTools.Go.Packages = []GoPackage{{Path: "github.com/junegunn/fzf@latest"}}

	added, err := cfg.EnsureGoPackages()
	if err != nil {
		t.Fatalf("ensure go packages: %v", err)
	}
	if !added {
		t.Fatal("expected missing default packages to be appended")
	}

	data, err := os.ReadFile(cfg.File)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"# 用户注释",
		"# 保留这条注释",
		"github.com/asdf-vm/asdf/cmd/asdf@latest",
		"github.com/HanamiSakiP/u-ani/cmd/uani@latest",
		"github.com/HanamiSakiP/u-comic/cmd/ucom@latest",
		"github.com/HanamiSakiP/u-game/cmd/ugal@latest",
		"github.com/jesseduffield/lazydocker@latest",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("config missing %q:\n%s", want, text)
		}
	}

	var parsed Config
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse merged config: %v", err)
	}
	if got, want := len(parsed.DevTools.Go.Packages), len(cfg.DevTools.Go.Packages); got != want {
		t.Fatalf("go package count = %d, want %d", got, want)
	}
	if got, want := len(cfg.DevTools.Go.Packages), len(Defaults(home).DevTools.Go.Packages); got != want {
		t.Fatalf("in-memory go package count = %d, want %d", got, want)
	}
	last := Defaults(home).DevTools.Go.Packages
	if parsed.DevTools.Go.Packages[len(parsed.DevTools.Go.Packages)-1].Path != last[len(last)-1].Path {
		t.Fatalf("appended packages order mismatch")
	}

	added, err = cfg.EnsureGoPackages()
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if added {
		t.Fatal("expected second call to be a no-op")
	}
}

func TestEnsureGoPackagesCreatesMissingList(t *testing.T) {
	home := t.TempDir()
	cfg := Defaults(home)
	cfg.Home = home
	cfg.File = filepath.Join(home, "config.yaml")
	if err := os.WriteFile(cfg.File, []byte("dev_tools:\n  go:\n    g_version: latest\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	added, err := cfg.EnsureGoPackages()
	if err != nil {
		t.Fatalf("ensure go packages: %v", err)
	}
	if !added {
		t.Fatal("expected packages list to be created")
	}

	data, err := os.ReadFile(cfg.File)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var parsed Config
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse merged config: %v", err)
	}
	want := len(Defaults(home).DevTools.Go.Packages)
	if len(parsed.DevTools.Go.Packages) != want {
		t.Fatalf("go package count = %d, want %d", len(parsed.DevTools.Go.Packages), want)
	}
	if len(cfg.DevTools.Go.Packages) != want {
		t.Fatalf("in-memory go package count = %d, want %d", len(cfg.DevTools.Go.Packages), want)
	}
}

func TestPackageCustomForms(t *testing.T) {
	var g GoSection
	if err := yaml.Unmarshal([]byte(`
packages:
  - github.com/junegunn/fzf@latest
  - path: github.com/cli/cli/v2/cmd/gh@latest
    bin: gh
`), &g); err != nil {
		t.Fatalf("go unmarshal: %v", err)
	}
	if g.Packages[0].EffectiveBin() != "fzf" {
		t.Fatalf("fzf bin = %q", g.Packages[0].EffectiveBin())
	}
	if g.Packages[1].EffectiveBin() != "gh" {
		t.Fatalf("gh bin = %q", g.Packages[1].EffectiveBin())
	}

	var c CargoSection
	if err := yaml.Unmarshal([]byte(`
packages:
  - starship
  - name: yazi-build
    args: ["--force", "--git", "https://example.com/yazi.git"]
`), &c); err != nil {
		t.Fatalf("cargo unmarshal: %v", err)
	}
	if c.Packages[0].Name != "starship" || len(c.Packages[0].Args) != 0 {
		t.Fatalf("starship parse mismatch")
	}
	if c.Packages[1].Name != "yazi-build" || len(c.Packages[1].Args) != 3 {
		t.Fatalf("yazi parse mismatch")
	}
}
