// Package releases 负责从 GitHub Release 下载配置文件中的发布文件。
package releases

import (
	"context"
	"encoding/json"
	"fmt"
	neturl "net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/hanamisakip/udev/internal/config"
	"github.com/hanamisakip/udev/internal/ui"
	"github.com/hanamisakip/udev/internal/util"
)

const apiBase = "https://api.github.com"

// Options 是 release 命令的运行时选项。
type Options struct {
	Tool  string   // auto | gh | api
	Dir   string   // 覆盖配置中的下载目录
	List  bool     // 只列出将要下载的文件
	Force bool     // 覆盖已存在的文件
	Args  []string // 仓库名、owner/repo 或 owner/repo#pattern
}

// ParseOptions 解析 release 命令参数。
func ParseOptions(args []string) (Options, error) {
	opts := Options{Tool: "auto"}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			return opts, errHelp
		case a == "--list" || a == "-l":
			opts.List = true
		case a == "--force" || a == "-f":
			opts.Force = true
		case a == "--gh":
			opts.Tool = "gh"
		case a == "--api":
			opts.Tool = "api"
		case a == "--tool" || a == "-t":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s 需要一个值（auto|gh|api）", a)
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

// Run 执行 Release 文件下载。
func Run(ctx context.Context, cfg *config.Config, u *ui.UI, args []string) error {
	opts, err := ParseOptions(args)
	if err != nil {
		return err
	}
	list, err := Resolve(cfg, opts)
	if err != nil {
		return err
	}
	dir := DirOf(cfg, opts)

	u.Header("udev release")
	u.Infof("下载目录: %s", dir)
	u.Infof("文件数量: %d", len(list))

	if opts.List {
		u.Infof("使用工具: %s（auto 时 gh 已登录优先 gh，否则使用 GitHub API）", firstNonEmpty(opts.Tool, cfg.Releases.Tool, "auto"))
		fmt.Fprintln(u.Out)
		for _, item := range list {
			label := item.Repo
			if item.Tag != "" {
				label += "@" + item.Tag
			}
			fmt.Fprintf(u.Out, "  %-34s %-28s %s\n", label, item.Pattern, u.Dim(dir))
		}
		return nil
	}

	tool, err := DetectTool(firstNonEmpty(opts.Tool, cfg.Releases.Tool), u)
	if err != nil {
		return err
	}
	if tool == "gh" {
		u.Infof("使用工具: gh（已登录，优先使用）")
	} else {
		u.Infof("使用工具: GitHub API")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建下载目录失败: %w", err)
	}

	var failed []string
	downloaded, skipped := 0, 0
	for i, item := range list {
		fmt.Fprintf(u.Out, "\n%s ==> [%d/%d] %s\n", u.Bold(""), i+1, len(list), item.Repo)
		u.Subst("pattern: %s", item.Pattern)
		if item.Tag != "" {
			u.Subst("tag: %s", item.Tag)
		}

		var got, skip int
		if tool == "gh" {
			got, skip, err = downloadWithGH(ctx, u, item, dir, opts.Force)
		} else {
			got, skip, err = downloadWithAPI(ctx, cfg, u, item, dir, opts.Force)
		}
		skipped += skip
		if err != nil {
			u.Errorf("%s 下载失败: %v", item.Repo, err)
			failed = append(failed, item.Repo)
			continue
		}
		downloaded += got
		if got > 0 {
			u.OKf("%s 完成", item.Repo)
		} else if skip > 0 {
			u.OKf("%s 已是最新（文件已存在，已跳过）", item.Repo)
		}
	}

	fmt.Fprintln(u.Out)
	u.Infof("完成: 下载 %d 个文件，跳过 %d 个已存在文件", downloaded, skipped)
	if len(failed) > 0 {
		return fmt.Errorf("以下 Release 下载失败: %s", strings.Join(failed, " "))
	}
	u.OKf("=== 全部完成 ===")
	return nil
}

// Resolve 根据配置与命令行参数计算待下载的 Release 文件列表。
func Resolve(cfg *config.Config, opts Options) ([]ReleaseItem, error) {
	list := make([]ReleaseItem, 0, len(cfg.Releases.List))
	for _, item := range cfg.Releases.List {
		repo, err := NormalizeRepo(item.Repo)
		if err != nil {
			return nil, err
		}
		pattern := strings.TrimSpace(item.Pattern)
		if pattern == "" {
			pattern = "*"
		}
		list = append(list, ReleaseItem{Repo: repo, Pattern: pattern, Tag: strings.TrimSpace(item.Tag)})
	}

	if len(opts.Args) == 0 {
		if len(list) == 0 {
			return nil, fmt.Errorf("配置中没有 Release 下载项")
		}
		return list, nil
	}

	var out []ReleaseItem
	seen := map[string]bool{}
	add := func(item ReleaseItem) {
		key := item.Repo + "\x00" + item.Tag + "\x00" + item.Pattern
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, item)
	}

	var missing []string
	for _, arg := range opts.Args {
		if i := strings.Index(arg, "#"); i > 0 {
			repo, err := NormalizeRepo(arg[:i])
			if err != nil {
				return nil, err
			}
			pattern := strings.TrimSpace(arg[i+1:])
			if pattern == "" {
				pattern = "*"
			}
			add(ReleaseItem{Repo: repo, Pattern: pattern})
			continue
		}

		matched := false
		lower := strings.ToLower(arg)
		for _, item := range list {
			name := RepoName(item.Repo)
			if strings.EqualFold(name, arg) ||
				strings.EqualFold(item.Repo, arg) ||
				strings.Contains(strings.ToLower(item.Repo), lower) {
				add(item)
				matched = true
			}
		}
		if matched {
			continue
		}
		missing = append(missing, arg)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("配置中没有匹配的 Release: %s（可写 owner/repo 或 owner/repo#pattern）", strings.Join(missing, " "))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("没有需要下载的文件")
	}
	return out, nil
}

