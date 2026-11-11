// Package config 定义 udev 的配置结构、默认值以及配置文件的读写。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 是 udev 的完整配置。
type Config struct {
	Home string `yaml:"-"` // 配置/存储根目录，默认 ~/.udev
	File string `yaml:"-"` // 配置文件路径

	Mirror    MirrorConfig    `yaml:"mirror"`
	Paths     PathsConfig     `yaml:"paths"`
	Shell     ShellConfig     `yaml:"shell"`
	Managers  ManagersConfig  `yaml:"managers"`
	DevTools  DevToolsConfig  `yaml:"dev_tools"`
	Uninstall UninstallConfig `yaml:"uninstall"`
}

// MirrorConfig 镜像相关配置。
type MirrorConfig struct {
	UseCN       bool   `yaml:"use_cn"`
	GitHubProxy string `yaml:"github_proxy"`
	Node        string `yaml:"node"`
	Go          string `yaml:"go"`
	Python      string `yaml:"python"`
}

// PathsConfig 各类工具与备份目录。
type PathsConfig struct {
	BackupDir  string `yaml:"backup_dir"`
	PyenvRoot  string `yaml:"pyenv_root"`
	GoRoot     string `yaml:"go_root"` // g 的安装目录（默认 ~/.g）
	GoPath     string `yaml:"gopath"`
	NvmDir     string `yaml:"nvm_dir"`
	RustupHome string `yaml:"rustup_home"`
	CargoHome  string `yaml:"cargo_home"`
	FvmCache   string `yaml:"fvm_cache"`
	FvmInstall string `yaml:"fvm_install"`
}

// ShellConfig 指定需要写入环境变量的 shell 配置文件。
type ShellConfig struct {
	Files []string `yaml:"files"`
}

// ManagersConfig 各版本管理器的安装信息。
type ManagersConfig struct {
	Pyenv ManagerInstall `yaml:"pyenv"`
	G     ManagerInstall `yaml:"g"`
	Nvm   ManagerInstall `yaml:"nvm"`
	Rust  RustManager    `yaml:"rust"`
	Fvm   ManagerInstall `yaml:"fvm"`
}

// ManagerInstall 通用安装配置。
type ManagerInstall struct {
	InstallURL string `yaml:"install_url"`
}

// RustManager rust 安装配置。
type RustManager struct {
	InstallURL       string `yaml:"install_url"`
	DefaultToolchain string `yaml:"default_toolchain"`
	NoModifyPath     bool   `yaml:"no_modify_path"`
}

// UninstallConfig 卸载行为配置。
type UninstallConfig struct {
	IncludeGopath bool `yaml:"include_gopath"` // 卸载 g 时是否默认删除 GOPATH
}

// DevToolsConfig 是开发工具的所有可自定义清单。
type DevToolsConfig struct {
	Node   NodeSection   `yaml:"node"`
	Go     GoSection     `yaml:"go"`
	Python PythonSection `yaml:"python"`
	Cargo  CargoSection  `yaml:"cargo"`
}

// NodeSection Node.js / npm 相关步骤。
type NodeSection struct {
	NvmVersion string   `yaml:"nvm_version"` // 例如 lts、20；为空表示跳过
	Npm        []string `yaml:"npm"`
	Commands   []string `yaml:"commands"` // 额外 shell 命令，失败不中断
	Cleanup    []string `yaml:"cleanup"`  // 清理命令，失败不中断
}

// GoSection Go 相关步骤。
type GoSection struct {
	GVersion string      `yaml:"g_version"` // 例如 latest；为空表示不执行 g install
	Packages []GoPackage `yaml:"packages"`
	Cleanup  []string    `yaml:"cleanup"`
}

// PythonSection Python / pip 相关步骤。
type PythonSection struct {
	PyenvVersion string   `yaml:"pyenv_version"` // 为空表示跳过 pyenv install/global
	PipUpgrade   bool     `yaml:"pip_upgrade"`   // 是否执行 python -m pip install --upgrade pip
	Packages     []string `yaml:"packages"`
	Commands     []string `yaml:"commands"`
	Cleanup      []string `yaml:"cleanup"`
}

