// Package util 提供 shell 配置写入、文件复制、下载与 tar 打包等通用能力。
package util

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ShellQuote 将参数安全地转义为 POSIX shell 参数。
func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// AddLineIfMissing 向文件追加一行（按整行匹配）；create 为 true 时会创建文件。
func AddLineIfMissing(path, line string, create bool) (bool, error) {
	exists, err := fileExists(path)
	if err != nil {
		return false, err
	}
	if !exists {
		if !create {
			return false, nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return false, err
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			return false, err
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if sc.Text() == line {
			f.Close()
			return false, nil
		}
	}
	f.Close()
	if err := sc.Err(); err != nil {
		return false, err
	}
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return false, err
	}
	defer fh.Close()
	if _, err := fmt.Fprintln(fh, line); err != nil {
		return false, err
	}
	return true, nil
}

// AddConfigIfMissing 追加一段内容（按子串匹配），用于 pyenv 安装后的多行配置。
func AddConfigIfMissing(path, content string, create bool) (bool, error) {
	exists, err := fileExists(path)
	if err != nil {
		return false, err
	}
	if !exists {
		if !create {
			return false, nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return false, err
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			return false, err
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if strings.Contains(string(data), content) {
		return false, nil
	}
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return false, err
	}
	defer fh.Close()
	if _, err := fmt.Fprintln(fh, content); err != nil {
		return false, err
	}
	return true, nil
}

// AddBlockIfMissing 如果文件里不含 marker，则追加一个配置块。
func AddBlockIfMissing(path, marker, block string, create bool) (bool, error) {
	exists, err := fileExists(path)
	if err != nil {
		return false, err
	}
	if !exists {
		if !create {
			return false, nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return false, err
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			return false, err
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if strings.Contains(string(data), marker) {
		return false, nil
	}
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return false, err
	}
	defer fh.Close()
	if _, err := fmt.Fprintf(fh, "\n%s\n", block); err != nil {
		return false, err
	}
	return true, nil
}

// RemoveLines 按正则删除文件中的匹配行，并可选删除以 blockStart 开头、到 blockEnd 任一匹配为止的配置块。
// 返回清理前后的行数。
func RemoveLines(path string, patterns []string, blockStart string, blockEnd []string) (int, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	lines := strings.Split(string(data), "\n")
	before := len(lines)

	var res []*regexp.Regexp
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return 0, 0, fmt.Errorf("无效的清理正则 %q: %w", p, err)
		}
		res = append(res, re)
	}

	out := make([]string, 0, len(lines))
	inBlock := false
	for _, line := range lines {
		if inBlock {
			if matchAnyString(line, blockEnd) {
				inBlock = false
				continue
			}
			// 与旧脚本一致：块内只跳过典型行，遇到未知内容则退出块并保留该行。
			if strings.TrimSpace(line) == "" ||
				strings.HasPrefix(strings.TrimSpace(line), "#") ||
				strings.HasPrefix(strings.TrimSpace(line), "if ") ||
				strings.TrimSpace(line) == "fi" ||
				strings.HasPrefix(strings.TrimSpace(line), "unalias ") ||
				strings.HasPrefix(strings.TrimSpace(line), "[") ||
				strings.Contains(line, ".g/go") {
				continue
			}
			inBlock = false
			out = append(out, line)
			continue
		}
		if blockStart != "" && strings.HasPrefix(line, blockStart) {
			inBlock = true
			continue
		}
		if matchAnyRegex(line, res) {
			continue
		}
		out = append(out, line)
	}

	if before == len(out) && strings.Join(lines, "\n") == strings.Join(out, "\n") {
		return before, before, nil
	}
	info, statErr := os.Stat(path)
	mode := os.FileMode(0o644)
	if statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(path, []byte(strings.Join(out, "\n")), mode); err != nil {
		return before, len(out), err
	}
	return before, len(out), nil
}

func matchAnyString(line string, subs []string) bool {
	for _, s := range subs {
		if s != "" && strings.Contains(line, s) {
			return true
		}
	}
	return false
}

func matchAnyRegex(line string, res []*regexp.Regexp) bool {
	for _, re := range res {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// FileExists 判断路径是否存在。
func FileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// IsDir 判断路径是否为目录。
func IsDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// CopyTree 将 src 目录的内容合并复制到 dst，保留权限、时间与软链接。
func CopyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, info.Mode().Perm())
		}
		target := filepath.Join(dst, rel)
		return copyEntry(path, target, info)
	})
}

// MergePath 将 src 目录合并到 dst，保留 dst 中已有的其他内容。
// 适用于 GOPATH 等包含只读缓存、不能整体删除的目标目录。
func MergePath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return CopyPath(src, dst)
	}
	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}
	return CopyTree(src, dst)
}

// CopyPath 先删除 dst，再完整复制 src（文件、目录或软链接）。
func CopyPath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if info.IsDir() {
		return CopyTree(src, dst)
	}
	return copyEntry(src, dst, info)
}

func copyEntry(src, dst string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(src)
		if err != nil {
			return err
		}
		_ = os.RemoveAll(dst)
		return os.Symlink(link, dst)
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		return os.Chtimes(dst, info.ModTime(), info.ModTime())
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	// 目标文件可能是只读的（例如 Go module cache），先删除再写入；
	// 若所在目录也不可写，则保留已有文件，避免触发整体回滚。
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		if st, statErr := os.Stat(dst); statErr == nil && st.Mode().IsRegular() {
			return nil
		}
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}
