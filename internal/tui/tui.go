package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hanamisakip/udev/internal/config"
	"github.com/hanamisakip/udev/internal/devtools"
	"github.com/hanamisakip/udev/internal/manager"
	"github.com/hanamisakip/udev/internal/releases"
	"github.com/hanamisakip/udev/internal/repos"
	"github.com/hanamisakip/udev/internal/ui"
	"github.com/hanamisakip/udev/internal/uninstall"
)

const tabCount = 6

var tabNames = []string{"概览", "工具", "开发工具", "仓库", "Release", "配置"}

type managerRow struct {
	name      string
	installed bool
	version   string
	dir       string
	backups   int
	latest    string
}

type devRow struct {
	key     string
	title   string
	summary string
	items   []string
	args    []string
}

type repoRow struct {
	name   string
	url    string
	exists bool
}

type releaseRow struct {
	repo    string
	pattern string
	tag     string
	matches int
}

type menuState struct {
	title string
	items []menuItem
	sel   int
}

type menuItem struct {
	label  string
	hint   string
	action func()
}

type confirmState struct {
	title   string
	message string
	action  func()
}

type opResult struct {
	name string
	err  error
}

type app struct {
	ctx     context.Context
	cfg     *config.Config
	version string
	term    *terminal
	width   int
	height  int

	tab    int
	sel    [tabCount]int
	scroll [tabCount]int

	managers  []managerRow
	devs      []devRow
	repos     []repoRow
	releases  []releaseRow
	cfgText   []string
	loadError string

	log       *logBuffer
	busy      bool
	opName    string
	opStart   time.Time
	opSummary string
	cancel    context.CancelFunc
	opDone    chan opResult

	showLog   bool
	showHelp  bool
	menu      *menuState
	confirm   *confirmState
	logOffset int

	status      string
	statusUntil time.Time
	tick        int
	dirty       bool
	quit        bool

	lastSizeCheck time.Time
	releaseForce  bool
}

// Run 启动 tui。调用方负责准备好配置。
func Run(ctx context.Context, cfg *config.Config, version string) error {
	term, err := openTerminal()
	if err != nil {
		return err
	}
	defer term.restore()

	width, height := terminalSize()
	a := &app{
		ctx:     ctx,
		cfg:     cfg,
		version: version,
		term:    term,
		width:   width,
		height:  height,
		log:     newLogBuffer(3000),
		opDone:  make(chan opResult, 1),
		dirty:   true,
	}
	a.status = "正在检测工具状态…"

	// 进入备用屏幕，退出时恢复。
	fmt.Fprint(os.Stdout, "\x1b[?1049h\x1b[?25l\x1b[2J")
	defer fmt.Fprint(os.Stdout, "\x1b[?25h\x1b[?1049l")

	a.draw()
	a.load()
	a.status = ""
	a.dirty = true
	return a.loop()
}

func (a *app) loop() error {
	keys := readKeys()
	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()

	for !a.quit {
		if a.dirty {
			a.draw()
		}
		select {
		case <-a.ctx.Done():
			if a.cancel != nil {
				a.cancel()
			}
			return nil
		case k, ok := <-keys:
			if !ok {
				return nil
			}
			a.handleKey(k)
			a.dirty = true
		case res := <-a.opDone:
			a.finishOp(res)
			a.dirty = true
		case now := <-ticker.C:
			a.tick++
			if now.Sub(a.lastSizeCheck) >= time.Second {
				a.lastSizeCheck = now
				w, h := terminalSize()
				if w != a.width || h != a.height {
					a.width, a.height = w, h
					a.clampAll()
					a.dirty = true
				}
			}
			if a.status != "" && !a.statusUntil.IsZero() && now.After(a.statusUntil) {
				a.status = ""
				a.dirty = true
			}
			if a.busy || a.showLog || a.status != "" {
				a.dirty = true
			}
		}
	}
	return nil
}

func (a *app) draw() {
	if !a.dirty {
		return
	}
	fmt.Fprint(os.Stdout, a.render())
	a.dirty = false
}