// CargoSection Rust / cargo 相关步骤。
type CargoSection struct {
	Packages []CargoPackage `yaml:"packages"`
	Cleanup  []string       `yaml:"cleanup"`
}

// GoPackage 一个 go install 目标。配置中可直接写字符串，也可写成 {path, bin}。
type GoPackage struct {
	Path string `yaml:"path"`
	Bin  string `yaml:"bin"`
}

// EffectiveBin 返回用于判断是否已安装的二进制名。
func (p GoPackage) EffectiveBin() string {
	if p.Bin != "" {
		return p.Bin
	}
	path := p.Path
	if i := strings.LastIndex(path, "@"); i > strings.LastIndex(path, "/") {
		path = path[:i]
	}
	path = strings.TrimSuffix(path, "/")
	return filepath.Base(path)
}

// MarshalYAML 让常见的 go install 目标保持单行写法。
func (p GoPackage) MarshalYAML() (any, error) {
	if p.Bin == "" || p.Bin == p.EffectiveBin() {
		return p.Path, nil
	}
	type plain struct {
		Path string `yaml:"path"`
		Bin  string `yaml:"bin"`
	}
	return plain{Path: p.Path, Bin: p.Bin}, nil
}

// UnmarshalYAML 同时接受字符串与 {path, bin} 对象。
func (p *GoPackage) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		p.Path = node.Value
		return nil
	}
	type plain struct {
		Path string `yaml:"path"`
		Bin  string `yaml:"bin"`
	}
	var v plain
	if err := node.Decode(&v); err != nil {
		return err
	}
	p.Path = v.Path
	p.Bin = v.Bin
	return nil
}

// CargoPackage 一个 cargo install 目标。
type CargoPackage struct {
	Name string   `yaml:"name"`
	Args []string `yaml:"args"`
}

// MarshalYAML 让无额外参数的包保持单行写法。
func (p CargoPackage) MarshalYAML() (any, error) {
	if len(p.Args) == 0 {
		return p.Name, nil
	}
	type plain struct {
		Name string   `yaml:"name"`
		Args []string `yaml:"args"`
	}
	return plain{Name: p.Name, Args: p.Args}, nil
}

// UnmarshalYAML 同时接受字符串与 {name, args} 对象。
func (p *CargoPackage) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		p.Name = node.Value
		return nil
	}
	type plain struct {
		Name string   `yaml:"name"`
		Args []string `yaml:"args"`
	}
	var v plain
	if err := node.Decode(&v); err != nil {
		return err
	}
	p.Name = v.Name
	p.Args = v.Args
	return nil
}

