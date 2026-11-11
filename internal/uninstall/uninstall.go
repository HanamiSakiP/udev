// Package uninstall 卸载工具链并清理 shell 配置。
package uninstall

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hanamisakip/udev/internal/config"
	"github.com/hanamisakip/udev/internal/ui"
	"github.com/hanamisakip/udev/internal/util"
)

// Options 卸载参数。
type Options struct {
	Yes        bool
	KeepGopath bool
	Managers   []string
}

type target struct {
	name       string
	dirs       []string
	patterns   []string
	blockStart string
	blockEnd   []string
}

// Run 执行卸载流程。
func Run(ctx context.Context, cfg *config.Config, u *ui.UI, opts Options) error {
	targets, err := resolveTargets(cfg, opts.Managers)
	if err != nil {
		return err
	}

	home, _ := os.UserHomeDir()
	removeGopath := false
	for _, t := range targets {
		if t.name == "g" && cfg.Uninstall.IncludeGopath {
			removeGopath = true
		}
	}
	if opts.KeepGopath {
		removeGopath = false
	}

	if !opts.Yes {
		if !isInteractive() {
			return fmt.Errorf("非交互环境，请使用 -y/--yes 显式确认卸载")
		}
		printPlan(cfg, u, targets, removeGopath)
		fmt.Fprint(u.Out, "确认继续? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
		default:
			u.Warnf("已取消卸载")
			return nil
		}
	}

	backupDir := filepath.Join(cfg.Home, "backups", "uninstall", time.Now().Format("20060102-150405"))
	shellFiles := cfg.Shell.Files

	total := len(shellFiles) + len(targets) + 1
	if removeGopath {
		total++
	}
	step := 0

	// 1) 备份并清理 shell 配置
	for _, file := range shellFiles {
		step++
		u.ProgressStep(step, total, "清理 shell 配置: "+file)
		if err := cleanShellFile(file, targets, backupDir, u); err != nil {
			return err
		}
	}

	// 2) 删除工具目录
	for _, t := range targets {
		step++
		u.ProgressStep(step, total, "删除 "+t.name+" 相关目录")
		for _, dir := range t.dirs {
			if dir == "" {
				continue
			}
			if err := safeRemove(dir, home, u); err != nil {
				return err
			}
		}
	}

	// 3) GOPATH
	if removeGopath {
		step++
		u.ProgressStep(step, total, "删除 GOPATH: "+cfg.Paths.GoPath)
		if err := safeRemove(cfg.Paths.GoPath, home, u); err != nil {
			return err
		}
	}

	// 4) 残留检查
	step++
	u.ProgressStep(step, total, "检查残留")
	postCheck(cfg, u, targets)

	fmt.Fprintln(u.Out)
	u.OKf("======= 卸载完成 =======")
	if util.IsDir(backupDir) {
		fmt.Fprintf(u.Out, "配置备份目录: %s\n", backupDir)
		fmt.Fprintln(u.Out, "如需回滚，可将备份复制回原位置。")
	}
	u.HintShellReload()
	_ = ctx
	return nil
}

func resolveTargets(cfg *config.Config, names []string) ([]target, error) {
	all := func() []target {
		home, _ := os.UserHomeDir()
		return []target{
			{
				name: "pyenv",
				dirs: []string{cfg.Paths.PyenvRoot},
				patterns: []string{
					`PYENV_ROOT`, `pyenv init`, `pyenv virtualenv-init`,
					`pyenv/bin`, `^# pyenv`, `PYTHON_BUILD_MIRROR_URL`,
				},
			},
			{
				name: "g",
				dirs: []string{
					cfg.Paths.GoRoot,
					filepath.Join(home, ".goenv"),
					filepath.Join(home, ".config", "g"),
				},
				patterns: []string{
					`g shell setup`, `\.g/`, `GOROOT=.*\.g/go`, `G_MIRROR`,
					`^[[:space:]]*unalias[[:space:]]+g[[:space:]]*$`,
				},
				blockStart: "# g (Go Version Manager) configuration",
				blockEnd:   []string{"g shell setup", ".g/env"},
			},
			{
				name: "nvm",
				dirs: []string{cfg.Paths.NvmDir},
				patterns: []string{
					`NVM_DIR`, `nvm\.sh`, `nvm/bash_completion`,
					`^# nvm配置`, `^# nvm$`, `NVM_NODEJS_ORG_MIRROR`,
				},
			},
			{
				name: "rust",
				dirs: []string{
					cfg.Paths.RustupHome,
					filepath.Join(cfg.Paths.CargoHome, "registry"),
					filepath.Join(cfg.Paths.CargoHome, "git"),
				},
				patterns: []string{`RUSTUP_HOME`, `CARGO_HOME`, `\.cargo/env`, `\.cargo/bin`},
			},
			{
				name:     "fvm",
				dirs:     []string{cfg.Paths.FvmCache, cfg.Paths.FvmInstall},
				patterns: []string{`FVM_CACHE_PATH`, `FVM_INSTALL_DIR`, `fvm/bin`},
			},
		}
	}

	if len(names) == 0 {
		return all(), nil
	}
	byName := map[string]target{}
	for _, t := range all() {
		byName[t.name] = t
	}
	var out []target
	seen := map[string]bool{}
	add := func(name string) error {
		t, ok := byName[name]
		if !ok {
			return fmt.Errorf("未知工具: %s（可选: pyenv g nvm rust fvm all）", name)
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, t)
		}
		return nil
	}
	for _, n := range names {
		switch strings.ToLower(n) {
		case "all":
			for _, name := range []string{"pyenv", "g", "nvm", "rust", "fvm"} {
				_ = add(name)
			}
		case "pyenv", "python":
			_ = add("pyenv")
		case "g", "golang", "go":
			_ = add("g")
		case "nvm", "node", "nodejs":
			_ = add("nvm")
		case "rust", "rustup", "cargo":
			_ = add("rust")
		case "fvm", "flutter":
			_ = add("fvm")
		default:
			return nil, fmt.Errorf("未知工具: %s（可选: pyenv g nvm rust fvm all）", n)
		}
	}
	return out, nil
}