func (a *app) flash(msg string) {
	a.status = msg
	a.statusUntil = time.Now().Add(4 * time.Second)
}

func (a *app) load() {
	a.loadError = ""
	a.managers = nil
	a.devs = nil
	a.repos = nil
	a.releases = nil

	env := &manager.Env{Cfg: a.cfg, UI: &ui.UI{Out: a.log, Err: a.log}, Ctx: context.Background()}
	for _, m := range manager.All(env) {
		latest := ""
		if p := manager.LatestBackup(a.cfg, m.Name()); p != "" {
			latest = filepath.Base(p)
		}
		row := managerRow{
			name:      m.Name(),
			installed: m.Installed(),
			dir:       m.InstallDir(),
			backups:   manager.CountBackups(a.cfg, m.Name()),
			latest:    latest,
		}
		if row.installed {
			row.version = m.Version()
		}
		a.managers = append(a.managers, row)
	}

	a.devs = a.buildDevRows()
	a.repos = a.buildRepoRows()
	a.releases = a.buildReleaseRows()

	if data, err := a.cfg.Marshal(); err != nil {
		a.loadError = err.Error()
		a.cfgText = []string{"读取配置失败: " + err.Error()}
	} else {
		a.cfgText = strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	}

	a.clampAll()
}

func (a *app) buildDevRows() []devRow {
	d := a.cfg.DevTools
	node := devRow{
		key:   "node",
		title: "Node / npm",
		items: append([]string{}, prefix("npm  ", d.Node.Npm)...),
		args:  []string{"node"},
	}
	if d.Node.NvmVersion != "" {
		node.items = append([]string{"nvm  " + d.Node.NvmVersion}, node.items...)
	}
	node.items = append(node.items, prefix("cmd  ", d.Node.Commands)...)
	node.items = append(node.items, prefix("clean ", d.Node.Cleanup)...)

	golang := devRow{
		key:   "go",
		title: "Go",
		args:  []string{"go"},
	}
	if d.Go.GVersion != "" {
		golang.items = append(golang.items, "g    "+d.Go.GVersion)
	}
	for _, p := range d.Go.Packages {
		golang.items = append(golang.items, "pkg  "+p.Path)
	}
	golang.items = append(golang.items, prefix("clean ", d.Go.Cleanup)...)

	python := devRow{
		key:   "python",
		title: "Python / pip",
		items: append([]string{}, prefix("pip  ", d.Python.Packages)...),
		args:  []string{"python"},
	}
	if d.Python.PyenvVersion != "" {
		python.items = append([]string{"pyenv " + d.Python.PyenvVersion}, python.items...)
	}
	if d.Python.PipUpgrade {
		python.items = append(python.items, "pip  upgrade pip")
	}
	python.items = append(python.items, prefix("cmd  ", d.Python.Commands)...)
	python.items = append(python.items, prefix("clean ", d.Python.Cleanup)...)

	cargo := devRow{key: "cargo", title: "Cargo", args: []string{"cargo"}}
	for _, p := range d.Cargo.Packages {
		line := "cargo " + p.Name
		if len(p.Args) > 0 {
			line += " " + strings.Join(p.Args, " ")
		}
		cargo.items = append(cargo.items, line)
	}
	cargo.items = append(cargo.items, prefix("clean ", d.Cargo.Cleanup)...)

	for _, row := range []*devRow{&node, &golang, &python, &cargo} {
		row.items = compact(row.items)
		row.summary = fmt.Sprintf("%d 项", len(row.items))
	}

	total := len(node.items) + len(golang.items) + len(python.items)
	all := devRow{
		key:     "default",
		title:   "默认全部（node + go + python）",
		summary: fmt.Sprintf("约 %d 项", total),
		args:    []string{"node", "go", "python"},
		items:   []string{"按顺序执行 node、go、python 三个分组。", "cargo 需要单独选择，避免一次性耗时过长。"},
	}
	allCargo := devRow{
		key:     "all",
		title:   "全部（含 cargo）",
		summary: fmt.Sprintf("约 %d 项", total+len(cargo.items)),
		args:    []string{"all"},
		items:   []string{"执行 node、go、python、cargo 全部分组。", "耗时较长，建议在网络稳定时运行。"},
	}

	return []devRow{all, allCargo, node, golang, python, cargo}
}

