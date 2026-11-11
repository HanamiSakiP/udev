// Package devtools 按配置批量安装开发工具。
package devtools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hanamisakip/udev/internal/config"
	"github.com/hanamisakip/udev/internal/ui"
	"github.com/hanamisakip/udev/internal/util"
)

// Runner 执行一次开发工具安装。
type Runner struct {
	cfg *config.Config
	ui  *ui.UI
	ctx context.Context

	prelude string
	step    int
	total   int

	cargoList   string
	cargoLoaded bool
}

// Run 按 section 列表执行安装。空列表等价于 node go python。
func Run(ctx context.Context, cfg *config.Config, u *ui.UI, args []string) error {
	sections, err := normalizeSections(args)
	if err != nil {
		return err
	}
	r := &Runner{cfg: cfg, ui: u, ctx: ctx, prelude: buildPrelude(cfg)}
	for _, s := range sections {
		r.total += r.count(s)
	}
	if r.total == 0 {
		u.Warnf("配置中的工具清单为空，请检查 %s", cfg.File)
		return nil
	}

	if err := r.preflight(sections); err != nil {
		return err
	}

	fmt.Fprintf(u.Out, "%s\n", u.Cyan(fmt.Sprintf("==> 开始执行，共 %d 步", r.total)))
	for _, s := range sections {
		switch s {
		case "node":
			if err := r.runNode(); err != nil {
				return err
			}
		case "go":
			if err := r.runGo(); err != nil {
				return err
			}
		case "python":
			if err := r.runPython(); err != nil {
				return err
			}
		case "cargo":
			if err := r.runCargo(); err != nil {
				return err
			}
		}
	}
	u.OKf("==> 全部完成。")
	return nil
}

func normalizeSections(args []string) ([]string, error) {
	if len(args) == 0 {
		return []string{"node", "go", "python"}, nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, a := range args {
		switch strings.ToLower(a) {
		case "all":
			for _, s := range []string{"node", "go", "python", "cargo"} {
				add(s)
			}
		case "node", "npm", "nodejs", "js":
			add("node")
		case "go", "golang":
			add("go")
		case "python", "pip", "py":
			add("python")
		case "cargo", "rust":
			add("cargo")
		default:
			return nil, fmt.Errorf("未知参数: %s（可选: node go python cargo all）", a)
		}
	}
	return out, nil
}

func (r *Runner) count(section string) int {
	d := r.cfg.DevTools
	switch section {
	case "node":
		n := len(d.Node.Npm) + len(d.Node.Commands) + len(d.Node.Cleanup)
		if d.Node.NvmVersion != "" {
			n++
		}
		return n
	case "go":
		n := len(d.Go.Packages) + len(d.Go.Cleanup)
		if d.Go.GVersion != "" {
			n++
		}
		return n
	case "python":
		n := len(d.Python.Packages) + len(d.Python.Commands) + len(d.Python.Cleanup)
		if d.Python.PyenvVersion != "" {
			n += 2
		}
		if d.Python.PipUpgrade {
			n++
		}
		return n
	case "cargo":
		return len(d.Cargo.Packages) + len(d.Cargo.Cleanup)
	}
	return 0
}

func (r *Runner) preflight(sections []string) error {
	for _, s := range sections {
		if r.count(s) == 0 {
			continue
		}
		switch s {
		case "node":
			if st, err := os.Stat(r.cfg.NvmScript()); err != nil || st.Size() == 0 {
				return fmt.Errorf("未找到 nvm（%s），请先运行: udev install nvm", r.cfg.NvmScript())
			}
		case "go":
			if r.cfg.DevTools.Go.GVersion != "" && !commandExists(filepath.Join(r.cfg.GBinDir(), "g")) && !commandExists("g") {
				return fmt.Errorf("未找到 g（Go 版本管理器），请先运行: udev install g")
			}
			if !commandExists(filepath.Join(r.cfg.GoSDKBin(), "go")) && !commandExists("go") {
				return fmt.Errorf("未找到 go，请先运行: udev install g")
			}
		case "python":
			if !commandExists(r.cfg.PyenvBin()) && !commandExists("pyenv") {
				return fmt.Errorf("未找到 pyenv，请先运行: udev install pyenv")
			}
		case "cargo":
			if !commandExists(filepath.Join(r.cfg.CargoBinDir(), "cargo")) && !commandExists("cargo") {
				return fmt.Errorf("未找到 cargo，请先运行: udev install rust")
			}
		}
	}
	return nil
}

func commandExists(name string) bool {
	if strings.ContainsRune(name, filepath.Separator) {
		st, err := os.Stat(name)
		return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
	}
	_, err := exec.LookPath(name)
	return err == nil
}

func buildPrelude(cfg *config.Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "export NVM_DIR=%s\n", util.ShellQuote(cfg.Paths.NvmDir))
	fmt.Fprintf(&b, "if [ -s \"$NVM_DIR/nvm.sh\" ]; then . \"$NVM_DIR/nvm.sh\" >/dev/null 2>&1 || true; fi\n")
	fmt.Fprintf(&b, "export PYENV_ROOT=%s\n", util.ShellQuote(cfg.Paths.PyenvRoot))
	fmt.Fprintf(&b, "if [ -x %s ]; then eval \"$(%s init - bash)\" 2>/dev/null || true; elif command -v pyenv >/dev/null 2>&1; then eval \"$(pyenv init - bash)\" 2>/dev/null || true; fi\n",
		util.ShellQuote(cfg.PyenvBin()), util.ShellQuote(cfg.PyenvBin()))
	fmt.Fprintf(&b, "export GOPATH=%s\n", util.ShellQuote(cfg.Paths.GoPath))
	fmt.Fprintf(&b, "export GOBIN=%s\n", util.ShellQuote(cfg.GoBinDir()))
	fmt.Fprintf(&b, "export RUSTUP_HOME=%s\n", util.ShellQuote(cfg.Paths.RustupHome))
	fmt.Fprintf(&b, "export CARGO_HOME=%s\n", util.ShellQuote(cfg.Paths.CargoHome))
	fmt.Fprintf(&b, "if [ -s %s ]; then . %s; fi\n",
		util.ShellQuote(filepath.Join(cfg.Paths.GoRoot, "env")), util.ShellQuote(filepath.Join(cfg.Paths.GoRoot, "env")))
	fmt.Fprintf(&b, "if [ -s %s ]; then . %s; fi\n",
		util.ShellQuote(filepath.Join(cfg.Paths.CargoHome, "env")), util.ShellQuote(filepath.Join(cfg.Paths.CargoHome, "env")))
	bins := strings.Join([]string{
		filepath.Join(cfg.Paths.PyenvRoot, "bin"),
		filepath.Join(cfg.Paths.GoRoot, "bin"),
		filepath.Join(cfg.Paths.GoRoot, "go", "bin"),
		cfg.GoBinDir(),
		cfg.CargoBinDir(),
		filepath.Join(cfg.Paths.FvmInstall, "bin"),
	}, ":")
	fmt.Fprintf(&b, "export PATH=\"%s:$PATH\"\n", bins)
	return b.String()
}