func printPlan(cfg *config.Config, u *ui.UI, targets []target, removeGopath bool) {
	fmt.Fprintln(u.Out, "即将卸载以下工具链:")
	for _, t := range targets {
		for _, d := range t.dirs {
			if d != "" {
				fmt.Fprintf(u.Out, "  - %-6s: %s\n", t.name, d)
			}
		}
	}
	if removeGopath {
		fmt.Fprintf(u.Out, "  - %-6s: %s（包含项目代码与已编译二进制，将被删除）\n", "GOPATH", cfg.Paths.GoPath)
	} else {
		fmt.Fprintf(u.Out, "  - %-6s: 保留（%s）\n", "GOPATH", cfg.Paths.GoPath)
	}
	fmt.Fprintln(u.Out, "\n并会备份后清理以下文件中的相关配置:")
	for _, f := range cfg.Shell.Files {
		fmt.Fprintf(u.Out, "  - %s\n", f)
	}
	fmt.Fprintln(u.Out)
}

func isInteractive() bool {
	st, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	out, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0 && out.Mode()&os.ModeCharDevice != 0
}

func cleanShellFile(file string, targets []target, backupDir string, u *ui.UI) error {
	if _, err := os.Stat(file); err != nil {
		if os.IsNotExist(err) {
			u.Subst("%s 不存在，跳过", file)
			return nil
		}
		return err
	}
	if !fileNeedsClean(file, targets) {
		u.Subst("%s 无需清理", file)
		return nil
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return err
	}
	backup := filepath.Join(backupDir, filepath.Base(file)+".bak")
	if err := util.CopyPath(file, backup); err != nil {
		return err
	}
	u.Subst("已备份 %s -> %s", file, backup)

	patterns := []string{}
	blockStart := ""
	blockEnd := []string{}
	for _, t := range targets {
		patterns = append(patterns, t.patterns...)
		if t.blockStart != "" {
			blockStart = t.blockStart
			blockEnd = append(blockEnd, t.blockEnd...)
		}
	}
	before, after, err := util.RemoveLines(file, patterns, blockStart, blockEnd)
	if err != nil {
		return err
	}
	u.Subst("已清理 %s（%d -> %d 行）", file, before, after)
	return nil
}

func fileNeedsClean(file string, targets []target) bool {
	data, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	text := string(data)
	for _, t := range targets {
		if t.blockStart != "" && strings.Contains(text, t.blockStart) {
			return true
		}
		for _, p := range t.patterns {
			re, err := regexp.Compile(p)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(text, "\n") {
				if re.MatchString(line) {
					return true
				}
			}
		}
	}
	return false
}

func safeRemove(path, home string, u *ui.UI) error {
	if path == "" {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if abs == "/" || abs == home {
		return fmt.Errorf("拒绝删除异常路径: %s", abs)
	}
	if !util.IsDir(abs) {
		u.Subst("未发现 %s，跳过", abs)
		return nil
	}
	u.Subst("删除 %s", abs)
	return os.RemoveAll(abs)
}

func postCheck(cfg *config.Config, u *ui.UI, targets []target) {
	u.Subst("检查残留...")
	found := false
	for _, t := range targets {
		for _, d := range t.dirs {
			if d != "" && util.IsDir(d) {
				u.Warnf("残留目录: %s", d)
				found = true
			}
		}
	}
	for _, f := range cfg.Shell.Files {
		if fileNeedsClean(f, targets) {
			u.Warnf("残留配置: %s（可手动检查）", f)
			found = true
		}
	}
	if !found {
		u.Subst("未发现残留")
	}
}
