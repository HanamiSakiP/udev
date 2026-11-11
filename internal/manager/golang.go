package manager

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hanamisakip/udev/internal/util"
)

type gManager struct{ e *Env }

func (m *gManager) Name() string       { return "g" }
func (m *gManager) InstallDir() string { return m.e.Cfg.Paths.GoRoot }

func (m *gManager) Installed() bool {
	if ExistsExecutable(filepath.Join(m.e.Cfg.GBinDir(), "g")) {
		return true
	}
	_, err := exec.LookPath("g")
	return err == nil
}

func (m *gManager) Version() string {
	bin := filepath.Join(m.e.Cfg.GBinDir(), "g")
	if !ExistsExecutable(bin) {
		p, err := exec.LookPath("g")
		if err != nil {
			return ""
		}
		bin = p
	}
	out, err := util.Output(bin, "--version")
	if err != nil {
		return ""
	}
	return firstLine(out)
}

func (m *gManager) Backup() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	out, err := BackupPath(m.e.Cfg, "g")
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp("", "udev-g-backup-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	stage, err := os.MkdirTemp("", "udev-g-stage-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)

	m.e.DiskspaceWarning()

	found := false
	packed := map[string]bool{}
	pack := func(src, dst string, strict bool) error {
		abs, err := filepath.Abs(src)
		if err == nil && packed[abs] {
			return nil
		}
		ok, err := util.PackDirStage(stage, src, filepath.Join(tmp, dst), BaseExcludes, strict)
		if ok {
			found = true
			if abs != "" {
				packed[abs] = true
			}
		}
		return err
	}

	m.e.UI.Subst("打包 %s", m.e.Cfg.Paths.GoRoot)
	if err := pack(m.e.Cfg.Paths.GoRoot, ".g", true); err != nil {
		return "", err
	}
	if err := pack(filepath.Join(home, ".goenv"), ".goenv", true); err != nil {
		return "", err
	}
	if err := pack(filepath.Join(home, ".config", "g"), ".config/g", true); err != nil {
		return "", err
	}

	gopath := m.e.Cfg.Paths.GoPath
	if util.IsDir(gopath) {
		arc := gopathArc(home, gopath)
		m.e.UI.Subst("打包 GOPATH (%s)", gopath)
		if err := pack(gopath, arc, true); err != nil {
			return "", err
		}
	}
	if util.IsDir("/go") {
		m.e.UI.Subst("打包 /go")
		_ = pack("/go", "_go", false)
	}

	if !found {
		os.Remove(out)
		return "", fmt.Errorf("未找到 g/go 相关目录")
	}

	envConfig := m.e.BackupToolsEnv(`(PATH.*\.g/bin|GOROOT|GOPATH|/go/bin)`)
	if envConfig != "" {
		_ = os.WriteFile(filepath.Join(tmp, "env_config"), []byte(envConfig), 0o644)
	}
	_ = WritePlatformInfo(filepath.Join(tmp, "platform_info"), map[string]string{
		"GOPATH": gopath,
	})

	if _, err := util.CreateTarGz(out, tmp, []string{"."}, nil); err != nil {
		os.Remove(out)
		return "", fmt.Errorf("生成 g 备份失败: %w", err)
	}
	return out, nil
}