// Defaults 返回内置默认配置。home 为 ~/.udev。
func Defaults(home string) *Config {
	c := &Config{
		Home: home,
		File: filepath.Join(home, "config.yaml"),
		Mirror: MirrorConfig{
			UseCN:       false,
			GitHubProxy: "https://ghfast.top/",
			Node:        "https://npmmirror.com/mirrors/node",
			Go:          "https://golang.google.cn/dl/",
			Python:      "https://registry.npmmirror.com/-/binary/python",
		},
		Paths: PathsConfig{
			BackupDir:  filepath.Join(home, "backups"),
			PyenvRoot:  "~/.pyenv",
			GoRoot:     "~/.g",
			GoPath:     "~/go",
			NvmDir:     "~/.nvm",
			RustupHome: "~/.rustup",
			CargoHome:  "~/.cargo",
			FvmCache:   "~/fvm",
			FvmInstall: "~/fvm",
		},
		Shell: ShellConfig{
			Files: []string{"~/.bashrc", "~/.profile", "~/.zshrc"},
		},
		Managers: ManagersConfig{
			Pyenv: ManagerInstall{InstallURL: "https://pyenv.run"},
			G:     ManagerInstall{InstallURL: "https://raw.githubusercontent.com/voidint/g/master/install.sh"},
			Nvm:   ManagerInstall{InstallURL: "https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.2/install.sh"},
			Rust: RustManager{
				InstallURL:       "https://sh.rustup.rs",
				DefaultToolchain: "stable",
			},
			Fvm: ManagerInstall{InstallURL: "https://fvm.app/install.sh"},
		},
		DevTools: DevToolsConfig{
			Node: NodeSection{
				NvmVersion: "lts",
				Npm: []string{
					"nrm",
					"yarn",
					"bun",
					"pnpm",
					"fanyi",
					"@officecli/officecli",
					"@fresh-editor/fresh-editor",
					"@dbx-app/cli",
					"@playwright/cli@latest",
					"@jackwener/opencli",
					"microsandbox",
					"cc-connect",
					"@bitwarden/cli",
					"@earendil-works/pi-coding-agent",
					"@anthropic-ai/claude-code",
					"@deepseek-ai/dsh",
					"@deepseek-harness-tui/dsh-tui",
					"rea-agents",
					"@opencode/cli",
					"@openai/codex",
				},
				Commands: []string{
					"dsh plugin --profile web add -w @xmanrui/dsh-im",
					"dsh plugin --profile web add dsh-pet",
				},
				Cleanup: []string{
					"nvm cache clear",
					"npm cache verify",
				},
			},
			Go: GoSection{
				GVersion: "latest",
				Packages: []GoPackage{
					{Path: "github.com/restic/restic/cmd/restic@latest", Bin: "restic"},
					{Path: "github.com/rclone/rclone@latest", Bin: "rclone"},
					{Path: "github.com/junegunn/fzf@latest", Bin: "fzf"},
					{Path: "github.com/jesseduffield/lazygit@latest", Bin: "lazygit"},
					{Path: "github.com/charmbracelet/glow@latest", Bin: "glow"},
					{Path: "github.com/hybridgroup/yzma@latest", Bin: "yzma"},
					{Path: "github.com/charmbracelet/crush@latest", Bin: "crush"},
					{Path: "github.com/google/go-containerregistry/cmd/crane@latest", Bin: "crane"},
					{Path: "github.com/regclient/regclient/cmd/regctl@latest", Bin: "regctl"},
					{Path: "github.com/cli/cli/v2/cmd/gh@latest", Bin: "gh"},
					{Path: "gitlab.com/gitlab-org/cli/cmd/glab@latest", Bin: "glab"},
				},
				Cleanup: []string{
					"g clean",
					"go clean -cache -testcache -modcache",
				},
			},
			Python: PythonSection{
				PyenvVersion: "3.12.3",
				PipUpgrade:   true,
				Packages: []string{
					"code2prompt-rs",
					"uv",
					"cli-anything-hub",
				},
				Cleanup: []string{"pip cache purge"},
			},
			Cargo: CargoSection{
				Packages: []CargoPackage{
					{Name: "starship", Args: []string{"--locked"}},
					{Name: "navi", Args: []string{"--locked"}},
					{Name: "zellij", Args: []string{"--locked"}},
					{Name: "zoxide", Args: []string{"--locked"}},
					{Name: "caligula"},
					{Name: "bluetui"},
					{Name: "ouch"},
					{Name: "bookokrat"},
					{Name: "abtop"},
					{Name: "fd-find"},
					{Name: "jj-cli"},
					{Name: "lsd"},
					{Name: "binsider"},
					{Name: "microsandbox"},
					{Name: "lychee"},
					{Name: "code2prompt"},
					{Name: "yazi-build", Args: []string{"--force", "--git", "https://github.com/sxyazi/yazi.git"}},
					{Name: "openfang-cli", Args: []string{"--force", "--git", "https://github.com/RightNow-AI/openfang"}},
				},
				Cleanup: []string{"rm -rf ~/.cargo/git ~/.cargo/registry"},
			},
		},
		Uninstall: UninstallConfig{IncludeGopath: true},
	}
	return c
}

// HomeDir 返回用于默认配置的用户目录，可用 UDEV_HOME 覆盖。
func HomeDir() (string, error) {
	if v := os.Getenv("UDEV_HOME"); v != "" {
		return filepath.Abs(expandPath(v))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".udev"), nil
}

