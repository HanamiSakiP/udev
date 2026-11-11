package manager

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hanamisakip/udev/internal/util"
)

type pyenvManager struct{ e *Env }

func (m *pyenvManager) Name() string       { return "pyenv" }
func (m *pyenvManager) InstallDir() string { return m.e.Cfg.Paths.PyenvRoot }
func (m *pyenvManager) Installed() bool {
	if ExistsExecutable(m.e.Cfg.PyenvBin()) {
		return true
	}
	_, err := exec.LookPath("pyenv")
	return err == nil
}

func (m *pyenvManager) Version() string {
	bin := m.e.Cfg.PyenvBin()
	if !ExistsExecutable(bin) {
		if p, err := exec.LookPath("pyenv"); err == nil {
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

func (m *pyenvManager) Backup() (string, error) {
	root := m.e.Cfg.Paths.PyenvRoot
	if !util.IsDir(root) {
		return "", fmt.Errorf("pyenv 根目录不存在: %s", root)
	}
	out, err := BackupPath(m.e.Cfg, "pyenv")
	if err != nil {
		return "", err
	}
	m.e.DiskspaceWarning()
	m.e.UI.Subst("打包 %s", root)
	warned, err := util.CreateTarGz(out, filepath.Dir(root), []string{filepath.Base(root)}, BaseExcludes)
	if err != nil {
		os.Remove(out)
		return "", fmt.Errorf("打包 pyenv 失败: %w", err)
	}
	if warned {
		m.e.UI.Warnf("pyenv 备份部分文件被跳过（已容忍）")
	}
	return out, nil
}

func (m *pyenvManager) Restore(archive string) error {
	if err := util.ValidateTarGz(archive); err != nil {
		return err
	}
	temp, err := os.MkdirTemp("", "udev-pyenv-restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)

	m.e.UI.Subst("解压 pyenv 备份")
	if err := util.ExtractTar(archive, temp, true); err != nil {
		return err
	}

	source := temp
	if util.IsDir(filepath.Join(temp, ".pyenv")) {
		source = filepath.Join(temp, ".pyenv")
	} else if entries, _ := os.ReadDir(temp); len(entries) == 1 && entries[0].IsDir() {
		source = filepath.Join(temp, entries[0].Name())
	}

	root := m.e.Cfg.Paths.PyenvRoot
	m.e.UI.Subst("复制到 %s", root)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	if err := util.CopyTree(source, root); err != nil {
		return err
	}

	m.e.AddShellLine(fmt.Sprintf("export PYENV_ROOT=%s", util.ShellQuote(root)))
	m.e.AddShellLine(`[[ -d $PYENV_ROOT/bin ]] && export PATH="$PYENV_ROOT/bin:$PATH"`)
	m.e.AddShellLine(`eval "$(pyenv init - bash)"`)
	return nil
}

func (m *pyenvManager) Install() error {
	url := m.e.Cfg.Managers.Pyenv.InstallURL
	if m.e.Cfg.Mirror.UseCN {
		// 国内环境下改用 raw 安装脚本，DownloadScript 会转换为 jsdelivr 镜像。
		url = "https://raw.githubusercontent.com/pyenv/pyenv-installer/master/bin/pyenv-installer"
	}
	script, err := m.e.DownloadScript(url)
	if err != nil {
		return fmt.Errorf("下载 pyenv 安装脚本失败: %w", err)
	}
	defer os.Remove(script)

	m.e.UI.Subst("执行 pyenv 安装脚本")
	if err := util.RunScript(m.e.UI, "执行 pyenv 安装脚本", "bash", script, nil, m.e.GitEnv()); err != nil {
		return fmt.Errorf("pyenv 安装脚本执行失败，可尝试 CN_GITHUB_PROXY=https://ghproxy.net/ udev install --cn pyenv")
	}

	root := m.e.Cfg.Paths.PyenvRoot
	m.e.AddShellLine(fmt.Sprintf("export PYENV_ROOT=%s", util.ShellQuote(root)))
	m.e.AddShellLine(`[[ -d $PYENV_ROOT/bin ]] && export PATH="$PYENV_ROOT/bin:$PATH"`)
	m.e.AddShellLine(`eval "$(pyenv init - bash)"`)

	if m.e.Cfg.Mirror.UseCN {
		m.e.AddShellLine(fmt.Sprintf("export PYTHON_BUILD_MIRROR_URL=%s", util.ShellQuote(m.e.Cfg.Mirror.Python)))
		m.e.AddShellLine("export PYTHON_BUILD_MIRROR_URL_SKIP_CHECKSUM=1")
	}
	return nil
}

func (m *pyenvManager) Clean() error {
	// 不主动删除 pyenv，避免误删用户已安装的 Python 版本。
	m.e.UI.Subst("保留已有 pyenv 目录（避免误删 Python 版本）")
	return nil
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' || r == '\r' {
			return s[:i]
		}
	}
	return s
}
