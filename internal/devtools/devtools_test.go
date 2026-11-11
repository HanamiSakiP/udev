package devtools

import "testing"

func TestNpmPackageName(t *testing.T) {
	cases := map[string]string{
		"nrm":                    "nrm",
		"yarn":                   "yarn",
		"@playwright/cli@latest": "@playwright/cli",
		"@scope/pkg@1.2.3":       "@scope/pkg",
		"@scope/pkg":             "@scope/pkg",
		"pkg@1.2.3":              "pkg",
	}
	for in, want := range cases {
		if got := npmPackageName(in); got != want {
			t.Errorf("npmPackageName(%q) = %q, want %q", in, got, want)
		}
	}
}
