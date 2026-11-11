package manager

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hanamisakip/udev/internal/util"
)

type rustManager struct{ e *Env }

func (m *rustManager) Name() string       { return "rust" }
func (m *rustManager) InstallDir() string { return m.e.Cfg.Paths.CargoHome }

func (m *rustManager) Installed() bool {
	for _, name := range []string{"rustup", "cargo"} {
		if ExistsExecutable(filepath.Join(m.e.Cfg.CargoBinDir(), name)) {
			return true
		}
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}
	return false
}

func (m *rustManager) Version() string {
	bin := filepath.Join(m.e.Cfg.CargoBinDir(), "rustc")
	if !ExistsExecutable(bin) {
		if p, err := exec.LookPath("rustc"); err == nil {
			bin = p
		} else {
			return ""
		}
	}
	out, err := util.Output(bin, "--version")
	if err != nil {
		return ""
	}
	return firstLine(out)
}

func (m *rustManager) Backup() (string, error) {
	out, err := BackupPath(m.e.Cfg, "rust")
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp("", "udev-rust-backup-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	stage, err := os.MkdirTemp("", "udev-rust-stage-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)

	m.e.DiskspaceWarning()
	found := false

	if util.IsDir(m.e.Cfg.Paths.RustupHome) {
		m.e.UI.Subst("打包 %s", m.e.Cfg.Paths.RustupHome)
		ok, err := util.PackDirStage(stage, m.e.Cfg.Paths.RustupHome, filepath.Join(tmp, "rustup"), BaseExcludes, true)
		if err != nil {
			os.Remove(out)
			return "", err
		}
		found = found || ok
	}
	if util.IsDir(m.e.Cfg.Paths.CargoHome) {
		m.e.UI.Subst("打包 %s（排除 registry / git 缓存）", m.e.Cfg.Paths.CargoHome)
		excludes := append(append([]string{}, BaseExcludes...), "registry", "git")
		ok, err := util.PackDirStage(stage, m.e.Cfg.Paths.CargoHome, filepath.Join(tmp, "cargo"), excludes, true)
		if err != nil {
			os.Remove(out)
			return "", err
		}
		found = found || ok
	}

	if !found {
		os.Remove(out)
		return "", fmt.Errorf("未找到 Rust 相关目录（%s / %s）", m.e.Cfg.Paths.RustupHome, m.e.Cfg.Paths.CargoHome)
	}

	envConfig := m.e.BackupToolsEnv(`(RUSTUP_HOME|CARGO_HOME|\.cargo/env|\.cargo/bin)`)
	if envConfig != "" {
		_ = os.WriteFile(filepath.Join(tmp, "env_config"), []byte(envConfig), 0o644)
	}
	_ = WritePlatformInfo(filepath.Join(tmp, "platform_info"), map[string]string{
		"RUSTUP_HOME": m.e.Cfg.Paths.RustupHome,
		"CARGO_HOME":  m.e.Cfg.Paths.CargoHome,
	})

	if _, err := util.CreateTarGz(out, tmp, []string{"."}, nil); err != nil {
		os.Remove(out)
		return "", fmt.Errorf("生成 Rust 备份失败: %w", err)
	}
	return out, nil
}

func (m *rustManager) Restore(archive string) error {
	if err := util.ValidateTarGz(archive); err != nil {
		return err
	}
	temp, err := os.MkdirTemp("", "udev-rust-restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)

	m.e.UI.Subst("解压 Rust 备份")
	if err := util.ExtractTar(archive, temp, true); err != nil {
		return err
	}
	if data, err := os.ReadFile(filepath.Join(temp, "platform_info")); err == nil {
		m.e.UI.Subst("备份平台信息:")
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			fmt.Fprintf(m.e.UI.Out, "         %s\n", line)
		}
	}

	restored := false
	if util.IsDir(filepath.Join(temp, "rustup")) {
		m.e.UI.Subst("恢复 %s", m.e.Cfg.Paths.RustupHome)
		if err := util.CopyTree(filepath.Join(temp, "rustup"), m.e.Cfg.Paths.RustupHome); err != nil {
			return err
		}
		restored = true
	}
	if util.IsDir(filepath.Join(temp, "cargo")) {
		m.e.UI.Subst("恢复 %s", m.e.Cfg.Paths.CargoHome)
		if err := util.CopyTree(filepath.Join(temp, "cargo"), m.e.Cfg.Paths.CargoHome); err != nil {
			return err
		}
		restored = true
	}
	if !restored {
		return fmt.Errorf("备份中未找到可恢复的 Rust 数据")
	}

	m.e.AddShellLine(fmt.Sprintf("export RUSTUP_HOME=%s", util.ShellQuote(m.e.Cfg.Paths.RustupHome)))
	m.e.AddShellLine(fmt.Sprintf("export CARGO_HOME=%s", util.ShellQuote(m.e.Cfg.Paths.CargoHome)))
	m.e.AddShellLine(fmt.Sprintf(". %s", util.ShellQuote(filepath.Join(m.e.Cfg.Paths.CargoHome, "env"))))
	m.e.AddShellLine(fmt.Sprintf(`export PATH="%s:$PATH"`, m.e.Cfg.CargoBinDir()))
	return nil
}

func (m *rustManager) Install() error {
	script, err := m.e.DownloadScript(m.e.Cfg.Managers.Rust.InstallURL)
	if err != nil {
		return fmt.Errorf("下载 rustup 安装脚本失败: %w", err)
	}
	defer os.Remove(script)

	toolchain := m.e.Cfg.Managers.Rust.DefaultToolchain
	if toolchain == "" {
		toolchain = "stable"
	}
	args := []string{"-y", "--default-toolchain", toolchain}
	if m.e.NoModifyPath || m.e.Cfg.Managers.Rust.NoModifyPath {
		args = append(args, "--no-modify-path")
	}
	env := util.SetEnv(os.Environ(),
		"RUSTUP_HOME="+m.e.Cfg.Paths.RustupHome,
		"CARGO_HOME="+m.e.Cfg.Paths.CargoHome,
	)

	m.e.UI.Subst("执行 rustup 安装脚本")
	if err := util.RunScript(m.e.UI, "执行 rustup 安装脚本", "sh", script, args, env); err != nil {
		return fmt.Errorf("rustup 安装失败")
	}

	m.e.AddShellLine(fmt.Sprintf("export RUSTUP_HOME=%s", util.ShellQuote(m.e.Cfg.Paths.RustupHome)))
	m.e.AddShellLine(fmt.Sprintf("export CARGO_HOME=%s", util.ShellQuote(m.e.Cfg.Paths.CargoHome)))
	if m.e.NoModifyPath || m.e.Cfg.Managers.Rust.NoModifyPath {
		m.e.AddShellLine(fmt.Sprintf(". %s", util.ShellQuote(filepath.Join(m.e.Cfg.Paths.CargoHome, "env"))))
	}
	return nil
}

func (m *rustManager) Clean() error {
	_ = os.RemoveAll(m.e.Cfg.Paths.RustupHome)
	_ = os.RemoveAll(filepath.Join(m.e.Cfg.Paths.CargoHome, "registry"))
	_ = os.RemoveAll(filepath.Join(m.e.Cfg.Paths.CargoHome, "git"))
	return nil
}
