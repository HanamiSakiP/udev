package storage

import (
	"strings"
	"testing"
)

func TestParseOptions(t *testing.T) {
	o, err := parseOptions([]string{"cloud", "dropbox", "--name", "work", "--force"})
	if err != nil {
		t.Fatalf("parseOptions: %v", err)
	}
	if o.Sub != "cloud" || o.Provider != "dropbox" || o.Name != "work" || !o.Force {
		t.Fatalf("unexpected options: %+v", o)
	}

	o, err = parseOptions([]string{"gh", "--token-file=/tmp/token", "--git-protocol=ssh"})
	if err != nil {
		t.Fatalf("parseOptions: %v", err)
	}
	if o.Sub != "gh" || o.TokenFile != "/tmp/token" || o.GitProto != "ssh" {
		t.Fatalf("unexpected options: %+v", o)
	}

	o, err = parseOptions([]string{"onedrive"})
	if err != nil {
		t.Fatalf("parseOptions: %v", err)
	}
	if o.Sub != "cloud" || o.Provider != "onedrive" {
		t.Fatalf("provider shorthand: %+v", o)
	}

	o, err = parseOptions([]string{"restic", "work"})
	if err != nil {
		t.Fatalf("parseOptions: %v", err)
	}
	if o.Sub != "restic" || o.Name != "work" {
		t.Fatalf("restic name: %+v", o)
	}
}

func TestNormalizeProvider(t *testing.T) {
	cases := map[string]string{
		"":         "pcloud",
		"pcloud":   "pcloud",
		"OneDrive": "onedrive",
		"dropbox":  "dropbox",
	}
	for in, want := range cases {
		got, err := normalizeProvider(in, "pcloud")
		if err != nil {
			t.Fatalf("normalizeProvider(%q): %v", in, err)
		}
		if got != want {
			t.Fatalf("normalizeProvider(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := normalizeProvider("s3", ""); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestGeneratePassword(t *testing.T) {
	pw, err := generatePassword()
	if err != nil {
		t.Fatalf("generatePassword: %v", err)
	}
	if len(pw) != 32 {
		t.Fatalf("password length = %d, want 32", len(pw))
	}
	if strings.ContainsAny(pw, "\r\n") {
		t.Fatalf("password contains newline: %q", pw)
	}
}

func TestIsLocalRepo(t *testing.T) {
	local := []string{"/tmp/restic", "~/.udev/restic/udev", "./repo", "repo", ""}
	for _, repo := range local {
		if !isLocalRepo(repo) {
			t.Errorf("isLocalRepo(%q) = false", repo)
		}
	}
	remote := []string{"rclone:remote:path", "s3:bucket/path", "rest:https://example.com"}
	for _, repo := range remote {
		if isLocalRepo(repo) {
			t.Errorf("isLocalRepo(%q) = true", repo)
		}
	}
}

func TestValidRemoteName(t *testing.T) {
	for _, name := range []string{"udev-pcloud", "my.backup_1", "a@b"} {
		if !validRemoteName(name) {
			t.Errorf("validRemoteName(%q) = false", name)
		}
	}
	for _, name := range []string{"", ".", "..", "a:b", "a b", "a/b"} {
		if validRemoteName(name) {
			t.Errorf("validRemoteName(%q) = true", name)
		}
	}
}