func (a *app) buildRepoRows() []repoRow {
	list, err := repos.Resolve(a.cfg, repos.Options{})
	if err != nil {
		if a.loadError == "" {
			a.loadError = "仓库清单: " + err.Error()
		}
		return nil
	}
	out := make([]repoRow, 0, len(list))
	for _, r := range list {
		st, err := os.Stat(r.Dest)
		out = append(out, repoRow{
			name:   r.Name,
			url:    r.URL,
			exists: err == nil && st.IsDir(),
		})
	}
	return out
}

func releasesDir(cfg *config.Config) string {
	return releases.DirOf(cfg, releases.Options{})
}

func (a *app) buildReleaseRows() []releaseRow {
	list, err := releases.Resolve(a.cfg, releases.Options{})
	if err != nil {
		if a.loadError == "" {
			a.loadError = "Release 清单: " + err.Error()
		}
		return nil
	}
	dir := releases.DirOf(a.cfg, releases.Options{})
	out := make([]releaseRow, 0, len(list))
	for _, item := range list {
		matches, _ := filepath.Glob(filepath.Join(dir, item.Pattern))
		out = append(out, releaseRow{
			repo:    item.Repo,
			pattern: item.Pattern,
			tag:     item.Tag,
			matches: len(matches),
		})
	}
	return out
}

func prefix(p string, items []string) []string {
	out := make([]string, 0, len(items))
	for _, s := range items {
		if strings.TrimSpace(s) != "" {
			out = append(out, p+s)
		}
	}
	return out
}

