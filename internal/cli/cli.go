// Package cli 实现 udev 的命令行入口与子命令调度。
package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/hanamisakip/udev/internal/config"
	"github.com/hanamisakip/udev/internal/devtools"
	"github.com/hanamisakip/udev/internal/manager"
	"github.com/hanamisakip/udev/internal/ui"
	"github.com/hanamisakip/udev/internal/uninstall"
)

// Version 由构建时通过 -ldflags 覆盖，或直接修改此处。
var Version = "1.0.0"

// Run 执行命令行，返回进程退出码。
func Run(args []string) int {
	u := ui.New()
	if len(args) == 0 {
		printHelp(u)
		return 1
	}
	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "help", "-h", "--help":
		printHelp(u)
		return 0
	case "version", "-v", "--version":
		fmt.Fprintf(u.Out, "udev %s\n", Version)
		return 0
	case "__complete":
		for _, c := range completeWords(rest) {
			fmt.Fprintln(u.Out, c)
		}
		return 0
	}

	cfg, err := config.Load()
	if err != nil {
		u.Errorf("%v", err)
		return 1
	}
	created, err := cfg.Ensure()
	if err != nil {
		u.Errorf("初始化配置目录失败: %v", err)
		return 1
	}
	if created {
		u.Infof("已生成默认配置: %s", cfg.File)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "install", "i":
		err = cmdInstall(ctx, cfg, u, rest)
	case "backup", "b":
		err = cmdBackup(cfg, u, rest)
	case "restore", "r", "recover":
		err = cmdRestore(ctx, cfg, u, rest)
	case "status", "st", "s":
		err = cmdStatus(cfg, u)
	case "list", "ls", "l":
		err = cmdList(cfg, u, rest)
	case "tools", "dev", "dev-tool":
		if len(rest) > 0 && (rest[0] == "-h" || rest[0] == "--help") {
			printHelp(u)
			return 0
		}
		err = devtools.Run(ctx, cfg, u, rest)
	case "uninstall", "remove", "rm":
		err = cmdUninstall(ctx, cfg, u, rest)
	case "config", "cfg":
		err = cmdConfig(cfg, u, rest)
	case "completion", "comp":
		err = cmdCompletion(cfg, u, rest)
	default:
		u.Errorf("未知命令: %s", cmd)
		printHelp(u)
		return 1
	}
	if err != nil {
		u.Errorf("%v", err)
		return 1
	}
	return 0
}

type installOptions struct {
	CN           bool
	Force        bool
	NoRestore    bool
	NoModifyPath bool
}

func cmdInstall(ctx context.Context, cfg *config.Config, u *ui.UI, args []string) error {
	opts := installOptions{}
	var names []string
	for _, a := range args {
		switch a {
		case "--cn", "-c":
			opts.CN = true
		case "--force", "-f":
			opts.Force = true
		case "--no-restore":
			opts.NoRestore = true
		case "--no-modify-path":
			opts.NoModifyPath = true
		case "-h", "--help":
			printHelp(u)
			return nil
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("未知参数: %s", a)
			}
			names = append(names, a)
		}
	}
	if opts.CN {
		cfg.Mirror.UseCN = true
	}
	env := &manager.Env{Cfg: cfg, UI: u, Ctx: ctx, NoModifyPath: opts.NoModifyPath}
	managers, err := manager.ResolveMany(env, names)
	if err != nil {
		return err
	}

	u.Header("udev install")
	u.Infof("目标工具: %s", managerNames(managers))
	if cfg.Mirror.UseCN {
		u.Infof("启用镜像源")
	}
	if opts.Force {
		u.Warnf("启用 --force，将重新安装已存在的工具")
	}
	if err := checkDeps(true); err != nil {
		return err
	}

	var failed []string
	for i, m := range managers {
		fmt.Fprintf(u.Out, "\n%s ==> [%d/%d] %s\n", u.Bold(""), i+1, len(managers), m.Name())

		if m.Installed() && !opts.Force {
			u.Warnf("%s 已安装，跳过（如需强制重装请加 --force）", m.Name())
			continue
		}
		if opts.Force && m.Installed() {
			u.Subst("清理已有的 %s", m.Name())
			if err := m.Clean(); err != nil {
				u.Warnf("清理 %s 失败: %v", m.Name(), err)
			}
		}

		restored := false
		if !opts.NoRestore {
			if backup := manager.LatestBackup(cfg, m.Name()); backup != "" {
				u.Subst("发现本地备份: %s", filepath.Base(backup))
				if err := m.Restore(backup); err == nil {
					restored = true
					u.OKf("%s 已从备份恢复", m.Name())
				} else {
					u.Warnf("%s 从备份恢复失败，将回退到在线安装: %v", m.Name(), err)
				}
			}
		} else {
			u.Subst("已禁用本地恢复 (--no-restore)")
		}

		if !restored {
			u.Subst("在线安装 %s", m.Name())
			if err := m.Install(); err != nil {
				u.Errorf("%s 安装失败: %v", m.Name(), err)
				failed = append(failed, m.Name())
				continue
			}
		}
		if !m.Installed() {
			u.Errorf("%s 验证失败", m.Name())
			failed = append(failed, m.Name())
		} else {
			u.OKf("%s 验证通过", m.Name())
		}
	}

	fmt.Fprintln(u.Out)
	if len(failed) > 0 {
		return fmt.Errorf("以下工具处理失败: %s", strings.Join(failed, " "))
	}
	u.OKf("=== 全部完成 ===")
	u.HintShellReload()
	return nil
}

