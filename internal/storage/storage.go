// Package storage 提供云存储、gh 登录与 restic 本地仓库的一键配置。
package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hanamisakip/udev/internal/config"
	"github.com/hanamisakip/udev/internal/ui"
)

// Options 是 udev setup 的选项。
type Options struct {
	Name         string
	Provider     string
	Remote       string
	Repo         string
	PasswordFile string
	SecretsDir   string

	Token     string
	TokenFile string
	Hostname  string
	GitProto  string

	User     string
	PCloudPW string

	Force    bool
	NoGH     bool
	NoCloud  bool
	NoRestic bool
	Help     bool
	Sub      string
}

// RunSetup 执行 udev setup。
func RunSetup(ctx context.Context, cfg *config.Config, u *ui.UI, args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	if opts.Help {
		PrintHelp(u)
		return nil
	}
	if strings.TrimSpace(opts.SecretsDir) != "" {
		cfg.Storage.SecretsDir = expandUserPath(opts.SecretsDir)
	}
	if opts.Sub == "status" {
		return status(cfg, u, opts)
	}

	name := cfg.StorageName(opts.Name)
	opts.Name = name
	provider, err := normalizeProvider(opts.Provider, cfg.StorageProvider())
	if err != nil {
		return err
	}
	opts.Provider = provider

	u.Header("udev setup")
	u.Infof("存储名: %s", name)
	u.Infof("云存储: %s", provider)
	if opts.Remote == "" {
		opts.Remote = cfg.RemoteName(name)
	}
	u.Infof("远程名: %s", opts.Remote)
	if opts.Repo == "" {
		opts.Repo = cfg.ResticRepo(name)
	}
	u.Infof("restic 仓库: %s", opts.Repo)
	u.Infof("凭据目录: %s", cfg.SecretsDir())

	var errs []error
	runGH := !opts.NoGH && (opts.Sub == "all" || opts.Sub == "gh")
	runCloud := !opts.NoCloud && (opts.Sub == "all" || opts.Sub == "cloud")
	runRestic := !opts.NoRestic && (opts.Sub == "all" || opts.Sub == "restic")

	if runGH {
		if err := ghLogin(ctx, cfg, u, opts); err != nil {
			u.Errorf("gh 登录失败: %v", err)
			errs = append(errs, err)
		}
	}
	if runCloud {
		if err := createRemote(ctx, cfg, u, opts); err != nil {
			u.Errorf("创建远程存储失败: %v", err)
			errs = append(errs, err)
		}
	}
	if runRestic {
		if err := initRestic(ctx, cfg, u, opts); err != nil {
			u.Errorf("初始化 restic 仓库失败: %v", err)
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	if runGH || runCloud || runRestic {
		u.OKf("=== 配置完成 ===")
	}
	return nil
}

func parseOptions(args []string) (Options, error) {
	o := Options{Sub: "all"}
	var positional []string

	take := func(i *int, inline string, hasInline bool) (string, error) {
		if hasInline {
			return inline, nil
		}
		if *i+1 >= len(args) {
			return "", fmt.Errorf("参数缺少值")
		}
		*i = *i + 1
		return args[*i], nil
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		key, inline, hasInline := strings.Cut(a, "=")
		switch key {
		case "-h", "--help":
			o.Help = true
		case "-f", "--force":
			o.Force = true
		case "--no-gh":
			o.NoGH = true
		case "--no-cloud":
			o.NoCloud = true
		case "--no-restic":
			o.NoRestic = true
		case "-n", "--name":
			v, err := take(&i, inline, hasInline)
			if err != nil {
				return o, fmt.Errorf("--name %w", err)
			}
			o.Name = v
		case "-p", "--provider":
			v, err := take(&i, inline, hasInline)
			if err != nil {
				return o, fmt.Errorf("--provider %w", err)
			}
			o.Provider = v
		case "--remote":
			v, err := take(&i, inline, hasInline)
			if err != nil {
				return o, fmt.Errorf("--remote %w", err)
			}
			o.Remote = v
		case "--repo", "--restic-repo":
			v, err := take(&i, inline, hasInline)
			if err != nil {
				return o, fmt.Errorf("--repo %w", err)
			}
			o.Repo = v
		case "--password-file", "--pass-file":
			v, err := take(&i, inline, hasInline)
			if err != nil {
				return o, fmt.Errorf("--password-file %w", err)
			}
			o.PasswordFile = v
		case "--secrets-dir":
			v, err := take(&i, inline, hasInline)
			if err != nil {
				return o, fmt.Errorf("--secrets-dir %w", err)
			}
			o.SecretsDir = v
		case "--token":
			v, err := take(&i, inline, hasInline)
			if err != nil {
				return o, fmt.Errorf("--token %w", err)
			}
			o.Token = v
		case "--token-file":
			v, err := take(&i, inline, hasInline)
			if err != nil {
				return o, fmt.Errorf("--token-file %w", err)
			}
			o.TokenFile = v
		case "--hostname":
			v, err := take(&i, inline, hasInline)
			if err != nil {
				return o, fmt.Errorf("--hostname %w", err)
			}
			o.Hostname = v
		case "--git-protocol":
			v, err := take(&i, inline, hasInline)
			if err != nil {
				return o, fmt.Errorf("--git-protocol %w", err)
			}
			o.GitProto = v
		case "--user", "--pcloud-user":
			v, err := take(&i, inline, hasInline)
			if err != nil {
				return o, fmt.Errorf("--user %w", err)
			}
			o.User = v
		case "--pcloud-password", "--cloud-password":
			v, err := take(&i, inline, hasInline)
			if err != nil {
				return o, fmt.Errorf("--pcloud-password %w", err)
			}
			o.PCloudPW = v
		default:
			return o, fmt.Errorf("未知参数: %s", a)
		}
	}

	if len(positional) > 0 {
		sub := strings.ToLower(positional[0])
		switch sub {
		case "all", "gh", "github", "cloud", "restic", "status":
			o.Sub = sub
			if sub == "github" {
				o.Sub = "gh"
			}
		default:
			if p, ok := providerAlias(sub); ok {
				o.Sub = "cloud"
				o.Provider = p
			} else {
				return o, fmt.Errorf("未知 setup 子命令: %s（可选: all gh cloud restic status）", positional[0])
			}
		}
		if len(positional) > 1 {
			switch o.Sub {
			case "cloud":
				if o.Provider == "" {
					p, ok := providerAlias(strings.ToLower(positional[1]))
					if !ok {
						return o, fmt.Errorf("不支持的云存储: %s（可选: pcloud onedrive dropbox）", positional[1])
					}
					o.Provider = p
				}
			case "restic", "all":
				if o.Name == "" {
					o.Name = positional[1]
				}
			}
		}
	}
	return o, nil
}

func normalizeProvider(v, fallback string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		v = fallback
	}
	if p, ok := providerAlias(strings.ToLower(v)); ok {
		return p, nil
	}
	return "", fmt.Errorf("不支持的云存储: %s（可选: pcloud onedrive dropbox）", v)
}

func providerAlias(v string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "pcloud", "p-cloud":
		return "pcloud", true
	case "onedrive", "one-drive", "onedrive-business":
		return "onedrive", true
	case "dropbox":
		return "dropbox", true
	}
	return "", false
}

