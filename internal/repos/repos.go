// Package repos 负责批量克隆与更新配置中的代码仓库。
package repos

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hanamisakip/udev/internal/config"
	"github.com/hanamisakip/udev/internal/ui"
	"github.com/hanamisakip/udev/internal/util"
)

// Options 是 repos 命令的运行时选项。
type Options struct {
	Tool  string   // auto | jj | git | gh
	Dir   string   // 覆盖配置中的克隆根目录
	List  bool     // 只列出将要处理的仓库
	Check bool     // 只检查更新，不修改工作副本
	Args  []string // 仓库名、owner/repo 或完整地址
}

// Repo 描述一个待处理仓库。
type Repo struct {
	Spec string // 配置中的原始写法
	URL  string // 实际克隆地址
	Name string // 本地目录名
	Dest string // 本地完整路径
}

// ParseOptions 解析 repos 命令参数。
func ParseOptions(args []string) (Options, error) {
	opts := Options{Tool: "auto"}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			return opts, errHelp
		case a == "--list" || a == "-l":
			opts.List = true
		case a == "--check" || a == "-c":
			opts.Check = true
		case a == "--jj":
			opts.Tool = "jj"
		case a == "--git":
			opts.Tool = "git"
		case a == "--gh":
			opts.Tool = "gh"
		case a == "--tool" || a == "-t":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s 需要一个值（auto|jj|git|gh）", a)
			}
			i++
			opts.Tool = args[i]
		case strings.HasPrefix(a, "--tool="):
			opts.Tool = strings.TrimPrefix(a, "--tool=")
		case a == "--dir" || a == "-d":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s 需要一个目录", a)
			}
			i++
			opts.Dir = args[i]
		case strings.HasPrefix(a, "--dir="):
			opts.Dir = strings.TrimPrefix(a, "--dir=")
		case strings.HasPrefix(a, "-"):
			return opts, fmt.Errorf("未知参数: %s", a)
		default:
			opts.Args = append(opts.Args, a)
		}
	}
	return opts, nil
}

// errHelp 表示用户请求帮助，由调用方转换成正常退出。
var errHelp = fmt.Errorf("help")

// IsHelp 判断解析结果是否只是请求帮助。
func IsHelp(err error) bool { return err == errHelp }

// Run 执行批量克隆/更新。
func Run(ctx context.Context, cfg *config.Config, u *ui.UI, args []string) error {
	opts, err := ParseOptions(args)
	if err != nil {
		return err
	}
	list, err := Resolve(cfg, opts)
	if err != nil {
		return err
	}

	u.Header("udev repos")
	u.Infof("仓库目录: %s", dirOf(cfg, opts))
	u.Infof("仓库数量: %d", len(list))

	if opts.List {
		u.Infof("使用工具: %s（配置值，执行时会自动检测 jj/git/gh）", firstNonEmpty(opts.Tool, cfg.Repos.Tool))
		fmt.Fprintln(u.Out)
		for _, r := range list {
			state := "待克隆"
			if isRepo(r.Dest) {
				state = "已存在"
			}
			fmt.Fprintf(u.Out, "  %-28s %-8s %s\n", r.Name, state, u.Dim(r.URL))
			fmt.Fprintf(u.Out, "  %-28s %-8s %s\n", "", "", u.Dim(r.Dest))
		}
		return nil
	}

	if opts.Check {
		u.Infof("模式: 检查更新（git 使用 fetch --dry-run，jj 会 fetch 远端状态，不修改工作副本）")
		return checkAll(ctx, u, list)
	}

	tool, err := DetectTool(firstNonEmpty(opts.Tool, cfg.Repos.Tool))
	if err != nil {
		return err
	}
	u.Infof("使用工具: %s（可用 jj、git、gh，优先 jj）", tool)

	if err := os.MkdirAll(dirOf(cfg, opts), 0o755); err != nil {
		return fmt.Errorf("创建仓库目录失败: %w", err)
	}

	var failed []string
	for i, r := range list {
		fmt.Fprintf(u.Out, "\n%s ==> [%d/%d] %s\n", u.Bold(""), i+1, len(list), r.Name)
		if isRepo(r.Dest) {
			u.Subst("更新已有仓库: %s", r.Dest)
			if err := update(ctx, u, r); err != nil {
				u.Errorf("%s 更新失败: %v", r.Name, err)
				failed = append(failed, r.Name)
			}
			continue
		}
		if _, err := os.Stat(r.Dest); err == nil {
			u.Errorf("%s 目标目录已存在但不是 git/jj 仓库: %s", r.Name, r.Dest)
			failed = append(failed, r.Name)
			continue
		} else if !os.IsNotExist(err) {
			u.Errorf("%s 检查目标目录失败: %v", r.Name, err)
			failed = append(failed, r.Name)
			continue
		}
		u.Subst("克隆 %s", r.URL)
		if err := clone(ctx, u, r, tool); err != nil {
			u.Errorf("%s 克隆失败: %v", r.Name, err)
			failed = append(failed, r.Name)
			continue
		}
		u.OKf("%s 已克隆到 %s", r.Name, r.Dest)
	}

	fmt.Fprintln(u.Out)
	if len(failed) > 0 {
		return fmt.Errorf("以下仓库处理失败: %s", strings.Join(failed, " "))
	}
	u.OKf("=== 仓库全部就绪 ===")
	return nil
}