func cmdBackup(cfg *config.Config, u *ui.UI, args []string) error {
	names, help, err := collectToolArgs(u, args)
	if help || err != nil {
		return err
	}
	env := &manager.Env{Cfg: cfg, UI: u, Ctx: context.Background()}
	managers, err := manager.ResolveMany(env, names)
	if err != nil {
		return err
	}
	u.Header("udev backup")
	u.Infof("备份目录: %s", cfg.Paths.BackupDir)

	var failed, succeeded []string
	for i, m := range managers {
		fmt.Fprintf(u.Out, "\n%s ==> [%d/%d] 备份 %s\n", u.Bold(""), i+1, len(managers), m.Name())
		out, err := m.Backup()
		if err != nil {
			u.Errorf("%s 备份失败: %v", m.Name(), err)
			failed = append(failed, m.Name())
			continue
		}
		u.OKf("已创建: %s", out)
		succeeded = append(succeeded, m.Name())
	}
	fmt.Fprintln(u.Out)
	if len(succeeded) > 0 {
		u.OKf("已备份: %s", strings.Join(succeeded, " "))
	}
	if len(failed) > 0 {
		return fmt.Errorf("备份失败: %s", strings.Join(failed, " "))
	}
	return nil
}

func cmdRestore(ctx context.Context, cfg *config.Config, u *ui.UI, args []string) error {
	force := false
	var names []string
	for _, a := range args {
		switch a {
		case "--force", "-f":
			force = true
		case "-h", "--help":
			printHelp(u)
			return nil
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("未知参数: %s", a)
			}
			names = append(names, a)
		}
	}
	env := &manager.Env{Cfg: cfg, UI: u, Ctx: ctx}
	managers, err := manager.ResolveMany(env, names)
	if err != nil {
		return err
	}
	u.Header("udev restore")

	var failed, restored []string
	for i, m := range managers {
		fmt.Fprintf(u.Out, "\n%s ==> [%d/%d] 恢复 %s\n", u.Bold(""), i+1, len(managers), m.Name())
		if m.Installed() && !force {
			u.Warnf("%s 已安装，跳过（如需覆盖恢复请加 --force）", m.Name())
			continue
		}
		backup := manager.LatestBackup(cfg, m.Name())
		if backup == "" {
			u.Warnf("未找到 %s 的备份，跳过", m.Name())
			continue
		}
		u.Subst("使用备份: %s", filepath.Base(backup))
		if err := m.Restore(backup); err != nil {
			u.Errorf("%s 恢复失败: %v", m.Name(), err)
			failed = append(failed, m.Name())
			continue
		}
		if !m.Installed() {
			u.Errorf("%s 恢复后验证失败", m.Name())
			failed = append(failed, m.Name())
			continue
		}
		restored = append(restored, m.Name())
	}
	fmt.Fprintln(u.Out)
	if len(restored) > 0 {
		u.OKf("已恢复: %s", strings.Join(restored, " "))
	}
	if len(failed) > 0 {
		return fmt.Errorf("恢复失败: %s", strings.Join(failed, " "))
	}
	u.HintShellReload()
	return nil
}

