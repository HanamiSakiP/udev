package manager

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/hanamisakip/udev/internal/config"
	"github.com/hanamisakip/udev/internal/ui"
	"github.com/hanamisakip/udev/internal/util"
)

func writeBackup(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindBackupsLegacyDir(t *testing.T) {
	home := t.TempDir()
	cfg := &config.Config{
		Home:  home,
		Paths: config.PathsConfig{BackupDir: filepath.Join(home, "backups")},
	}
	legacy := filepath.Join(home, "backup", "pyenv", "pyenv_backup_20260101000000.tar.gz")
	writeBackup(t, legacy)

	files := FindBackups(cfg, "pyenv")
	if len(files) != 1 || files[0] != legacy {
		t.Fatalf("FindBackups = %v, want [%s]", files, legacy)
	}
	if got := LatestBackup(cfg, "pyenv"); got != legacy {
		t.Fatalf("LatestBackup = %q, want %q", got, legacy)
	}
}

func TestFindBackupsCollectsAllDirs(t *testing.T) {
	home := t.TempDir()
	cfg := &config.Config{
		Home:  home,
		Paths: config.PathsConfig{BackupDir: filepath.Join(home, "backups")},
	}
	sub := filepath.Join(home, "backups", "g", "g_backup_20260101000000.tar.gz")
	root := filepath.Join(home, "backups", "g_backup_20260102000000.tar.gz")
	legacy := filepath.Join(home, "backup", "g_backup_20260103000000.tar.gz")
	writeBackup(t, sub)
	writeBackup(t, root)
	writeBackup(t, legacy)

	if got := CountBackups(cfg, "g"); got != 3 {
		t.Fatalf("CountBackups = %d, want 3", got)
	}
	if got := LatestBackup(cfg, "g"); got != legacy {
		t.Fatalf("LatestBackup = %q, want %q", got, legacy)
	}
}

func TestGRestorePreservesExistingFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	stage := t.TempDir()
	writeBackup(t, filepath.Join(stage, ".g", "bin", "g"))
	if err := os.Chmod(filepath.Join(stage, ".g", "bin", "g"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeBackup(t, filepath.Join(stage, ".config", "g", "config"))
	writeBackup(t, filepath.Join(stage, "go", "bin", "tool"))
	writeBackup(t, filepath.Join(stage, "go", "pkg", "mod", "example@v1", "file"))
	archive := filepath.Join(t.TempDir(), "g_backup_test.tar.gz")
	if _, err := util.CreateTarGz(archive, stage, []string{"."}, nil); err != nil {
		t.Fatal(err)
	}

	keep := filepath.Join(home, ".config", "keep")
	writeBackup(t, keep)

	// 模拟 Go module cache 中不可写的目录，整体删除 GOPATH 会失败。
	modCache := filepath.Join(home, "go", "pkg", "mod", "example@v1")
	writeBackup(t, filepath.Join(modCache, "file"))
	locked := filepath.Join(home, "go", "pkg", "mod", "example@v1")
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	env := &Env{
		Cfg: &config.Config{
			Home: home,
			Paths: config.PathsConfig{
				BackupDir: filepath.Join(home, ".udev", "backups"),
				GoRoot:    filepath.Join(home, ".g"),
				GoPath:    filepath.Join(home, "go"),
			},
		},
		UI:  &ui.UI{Out: io.Discard, Err: io.Discard},
		Ctx: context.Background(),
	}
	if err := (&gManager{e: env}).Restore(archive); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unrelated ~/.config file was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "g", "config")); err != nil {
		t.Fatalf("~/.config/g was not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".g", "bin", "g")); err != nil {
		t.Fatalf("~/.g was not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "go", "bin", "tool")); err != nil {
		t.Fatalf("GOPATH was not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(modCache, "file")); err != nil {
		t.Fatalf("existing module cache was removed: %v", err)
	}
}
