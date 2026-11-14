package storage

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/hanamisakip/udev/internal/config"
	"github.com/hanamisakip/udev/internal/ui"
	"github.com/hanamisakip/udev/internal/util"
)

func ghStatus(cfg *config.Config) string {
	bin, ok := findTool(cfg, "gh")
	if !ok {
		return "未安装（可用 udev tools go 安装 gh）"
	}
	if _, err := util.Output(bin, "auth", "status", "--hostname", "github.com"); err != nil {
		return "已安装但未登录（" + bin + "）"
	}
	return "已登录（" + bin + "）"
}

func ghLogin(ctx context.Context, cfg *config.Config, u *ui.UI, o Options) error {
	bin, ok := findTool(cfg, "gh")
	if !ok {
		return fmt.Errorf("未找到 gh，请先运行: udev tools go（或安装 GitHub CLI）")
	}
	u.Header("GitHub 登录")

	host := strings.TrimSpace(o.Hostname)
	if host == "" {
		host = "github.com"
	}
	proto := strings.ToLower(strings.TrimSpace(o.GitProto))
	if proto == "" {
		proto = "https"
	}
	if proto != "https" && proto != "ssh" {
		return fmt.Errorf("--git-protocol 只能是 https 或 ssh")
	}

	if !o.Force {
		if _, err := util.Output(bin, "auth", "status", "--hostname", host); err == nil {
			saveLocalGhToken(bin, host, cfg, u)
			u.OKf("已登录 %s，跳过（--force 可重新登录）", host)
			return nil
		}
	}

	tokenFile := strings.TrimSpace(o.TokenFile)
	if tokenFile == "" {
		tokenFile = cfg.GhTokenFile()
	}
	tokenFile = expandUserPath(tokenFile)

	if o.Token != "" {
		if err := writeSecret(cfg.GhTokenFile(), o.Token+"\n"); err != nil {
			return fmt.Errorf("保存 token 失败: %w", err)
		}
		u.OKf("token 已保存: %s", cfg.GhTokenFile())
		tokenFile = cfg.GhTokenFile()
	}

	args := []string{"auth", "login", "--hostname", host, "--git-protocol", proto}
	if data, err := os.ReadFile(tokenFile); err == nil && strings.TrimSpace(string(data)) != "" {
		u.Subst("使用本地 token 文件: %s", tokenFile)
		cmd := exec.CommandContext(ctx, bin, append(args, "--with-token")...)
		cmd.Stdin = strings.NewReader(string(data))
		cmd.Stdout = u.Out
		cmd.Stderr = u.Err
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("gh 登录失败: %w", err)
		}
	} else {
		if !isTerminal(os.Stdin) {
			return fmt.Errorf("未提供 token，且当前不是交互式终端；请使用 --token 或 --token-file")
		}
		args = append(args, "--web")
		if proto == "ssh" {
			args = append(args, "--skip-ssh-key")
		}
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = u.Out
		cmd.Stderr = u.Err
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("gh 登录失败: %w", err)
		}
	}

	if out, err := util.Output(bin, "auth", "status", "--hostname", host); err != nil {
		return fmt.Errorf("gh 登录后校验失败: %v\n%s", err, strings.TrimSpace(out))
	}
	saveLocalGhToken(bin, host, cfg, u)
	u.OKf("GitHub 登录成功: %s", host)
	return nil
}

// saveLocalGhToken 把 gh 当前 token 备份到 ~/.udev/secrets，供 update 与重新登录使用。
func saveLocalGhToken(bin, host string, cfg *config.Config, u *ui.UI) {
	token, err := util.Output(bin, "auth", "token", "--hostname", host)
	if err != nil || strings.TrimSpace(token) == "" {
		return
	}
	if err := writeSecret(cfg.GhTokenFile(), strings.TrimSpace(token)+"\n"); err != nil {
		u.Warnf("保存本地 token 失败: %v", err)
		return
	}
	u.OKf("token 已保存到本地: %s", cfg.GhTokenFile())
}