func cmdStatus(cfg *config.Config, u *ui.UI) error {
	u.Header("udev status")
	fmt.Fprintf(u.Out, "%-8s %-12s %-38s %s\n", "TOOL", "STATUS", "LOCATION", "VERSION")
	fmt.Fprintln(u.Out, strings.Repeat("-", 78))

	for _, m := range manager.All(&manager.Env{Cfg: cfg, UI: u, Ctx: context.Background()}) {
		status := "missing"
		version := "-"
		if m.Installed() {
			status = "installed"
			if v := m.Version(); v != "" {
				version = v
			}
		}
		fmt.Fprintf(u.Out, "%-8s %-12s %-38s %s\n", m.Name(), status, m.InstallDir(), version)
		fmt.Fprintf(u.Out, "         %sbackups: %d%s\n", u.Dim(""), manager.CountBackups(cfg, m.Name()), "")
	}

	d := cfg.DevTools
	fmt.Fprintf(u.Out, "\n%s\n", u.Bold("开发工具清单"))
	fmt.Fprintf(u.Out, "  node   : npm %d 个，命令 %d 条\n", len(d.Node.Npm), len(d.Node.Commands))
	fmt.Fprintf(u.Out, "  go     : go install %d 个\n", len(d.Go.Packages))
	fmt.Fprintf(u.Out, "  python : pip %d 个\n", len(d.Python.Packages))
	fmt.Fprintf(u.Out, "  cargo  : %d 个\n", len(d.Cargo.Packages))

	fmt.Fprintf(u.Out, "\n配置目录: %s\n", cfg.Home)
	fmt.Fprintf(u.Out, "配置文件: %s\n", cfg.File)
	fmt.Fprintf(u.Out, "备份目录: %s\n", cfg.Paths.BackupDir)
	return nil
}

func cmdList(cfg *config.Config, u *ui.UI, args []string) error {
	names, help, err := collectToolArgs(u, args)
	if help || err != nil {
		return err
	}
	env := &manager.Env{Cfg: cfg, UI: u, Ctx: context.Background()}
	managers, err := manager.ResolveMany(env, names)
	if err != nil {
		return err
	}
	u.Header("udev list")
	for _, m := range managers {
		fmt.Fprintf(u.Out, "\n%s\n", u.Bold("["+m.Name()+"]"))
		files := manager.FindBackups(cfg, m.Name())
		if len(files) == 0 {
			fmt.Fprintf(u.Out, "  %s\n", u.Dim("(无备份)"))
			continue
		}
		sort.Strings(files)
		for _, f := range files {
			fmt.Fprintf(u.Out, "  %s  %s\n", filepath.Base(f), u.Dim("("+humanSize(f)+")"))
		}
	}
	fmt.Fprintf(u.Out, "\n备份目录: %s\n", cfg.Paths.BackupDir)
	return nil
}

func cmdUninstall(ctx context.Context, cfg *config.Config, u *ui.UI, args []string) error {
	opts := uninstall.Options{}
	for _, a := range args {
		switch a {
		case "-y", "--yes":
			opts.Yes = true
		case "-k", "--keep-gopath":
			opts.KeepGopath = true
		case "-h", "--help":
			printHelp(u)
			return nil
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("未知参数: %s", a)
			}
			opts.Managers = append(opts.Managers, a)
		}
	}
	u.Header("udev uninstall")
	return uninstall.Run(ctx, cfg, u, opts)
}