// Load 读取配置；文件不存在时返回默认配置。
func Load() (*Config, error) {
	home, err := HomeDir()
	if err != nil {
		return nil, err
	}
	cfg := Defaults(home)

	data, err := os.ReadFile(cfg.File)
	if err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("解析配置文件 %s 失败: %w", cfg.File, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取配置文件 %s 失败: %w", cfg.File, err)
	}

	cfg.Home = home
	cfg.File = filepath.Join(home, "config.yaml")
	cfg.fillEmpty(Defaults(home))
	cfg.applyEnv()
	cfg.expand()
	return cfg, nil
}

// fillEmpty 用默认值填补关键字段，避免配置文件中的空值导致路径解析异常。
func (c *Config) fillEmpty(d *Config) {
	if c.Paths.BackupDir == "" {
		c.Paths.BackupDir = d.Paths.BackupDir
	}
	if c.Paths.PyenvRoot == "" {
		c.Paths.PyenvRoot = d.Paths.PyenvRoot
	}
	if c.Paths.GoRoot == "" {
		c.Paths.GoRoot = d.Paths.GoRoot
	}
	if c.Paths.GoPath == "" {
		c.Paths.GoPath = d.Paths.GoPath
	}
	if c.Paths.NvmDir == "" {
		c.Paths.NvmDir = d.Paths.NvmDir
	}
	if c.Paths.RustupHome == "" {
		c.Paths.RustupHome = d.Paths.RustupHome
	}
	if c.Paths.CargoHome == "" {
		c.Paths.CargoHome = d.Paths.CargoHome
	}
	if c.Paths.FvmCache == "" {
		c.Paths.FvmCache = d.Paths.FvmCache
	}
	if c.Paths.FvmInstall == "" {
		c.Paths.FvmInstall = d.Paths.FvmInstall
	}
	if len(c.Shell.Files) == 0 {
		c.Shell.Files = d.Shell.Files
	}
	if c.Mirror.GitHubProxy == "" {
		c.Mirror.GitHubProxy = d.Mirror.GitHubProxy
	}
	if c.Managers.Pyenv.InstallURL == "" {
		c.Managers.Pyenv.InstallURL = d.Managers.Pyenv.InstallURL
	}
	if c.Managers.G.InstallURL == "" {
		c.Managers.G.InstallURL = d.Managers.G.InstallURL
	}
	if c.Managers.Nvm.InstallURL == "" {
		c.Managers.Nvm.InstallURL = d.Managers.Nvm.InstallURL
	}
	if c.Managers.Rust.InstallURL == "" {
		c.Managers.Rust.InstallURL = d.Managers.Rust.InstallURL
	}
	if c.Managers.Rust.DefaultToolchain == "" {
		c.Managers.Rust.DefaultToolchain = d.Managers.Rust.DefaultToolchain
	}
	if c.Managers.Fvm.InstallURL == "" {
		c.Managers.Fvm.InstallURL = d.Managers.Fvm.InstallURL
	}
}

// Ensure 创建配置目录与默认配置文件，返回是否为本次新建。
func (c *Config) Ensure() (bool, error) {
	if err := os.MkdirAll(c.Home, 0o755); err != nil {
		return false, err
	}
	if err := os.MkdirAll(c.Paths.BackupDir, 0o755); err != nil {
		return false, err
	}
	if _, err := os.Stat(c.File); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	// 首次生成时写入内置默认值，避免把本次运行的环境变量覆盖结果固化进配置文件。
	if err := Defaults(c.Home).Save(); err != nil {
		return false, err
	}
	return true, nil
}

// Save 原子写入配置文件。
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.File), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	header := []byte("# udev 配置文件\n# 配置/存储目录: " + c.Home + "\n# 修改后下次运行 udev 时生效。\n\n")
	tmp, err := os.CreateTemp(filepath.Dir(c.File), ".config-*.yaml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(header); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, c.File)
}

// Reset 使用内置默认值覆盖配置文件。
func (c *Config) Reset() error {
	d := Defaults(c.Home)
	c.Mirror = d.Mirror
	c.Paths = d.Paths
	c.Shell = d.Shell
	c.Managers = d.Managers
	c.DevTools = d.DevTools
	c.Uninstall = d.Uninstall
	return c.Save()
}

