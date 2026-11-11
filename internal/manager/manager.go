// Package manager 实现 pyenv / g / nvm / rust / fvm 的安装、备份、恢复与状态查询。
package manager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/hanamisakip/udev/internal/config"
	"github.com/hanamisakip/udev/internal/ui"
	"github.com/hanamisakip/udev/internal/util"
)

// Env 是管理器共享的运行环境。
type Env struct {
	Cfg          *config.Config
	UI           *ui.UI
	Ctx          context.Context
	NoModifyPath bool
}

// Manager 描述一个可管理的版本管理器。
type Manager interface {
	Name() string
	InstallDir() string
	Installed() bool
	Version() string
	Backup() (string, error)
	Restore(archive string) error
	Install() error
	Clean() error
}

// All 返回全部管理器（固定顺序）。
func All(e *Env) []Manager {
	return []Manager{
		&pyenvManager{e: e},
		&gManager{e: e},
		&nvmManager{e: e},
		&rustManager{e: e},
		&fvmManager{e: e},
	}
}

// Resolve 将名称/别名转换为管理器。
func Resolve(e *Env, name string) (Manager, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "pyenv", "python":
		return &pyenvManager{e: e}, true
	case "g", "golang", "go":
		return &gManager{e: e}, true
	case "nvm", "node", "nodejs":
		return &nvmManager{e: e}, true
	case "rust", "rustup", "cargo":
		return &rustManager{e: e}, true
	case "fvm", "flutter":
		return &fvmManager{e: e}, true
	}
	return nil, false
}

// ResolveMany 解析一组名称；names 为空或包含 all 时返回全部。
func ResolveMany(e *Env, names []string) ([]Manager, error) {
	if len(names) == 0 {
		return All(e), nil
	}
	var out []Manager
	seen := map[string]bool{}
	add := func(m Manager) {
		if !seen[m.Name()] {
			seen[m.Name()] = true
			out = append(out, m)
		}
	}
	for _, n := range names {
		if strings.EqualFold(n, "all") {
			for _, m := range All(e) {
				add(m)
			}
			continue
		}
		m, ok := Resolve(e, n)
		if !ok {
			return nil, fmt.Errorf("未知工具: %s（可选: pyenv g nvm rust fvm all）", n)
		}
		add(m)
	}
	return out, nil
}

// BaseExcludes 是通用备份排除规则。
var BaseExcludes = []string{
	".git", ".github", ".gitignore",
	".gitmodules", ".gitattributes", ".cache",
	"test", "tests", "docs", "man",
	"*.md", "LICENSE", "LICENSE.*",
	"Makefile", "Dockerfile",
	"*.png", "*.gif", "*.jpg", "*.jpeg",
	"*.svg", "*.ico",
	"*.pyc", "__pycache__", "*.pyo", "*.log",
	"tmp", "temp",
	"rename_test.sh", "update_test_mocks.sh",
	"install.sh", "package.json", "package-lock.json",
}

// ExcludesWithout 返回去掉指定项的排除规则副本。
func ExcludesWithout(base []string, items ...string) []string {
	drop := map[string]bool{}
	for _, it := range items {
		drop[it] = true
	}
	out := make([]string, 0, len(base))
	for _, b := range base {
		if !drop[b] {
			out = append(out, b)
		}
	}
	return out
}

// BackupPath 返回指定工具的新备份文件路径。
func BackupPath(cfg *config.Config, tool string) (string, error) {
	dir := filepath.Join(cfg.Paths.BackupDir, tool)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("%s_backup_%s.tar.gz", tool, time.Now().Format("20060102150405"))), nil
}

// FindBackups 查找工具备份。
//
// 除当前配置的备份目录外，也会查找 ~/.udev/backups 与旧版的 ~/.udev/backup，
// 避免升级或修改 backup_dir 后找不到已有备份。
func FindBackups(cfg *config.Config, tool string) []string {
	dirs := []string{
		filepath.Join(cfg.Paths.BackupDir, tool),
		cfg.Paths.BackupDir,
		filepath.Join(cfg.Home, "backups", tool),
		filepath.Join(cfg.Home, "backups"),
		filepath.Join(cfg.Home, "backup", tool),
		filepath.Join(cfg.Home, "backup"),
	}
	seenDirs := map[string]bool{}
	seenFiles := map[string]bool{}
	var files []string
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		dir = filepath.Clean(dir)
		if seenDirs[dir] {
			continue
		}
		seenDirs[dir] = true

		matches, err := filepath.Glob(filepath.Join(dir, tool+"_backup_*.tar.gz"))
		if err != nil {
			continue
		}
		for _, f := range matches {
			f = filepath.Clean(f)
			if seenFiles[f] {
				continue
			}
			seenFiles[f] = true
			files = append(files, f)
		}
	}
	sort.Strings(files)
	return files
}