func compact(items []string) []string {
	out := items[:0]
	for _, s := range items {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func (a *app) clampAll() {
	a.tab = clamp(a.tab, 0, tabCount-1)
	for i := 0; i < tabCount; i++ {
		count := a.listCount(i)
		view := a.listView(i)
		if count == 0 {
			a.sel[i] = 0
			a.scroll[i] = 0
			continue
		}
		a.sel[i] = clamp(a.sel[i], 0, count-1)
		maxScroll := count - view
		if maxScroll < 0 {
			maxScroll = 0
		}
		a.scroll[i] = clamp(a.scroll[i], 0, maxScroll)
		if a.sel[i] < a.scroll[i] {
			a.scroll[i] = a.sel[i]
		}
		if a.sel[i] >= a.scroll[i]+view {
			a.scroll[i] = a.sel[i] - view + 1
		}
	}
}

func (a *app) listCount(tab int) int {
	switch tab {
	case 1:
		return len(a.managers)
	case 2:
		return len(a.devs)
	case 3:
		return len(a.repos)
	case 4:
		return len(a.releases)
	case 5:
		return len(a.cfgText)
	}
	return 0
}

func (a *app) listView(tab int) int {
	body := a.height - 6
	if body < 3 {
		body = 3
	}
	switch tab {
	case 1:
		return max(1, body-7)
	case 2:
		return max(1, body-7)
	case 3, 4:
		return max(1, body-4)
	case 5:
		return max(1, body-2)
	}
	return max(1, body)
}

func (a *app) move(delta int) {
	tab := a.tab
	count := a.listCount(tab)
	if count == 0 {
		return
	}
	view := a.listView(tab)
	if tab == 0 {
		return
	}
	a.sel[tab] = clamp(a.sel[tab]+delta, 0, count-1)
	if a.sel[tab] < a.scroll[tab] {
		a.scroll[tab] = a.sel[tab]
	}
	if a.sel[tab] >= a.scroll[tab]+view {
		a.scroll[tab] = a.sel[tab] - view + 1
	}
}

func (a *app) jump(delta int) {
	tab := a.tab
	count := a.listCount(tab)
	if count == 0 {
		return
	}
	view := a.listView(tab)
	a.sel[tab] = clamp(a.sel[tab]+delta*view, 0, count-1)
	if a.sel[tab] < a.scroll[tab] {
		a.scroll[tab] = a.sel[tab]
	}
	if a.sel[tab] >= a.scroll[tab]+view {
		a.scroll[tab] = a.sel[tab] - view + 1
	}
}

func (a *app) scrollBy(delta int) {
	tab := a.tab
	count := a.listCount(tab)
	view := a.listView(tab)
	maxScroll := count - view
	if maxScroll < 0 {
		maxScroll = 0
	}
	a.scroll[tab] = clamp(a.scroll[tab]+delta, 0, maxScroll)
	if tab == 5 {
		a.sel[tab] = a.scroll[tab]
	}
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (a *app) handleKey(k key) {
	if a.showHelp {
		a.showHelp = false
		return
	}
	if a.confirm != nil {
		switch {
		case k.name == "esc", k.ch == 'n', k.ch == 'N':
			a.confirm = nil
		case k.name == "enter", k.ch == 'y', k.ch == 'Y':
			action := a.confirm.action
			a.confirm = nil
			if action != nil {
				action()
			}
		}
		return
	}
	if a.menu != nil {
		switch k.name {
		case "esc":
			a.menu = nil
		case "up":
			a.menu.sel = clamp(a.menu.sel-1, 0, len(a.menu.items)-1)
		case "down":
			a.menu.sel = clamp(a.menu.sel+1, 0, len(a.menu.items)-1)
		case "enter":
			item := a.menu.items[a.menu.sel]
			a.menu = nil
			if item.action != nil {
				item.action()
			}
		default:
			switch k.ch {
			case 'q':
				a.menu = nil
			case 'k':
				a.menu.sel = clamp(a.menu.sel-1, 0, len(a.menu.items)-1)
			case 'j':
				a.menu.sel = clamp(a.menu.sel+1, 0, len(a.menu.items)-1)
			}
		}
		return
	}
	if a.showLog {
		switch k.name {
		case "esc", "q":
			a.showLog = false
			a.logOffset = 0
			return
		case "up":
			a.logOffset++
			return
		case "down":
			if a.logOffset > 0 {
				a.logOffset--
			}
			return
		case "pgup":
			a.logOffset += max(1, a.listView(5))
			return
		case "pgdn":
			a.logOffset = max(0, a.logOffset-max(1, a.listView(5)))
			return
		case "end":
			a.logOffset = 0
			return
		case "home":
			a.logOffset = a.log.count()
			return
		}
		if k.ch == 'x' || k.name == "ctrl-c" {
			if a.busy {
				a.cancelOp()
			}
			return
		}
		if k.ch == 'j' {
			a.logOffset++
		} else if k.ch == 'k' && a.logOffset > 0 {
			a.logOffset--
		}
		return
	}

	switch k.name {
	case "ctrl-c":
		if a.cancel != nil {
			a.cancel()
		}
		a.quit = true
		return
	case "tab":
		a.tab = (a.tab + 1) % tabCount
		return
	case "shift-tab":
		a.tab = (a.tab + tabCount - 1) % tabCount
		return
	case "up":
		if a.tab == 0 || a.tab == 5 {
			a.scrollBy(-1)
		} else {
			a.move(-1)
		}
		return
	case "down":
		if a.tab == 0 || a.tab == 5 {
			a.scrollBy(1)
		} else {
			a.move(1)
		}
		return
	case "pgup":
		if a.tab == 0 || a.tab == 5 {
			a.scrollBy(-a.listView(a.tab))
		} else {
			a.jump(-1)
		}
		return
	case "pgdn":
		if a.tab == 0 || a.tab == 5 {
			a.scrollBy(a.listView(a.tab))
		} else {
			a.jump(1)
		}
		return
	case "home":
		if a.tab == 0 || a.tab == 5 {
			a.scroll[a.tab] = 0
			if a.tab == 5 {
				a.sel[a.tab] = 0
			}
		} else {
			a.sel[a.tab] = 0
			a.scroll[a.tab] = 0
		}
		return
	case "end":
		if a.tab == 0 || a.tab == 5 {
			maxScroll := max(0, a.listCount(a.tab)-a.listView(a.tab))
			a.scroll[a.tab] = maxScroll
			if a.tab == 5 {
				a.sel[a.tab] = maxScroll
			}
		} else if n := a.listCount(a.tab); n > 0 {
			a.sel[a.tab] = n - 1
			a.scroll[a.tab] = max(0, n-a.listView(a.tab))
		}
		return
	case "esc":
		return
	}

	if k.ch >= '1' && k.ch <= '6' {
		a.tab = int(k.ch - '1')
		return
	}

	switch k.ch {
	case 'q':
		if a.busy {
			a.flash("操作正在运行：x 取消，Esc 查看日志")
			return
		}
		a.quit = true
		return
	case '?':
		a.showHelp = true
		return
	case 'r':
		if a.busy {
			a.flash("操作进行中，完成后再刷新")
			return
		}
		a.load()
		a.flash("已刷新")
		return
	}

	switch a.tab {
	case 1:
		a.handleManagerKey(k)
	case 2:
		a.handleDevKey(k)
	case 3:
		a.handleRepoKey(k)
	case 4:
		a.handleReleaseKey(k)
	}
}

func (a *app) handleManagerKey(k key) {
	if len(a.managers) == 0 {
		return
	}
	row := a.managers[a.sel[1]]
	switch k.name {
	case "enter":
		a.openManagerMenu(row)
		return
	}
	switch k.ch {
	case 'i':
		a.installManager(row)
	case 'b':
		a.backupManager(row)
	case 's', 'R':
		a.restoreManager(row)
	case 'u':
		a.uninstallManager(row)
	case 'a':
		a.installMissing()
	case 'j':
		a.move(1)
	case 'k':
		a.move(-1)
	}
}

func (a *app) handleDevKey(k key) {
	if len(a.devs) == 0 {
		return
	}
	row := a.devs[a.sel[2]]
	switch k.name {
	case "enter":
		a.confirmDev(row)
		return
	}
	switch k.ch {
	case 'a':
		a.confirmDev(a.devs[0])
	case 'A':
		a.confirmDev(a.devs[1])
	case 'j':
		a.move(1)
	case 'k':
		a.move(-1)
	}
}

func (a *app) confirmDev(row devRow) {
	a.confirm = &confirmState{
		title:   "执行开发工具 · " + row.title,
		message: "将执行 " + row.title + " 分组（" + row.summary + "），期间会下载并安装软件包。\n确定继续？",
		action:  func() { a.runDev(row) },
	}
}

func (a *app) handleRepoKey(k key) {
	switch k.name {
	case "enter":
		if len(a.repos) > 0 {
			row := a.repos[a.sel[3]]
			a.confirmRepos([]string{row.name}, "更新仓库 "+row.name,
				"将克隆或更新仓库 "+row.name+"。\n确定继续？")
		}
		return
	}
	switch k.ch {
	case 'a':
		a.confirmRepos(nil, "更新全部仓库", "将克隆或更新配置中的全部仓库。\n确定继续？")
	case 'c':
		a.runRepos([]string{"--check"}, "检查仓库更新")
	case 'j':
		a.move(1)
	case 'k':
		a.move(-1)
	}
}

func (a *app) confirmRepos(args []string, title, message string) {
	args = append([]string(nil), args...)
	a.confirm = &confirmState{
		title:   title,
		message: message,
		action:  func() { a.runRepos(args, title) },
	}
}

func (a *app) handleReleaseKey(k key) {
	switch k.name {
	case "enter":
		if len(a.releases) > 0 {
			row := a.releases[a.sel[4]]
			a.confirmReleases([]string{row.repo}, "下载 Release · "+row.repo,
				"将下载 "+row.repo+" 匹配 "+row.pattern+" 的文件。\n确定继续？")
		}
		return
	}
	switch k.ch {
	case 'a':
		a.confirmReleases(nil, "下载全部 Release", "将按配置下载全部 Release 文件。\n确定继续？")
	case 'f':
		a.releaseForce = !a.releaseForce
		state := "关闭"
		if a.releaseForce {
			state = "开启"
		}
		a.flash("覆盖已有文件: " + state)
	case 'j':
		a.move(1)
	case 'k':
		a.move(-1)
	}
}

func (a *app) confirmReleases(args []string, title, message string) {
	args = append([]string(nil), args...)
	a.confirm = &confirmState{
		title:   title,
		message: message,
		action:  func() { a.runReleases(args, title) },
	}
}

func (a *app) openManagerMenu(row managerRow) {
	items := []menuItem{
		{label: "安装 / 重装", hint: "i", action: func() { a.installManager(row) }},
		{label: "备份", hint: "b", action: func() { a.backupManager(row) }},
		{label: "从最新备份恢复", hint: "s", action: func() { a.restoreManager(row) }},
		{label: "卸载", hint: "u", action: func() { a.uninstallManager(row) }},
		{label: "取消", hint: "Esc", action: func() {}},
	}
	a.menu = &menuState{title: "工具操作 · " + row.name, items: items}
}

func (a *app) installManager(row managerRow) {
	if row.installed {
		a.confirm = &confirmState{
			title:   "重新安装 " + row.name,
			message: "将先执行清理再重新安装。建议先备份，确定继续？",
			action:  func() { a.doInstall(row.name, true) },
		}
		return
	}
	a.doInstall(row.name, false)
}

func (a *app) doInstall(name string, force bool) {
	a.runOp("安装 "+name, func(ctx context.Context, u *ui.UI) error {
		env := &manager.Env{Cfg: a.cfg, UI: u, Ctx: ctx}
		m, ok := manager.Resolve(env, name)
		if !ok {
			return fmt.Errorf("未知工具: %s", name)
		}
		if force && m.Installed() {
			u.Warnf("清理已有的 %s", name)
			if err := m.Clean(); err != nil {
				u.Warnf("清理 %s 失败: %v", name, err)
			}
		}
		u.Infof("在线安装 %s", name)
		if err := m.Install(); err != nil {
			return fmt.Errorf("%s 安装失败: %w", name, err)
		}
		if !m.Installed() {
			return fmt.Errorf("%s 验证失败", name)
		}
		u.OKf("%s 验证通过", name)
		return nil
	})
}

func (a *app) backupManager(row managerRow) {
	a.runOp("备份 "+row.name, func(ctx context.Context, u *ui.UI) error {
		env := &manager.Env{Cfg: a.cfg, UI: u, Ctx: ctx}
		m, ok := manager.Resolve(env, row.name)
		if !ok {
			return fmt.Errorf("未知工具: %s", row.name)
		}
		out, err := m.Backup()
		if err != nil {
			return fmt.Errorf("%s 备份失败: %w", row.name, err)
		}
		u.OKf("已创建备份: %s", out)
		return nil
	})
}

func (a *app) restoreManager(row managerRow) {
	backup := manager.LatestBackup(a.cfg, row.name)
	if backup == "" {
		a.flash(row.name + " 没有可用备份")
		return
	}
	message := "将使用最新备份覆盖当前目录，确定继续？"
	if !row.installed {
		message = "当前未安装，将从最新备份恢复，确定继续？"
	}
	a.confirm = &confirmState{
		title:   "恢复 " + row.name,
		message: message + "\n" + filepath.Base(backup),
		action: func() {
			a.runOp("恢复 "+row.name, func(ctx context.Context, u *ui.UI) error {
				env := &manager.Env{Cfg: a.cfg, UI: u, Ctx: ctx}
				m, ok := manager.Resolve(env, row.name)
				if !ok {
					return fmt.Errorf("未知工具: %s", row.name)
				}
				u.Infof("使用备份: %s", filepath.Base(backup))
				if err := m.Restore(backup); err != nil {
					return fmt.Errorf("%s 恢复失败: %w", row.name, err)
				}
				if !m.Installed() {
					return fmt.Errorf("%s 恢复后验证失败", row.name)
				}
				u.OKf("%s 已恢复", row.name)
				return nil
			})
		},
	}
}

func (a *app) uninstallManager(row managerRow) {
	a.confirm = &confirmState{
		title: "卸载 " + row.name,
		message: "将删除相关目录并清理 shell 配置，此操作不可撤销。\n" +
			"确定卸载 " + row.name + "？",
		action: func() {
			a.runOp("卸载 "+row.name, func(ctx context.Context, u *ui.UI) error {
				return uninstall.Run(ctx, a.cfg, u, uninstall.Options{
					Yes:        true,
					KeepGopath: !a.cfg.Uninstall.IncludeGopath,
					Managers:   []string{row.name},
				})
			})
		},
	}
}

func (a *app) installMissing() {
	var missing []string
	for _, row := range a.managers {
		if !row.installed {
			missing = append(missing, row.name)
		}
	}
	if len(missing) == 0 {
		a.flash("全部工具均已安装")
		return
	}
	a.confirm = &confirmState{
		title:   "安装缺失工具",
		message: "将安装: " + strings.Join(missing, " "),
		action: func() {
			a.runOp("安装缺失工具", func(ctx context.Context, u *ui.UI) error {
				env := &manager.Env{Cfg: a.cfg, UI: u, Ctx: ctx}
				var failed []string
				for i, name := range missing {
					fmt.Fprintf(u.Out, "\n==> [%d/%d] %s\n", i+1, len(missing), name)
					m, ok := manager.Resolve(env, name)
					if !ok {
						failed = append(failed, name)
						continue
					}
					if err := m.Install(); err != nil {
						u.Errorf("%s 安装失败: %v", name, err)
						failed = append(failed, name)
						continue
					}
					if !m.Installed() {
						u.Errorf("%s 验证失败", name)
						failed = append(failed, name)
						continue
					}
					u.OKf("%s 验证通过", name)
				}
				if len(failed) > 0 {
					return fmt.Errorf("以下工具安装失败: %s", strings.Join(failed, " "))
				}
				return nil
			})
		},
	}
}

func (a *app) runDev(row devRow) {
	if len(row.args) == 0 {
		return
	}
	args := append([]string(nil), row.args...)
	a.runOp("开发工具 · "+row.title, func(ctx context.Context, u *ui.UI) error {
		return devtools.Run(ctx, a.cfg, u, args)
	})
}

func (a *app) runRepos(args []string, title string) {
	a.runOp(title, func(ctx context.Context, u *ui.UI) error {
		return repos.Run(ctx, a.cfg, u, args)
	})
}

func (a *app) runReleases(args []string, title string) {
	if a.releaseForce {
		args = append(args, "--force")
	}
	a.runOp(title, func(ctx context.Context, u *ui.UI) error {
		return releases.Run(ctx, a.cfg, u, args)
	})
}

func (a *app) runOp(name string, fn func(context.Context, *ui.UI) error) {
	if a.busy {
		a.flash("已有操作正在运行")
		return
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.cancel = cancel
	a.busy = true
	a.opName = name
	a.opStart = time.Now()
	a.opSummary = ""
	a.log.reset()
	a.logOffset = 0
	a.showLog = true
	a.status = ""

	u := &ui.UI{Out: a.log, Err: a.log, Color: false, TTY: false}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				a.opDone <- opResult{name: name, err: fmt.Errorf("操作内部错误: %v", r)}
			}
		}()
		err := fn(ctx, u)
		a.opDone <- opResult{name: name, err: err}
	}()
}

func (a *app) finishOp(res opResult) {
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.busy = false
	elapsed := int(time.Since(a.opStart).Seconds())
	if res.err != nil {
		a.opSummary = "失败"
		fmt.Fprintf(a.log, "\n[失败] %v\n", res.err)
		a.flash(fmt.Sprintf("%s 失败（%ds）", res.name, elapsed))
	} else {
		a.opSummary = "完成"
		fmt.Fprintf(a.log, "\n[完成] 耗时 %ds\n", elapsed)
		a.flash(fmt.Sprintf("%s 完成（%ds）", res.name, elapsed))
	}
	a.load()
}

func (a *app) cancelOp() {
	if a.cancel == nil {
		return
	}
	a.cancel()
	a.flash("已请求取消操作")
}