// Marshal 输出当前配置（含注释头）。
func (c *Config) Marshal() ([]byte, error) {
	data, err := yaml.Marshal(c)
	if err != nil {
		return nil, err
	}
	return append([]byte("# udev 当前生效配置\n# 配置文件: "+c.File+"\n\n"), data...), nil
}

func (c *Config) applyEnv() {
	set := func(dst *string, keys ...string) {
		for _, k := range keys {
			if v := os.Getenv(k); v != "" {
				*dst = v
				return
			}
		}
	}
	set(&c.Paths.BackupDir, "UDEV_BACKUP_DIR")
	set(&c.Paths.PyenvRoot, "PYENV_ROOT")
	set(&c.Paths.NvmDir, "NVM_DIR")
	set(&c.Paths.GoPath, "GOPATH")
	set(&c.Paths.RustupHome, "RUSTUP_HOME")
	set(&c.Paths.CargoHome, "CARGO_HOME")
	set(&c.Paths.FvmCache, "FVM_CACHE_PATH", "FVM_HOME")
	set(&c.Paths.FvmInstall, "FVM_INSTALL_DIR")
	set(&c.Mirror.GitHubProxy, "CN_GITHUB_PROXY")
	set(&c.Mirror.Node, "CN_NODE_MIRROR")
	set(&c.Mirror.Go, "CN_GO_MIRROR")
	set(&c.Mirror.Python, "CN_PYTHON_MIRROR")
	if v := os.Getenv("UDEV_USE_CN"); v != "" {
		c.Mirror.UseCN = truthy(v)
	} else if v := os.Getenv("USE_CN_MIRROR"); v != "" {
		c.Mirror.UseCN = truthy(v)
	}
}

func (c *Config) expand() {
	c.Paths.BackupDir = expandPath(c.Paths.BackupDir)
	c.Paths.PyenvRoot = expandPath(c.Paths.PyenvRoot)
	c.Paths.GoRoot = expandPath(c.Paths.GoRoot)
	c.Paths.GoPath = expandPath(c.Paths.GoPath)
	c.Paths.NvmDir = expandPath(c.Paths.NvmDir)
	c.Paths.RustupHome = expandPath(c.Paths.RustupHome)
	c.Paths.CargoHome = expandPath(c.Paths.CargoHome)
	c.Paths.FvmCache = expandPath(c.Paths.FvmCache)
	c.Paths.FvmInstall = expandPath(c.Paths.FvmInstall)
	for i := range c.Shell.Files {
		c.Shell.Files[i] = expandPath(c.Shell.Files[i])
	}
}

func expandPath(p string) string {
	p = os.ExpandEnv(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "y":
		return true
	}
	return false
}

// PyenvBin pyenv 可执行文件路径。
func (c *Config) PyenvBin() string { return filepath.Join(c.Paths.PyenvRoot, "bin", "pyenv") }

// GBinDir g 可执行文件目录。
func (c *Config) GBinDir() string { return filepath.Join(c.Paths.GoRoot, "bin") }

// GoSDKBin go SDK 可执行文件目录。
func (c *Config) GoSDKBin() string { return filepath.Join(c.Paths.GoRoot, "go", "bin") }

// GoBinDir GOPATH 下的 bin。
func (c *Config) GoBinDir() string { return filepath.Join(c.Paths.GoPath, "bin") }

// CargoBinDir cargo 可执行文件目录。
func (c *Config) CargoBinDir() string { return filepath.Join(c.Paths.CargoHome, "bin") }

// NvmScript nvm 初始化脚本。
func (c *Config) NvmScript() string { return filepath.Join(c.Paths.NvmDir, "nvm.sh") }

// RustupBin rustup 可执行文件。
func (c *Config) RustupBin() string { return filepath.Join(c.Paths.CargoHome, "bin", "rustup") }

// FvmBin fvm 可执行文件。
func (c *Config) FvmBin() string { return filepath.Join(c.Paths.FvmInstall, "bin", "fvm") }