// LatestBackup 返回最新备份。
func LatestBackup(cfg *config.Config, tool string) string {
	files := FindBackups(cfg, tool)
	if len(files) == 0 {
		return ""
	}
	latest := files[0]
	for _, f := range files[1:] {
		if filepath.Base(f) > filepath.Base(latest) {
			latest = f
		}
	}
	return latest
}

// CountBackups 返回备份数量。
func CountBackups(cfg *config.Config, tool string) int {
	return len(FindBackups(cfg, tool))
}

// DiskspaceWarning 备份目录空间不足时给出提示。
func (e *Env) DiskspaceWarning() {
	out, err := util.Output("df", "-Pk", e.Cfg.Paths.BackupDir)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return
	}
	var kb int64
	fmt.Sscanf(fields[3], "%d", &kb)
	if kb > 0 && kb < 204800 {
		e.UI.Warnf("备份目录剩余空间较小（%dKB），备份可能失败", kb)
	}
}

// ShellFiles 返回需要写入的 shell 配置文件。
func (e *Env) ShellFiles() []string {
	return e.Cfg.Shell.Files
}

func shouldCreate(path string) bool {
	base := filepath.Base(path)
	return base == ".bashrc" || base == ".profile" || base == ".bash_profile"
}

// AddShellLine 向所有 shell 配置写入一行，返回写入的文件数。
func (e *Env) AddShellLine(line string) int {
	n := 0
	for _, f := range e.ShellFiles() {
		added, err := util.AddLineIfMissing(f, line, shouldCreate(f))
		if err != nil {
			e.UI.Warnf("写入 %s 失败: %v", f, err)
			continue
		}
		if added {
			e.UI.Subst("写入 %s: %s", f, line)
			n++
		}
	}
	return n
}

// AddShellConfig 向所有 shell 配置写入一段内容（子串去重）。
func (e *Env) AddShellConfig(content string) int {
	n := 0
	for _, f := range e.ShellFiles() {
		added, err := util.AddConfigIfMissing(f, content, shouldCreate(f))
		if err != nil {
			e.UI.Warnf("写入 %s 失败: %v", f, err)
			continue
		}
		if added {
			e.UI.Subst("写入 %s: %s", f, content)
			n++
		}
	}
	return n
}

// AddShellBlock 向所有 shell 配置写入配置块。
func (e *Env) AddShellBlock(marker, block string) int {
	n := 0
	for _, f := range e.ShellFiles() {
		added, err := util.AddBlockIfMissing(f, marker, block, shouldCreate(f))
		if err != nil {
			e.UI.Warnf("写入 %s 失败: %v", f, err)
			continue
		}
		if added {
			e.UI.Subst("写入 %s: %s", f, marker)
			n++
		}
	}
	return n
}

// RemoveShellLines 从所有 shell 配置删除匹配行/块。
func (e *Env) RemoveShellLines(patterns []string, blockStart string, blockEnd []string) {
	for _, f := range e.ShellFiles() {
		before, after, err := util.RemoveLines(f, patterns, blockStart, blockEnd)
		if err != nil {
			e.UI.Warnf("清理 %s 失败: %v", f, err)
			continue
		}
		if before != after {
			e.UI.Subst("清理 %s（%d -> %d 行）", f, before, after)
		}
	}
}

// BashOutput 执行 shell 脚本并返回输出。
func (e *Env) BashOutput(script string) (string, error) {
	return util.OutputShell(script, nil)
}

// MirrorURL 根据镜像配置转换地址。
func (e *Env) MirrorURL(url string) string {
	if !e.Cfg.Mirror.UseCN {
		return url
	}
	return util.MirrorRawURL(url)
}

// GitEnv 返回带 git 镜像的环境变量。
func (e *Env) GitEnv() []string {
	if e.Cfg.Mirror.UseCN {
		return util.GitMirrorEnv(e.Cfg.Mirror.GitHubProxy)
	}
	return os.Environ()
}

// DownloadScript 下载安装脚本到临时文件。
func (e *Env) DownloadScript(url string) (string, error) {
	url = e.MirrorURL(url)
	e.UI.Subst("下载: %s", url)
	f, err := os.CreateTemp("", "udev-install-*.sh")
	if err != nil {
		return "", err
	}
	path := f.Name()
	f.Close()
	if err := util.Download(e.Ctx, url, path); err != nil {
		os.Remove(path)
		return "", err
	}
	if err := os.Chmod(path, 0o700); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// BackupToolsEnv 过滤出与工具相关的 shell 配置行。
func (e *Env) BackupToolsEnv(pattern string) string {
	return util.GrepLines(pattern, e.ShellFiles())
}

// ExistsExecutable 判断文件是否为可执行文件。
func ExistsExecutable(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

func goArch() string { return runtime.GOARCH }
func goOS() string   { return runtime.GOOS }

// WritePlatformInfo 写入备份平台信息文件。
func WritePlatformInfo(path string, extra map[string]string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "ARCH=%s\n", goArch())
	fmt.Fprintf(&b, "OS=%s\n", goOS())
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, extra[k])
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
