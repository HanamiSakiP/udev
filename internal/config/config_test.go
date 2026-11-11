package config

import (
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