func status(cfg *config.Config, u *ui.UI, o Options) error {
	name := cfg.StorageName(o.Name)
	repo := expandUserPath(o.Repo)
	if repo == "" {
		repo = cfg.ResticRepo(name)
	}
	passFile := expandUserPath(o.PasswordFile)
	if passFile == "" {
		passFile = cfg.ResticPasswordFile(name)
	}
	provider := cfg.StorageProvider()
	if p, err := normalizeProvider(o.Provider, provider); err == nil {
		provider = p
	}
	u.Header("udev setup status")
	u.Infof("默认存储名: %s", name)
	u.Infof("默认云存储: %s", provider)
	u.Infof("远程名: %s", cfg.RemoteName(name))
	u.Infof("凭据目录: %s", cfg.SecretsDir())
	u.Infof("rclone 配置: %s", cfg.RcloneConfig())
	u.Infof("restic 仓库: %s", repo)
	u.Infof("restic 密码: %s", passFile)
	u.Infof("源码目录: %s", cfg.SourceDir())

	u.Subst("gh: %s", ghStatus(cfg))
	u.Subst("rclone: %s", rcloneStatus(cfg))
	u.Subst("restic: %s", resticStatus(cfg, repo, passFile))
	return nil
}

func expandUserPath(p string) string {
	p = os.ExpandEnv(strings.TrimSpace(p))
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func ensurePrivateDir(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

func findTool(cfg *config.Config, name string) (string, bool) {
	if p, err := exec.LookPath(name); err == nil {
		return p, true
	}
	candidates := []string{
		filepath.Join(cfg.GoBinDir(), name),
		filepath.Join(cfg.Paths.GoRoot, "bin", name),
		filepath.Join(cfg.CargoBinDir(), name),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, true
		}
	}
	return name, false
}

// PrintHelp 输出 udev setup 的帮助。
func PrintHelp(u *ui.UI) {
	fmt.Fprint(u.Out, `用法: udev setup [all|gh|cloud|restic|status] [选项]

一键完成：gh 登录、创建 rclone 远程存储、初始化 restic 本地仓库。
默认执行 all，并且所有凭据都保存在本地目录中。

子命令:
  all              gh + cloud + restic（默认）
  gh               GitHub CLI 登录
  cloud [provider] 创建 rclone 远程存储（pcloud | onedrive | dropbox）
  restic [name]    初始化本地 restic 仓库
  status           查看当前配置状态

通用选项:
  -n, --name NAME          存储名，默认取 storage.default_name（udev）
  -p, --provider NAME      云存储类型，默认 pcloud
      --remote NAME        rclone 远程名，默认 <name>-<provider>
      --repo PATH          restic 仓库，默认 <home>/restic/<name>
      --password-file FILE restic 密码文件，默认 <home>/secrets/restic-<name>.pass
      --secrets-dir DIR    密码/token 目录，默认 <home>/secrets
  -f, --force              已存在时重新创建
      --no-gh              在 all 中跳过 gh
      --no-cloud           在 all 中跳过云存储
      --no-restic          在 all 中跳过 restic

gh 选项:
      --token TOKEN        GitHub Personal Access Token（会保存到本地 token 文件）
      --token-file FILE    token 文件，默认 <home>/secrets/gh-token
      --hostname HOST      默认 github.com
      --git-protocol P     https 或 ssh，默认 https

pcloud 选项:
      --user USER          pCloud 用户名（可选）
      --pcloud-password PW pCloud 密码（可选，会保存到本地密码文件）

一键执行:
  udev setup
  udev setup gh --token-file ~/.udev/secrets/gh-token
  udev setup cloud onedrive --name udev
  udev setup restic --name udev

配置文件 storage 段可修改默认名字、仓库与凭据位置。
`)
}