// Resolve 根据配置与命令行参数计算待处理仓库列表。
func Resolve(cfg *config.Config, opts Options) ([]Repo, error) {
	dir := dirOf(cfg, opts)
	protocol := strings.ToLower(firstNonEmpty(cfg.Repos.Protocol, "https"))

	makeRepo := func(spec string) Repo {
		url := cloneURL(spec, protocol)
		name := RepoName(spec)
		return Repo{Spec: spec, URL: url, Name: name, Dest: filepath.Join(dir, name)}
	}

	if len(opts.Args) == 0 {
		out := make([]Repo, 0, len(cfg.Repos.List))
		seen := map[string]bool{}
		for _, spec := range cfg.Repos.List {
			spec = strings.TrimSpace(spec)
			if spec == "" {
				continue
			}
			r := makeRepo(spec)
			if seen[r.Dest] {
				continue
			}
			seen[r.Dest] = true
			out = append(out, r)
		}
		return out, nil
	}

	out := make([]Repo, 0, len(opts.Args))
	seen := map[string]bool{}
	var missing []string
	for _, arg := range opts.Args {
		matched := false
		lower := strings.ToLower(arg)
		for _, spec := range cfg.Repos.List {
			spec = strings.TrimSpace(spec)
			if spec == "" {
				continue
			}
			r := makeRepo(spec)
			if strings.EqualFold(r.Name, arg) ||
				strings.EqualFold(r.Spec, arg) ||
				strings.Contains(strings.ToLower(r.Spec), lower) {
				if !seen[r.Dest] {
					seen[r.Dest] = true
					out = append(out, r)
				}
				matched = true
			}
		}
		if matched {
			continue
		}
		if !looksLikeRepo(arg) {
			missing = append(missing, arg)
			continue
		}
		r := makeRepo(arg)
		if !seen[r.Dest] {
			seen[r.Dest] = true
			out = append(out, r)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("配置中没有匹配的仓库: %s（可写完整 owner/repo 或地址）", strings.Join(missing, " "))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("没有需要处理的仓库")
	}
	return out, nil
}

// DetectTool 检测可用的克隆工具；pref 为 auto 时按 jj、git、gh 的顺序选择。
func DetectTool(pref string) (string, error) {
	pref = strings.ToLower(strings.TrimSpace(pref))
	if pref == "" || pref == "auto" {
		for _, tool := range []string{"jj", "git", "gh"} {
			if _, err := exec.LookPath(tool); err == nil {
				return tool, nil
			}
		}
		return "", fmt.Errorf("未找到 jj、git 或 gh，请先安装其中之一")
	}
	switch pref {
	case "jj", "git", "gh":
	default:
		return "", fmt.Errorf("不支持的工具: %s（可选: auto jj git gh）", pref)
	}
	if _, err := exec.LookPath(pref); err != nil {
		return "", fmt.Errorf("未找到 %s，请先安装或改用其他工具", pref)
	}
	return pref, nil
}

// RepoName 从 owner/repo 或完整地址中提取仓库目录名。
func RepoName(spec string) string {
	s := strings.TrimSpace(spec)
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")
	if i := strings.LastIndexAny(s, "/:"); i >= 0 {
		s = s[i+1:]
	}
	if s == "" {
		return "repo"
	}
	return s
}

func cloneURL(spec, protocol string) string {
	spec = strings.TrimSpace(spec)
	if isURL(spec) {
		return spec
	}
	spec = strings.TrimSuffix(strings.TrimSuffix(spec, "/"), ".git")
	host, path, hosted := splitHosted(spec)
	if protocol == "ssh" {
		if hosted {
			return "git@" + host + ":" + path + ".git"
		}
		return "git@github.com:" + spec + ".git"
	}
	if hosted {
		return "https://" + host + "/" + path + ".git"
	}
	return "https://github.com/" + spec + ".git"
}

func isURL(spec string) bool {
	lower := strings.ToLower(spec)
	return strings.Contains(lower, "://") || strings.HasPrefix(lower, "git@")
}

// splitHosted 识别 gitlab.com/owner/repo 这类带主机名的写法。
func splitHosted(spec string) (host, path string, ok bool) {
	i := strings.IndexByte(spec, '/')
	if i <= 0 || strings.Count(spec, "/") < 2 {
		return "", "", false
	}
	host = spec[:i]
	if !strings.Contains(host, ".") {
		return "", "", false
	}
	return host, spec[i+1:], true
}

func looksLikeRepo(arg string) bool {
	return isURL(arg) || (strings.Contains(arg, "/") && !strings.HasPrefix(arg, "/"))
}

func dirOf(cfg *config.Config, opts Options) string {
	if v := strings.TrimSpace(opts.Dir); v != "" {
		return expandPath(v)
	}
	if v := strings.TrimSpace(cfg.Repos.Dir); v != "" {
		return expandPath(v)
	}
	if cfg.Home != "" {
		return filepath.Join(cfg.Home, "git")
	}
	return expandPath("~/.udev/git")
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

func isRepo(dest string) bool {
	return exists(filepath.Join(dest, ".jj")) || exists(filepath.Join(dest, ".git"))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func clone(ctx context.Context, u *ui.UI, r Repo, tool string) error {
	switch tool {
	case "jj":
		return util.RunCmdContext(ctx, u, "jj git clone "+r.Name, nil, "jj", "git", "clone", r.URL, r.Dest)
	case "gh":
		src := r.Spec
		if _, _, hosted := splitHosted(src); isURL(src) || hosted {
			src = r.URL
		}
		return util.RunCmdContext(ctx, u, "gh repo clone "+r.Name, nil, "gh", "repo", "clone", src, r.Dest)
	default:
		return util.RunCmdContext(ctx, u, "git clone "+r.Name, nil, "git", "clone", r.URL, r.Dest)
	}
}

func update(ctx context.Context, u *ui.UI, r Repo) error {
	if exists(filepath.Join(r.Dest, ".jj")) {
		if _, err := exec.LookPath("jj"); err != nil {
			return fmt.Errorf("仓库使用 jj，但未找到 jj")
		}
		return util.RunCmdInContext(ctx, r.Dest, u, "jj git fetch "+r.Name, nil, "jj", "git", "fetch")
	}
	if exists(filepath.Join(r.Dest, ".git")) {
		if _, err := exec.LookPath("git"); err != nil {
			return fmt.Errorf("仓库使用 git，但未找到 git")
		}
		return util.RunCmdInContext(ctx, r.Dest, u, "git pull --ff-only "+r.Name, nil, "git", "pull", "--ff-only")
	}
	return fmt.Errorf("目标目录不是 git/jj 仓库: %s", r.Dest)
}

// checkAll 检查每个仓库是否有远端更新，不修改工作副本。
func checkAll(ctx context.Context, u *ui.UI, list []Repo) error {
	var failed []string
	var upToDate, updates, missing int
	for i, r := range list {
		fmt.Fprintf(u.Out, "\n%s ==> [%d/%d] %s\n", u.Bold(""), i+1, len(list), r.Name)
		if !isRepo(r.Dest) {
			u.Infof("%s 未克隆: %s", r.Name, r.Dest)
			missing++
			continue
		}
		changed, err := checkRepo(ctx, r)
		if err != nil {
			u.Errorf("%s 检查失败: %v", r.Name, err)
			failed = append(failed, r.Name)
			continue
		}
		if changed {
			u.Warnf("%s 有可用更新", r.Name)
			updates++
		} else {
			u.OKf("%s 已是最新", r.Name)
			upToDate++
		}
	}

	fmt.Fprintln(u.Out)
	u.Infof("检查完成: 已最新 %d，有更新 %d，未克隆 %d", upToDate, updates, missing)
	if updates > 0 {
		u.Infof("执行 udev repos 即可更新")
	}
	if len(failed) > 0 {
		return fmt.Errorf("以下仓库检查失败: %s", strings.Join(failed, " "))
	}
	return nil
}

// checkRepo 返回仓库是否有远端更新。
func checkRepo(ctx context.Context, r Repo) (bool, error) {
	if exists(filepath.Join(r.Dest, ".jj")) {
		if _, err := exec.LookPath("jj"); err != nil {
			return false, fmt.Errorf("仓库使用 jj，但未找到 jj")
		}
		before, err := jjRemoteSnapshot(ctx, r.Dest)
		if err != nil {
			return false, err
		}
		if out, err := runOutput(ctx, r.Dest, "jj", "git", "fetch"); err != nil {
			return false, commandError(out, err)
		}
		after, err := jjRemoteSnapshot(ctx, r.Dest)
		if err != nil {
			return false, err
		}
		return before != after, nil
	}
	if exists(filepath.Join(r.Dest, ".git")) {
		if _, err := exec.LookPath("git"); err != nil {
			return false, fmt.Errorf("仓库使用 git，但未找到 git")
		}
		out, err := runOutput(ctx, r.Dest, "git", "fetch", "--dry-run")
		if err != nil {
			return false, commandError(out, err)
		}
		return strings.TrimSpace(out) != "", nil
	}
	return false, fmt.Errorf("目标目录不是 git/jj 仓库: %s", r.Dest)
}

// jjRemoteSnapshot 记录所有 bookmark 及其远端目标，用于比较 fetch 前后是否变化。
func jjRemoteSnapshot(ctx context.Context, dir string) (string, error) {
	out, err := runOutput(ctx, dir, "jj", "bookmark", "list", "--all-remotes",
		"-T", `name ++ " " ++ remote ++ " " ++ normal_target.commit_id() ++ "\n"`)
	if err != nil {
		return "", commandError(out, err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	sort.Strings(lines)
	return strings.Join(lines, "\n"), nil
}

func runOutput(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func commandError(out string, err error) error {
	msg := strings.TrimSpace(out)
	if msg == "" {
		return err
	}
	lines := strings.Split(msg, "\n")
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	return fmt.Errorf("%v: %s", err, strings.Join(lines, " | "))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