func (r *Runner) shell(cmd string) string {
	return r.prelude + "\n" + cmd
}

func (r *Runner) stream(cmd string) error {
	return util.RunStreaming(r.ctx, r.shell(cmd), nil)
}

func (r *Runner) quiet(cmd string) (string, error) {
	return util.OutputShell(r.shell(cmd), nil)
}

func (r *Runner) track(desc string, optional bool, fn func() (bool, error)) error {
	r.step++
	r.ui.ProgressBegin(r.step, r.total, desc)
	start := time.Now()
	skipped, err := fn()
	if err != nil {
		r.ui.ProgressFail(util.ExitCode(err))
		if optional {
			return nil
		}
		return fmt.Errorf("%s: %w", desc, err)
	}
	if skipped {
		r.ui.ProgressSkip()
	} else {
		r.ui.ProgressOK(time.Since(start))
	}
	return nil
}

func (r *Runner) runNode() error {
	s := r.cfg.DevTools.Node
	r.ui.Header("Node.js")

	if s.NvmVersion != "" {
		cmd := "nvm install " + util.ShellQuote(s.NvmVersion)
		if strings.EqualFold(s.NvmVersion, "lts") {
			cmd = "nvm install --lts"
		}
		if err := r.track(cmd, false, func() (bool, error) {
			return false, r.stream(cmd)
		}); err != nil {
			return err
		}
	}
	for _, pkg := range s.Npm {
		pkg := pkg
		if err := r.track("npm install -g "+pkg, false, func() (bool, error) {
			name := npmPackageName(pkg)
			if _, err := r.quiet("npm list -g --depth=0 " + util.ShellQuote(name) + " >/dev/null 2>&1"); err == nil {
				return true, nil
			}
			return false, r.stream("npm install -g " + util.ShellQuote(pkg))
		}); err != nil {
			return err
		}
	}
	for _, cmd := range s.Commands {
		cmd := cmd
		if err := r.track(cmd, true, func() (bool, error) {
			return false, r.stream(cmd)
		}); err != nil {
			return err
		}
	}
	for _, cmd := range s.Cleanup {
		cmd := cmd
		if err := r.track(cmd, true, func() (bool, error) {
			return false, r.stream(cmd)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) runGo() error {
	s := r.cfg.DevTools.Go
	r.ui.Header("Go")

	if s.GVersion != "" {
		cmd := "g install " + util.ShellQuote(s.GVersion)
		if err := r.track(cmd, false, func() (bool, error) {
			out, err := r.quiet(cmd)
			fmt.Fprint(os.Stdout, out)
			if err != nil && strings.Contains(out, "has been installed") {
				return false, nil
			}
			return false, err
		}); err != nil {
			return err
		}
	}
	for _, pkg := range s.Packages {
		pkg := pkg
		bin := pkg.EffectiveBin()
		if err := r.track("go install "+pkg.Path, false, func() (bool, error) {
			if commandExists(filepath.Join(r.cfg.GoBinDir(), bin)) {
				return true, nil
			}
			if gobin := os.Getenv("GOBIN"); gobin != "" && commandExists(filepath.Join(gobin, bin)) {
				return true, nil
			}
			return false, r.stream("go install " + util.ShellQuote(pkg.Path))
		}); err != nil {
			return err
		}
	}
	for _, cmd := range s.Cleanup {
		cmd := cmd
		if err := r.track(cmd, true, func() (bool, error) {
			return false, r.stream(cmd)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) runPython() error {
	s := r.cfg.DevTools.Python
	r.ui.Header("Python")

	if s.PyenvVersion != "" {
		version := s.PyenvVersion
		if err := r.track("pyenv install "+version, false, func() (bool, error) {
			out, err := r.quiet("pyenv versions --bare 2>/dev/null")
			if err == nil {
				for _, line := range strings.Split(out, "\n") {
					if strings.TrimSpace(line) == version {
						return true, nil
					}
				}
			}
			return false, r.stream("pyenv install -s " + util.ShellQuote(version))
		}); err != nil {
			return err
		}
		if err := r.track("pyenv global "+version, false, func() (bool, error) {
			return false, r.stream("pyenv global " + util.ShellQuote(version))
		}); err != nil {
			return err
		}
	}
	if s.PipUpgrade {
		cmd := "python -m pip install --upgrade pip"
		if err := r.track(cmd, false, func() (bool, error) {
			return false, r.stream(cmd)
		}); err != nil {
			return err
		}
	}
	for _, pkg := range s.Packages {
		pkg := pkg
		if err := r.track("pip install "+pkg, false, func() (bool, error) {
			return false, r.stream("pip install " + util.ShellQuote(pkg))
		}); err != nil {
			return err
		}
	}
	for _, cmd := range s.Commands {
		cmd := cmd
		if err := r.track(cmd, true, func() (bool, error) {
			return false, r.stream(cmd)
		}); err != nil {
			return err
		}
	}
	for _, cmd := range s.Cleanup {
		cmd := cmd
		if err := r.track(cmd, true, func() (bool, error) {
			return false, r.stream(cmd)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) runCargo() error {
	s := r.cfg.DevTools.Cargo
	r.ui.Header("Cargo")

	for _, pkg := range s.Packages {
		pkg := pkg
		args := make([]string, 0, len(pkg.Args))
		for _, a := range pkg.Args {
			args = append(args, util.ShellQuote(a))
		}
		cmd := "cargo install " + util.ShellQuote(pkg.Name)
		if len(args) > 0 {
			cmd += " " + strings.Join(args, " ")
		}
		if err := r.track(cmd, false, func() (bool, error) {
			if r.cargoPackageInstalled(pkg.Name) {
				return true, nil
			}
			return false, r.stream(cmd)
		}); err != nil {
			return err
		}
	}
	for _, cmd := range s.Cleanup {
		cmd := cmd
		if err := r.track(cmd, true, func() (bool, error) {
			return false, r.stream(cmd)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) cargoPackageInstalled(name string) bool {
	if !r.cargoLoaded {
		r.cargoList, _ = r.quiet("cargo install --list 2>/dev/null")
		r.cargoLoaded = true
	}
	prefix := name + " "
	for _, line := range strings.Split(r.cargoList, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return true
		}
	}
	return false
}

func npmPackageName(spec string) string {
	if strings.HasPrefix(spec, "@") {
		if i := strings.Index(spec[1:], "@"); i >= 0 {
			return spec[:i+1]
		}
		return spec
	}
	if i := strings.Index(spec, "@"); i > 0 {
		return spec[:i]
	}
	return spec
}