// ReleaseItem 描述一个待下载的 Release 文件。
type ReleaseItem struct {
	Repo    string
	Pattern string
	Tag     string
}

// DirOf 返回下载目录；配置为空时使用 <home>/git/release。
func DirOf(cfg *config.Config, opts Options) string {
	if v := strings.TrimSpace(opts.Dir); v != "" {
		return expandPath(v)
	}
	if v := strings.TrimSpace(cfg.Releases.Dir); v != "" {
		return expandPath(v)
	}
	if cfg.Home != "" {
		return filepath.Join(cfg.Home, "git", "release")
	}
	return expandPath("~/.udev/git/release")
}

// DetectTool 选择下载工具：auto 时 gh 已登录优先 gh，否则使用 GitHub API。
func DetectTool(pref string, u *ui.UI) (string, error) {
	pref = strings.ToLower(strings.TrimSpace(pref))
	if pref == "" || pref == "auto" {
		if ghAvailable() {
			if ghLoggedIn() {
				return "gh", nil
			}
			u.Infof("gh 已安装但未登录，回退到 GitHub API（可运行 udev setup gh 启用 gh）")
		}
		return "api", nil
	}
	switch pref {
	case "gh":
		if !ghAvailable() {
			return "", fmt.Errorf("未找到 gh，请先安装 GitHub CLI（udev tools go），或改用 --tool api")
		}
		if !ghLoggedIn() {
			return "", fmt.Errorf("gh 未登录，请先运行 udev setup gh，或改用 --tool api")
		}
		return "gh", nil
	case "api", "curl", "wget":
		return "api", nil
	default:
		return "", fmt.Errorf("不支持的工具: %s（可选: auto gh api）", pref)
	}
}

// NormalizeRepo 把 owner/repo、GitHub 地址或 git 地址统一成 owner/repo。
func NormalizeRepo(spec string) (string, error) {
	s := strings.TrimSpace(spec)
	s = strings.TrimSuffix(s, "/")
	if s == "" {
		return "", fmt.Errorf("仓库地址为空")
	}
	switch {
	case strings.Contains(s, "://"):
		u, err := neturl.Parse(s)
		if err != nil {
			return "", fmt.Errorf("无效仓库地址 %s: %w", spec, err)
		}
		s = strings.TrimPrefix(u.Path, "/")
	case strings.HasPrefix(s, "git@"):
		if i := strings.Index(s, ":"); i >= 0 {
			s = s[i+1:]
		}
	default:
		s = strings.TrimPrefix(s, "github.com/")
	}
	s = strings.TrimSuffix(s, ".git")
	parts := strings.Split(s, "/")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("无效仓库: %s（应为 owner/repo）", spec)
	}
	return parts[0] + "/" + parts[1], nil
}

// RepoName 从 owner/repo 中提取仓库名。
func RepoName(repo string) string {
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		return repo[i+1:]
	}
	return repo
}

func downloadWithGH(ctx context.Context, u *ui.UI, item ReleaseItem, dir string, force bool) (int, int, error) {
	names, err := ghAssetNames(ctx, item)
	if err != nil {
		return 0, 0, err
	}
	matched, err := matchAssetNames(names, item.Pattern)
	if err != nil {
		return 0, 0, err
	}
	if len(matched) == 0 {
		return 0, 0, fmt.Errorf("Release %s 中没有匹配 %q 的文件（共 %d 个资产）", ghReleaseLabel(item), item.Pattern, len(names))
	}

	downloaded, skipped := 0, 0
	for _, name := range matched {
		name = filepath.Base(name)
		dest := filepath.Join(dir, name)
		if _, err := os.Stat(dest); err == nil && !force {
			u.Warnf("%s 已存在，跳过（--force 覆盖）", name)
			skipped++
			continue
		}

		args := []string{"release", "download", "--repo", item.Repo, "--pattern", name, "--dir", dir}
		if item.Tag != "" {
			args = append(args, "--tag", item.Tag)
		}
		if force {
			args = append(args, "--clobber")
		}
		desc := "gh release download --repo " + item.Repo + " --pattern " + name
		if err := util.RunCmdInContext(ctx, dir, u, desc, nil, "gh", args...); err != nil {
			return downloaded, skipped, err
		}
		u.OKf("已保存: %s", dest)
		downloaded++
	}
	return downloaded, skipped, nil
}

