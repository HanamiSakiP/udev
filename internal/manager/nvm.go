package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hanamisakip/udev/internal/util"
)

type nvmManager struct{ e *Env }

func (m *nvmManager) Name() string       { return "nvm" }
func (m *nvmManager) InstallDir() string { return m.e.Cfg.Paths.NvmDir }

func (m *nvmManager) Installed() bool {
	st, err := os.Stat(m.e.Cfg.NvmScript())
	return err == nil && st.Size() > 0
}

func (m *nvmManager) Version() string {
	if !m.Installed() {
		return ""
	}
	script := fmt.Sprintf(`export NVM_DIR=%s
. "$NVM_DIR/nvm.sh" >/dev/null 2>&1
nvm --version 2>/dev/null`, util.ShellQuote(m.e.Cfg.Paths.NvmDir))
	out, err := m.e.BashOutput(script)
	if err != nil {
		return ""
	}
	return firstLine(strings.TrimSpace(out))
}

func (m *nvmManager) Backup() (string, error) {
	root := m.e.Cfg.Paths.NvmDir
	if !util.IsDir(root) {
		return "", fmt.Errorf("%s 不存在，跳过 nvm 备份", root)
	}
	out, err := BackupPath(m.e.Cfg, "nvm")
	if err != nil {
		return "", err
	}
	m.e.DiskspaceWarning()

	excludes := ExcludesWithout(BaseExcludes, "package.json", "package-lock.json")
	m.e.UI.Subst("打包 %s", root)
	warned, err := util.CreateTarGz(out, filepath.Dir(root), []string{filepath.Base(root)}, excludes)
	if err != nil {
		os.Remove(out)
		return "", fmt.Errorf("打包 nvm 失败: %w", err)
	}
	if warned {
		m.e.UI.Warnf("nvm 备份部分文件被跳过（已容忍）")
	}
	return out, nil
}

func (m *nvmManager) Restore(archive string) error {
	if err := util.ValidateTarGz(archive); err != nil {
		return err
	}
	temp, err := os.MkdirTemp("", "udev-nvm-restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)

	m.e.UI.Subst("解压 nvm 备份")
	if err := util.ExtractTar(archive, temp, true); err != nil {
		return err
	}

	source := filepath.Join(temp, ".nvm")
	if !util.IsDir(source) {
		entries, _ := os.ReadDir(temp)
		if len(entries) == 1 && entries[0].IsDir() {
			source = filepath.Join(temp, entries[0].Name())
		}
	}
	if !util.IsDir(source) {
		return fmt.Errorf("备份中未找到 nvm 目录")
	}

	root := m.e.Cfg.Paths.NvmDir
	m.e.UI.Subst("恢复 %s", root)
	if err := util.CopyPath(source, root); err != nil {
		return err
	}

	block := fmt.Sprintf(`export NVM_DIR=%s
[ -s "$NVM_DIR/nvm.sh" ] && \. "$NVM_DIR/nvm.sh" # This loads nvm`, util.ShellQuote(root))
	m.e.AddShellBlock("NVM_DIR", block)
	return nil
}

func (m *nvmManager) Install() error {
	script, err := m.e.DownloadScript(m.e.Cfg.Managers.Nvm.InstallURL)
	if err != nil {
		return fmt.Errorf("下载 nvm 安装脚本失败: %w", err)
	}
	defer os.Remove(script)

	if err := util.RunScript(m.e.UI, "执行 nvm 安装脚本", "bash", script, nil, m.e.GitEnv()); err != nil {
		return fmt.Errorf("nvm 安装失败")
	}

	root := m.e.Cfg.Paths.NvmDir
	m.e.AddShellLine(fmt.Sprintf("export NVM_DIR=%s", util.ShellQuote(root)))
	m.e.AddShellLine(`[ -s "$NVM_DIR/nvm.sh" ] && \. "$NVM_DIR/nvm.sh"`)
	m.e.AddShellLine(`[ -s "$NVM_DIR/bash_completion" ] && \. "$NVM_DIR/bash_completion"`)

	if m.e.Cfg.Mirror.UseCN {
		m.e.AddShellLine(fmt.Sprintf("export NVM_NODEJS_ORG_MIRROR=%s", util.ShellQuote(m.e.Cfg.Mirror.Node)))
	}
	return nil
}

func (m *nvmManager) Clean() error {
	_ = os.RemoveAll(m.e.Cfg.Paths.NvmDir)
	m.e.RemoveShellLines([]string{`NVM_DIR`, `nvm\.sh`, `nvm/bash_completion`, `NVM_NODEJS_ORG_MIRROR`}, "", nil)
	return nil
}
