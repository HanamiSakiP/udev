package config

import (
	"path/filepath"
	"testing"
)

func TestStorageDefaults(t *testing.T) {
	home := t.TempDir()
	c := Defaults(home)
	if got := c.StorageName(""); got != "udev" {
		t.Fatalf("StorageName() = %q", got)
	}
	if got := c.StorageName("work"); got != "work" {
		t.Fatalf("StorageName(work) = %q", got)
	}
	if got := c.StorageProvider(); got != "pcloud" {
		t.Fatalf("StorageProvider() = %q", got)
	}
	if got := c.RemoteName(""); got != "udev-pcloud" {
		t.Fatalf("RemoteName() = %q", got)
	}
	if got, want := c.ResticRepo("test"), filepath.Join(home, "restic", "test"); got != want {
		t.Fatalf("ResticRepo() = %q, want %q", got, want)
	}
	if got, want := c.ResticPasswordFile("test"), filepath.Join(home, "secrets", "restic-test.pass"); got != want {
		t.Fatalf("ResticPasswordFile() = %q, want %q", got, want)
	}
	if got, want := c.SourceDir(), filepath.Join(home, "source"); got != want {
		t.Fatalf("SourceDir() = %q, want %q", got, want)
	}
}

func TestLoadEmptyStorageRepoStaysEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("UDEV_HOME", home)
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 空值必须保持“自动”语义，不能被 expandPath 变成当前工作目录。
	if c.Storage.ResticRepo != "" {
		t.Fatalf("restic_repo = %q, want empty", c.Storage.ResticRepo)
	}
	if got, want := c.ResticRepo(""), filepath.Join(home, "restic", "udev"); got != want {
		t.Fatalf("ResticRepo() = %q, want %q", got, want)
	}
}
