package manager

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hanamisakip/udev/internal/util"
)

type fvmManager struct{ e *Env }

func (m *fvmManager) Name() string       { return "fvm" }
func (m *fvmManager) InstallDir() string { return m.e.Cfg.Paths.FvmInstall }

func (m *fvmManager) binPath() string {
	bin := m.e.Cfg.FvmBin()
	if ExistsExecutable(bin) {
		return bin
	}
	if p, err := exec.LookPath("fvm"); err == nil {
		return p
	}
	return ""
}

func (m *fvmManager) Installed() bool { return m.binPath() != "" }

func (m *fvmManager) Version() string {
	bin := m.binPath()
	if bin == "" {
		return ""
	}
	out, err := util.Output(bin, "--version")
	if err != nil {
		return ""
	}
	return firstLine(out)
}

// cacheDir 优先使用 fvm list 报告的实际缓存目录。
func (m *fvmManager) cacheDir() string {
	bin := m.binPath()
	if bin == "" {
		return m.e.Cfg.Paths.FvmCache
	}
	out, err := util.Output(bin, "list")
	if err == nil {
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Cache directory:") {
				if p := strings.TrimSpace(strings.TrimPrefix(line, "Cache directory:")); p != "" {
					return p
				}
			}
		}
	}
	return m.e.Cfg.Paths.FvmCache
}

func (m *fvmManager) Backup() (string, error) {
	out, err := BackupPath(m.e.Cfg, "fvm")
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp("", "udev-fvm-backup-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	stage, err := os.MkdirTemp("", "udev-fvm-stage-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)

	m.e.DiskspaceWarning()
	cacheDir := m.cacheDir()
	installDir := m.e.Cfg.Paths.FvmInstall
	found := false

	cacheExcludes := append(append([]string{}, BaseExcludes...),
		"versions/*/bin/cache/artifacts",
		"versions/*/bin/cache/engine",
	)
	if util.IsDir(cacheDir) {
		m.e.UI.Subst("打包 %s（FVM 缓存，排除引擎产物）", cacheDir)
		ok, err := util.PackDirStage(stage, cacheDir, filepath.Join(tmp, "fvm_cache"), cacheExcludes, true)
		if err != nil {
			os.Remove(out)
			return "", err
		}
		found = found || ok
	}
	if util.IsDir(installDir) && !samePath(installDir, cacheDir) {
		m.e.UI.Subst("打包 FVM 安装目录 %s", installDir)
		ok, err := util.PackDirStage(stage, installDir, filepath.Join(tmp, "fvm_install"), BaseExcludes, true)
		if err != nil {
			os.Remove(out)
			return "", err
		}
		found = found || ok
	}

	if !found {
		os.Remove(out)
		return "", fmt.Errorf("未找到 FVM 相关目录（缓存: %s / 安装: %s）", cacheDir, installDir)
	}

	envConfig := m.e.BackupToolsEnv(`(FVM_CACHE_PATH|FVM_HOME|fvm/bin|fvm/default)`)
	if envConfig != "" {
		_ = os.WriteFile(filepath.Join(tmp, "env_config"), []byte(envConfig), 0o644)
	}
	_ = WritePlatformInfo(filepath.Join(tmp, "platform_info"), map[string]string{
		"FVM_CACHE_PATH":  cacheDir,
		"FVM_INSTALL_DIR": installDir,
	})

	if _, err := util.CreateTarGz(out, tmp, []string{"."}, nil); err != nil {
		os.Remove(out)
		return "", fmt.Errorf("生成 FVM 备份失败: %w", err)
	}
	return out, nil
}

func (m *fvmManager) Restore(archive string) error {
	if err := util.ValidateTarGz(archive); err != nil {
		return err
	}
	temp, err := os.MkdirTemp("", "udev-fvm-restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)

	m.e.UI.Subst("解压 FVM 备份")
	if err := util.ExtractTar(archive, temp, true); err != nil {
		return err
	}
	if data, err := os.ReadFile(filepath.Join(temp, "platform_info")); err == nil {
		m.e.UI.Subst("备份平台信息:")
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			fmt.Fprintf(m.e.UI.Out, "         %s\n", line)
		}
	}

	cacheDir := m.e.Cfg.Paths.FvmCache
	restored := false
	if util.IsDir(filepath.Join(temp, "fvm_cache")) {
		m.e.UI.Subst("恢复 FVM 缓存到 %s", cacheDir)
		if err := util.CopyTree(filepath.Join(temp, "fvm_cache"), cacheDir); err != nil {
			return err
		}
		restored = true
	}
	if util.IsDir(filepath.Join(temp, "fvm_install")) {
		m.e.UI.Subst("恢复 FVM 安装目录到 %s", m.e.Cfg.Paths.FvmInstall)
		if err := util.CopyTree(filepath.Join(temp, "fvm_install"), m.e.Cfg.Paths.FvmInstall); err != nil {
			return err
		}
		restored = true
	}
	if !restored {
		return fmt.Errorf("备份中未找到可恢复的 FVM 数据")
	}

	m.e.AddShellLine(fmt.Sprintf(`export PATH="%s:$PATH"`, filepath.Join(m.e.Cfg.Paths.FvmInstall, "bin")))
	if cacheDir != filepath.Join(mustHome(), "fvm") {
		m.e.AddShellLine(fmt.Sprintf("export FVM_CACHE_PATH=%s", util.ShellQuote(cacheDir)))
	}
	return nil
}

func (m *fvmManager) Install() error {
	script, err := m.e.DownloadScript(m.e.Cfg.Managers.Fvm.InstallURL)
	if err != nil {
		return fmt.Errorf("下载 FVM 安装脚本失败: %w", err)
	}
	defer os.Remove(script)

	env := util.SetEnv(os.Environ(),
		"FVM_INSTALL_DIR="+m.e.Cfg.Paths.FvmInstall,
		"FVM_CACHE_PATH="+m.e.Cfg.Paths.FvmCache,
	)
	m.e.UI.Subst("执行 FVM 安装脚本")
	if err := util.RunScript(m.e.UI, "执行 FVM 安装脚本", "bash", script, nil, env); err != nil {
		return fmt.Errorf("FVM 安装失败")
	}

	m.e.AddShellLine(fmt.Sprintf(`export PATH="%s:$PATH"`, filepath.Join(m.e.Cfg.Paths.FvmInstall, "bin")))
	if m.e.Cfg.Paths.FvmCache != filepath.Join(mustHome(), "fvm") {
		m.e.AddShellLine(fmt.Sprintf("export FVM_CACHE_PATH=%s", util.ShellQuote(m.e.Cfg.Paths.FvmCache)))
	}
	return nil
}

func (m *fvmManager) Clean() error {
	cache := m.cacheDir()
	if samePath(cache, mustHome()) || cache == "/" {
		return fmt.Errorf("拒绝删除异常缓存路径: %s", cache)
	}
	_ = os.RemoveAll(cache)
	return nil
}

func samePath(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && aa == bb
}

func mustHome() string {
	h, _ := os.UserHomeDir()
	return h
}