func cmdConfig(cfg *config.Config, u *ui.UI, args []string) error {
	sub := "show"
	if len(args) > 0 {
		sub = args[0]
	}
	if sub == "-h" || sub == "--help" {
		printHelp(u)
		return nil
	}
	switch sub {
	case "path":
		fmt.Fprintln(u.Out, cfg.File)
		return nil
	case "show", "":
		data, err := cfg.Marshal()
		if err != nil {
			return err
		}
		_, err = u.Out.Write(data)
		return err
	case "init":
		force := false
		for _, a := range args[1:] {
			if a == "--force" || a == "-f" {
				force = true
			}
		}
		if !force {
			if _, err := os.Stat(cfg.File); err == nil {
				u.Infof("配置文件已存在: %s（使用 --force 重置为默认值）", cfg.File)
				return nil
			}
		}
		if err := cfg.Reset(); err != nil {
			return err
		}
		u.OKf("已写入默认配置: %s", cfg.File)
		return nil
	case "edit":
		editor := os.Getenv("VISUAL")
		if editor == "" {
			editor = os.Getenv("EDITOR")
		}
		if editor == "" {
			editor = "vi"
		}
		cmd := exec.Command(editor, cfg.File)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	default:
		return fmt.Errorf("未知 config 子命令: %s（可选: show path init edit）", sub)
	}
}

func collectToolArgs(u *ui.UI, args []string) ([]string, bool, error) {
	var names []string
	for _, a := range args {
		if a == "-h" || a == "--help" {
			printHelp(u)
			return nil, true, nil
		}
		if strings.HasPrefix(a, "-") {
			return nil, false, fmt.Errorf("未知参数: %s", a)
		}
		names = append(names, a)
	}
	return names, false, nil
}

func managerNames(managers []manager.Manager) string {
	names := make([]string, 0, len(managers))
	for _, m := range managers {
		names = append(names, m.Name())
	}
	return strings.Join(names, " ")
}

func checkDeps(install bool) error {
	required := []string{"bash", "tar"}
	if install {
		required = append(required, "git", "curl")
	}
	var missing []string
	for _, dep := range required {
		if _, err := exec.LookPath(dep); err != nil {
			missing = append(missing, dep)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("缺少必需依赖: %s", strings.Join(missing, " "))
	}
	return nil
}

func humanSize(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return "?"
	}
	size := float64(st.Size())
	units := []string{"B", "K", "M", "G", "T"}
	i := 0
	for size >= 1024 && i < len(units)-1 {
		size /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f%s", size, units[i])
	}
	return fmt.Sprintf("%.1f%s", size, units[i])
}

func printHelp(u *ui.UI) {
	help := fmt.Sprintf(`%s v%s - 一体化开发工具管理器

用法: udev <命令> [选项] [工具/分组...]

命令:
  install   安装工具（优先本地备份恢复，否则在线安装）
  backup    备份已安装的工具
  restore   从备份恢复工具（已安装时跳过，除非 --force）
  status    查看工具安装状态、版本与备份数量
  list      列出可用备份
  tools     按配置文件批量安装开发工具
  uninstall 卸载工具链并清理 shell 配置
  config    查看/编辑配置（show | path | init | edit）
  completion 生成或安装 shell Tab 补全（bash | zsh | fish | install）
  version   显示版本
  help      显示帮助

工具:
  pyenv | g | nvm | rust | fvm | all（默认 all）

install 选项:
  --cn, -c          使用镜像源
  --force, -f       强制重装
  --no-restore      跳过本地备份恢复
  --no-modify-path  传给 rustup：不自动修改 PATH

restore 选项:
  --force, -f       覆盖已安装的工具

uninstall 选项:
  -y, --yes         跳过确认
  -k, --keep-gopath 保留 GOPATH

tools 分组:
  node | go | python | cargo | all
  默认执行 node go python

配置/存储目录:
  %s
  配置文件: %s

示例:
  udev status
  udev install --cn all
  udev install --force pyenv
  udev backup all
  udev restore --force nvm
  udev tools node go
  udev tools all
  udev uninstall -y -k
  udev config edit

说明:
  - npm/go/pip/cargo 工具清单全部位于配置文件中，直接编辑即可自定义
  - 环境变量可覆盖配置：UDEV_HOME、UDEV_BACKUP_DIR、PYENV_ROOT、NVM_DIR、
    GOPATH、RUSTUP_HOME、CARGO_HOME、FVM_CACHE_PATH、FVM_INSTALL_DIR、
    USE_CN_MIRROR=1
`, u.Bold("udev"), Version, "~/.udev", "~/.udev/config.yaml")
	fmt.Fprint(u.Out, help)
}
