// Package selfupdate 负责把 udev 自身源码同步到本地目录，并支持从 GitHub 更新。
package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hanamisakip/udev/internal/config"
	"github.com/hanamisakip/udev/internal/ui"
)

//go:generate go run ./gen

//go:embed embedded/source.tar.gz
var embeddedFS embed.FS

const sourceArchiveName = "embedded/source.tar.gz"

// sourceVersionFile 记录本地源码对应的版本/提交。
const sourceVersionFile = ".udev-source-version"

// placeholderArchive 用于让 ~/.udev/source 中的 selfupdate 包保持可构建。
// 真正的源码归档会在每次执行 udev update 时由 GitHub 下载的源码替换。
var placeholderArchive = buildEmptyArchive()

// SyncSource 将编译进二进制的源码写入本地源码目录。
// 已存在且内容相同的文件不会重复写入；返回写入/更新的文件数。
func SyncSource(cfg *config.Config, u *ui.UI, version string) (int, error) {
	dest := cfg.SourceDir()
	if dest == "" {
		return 0, nil
	}
	data, err := embeddedFS.ReadFile(sourceArchiveName)
	if err != nil {
		return 0, err
	}
	written, err := extractTarGz(data, dest)
	if err != nil {
		return written, err
	}

	// 让本地源码中的 embed 目标存在，方便用户直接 go build 调试。
	placeholder := filepath.Join(dest, "internal", "selfupdate", sourceArchiveName)
	if _, err := os.Stat(placeholder); os.IsNotExist(err) {
		if err := writeFileAtomic(placeholder, placeholderArchive, 0o644); err == nil {
			written++
		}
	}

	if version != "" {
		marker := filepath.Join(dest, sourceVersionFile)
		if old, err := os.ReadFile(marker); err != nil || strings.TrimSpace(string(old)) != version {
			if err := writeFileAtomic(marker, []byte(version+"\n"), 0o644); err == nil {
				written++
			}
		}
	}
	if written > 0 && u != nil {
		u.Subst("已同步工具源码（%d 个文件）: %s", written, dest)
	}
	return written, nil
}

// extractTarGz 解压归档到 dest，仅写入内容有变化的文件。
func extractTarGz(data []byte, dest string) (int, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return 0, fmt.Errorf("读取源码归档失败: %w", err)
	}
	defer zr.Close()

	if err := os.MkdirAll(dest, 0o755); err != nil {
		return 0, err
	}
	tr := tar.NewReader(zr)
	written := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return written, fmt.Errorf("读取源码归档失败: %w", err)
		}
		rel, ok := safeRelPath(hdr.Name)
		if !ok {
			continue
		}
		target := filepath.Join(dest, rel)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return written, err
			}
		case tar.TypeReg, tar.TypeRegA:
			content, err := io.ReadAll(tr)
			if err != nil {
				return written, err
			}
			mode := os.FileMode(hdr.Mode).Perm()
			if mode == 0 {
				mode = 0o644
			}
			changed, err := writeIfChanged(target, content, mode)
			if err != nil {
				return written, err
			}
			if changed {
				written++
			}
		}
	}
	return written, nil
}

func writeIfChanged(path string, content []byte, mode os.FileMode) (bool, error) {
	if old, err := os.ReadFile(path); err == nil {
		info, statErr := os.Stat(path)
		if statErr == nil && bytes.Equal(old, content) && info.Mode().Perm() == mode {
			return false, nil
		}
	}
	if err := writeFileAtomic(path, content, mode); err != nil {
		return false, err
	}
	return true, nil
}

func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".udev-src-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// safeRelPath 清理并校验归档中的相对路径，防止路径穿越。
func safeRelPath(name string) (string, bool) {
	name = filepath.ToSlash(strings.TrimSpace(name))
	name = strings.TrimPrefix(name, "./")
	if name == "" || name == "." {
		return "", false
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	return clean, true
}

func buildEmptyArchive() []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	_ = tw.Close()
	_ = zw.Close()
	return buf.Bytes()
}