// ghAssetNames 通过 gh 获取 Release 中的资产文件名。
func ghAssetNames(ctx context.Context, item ReleaseItem) ([]string, error) {
	args := []string{"release", "view"}
	if item.Tag != "" {
		args = append(args, item.Tag)
	}
	args = append(args, "--repo", item.Repo, "--json", "assets", "--jq", ".assets[].name")
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return nil, fmt.Errorf("获取 Release 资产列表失败: %w", err)
		}
		return nil, fmt.Errorf("获取 Release 资产列表失败: %v: %s", err, msg)
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

func matchAssetNames(names []string, pattern string) ([]string, error) {
	var matched []string
	for _, name := range names {
		ok, err := path.Match(pattern, name)
		if err != nil {
			return nil, fmt.Errorf("无效的 pattern %q: %w", pattern, err)
		}
		if ok {
			matched = append(matched, name)
		}
	}
	return matched, nil
}

func ghReleaseLabel(item ReleaseItem) string {
	if item.Tag != "" {
		return item.Tag
	}
	return "latest"
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

func downloadWithAPI(ctx context.Context, cfg *config.Config, u *ui.UI, item ReleaseItem, dir string, force bool) (int, int, error) {
	release, err := fetchRelease(ctx, cfg, item)
	if err != nil {
		return 0, 0, err
	}

	var matched []ghAsset
	for _, asset := range release.Assets {
		ok, err := path.Match(item.Pattern, asset.Name)
		if err != nil {
			return 0, 0, fmt.Errorf("无效的 pattern %q: %w", item.Pattern, err)
		}
		if ok {
			matched = append(matched, asset)
		}
	}
	if len(matched) == 0 {
		return 0, 0, fmt.Errorf("Release %s 中没有匹配 %q 的文件（共 %d 个资产）", release.TagName, item.Pattern, len(release.Assets))
	}

	downloaded, skipped := 0, 0
	for _, asset := range matched {
		name := filepath.Base(asset.Name)
		dest := filepath.Join(dir, name)
		if _, err := os.Stat(dest); err == nil && !force {
			u.Warnf("%s 已存在，跳过（--force 覆盖）", name)
			skipped++
			continue
		}
		u.Subst("下载 %s", name)
		if err := util.Download(ctx, downloadURL(cfg, asset.URL), dest); err != nil {
			return downloaded, skipped, fmt.Errorf("下载 %s 失败: %w", name, err)
		}
		u.OKf("已保存: %s", dest)
		downloaded++
	}
	return downloaded, skipped, nil
}

func fetchRelease(ctx context.Context, cfg *config.Config, item ReleaseItem) (*ghRelease, error) {
	var endpoint string
	if item.Tag != "" {
		endpoint = fmt.Sprintf("%s/repos/%s/releases/tags/%s", apiBase, item.Repo, neturl.PathEscape(item.Tag))
	} else {
		endpoint = fmt.Sprintf("%s/repos/%s/releases/latest", apiBase, item.Repo)
	}

	data, err := apiGet(ctx, cfg, endpoint)
	if item.Tag == "" && err != nil {
		// 只有 prerelease 或尚未标记 latest 的仓库，退回最近一次 Release。
		data, err = apiGet(ctx, cfg, fmt.Sprintf("%s/repos/%s/releases?per_page=1", apiBase, item.Repo))
		if err != nil {
			return nil, err
		}
		var list []ghRelease
		if err := json.Unmarshal(data, &list); err != nil {
			return nil, fmt.Errorf("解析 Release 列表失败: %w", err)
		}
		if len(list) == 0 {
			return nil, fmt.Errorf("仓库 %s 没有可用的 Release", item.Repo)
		}
		return &list[0], nil
	}
	if err != nil {
		return nil, err
	}
	var release ghRelease
	if err := json.Unmarshal(data, &release); err != nil {
		return nil, fmt.Errorf("解析 Release 失败: %w", err)
	}
	return &release, nil
}

func apiGet(ctx context.Context, cfg *config.Config, url string) ([]byte, error) {
	headers := map[string]string{
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": "2022-11-28",
	}
	if token := githubToken(cfg); token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	return util.FetchHeaders(ctx, url, headers)
}

func githubToken(cfg *config.Config) string {
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	data, err := os.ReadFile(cfg.GhTokenFile())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func downloadURL(cfg *config.Config, raw string) string {
	if !cfg.Mirror.UseCN {
		return raw
	}
	proxy := strings.TrimRight(strings.TrimSpace(cfg.Mirror.GitHubProxy), "/")
	if proxy == "" || !strings.HasPrefix(raw, "https://github.com/") {
		return raw
	}
	return proxy + "/" + raw
}

func ghAvailable() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

func ghLoggedIn() bool {
	cmd := exec.Command("gh", "auth", "status", "--hostname", "github.com")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
