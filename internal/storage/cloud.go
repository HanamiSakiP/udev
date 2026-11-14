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

func rcloneStatus(cfg *config.Config) string {
	bin, ok := findTool(cfg, "rclone")
	if !ok {
		return "未安装（可用 udev tools go 安装 rclone）"
	}
	conf := cfg.RcloneConfig()
	if _, err := os.Stat(conf); err != nil {
		return "已安装，暂无远程存储（" + conf + "）"
	}
	env := util.SetEnv(os.Environ(), "RCLONE_CONFIG="+conf)
	out, err := util.OutputEnv(env, "", bin, "listremotes", "--config", conf)
	if err != nil {
		return "配置不可读（" + conf + "）"
	}
	var remotes []string
	for _, line := range strings.Split(out, "\n") {
		if r := strings.TrimSuffix(strings.TrimSpace(line), ":"); r != "" {
			remotes = append(remotes, r)
		}
	}
	if len(remotes) == 0 {
		return "已安装，暂无远程存储（" + conf + "）"
	}
	return "远程: " + strings.Join(remotes, ", ")
}

func createRemote(ctx context.Context, cfg *config.Config, u *ui.UI, o Options) error {
	bin, ok := findTool(cfg, "rclone")
	if !ok {
		return fmt.Errorf("未找到 rclone，请先运行: udev tools go（或安装 rclone）")
	}
	u.Header("云存储")

	remote := strings.TrimSpace(o.Remote)
	if remote == "" {
		remote = cfg.RemoteName(o.Name)
	}
	if !validRemoteName(remote) {
		return fmt.Errorf("无效的 rclone 远程名: %q（不能包含空白、冒号或路径分隔符）", remote)
	}
	provider := o.Provider
	if provider == "" {
		provider = cfg.StorageProvider()
	}
	conf := cfg.RcloneConfig()
	if err := ensurePrivateDir(filepath.Dir(conf)); err != nil {
		return err
	}
	env := util.SetEnv(os.Environ(), "RCLONE_CONFIG="+conf)

	if !o.Force && remoteExists(bin, conf, remote) {
		_ = os.Chmod(conf, 0o600)
		u.OKf("远程存储 %s 已存在，跳过（--force 可重新配置）", remote)
		u.Subst("rclone 配置: %s", conf)
		return nil
	}

	args := []string{"config", "create", remote, provider, "--config", conf}
	passFile := cfg.RcloneRemotePasswordFile(remote)
	if provider == "pcloud" && strings.TrimSpace(o.User) != "" {
		pass := o.PCloudPW
		if pass == "" {
			if v, err := readSecret(passFile); err == nil && v != "" {
				pass = v
			} else {
				var err error
				pass, err = promptSecret(u, "pCloud 密码: ")
				if err != nil {
					return err
				}
			}
		}
		if pass != "" {
			if err := writeSecret(passFile, pass+"\n"); err != nil {
				return fmt.Errorf("保存 pCloud 密码失败: %w", err)
			}
			u.OKf("pCloud 密码已保存: %s", passFile)
			args = append(args, "username="+strings.TrimSpace(o.User), "password="+pass, "--obscure")
		}
	}

	u.Subst("创建 rclone 远程: %s (%s)", remote, provider)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = u.Out
	cmd.Stderr = u.Err
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rclone config create 失败: %w", err)
	}
	if !remoteExists(bin, conf, remote) {
		return fmt.Errorf("rclone 未创建远程 %s，请检查上面的输出", remote)
	}
	_ = os.Chmod(conf, 0o600)
	u.OKf("远程存储已就绪: %s", remote)
	u.Subst("rclone 配置（含 token）: %s", conf)
	return nil
}

func remoteExists(bin, conf, remote string) bool {
	env := util.SetEnv(os.Environ(), "RCLONE_CONFIG="+conf)
	out, err := util.OutputEnv(env, "", bin, "listremotes", "--config", conf)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSuffix(strings.TrimSpace(line), ":") == remote {
			return true
		}
	}
	return false
}

func validRemoteName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '@':
		default:
			return false
		}
	}
	return true
}
