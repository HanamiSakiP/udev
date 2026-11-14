package storage

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hanamisakip/udev/internal/config"
	"github.com/hanamisakip/udev/internal/ui"
	"github.com/hanamisakip/udev/internal/util"
)

func resticStatus(cfg *config.Config, repo, passFile string) string {
	bin, ok := findTool(cfg, "restic")
	if !ok {
		return "未安装（可用 udev tools go 安装 restic）"
	}
	if _, err := os.Stat(passFile); err != nil {
		return "密码文件不存在（" + passFile + "）"
	}
	if _, err := util.Output(bin, "--repo", repo, "--password-file", passFile, "cat", "config"); err != nil {
		return "未初始化或密码不匹配（" + repo + "）"
	}
	return "已初始化（" + repo + "）"
}

func initRestic(ctx context.Context, cfg *config.Config, u *ui.UI, o Options) error {
	bin, ok := findTool(cfg, "restic")
	if !ok {
		return fmt.Errorf("未找到 restic，请先运行: udev tools go（或安装 restic）")
	}
	u.Header("restic 本地仓库")

	repo := expandUserPath(o.Repo)
	if repo == "" {
		repo = cfg.ResticRepo(o.Name)
	}
	passFile := expandUserPath(o.PasswordFile)
	if passFile == "" {
		passFile = cfg.ResticPasswordFile(o.Name)
	}
	if err := ensurePrivateDir(cfg.SecretsDir()); err != nil {
		return err
	}
	if err := ensurePrivateDir(filepath.Dir(passFile)); err != nil {
		return err
	}

	if _, err := os.Stat(passFile); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		pw, err := generatePassword()
		if err != nil {
			return fmt.Errorf("生成 restic 密码失败: %w", err)
		}
		if err := writeSecret(passFile, pw+"\n"); err != nil {
			return fmt.Errorf("写入 restic 密码文件失败: %w", err)
		}
		u.OKf("已生成 restic 密码: %s", passFile)
	} else {
		if pw, err := readSecret(passFile); err != nil || pw == "" {
			return fmt.Errorf("restic 密码文件为空或不可读: %s", passFile)
		}
	}

	local := isLocalRepo(repo)
	if local {
		if err := os.MkdirAll(repo, 0o700); err != nil {
			return err
		}
		_ = os.Chmod(repo, 0o700)
	}

	initialized := false
	if out, err := util.Output(bin, "--repo", repo, "--password-file", passFile, "cat", "config"); err == nil {
		initialized = true
		_ = out
	} else {
		if local {
			entries, readErr := os.ReadDir(repo)
			if readErr == nil && len(entries) > 0 {
				if _, statErr := os.Stat(filepath.Join(repo, "config")); statErr == nil {
					return fmt.Errorf("restic 仓库已存在但无法读取，请检查密码文件 %s: %w", passFile, err)
				}
				if !o.Force {
					return fmt.Errorf("目录非空且不是 restic 仓库: %s（使用 --force 仍可尝试初始化）", repo)
				}
			}
		}
	}

	if initialized {
		u.OKf("restic 仓库已初始化，跳过")
	} else {
		u.Subst("初始化仓库: %s", repo)
		cmd := exec.CommandContext(ctx, bin, "init", "--repo", repo, "--password-file", passFile)
		cmd.Stdin = os.Stdin
		cmd.Stdout = u.Out
		cmd.Stderr = u.Err
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("restic init 失败: %w", err)
		}
		if _, err := util.Output(bin, "--repo", repo, "--password-file", passFile, "cat", "config"); err != nil {
			return fmt.Errorf("restic 仓库初始化后校验失败: %w", err)
		}
		u.OKf("restic 仓库初始化完成")
	}

	envFile := filepath.Join(cfg.Home, "restic", cfg.StorageName(o.Name)+".env")
	content := fmt.Sprintf("# 由 udev setup 生成，可 source 后直接使用 restic\nexport RESTIC_REPOSITORY=%s\nexport RESTIC_PASSWORD_FILE=%s\n",
		util.ShellQuote(repo), util.ShellQuote(passFile))
	if err := ensurePrivateDir(filepath.Dir(envFile)); err != nil {
		return err
	}
	if err := writeSecret(envFile, content); err != nil {
		return fmt.Errorf("写入 restic 环境文件失败: %w", err)
	}

	u.OKf("restic 仓库: %s", repo)
	u.Subst("密码文件: %s", passFile)
	u.Subst("环境文件: %s", envFile)
	u.Subst("使用: source %s", util.ShellQuote(envFile))
	return nil
}

// isLocalRepo 判断 restic 仓库地址是否为本地目录。
func isLocalRepo(repo string) bool {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return true
	}
	if strings.HasPrefix(repo, "/") || strings.HasPrefix(repo, "~") || strings.HasPrefix(repo, ".") {
		return true
	}
	if filepath.IsAbs(repo) {
		return true
	}
	// rclone: / rest: / sftp: / s3: 等远程后端都带冒号。
	if i := strings.Index(repo, ":"); i >= 0 {
		// Windows 盘符（如 C:\repo）在非 Windows 环境不会走到这里。
		return false
	}
	return true
}