func (m *gManager) Restore(archive string) error {
	if err := util.ValidateTarGz(archive); err != nil {
		return err
	}
	temp, err := os.MkdirTemp("", "udev-g-restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)

	m.e.UI.Subst("解压 g 备份")
	if err := util.ExtractTar(archive, temp, true); err != nil {
		return err
	}
	platform := filepath.Join(temp, "platform_info")
	if data, err := os.ReadFile(platform); err == nil {
		m.e.UI.Subst("备份平台信息:")
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			fmt.Fprintf(m.e.UI.Out, "         %s\n", line)
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(temp)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		base := entry.Name()
		if base == "platform_info" || base == ".platform_info" || base == "env_config" {
			continue
		}
		src := filepath.Join(temp, base)
		if !entry.IsDir() {
			continue
		}
		var target string
		switch {
		case base == "_go":
			if writablePath("/go") {
				target = "/go"
			} else {
				target = filepath.Join(home, "_go")
				m.e.UI.Warnf("无权限写入 /go，恢复为 %s", target)
			}
		case strings.HasPrefix(base, "_abs"):
			target = filepath.Join(home, base)
			m.e.UI.Warnf("外部绝对路径（编码有损），恢复为 %s", target)
		default:
			target = filepath.Join(home, base)
		}
		m.e.UI.Subst("恢复 %s → %s", base, target)
		if err := util.CopyPath(src, target); err != nil {
			return err
		}
	}

	m.e.RemoveShellLines([]string{`GOROOT=.*versions/current/go`}, "", nil)

	goRoot := m.e.Cfg.Paths.GoRoot
	block := fmt.Sprintf(`# g (Go Version Manager) configuration
if [[ -n $(alias g 2>/dev/null) ]]; then
    unalias g
fi
[ -s %s ] && \. %s  # g shell setup`, util.ShellQuote(filepath.Join(goRoot, "env")), util.ShellQuote(filepath.Join(goRoot, "env")))
	m.e.AddShellBlock("g shell setup", block)
	return nil
}

func (m *gManager) Install() error {
	script, err := m.e.DownloadScript(m.e.Cfg.Managers.G.InstallURL)
	if err != nil {
		return fmt.Errorf("下载 g 安装脚本失败: %w", err)
	}
	defer os.Remove(script)

	if m.e.Cfg.Mirror.UseCN {
		data, err := os.ReadFile(script)
		if err != nil {
			return err
		}
		patched := strings.ReplaceAll(string(data), "https://api.github.com", m.e.Cfg.Mirror.GitHubProxy+"https://api.github.com")
		patched = strings.ReplaceAll(patched, "https://github.com", m.e.Cfg.Mirror.GitHubProxy+"https://github.com")
		m.e.UI.Subst("重写脚本 github 地址为镜像")
		if err := os.WriteFile(script, []byte(patched), 0o700); err != nil {
			return err
		}
	}

	if err := util.RunScript(m.e.UI, "执行 g 安装脚本", "bash", script, nil, m.e.GitEnv()); err != nil {
		return fmt.Errorf("g 安装失败，可尝试 CN_GITHUB_PROXY=https://ghproxy.net/ udev install --cn g")
	}

	goRoot := m.e.Cfg.Paths.GoRoot
	m.e.AddShellLine(fmt.Sprintf("export GOROOT=%s", util.ShellQuote(filepath.Join(goRoot, "go"))))
	m.e.AddShellLine(fmt.Sprintf(`export PATH="%s:$GOROOT/bin:$PATH"`, filepath.Join(goRoot, "bin")))
	if m.e.Cfg.Mirror.UseCN {
		m.e.AddShellLine(fmt.Sprintf("export G_MIRROR=%s", util.ShellQuote(m.e.Cfg.Mirror.Go)))
	}
	for _, d := range []string{"bin", "src", "pkg"} {
		if err := os.MkdirAll(filepath.Join(m.e.Cfg.Paths.GoPath, d), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (m *gManager) Clean() error {
	_ = os.RemoveAll(m.e.Cfg.Paths.GoRoot)
	m.e.RemoveShellLines([]string{`g shell setup`, `\.g/`, `GOROOT=.*\.g/go`, `G_MIRROR`}, "", nil)
	return nil
}

func gopathArc(home, gopath string) string {
	if strings.HasPrefix(gopath, home+string(filepath.Separator)) {
		return strings.TrimPrefix(gopath, home+string(filepath.Separator))
	}
	if gopath == "/go" {
		return "_go"
	}
	return "_abs" + strings.ReplaceAll(gopath, "/", "_")
}

func writablePath(path string) bool {
	testDir := path
	if !util.IsDir(path) {
		testDir = filepath.Dir(path)
	}
	f, err := os.CreateTemp(testDir, ".udev-write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}
