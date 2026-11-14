package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/hanamisakip/udev/internal/config"
	"github.com/hanamisakip/udev/internal/ui"
	"github.com/hanamisakip/udev/internal/util"
)

// Options 控制 udev update 的行为。
type Options struct {
	Check      bool
	Force      bool
	SourceOnly bool
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Name    string        `json:"name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type githubTag struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

type githubCommit struct {
	SHA string `json:"sha"`
}

type releaseInfo struct {
	Ref       string // 用于下载源码的 git ref
	Version   string // 展示与写入二进制 ldflags 的版本
	IsRelease bool
	Assets    []githubAsset
}

// Update 从 GitHub 检查并安装最新版 udev，同时同步本地源码目录。
func Update(ctx context.Context, cfg *config.Config, u *ui.UI, currentVersion string, args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	if cfg.Update.Repo == "" {
		cfg.Update.Repo = "HanamiSakiP/udev"
	}
	if cfg.Update.Branch == "" {
		cfg.Update.Branch = "main"
	}

	u.Header("udev update")
	u.Infof("当前版本: %s", displayVersion(currentVersion))

	info, err := resolveLatest(ctx, cfg)
	if err != nil {
		return err
	}
	u.Infof("最新版本: %s（来源: %s）", info.Version, info.Ref)

	if opts.Check {
		if upToDate(cfg, currentVersion, info) && !opts.Force {
			u.OKf("已是最新版本")
		} else {
			u.Infof("发现新版本，执行 udev update 即可更新")
		}
		return nil
	}

	if upToDate(cfg, currentVersion, info) && !opts.Force && !opts.SourceOnly {
		u.OKf("已是最新版本（使用 --force 可强制重新同步）")
		return nil
	}

	// 下载并替换源码目录，确保源码与更新后的版本一致。
	if err := updateSource(ctx, cfg, u, info); err != nil {
		if len(info.Assets) == 0 || opts.SourceOnly {
			return err
		}
		u.Warnf("源码同步失败，将继续尝试更新二进制: %v", err)
	}

	if opts.SourceOnly {
		u.OKf("源码已更新: %s", cfg.SourceDir())
		return nil
	}

	// 优先使用 GitHub Release 资产；没有匹配资产时从源码构建。
	newBin, cleanup, err := fetchReleaseBinary(ctx, cfg, u, info)
	if err != nil {
		return err
	}
	if newBin == "" {
		newBin, cleanup, err = buildLatest(ctx, cfg, u, info)
		if err != nil {
			return err
		}
	}
	defer cleanup()

	if err := installBinary(cfg, newBin); err != nil {
		return err
	}
	u.OKf("已更新到 %s", info.Version)
	u.Subst("源码目录: %s", cfg.SourceDir())
	return nil
}

func parseOptions(args []string) (Options, error) {
	var o Options
	for _, a := range args {
		switch a {
		case "--check", "-n":
			o.Check = true
		case "--force", "-f":
			o.Force = true
		case "--source-only":
			o.SourceOnly = true
		case "-h", "--help":
			return o, fmt.Errorf("用法: udev update [--check] [--force] [--source-only]")
		default:
			if strings.HasPrefix(a, "-") {
				return o, fmt.Errorf("未知参数: %s", a)
			}
			return o, fmt.Errorf("未知参数: %s（update 不接受位置参数）", a)
		}
	}
	return o, nil
}

func displayVersion(v string) string {
	if strings.TrimSpace(v) == "" {
		return "dev"
	}
	return v
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	return v
}

// upToDate 比较当前版本与远端版本。没有 Release 时使用本地源码版本标记。
func upToDate(cfg *config.Config, current string, info releaseInfo) bool {
	remote := normalizeVersion(info.Version)
	local := normalizeVersion(current)
	if info.IsRelease {
		return remote != "" && local == remote
	}
	if marker, err := os.ReadFile(filepath.Join(cfg.SourceDir(), sourceVersionFile)); err == nil {
		return strings.TrimSpace(string(marker)) == info.Version
	}
	return local == remote
}

func resolveLatest(ctx context.Context, cfg *config.Config) (releaseInfo, error) {
	repo := cfg.Update.Repo

	var rel githubRelease
	status, err := apiGet(ctx, cfg, "/repos/"+repo+"/releases/latest", &rel)
	if err == nil && status == http.StatusOK && rel.TagName != "" {
		return releaseInfo{
			Ref:       rel.TagName,
			Version:   normalizeVersion(rel.TagName),
			IsRelease: true,
			Assets:    rel.Assets,
		}, nil
	}

	var tags []githubTag
	tagStatus, tagErr := apiGet(ctx, cfg, "/repos/"+repo+"/tags", &tags)

	var commit githubCommit
	commitStatus, commitErr := apiGet(ctx, cfg, "/repos/"+repo+"/commits/"+cfg.Update.Branch, &commit)

	if tagErr == nil && tagStatus == http.StatusOK && len(tags) > 0 && tags[0].Name != "" {
		// 分支 HEAD 与最新 tag 相同，说明没有新提交，使用 tag 版本。
		if commitErr == nil && commitStatus == http.StatusOK && commit.SHA != "" &&
			tags[0].Commit.SHA != "" && tags[0].Commit.SHA != commit.SHA {
			short := commit.SHA
			if len(short) > 7 {
				short = short[:7]
			}
			return releaseInfo{
				Ref:     commit.SHA,
				Version: cfg.Update.Branch + "-" + short,
			}, nil
		}
		return releaseInfo{
			Ref:       tags[0].Name,
			Version:   normalizeVersion(tags[0].Name),
			IsRelease: true,
		}, nil
	}

	if commitErr != nil {
		return releaseInfo{}, fmt.Errorf("获取 GitHub 最新版本失败: %w", commitErr)
	}
	if commitStatus != http.StatusOK || commit.SHA == "" {
		if commitStatus == http.StatusForbidden || commitStatus == http.StatusTooManyRequests {
			return releaseInfo{}, fmt.Errorf("GitHub API 触发速率限制，请先运行 udev setup gh 或设置 GH_TOKEN")
		}
		return releaseInfo{}, fmt.Errorf("获取 GitHub 最新版本失败: HTTP %d", commitStatus)
	}
	short := commit.SHA
	if len(short) > 7 {
		short = short[:7]
	}
	return releaseInfo{
		Ref:     commit.SHA,
		Version: cfg.Update.Branch + "-" + short,
	}, nil
}

func apiGet(ctx context.Context, cfg *config.Config, path string, out any) (int, error) {
	direct := "https://api.github.com" + path
	urls := []string{direct}
	if cfg.Mirror.UseCN && cfg.Mirror.GitHubProxy != "" {
		urls = []string{strings.TrimRight(cfg.Mirror.GitHubProxy, "/") + "/" + direct, direct}
	}
	client := &http.Client{Timeout: 30 * time.Second}
	var lastStatus int
	var lastErr error
	for _, url := range urls {
		status, err := apiGetOnce(ctx, client, cfg, url, out)
		if err == nil && status >= 200 && status < 300 {
			return status, nil
		}
		lastStatus, lastErr = status, err
		if err != nil && !cfg.Mirror.UseCN {
			break
		}
	}
	return lastStatus, lastErr
}

func apiGetOnce(ctx context.Context, client *http.Client, cfg *config.Config, url string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "udev")
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := githubToken(cfg); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, nil
}

func githubToken(cfg *config.Config) string {
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	if data, err := os.ReadFile(cfg.GhTokenFile()); err == nil {
		return strings.TrimSpace(string(data))
	}
	return ""
}

func githubDownloadURL(cfg *config.Config, url string) string {
	if cfg.Mirror.UseCN && cfg.Mirror.GitHubProxy != "" {
		return strings.TrimRight(cfg.Mirror.GitHubProxy, "/") + "/" + url
	}
	return url
}

// downloadGitHub 优先走镜像，失败时回退到 GitHub 原始地址。
func downloadGitHub(ctx context.Context, cfg *config.Config, original, out string) error {
	url := githubDownloadURL(cfg, original)
	err := util.Download(ctx, url, out)
	if err == nil {
		return nil
	}
	if url == original {
		return err
	}
	return util.Download(ctx, original, out)
}

func updateSource(ctx context.Context, cfg *config.Config, u *ui.UI, info releaseInfo) error {
	repo := cfg.Update.Repo
	url := githubDownloadURL(cfg, fmt.Sprintf("https://codeload.github.com/%s/tar.gz/%s", repo, info.Ref))
	target := cfg.SourceDir()
	u.Subst("下载源码: %s", url)

	if err := os.MkdirAll(cfg.Home, 0o755); err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(cfg.Home, ".udev-src-*.tar.gz")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpName)

	if err := downloadGitHub(ctx, cfg, fmt.Sprintf("https://codeload.github.com/%s/tar.gz/%s", repo, info.Ref), tmpName); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp(cfg.Home, ".udev-unpack-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	if err := extractTarGzStrip(tmpName, tmpDir); err != nil {
		return err
	}
	if err := replaceDir(tmpDir, target); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(target, sourceVersionFile), []byte(info.Version+"\n"), 0o644); err != nil {
		return err
	}
	return nil
}

// extractTarGzStrip 解压 tar.gz，并去掉 GitHub 归档最外层的 udev-main/ 目录。
func extractTarGzStrip(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer zr.Close()

	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(filepath.ToSlash(hdr.Name), "./")
		parts := strings.SplitN(name, "/", 2)
		if len(parts) != 2 || parts[1] == "" {
			continue
		}
		rel, ok := safeRelPath(parts[1])
		if !ok {
			continue
		}
		target := filepath.Join(dest, rel)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			data, err := io.ReadAll(tr)
			if err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode).Perm()
			if mode == 0 {
				mode = 0o644
			}
			if err := writeFileAtomic(target, data, mode); err != nil {
				return err
			}
		}
	}
	return nil
}

// replaceDir 用 src 的内容原子替换 dst。
func replaceDir(src, dst string) error {
	parent := filepath.Dir(dst)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".udev-source-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := util.CopyTree(src, stage); err != nil {
		return err
	}

	old := dst + ".old"
	_ = os.RemoveAll(old)
	hadOld := false
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			return err
		}
		hadOld = true
	}
	if err := os.Rename(stage, dst); err != nil {
		if hadOld {
			_ = os.Rename(old, dst)
		}
		return err
	}
	_ = os.RemoveAll(old)
	return nil
}

// fetchReleaseBinary 下载与当前平台匹配的 Release 资产。没有匹配资产时返回空路径。
func fetchReleaseBinary(ctx context.Context, cfg *config.Config, u *ui.UI, info releaseInfo) (string, func(), error) {
	asset, ok := matchAsset(info.Assets)
	if !ok {
		return "", func() {}, nil
	}
	u.Subst("下载 Release 资产: %s", asset.Name)
	if err := os.MkdirAll(cfg.Home, 0o755); err != nil {
		return "", func() {}, err
	}
	tmpDir, err := os.MkdirTemp(cfg.Home, ".udev-release-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	archive := filepath.Join(tmpDir, asset.Name)
	if err := downloadGitHub(ctx, cfg, asset.BrowserDownloadURL, archive); err != nil {
		cleanup()
		return "", func() {}, err
	}
	bin, err := binaryFromAsset(archive, tmpDir)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return bin, cleanup, nil
}

func matchAsset(assets []githubAsset) (githubAsset, bool) {
	best := -1
	var found githubAsset
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		if !containsAny(name, osTokens()) || !containsAny(name, archTokens()) {
			continue
		}
		if strings.Contains(name, "sha256") || strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".sig") {
			continue
		}
		score := 0
		if strings.HasPrefix(name, "udev") {
			score += 4
		}
		switch {
		case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"), strings.HasSuffix(name, ".zip"):
			score += 2
		case !strings.Contains(filepath.Base(name), "."):
			score += 3
		}
		if score > best {
			best = score
			found = a
		}
	}
	return found, best >= 0
}

func osTokens() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"darwin", "macos", "osx"}
	case "windows":
		return []string{"windows", "win"}
	default:
		return []string{runtime.GOOS}
	}
}

func archTokens() []string {
	switch runtime.GOARCH {
	case "amd64":
		return []string{"amd64", "x86_64", "x64"}
	case "arm64":
		return []string{"arm64", "aarch64"}
	case "386":
		return []string{"386", "i386", "x86"}
	default:
		return []string{runtime.GOARCH}
	}
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if sub != "" && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func binaryFromAsset(archive, dir string) (string, error) {
	lower := strings.ToLower(archive)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return binaryFromTarGz(archive, dir)
	case strings.HasSuffix(lower, ".zip"):
		return binaryFromZip(archive, dir)
	default:
		out := filepath.Join(dir, binaryName())
		if err := copyFile(archive, out, 0o755); err != nil {
			return "", err
		}
		return out, nil
	}
}

func binaryFromTarGz(archive, dir string) (string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	out := filepath.Join(dir, binaryName())
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		base := strings.ToLower(filepath.Base(hdr.Name))
		if base != "udev" && base != "udev.exe" {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return "", err
		}
		if err := writeFileAtomic(out, data, 0o755); err != nil {
			return "", err
		}
		return out, nil
	}
	return "", fmt.Errorf("Release 归档中未找到 udev 可执行文件")
}

func binaryFromZip(archive, dir string) (string, error) {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	out := filepath.Join(dir, binaryName())
	for _, f := range zr.File {
		base := strings.ToLower(filepath.Base(f.Name))
		if base != "udev" && base != "udev.exe" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return "", err
		}
		if err := writeFileAtomic(out, data, 0o755); err != nil {
			return "", err
		}
		return out, nil
	}
	return "", fmt.Errorf("Release 归档中未找到 udev 可执行文件")
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "udev.exe"
	}
	return "udev"
}

// buildLatest 使用已下载的源码构建新二进制。
func buildLatest(ctx context.Context, cfg *config.Config, u *ui.UI, info releaseInfo) (string, func(), error) {
	goBin, err := findGo(cfg)
	if err != nil {
		return "", func() {}, fmt.Errorf("没有可用的 Release 资产，且未找到 Go 工具链: %w", err)
	}
	srcDir := cfg.SourceDir()
	if _, err := os.Stat(filepath.Join(srcDir, "go.mod")); err != nil {
		return "", func() {}, fmt.Errorf("源码目录不完整（%s），无法构建: %w", srcDir, err)
	}
	u.Subst("使用 %s 从源码构建", goBin)

	outDir, err := os.MkdirTemp(cfg.Home, ".udev-build-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(outDir) }
	out := filepath.Join(outDir, binaryName())
	version := info.Version
	if version == "" {
		version = "dev"
	}
	ldflags := "-s -w -X github.com/hanamisakip/udev/internal/cli.Version=" + version
	cmd := exec.CommandContext(ctx, goBin, "build", "-trimpath", "-ldflags", ldflags, "-o", out, ".")
	cmd.Dir = srcDir
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = u.Out
	cmd.Stderr = u.Err
	if err := cmd.Run(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("构建失败: %w", err)
	}
	return out, cleanup, nil
}

func findGo(cfg *config.Config) (string, error) {
	if p, err := exec.LookPath("go"); err == nil {
		return p, nil
	}
	candidates := []string{
		filepath.Join(cfg.Paths.GoRoot, "go", "bin", "go"),
		filepath.Join(cfg.Paths.GoRoot, "bin", "go"),
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".g", "go", "bin", "go"))
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("请先安装 Go（udev install g 或 udev tools go）")
}

// installBinary 将新二进制替换到当前可执行文件位置。
func installBinary(cfg *config.Config, newBin string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	tmp := filepath.Join(dir, "."+filepath.Base(exe)+".new")
	if err := copyFile(newBin, tmp, 0o755); err != nil {
		// 目标目录不可写时保存到本地目录，并提示手动安装。
		fallback := filepath.Join(cfg.Home, "update", binaryName())
		if copyErr := copyFile(newBin, fallback, 0o755); copyErr == nil {
			return fmt.Errorf("无法替换 %s: %w（新版本已保存到 %s，请手动覆盖或执行 go install）", exe, err, fallback)
		}
		return fmt.Errorf("无法替换 %s: %w", exe, err)
	}

	if runtime.GOOS == "windows" {
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		if err := os.Rename(tmp, exe); err != nil {
			_ = os.Rename(old, exe)
			return err
		}
		// 正在运行的旧文件无法删除，下次启动或系统重启后再清理。
		return nil
	}
	return os.Rename(tmp, exe)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
