// Command gen 生成内嵌的 udev 源码归档。
//
// 在 internal/selfupdate 目录执行：
//
//	go generate ./...
package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	wd, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	root, err := findRoot(wd)
	if err != nil {
		fatal(err)
	}
	outDir := filepath.Join(root, "internal", "selfupdate", "embedded")
	out := filepath.Join(outDir, "source.tar.gz")

	var files []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		switch {
		case rel == "udev", rel == "udev.exe":
			return nil
		case strings.HasPrefix(rel, "internal/selfupdate/embedded/"):
			return nil
		case strings.HasSuffix(rel, ".log"), strings.HasSuffix(rel, ".tmp"):
			return nil
		case rel == ".DS_Store":
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		fatal(err)
	}
	sort.Strings(files)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fatal(err)
	}
	f, err := os.Create(out)
	if err != nil {
		fatal(err)
	}
	defer f.Close()

	zw, err := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err != nil {
		fatal(err)
	}
	tw := tar.NewWriter(zw)
	epoch := time.Unix(0, 0).UTC()
	for _, rel := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil {
			fatal(err)
		}
		hdr := &tar.Header{
			Name:     rel,
			Mode:     int64(info.Mode().Perm()),
			Size:     info.Size(),
			ModTime:  epoch,
			Typeflag: tar.TypeReg,
			Format:   tar.FormatPAX,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			fatal(err)
		}
		in, err := os.Open(path)
		if err != nil {
			fatal(err)
		}
		_, err = io.Copy(tw, in)
		in.Close()
		if err != nil {
			fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		fatal(err)
	}
	if err := zw.Close(); err != nil {
		fatal(err)
	}
	if err := f.Close(); err != nil {
		fatal(err)
	}
	fmt.Printf("已生成 %s（%d 个文件）\n", out, len(files))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "生成源码归档失败:", err)
	os.Exit(1)
}

// findRoot 从 dir 向上查找包含 go.mod 的模块根目录。
func findRoot(dir string) (string, error) {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("未找到 go.mod")
		}
		dir = parent
	}
}
