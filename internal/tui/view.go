package tui

import (
	"fmt"
	"os"
	"strings"
	"time"
)

var colorOn = os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiBlue   = "\x1b[34m"
	ansiCyan   = "\x1b[36m"
	ansiWhite  = "\x1b[97m"
	ansiSel    = "\x1b[48;5;237m\x1b[97m"
)

func paint(code, s string) string {
	if !colorOn {
		return s
	}
	return code + s + ansiReset
}

func bold(s string) string   { return paint(ansiBold, s) }
func dim(s string) string    { return paint(ansiDim, s) }
func red(s string) string    { return paint(ansiRed, s) }
func green(s string) string  { return paint(ansiGreen, s) }
func yellow(s string) string { return paint(ansiYellow, s) }
func cyan(s string) string   { return paint(ansiCyan, s) }
func selected(s string) string {
	if !colorOn {
		return s
	}
	return ansiSel + s + ansiReset
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (a *app) spinner() string {
	return spinnerFrames[a.tick%len(spinnerFrames)]
}

func (a *app) render() string {
	w, h := a.width, a.height
	if w < 20 {
		w = 20
	}
	if h < 8 {
		h = 8
	}
	bodyH := h - 6
	if bodyH < 1 {
		bodyH = 1
	}

	body := a.renderBody(bodyH)
	if len(body) < bodyH {
		body = append(body, make([]string, bodyH-len(body))...)
	}
	body = body[:bodyH]

	lines := make([]string, 0, h)
	lines = append(lines, a.titleLine(w))
	lines = append(lines, a.tabLine(w))
	lines = append(lines, a.rule(w))
	lines = append(lines, body...)
	lines = append(lines, a.rule(w))
	lines = append(lines, a.statusLine(w))
	lines = append(lines, a.footerLine(w))

	var b strings.Builder
	b.WriteString("\x1b[H")
	for i := 0; i < h; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		b.WriteString(pad(line, w))
		b.WriteString(ansiReset)
		if i < h-1 {
			b.WriteString("\r\n")
		}
	}
	return b.String()
}

func (a *app) titleLine(w int) string {
	left := bold(cyan(" udev")) + " " + dim("v"+a.version) + "  " + dim(a.cfg.Home)
	right := green("● 就绪")
	if a.busy {
		right = yellow(a.spinner() + " 运行中")
	}
	rightW := visibleWidth(right) + 1
	if rightW >= w {
		return truncate(left, w)
	}
	return pad(left, w-rightW) + right + " "
}

func (a *app) tabLine(w int) string {
	var parts []string
	for i, name := range tabNames {
		label := fmt.Sprintf(" %d %s ", i+1, name)
		if i == a.tab {
			if colorOn {
				parts = append(parts, "\x1b[48;5;24m\x1b[97m"+ansiBold+label+ansiReset)
			} else {
				parts = append(parts, "["+label+"]")
			}
		} else {
			parts = append(parts, dim(label))
		}
	}
	return truncate(strings.Join(parts, " "), w)
}

func (a *app) rule(w int) string {
	return dim(strings.Repeat("─", w))
}

func (a *app) statusLine(w int) string {
	var s string
	switch {
	case a.busy:
		elapsed := int(time.Since(a.opStart).Seconds())
		s = fmt.Sprintf("%s %s · 已运行 %ds · Esc 查看日志 · x 取消", a.spinner(), a.opName, elapsed)
		s = yellow(s)
	case a.status != "":
		s = cyan(a.status)
	case a.showLog:
		s = dim("操作日志 · Esc 返回")
	default:
		s = dim("就绪 · 使用 Tab 切换页面，? 查看快捷键")
	}
	return truncate(" "+s, w)
}

func (a *app) footerLine(w int) string {
	var text string
	if a.showLog {
		text = "Esc 返回 · x 取消运行中的操作 · q 关闭日志后退出"
	} else {
		switch a.tab {
		case 0:
			text = "Tab 切换 · 1-6 直达页面 · r 刷新 · ? 帮助 · q 退出"
		case 1:
			text = "↑↓ 选择 · Enter 操作 · i 安装 · b 备份 · s 恢复 · u 卸载 · a 安装缺失 · q 退出"
		case 2:
			text = "↑↓ 选择 · Enter 执行 · a 默认全部 · A 含 cargo · q 退出"
		case 3:
			text = "↑↓ 选择 · Enter 更新选中 · a 全部更新 · c 检查更新 · q 退出"
		case 4:
			state := "关"
			if a.releaseForce {
				state = "开"
			}
			text = "↑↓ 选择 · Enter 下载选中 · a 全部下载 · f 覆盖:" + state + " · q 退出"
		case 5:
			text = "↑↓ 滚动 · r 重载 · 修改配置请运行 udev config edit · q 退出"
		}
	}
	return truncate(" "+dim(text), w)
}

func (a *app) renderBody(h int) []string {
	switch {
	case a.showHelp:
		return a.renderHelpBody(h)
	case a.confirm != nil:
		return a.renderConfirmBody(h)
	case a.menu != nil:
		return a.renderMenuBody(h)
	case a.showLog:
		return a.renderLogBody(h)
	}
	switch a.tab {
	case 0:
		return a.renderOverview(h)
	case 1:
		return a.renderManagers(h)
	case 2:
		return a.renderDevs(h)
	case 3:
		return a.renderRepos(h)
	case 4:
		return a.renderReleases(h)
	case 5:
		return a.renderConfig(h)
	}
	return nil
}

func (a *app) renderOverview(h int) []string {
	installed := 0
	for _, m := range a.managers {
		if m.installed {
			installed++
		}
	}
	cloned := 0
	for _, r := range a.repos {
		if r.exists {
			cloned++
		}
	}
	have := 0
	for _, r := range a.releases {
		if r.matches > 0 {
			have++
		}
	}

	lines := []string{
		bold("工具链") + "  " + dim(fmt.Sprintf("%d 个 · %d 已安装", len(a.managers), installed)),
	}
	for _, m := range a.managers {
		status := green("已安装")
		if !m.installed {
			status = dim("未安装")
		}
		version := m.version
		if version == "" {
			version = "-"
		}
		line := "  " + pad(m.name, 10) + " " + pad(status, 8) + " " + pad(truncate(version, 26), 26) +
			" " + dim(fmt.Sprintf("备份 %d", m.backups))
		lines = append(lines, line)
	}

	lines = append(lines, "", bold("开发工具清单"))
	counts := map[string]int{}
	for _, row := range a.devs {
		switch row.key {
		case "node", "go", "python", "cargo":
			counts[row.key] = len(row.items)
		}
	}
	lines = append(lines, fmt.Sprintf("  node %d 项 · go %d 项 · python %d 项 · cargo %d 项",
		counts["node"], counts["go"], counts["python"], counts["cargo"]))

	lines = append(lines, "", bold("仓库 / Release"))
	lines = append(lines, fmt.Sprintf("  仓库    %d 个 · 已克隆 %d · %s", len(a.repos), cloned, dim(a.cfg.Repos.Dir)))
	lines = append(lines, fmt.Sprintf("  Release %d 项 · 本地已有 %d · %s", len(a.releases), have, dim(releasesDir(a.cfg))))

	lines = append(lines, "", bold("路径"))
	lines = append(lines,
		"  配置  "+dim(a.cfg.File),
		"  备份  "+dim(a.cfg.Paths.BackupDir),
		"  源码  "+dim(a.cfg.SourceDir()),
	)
	if a.loadError != "" {
		lines = append(lines, "", red("  注意: "+a.loadError))
	}
	return lines
}

func (a *app) renderManagers(h int) []string {
	lines := []string{
		bold("工具链") + "  " + dim("Enter 操作 · i 安装 · b 备份 · s 恢复 · u 卸载"),
		dim(fmt.Sprintf("  %-10s %-8s %-26s %-6s %s", "工具", "状态", "版本", "备份", "安装目录")),
		dim("  " + strings.Repeat("─", max(10, a.width-4))),
	}
	view := max(1, h-7)
	count := len(a.managers)
	start := clamp(a.scroll[1], 0, max(0, count-view))
	end := min(count, start+view)
	for i := start; i < end; i++ {
		m := a.managers[i]
		prefix := "  "
		status := green("已安装")
		if !m.installed {
			status = yellow("未安装")
		}
		if i == a.sel[1] {
			prefix = "▸ "
			status = "已安装"
			if !m.installed {
				status = "未安装"
			}
		}
		version := m.version
		if version == "" {
			version = "-"
		}
		dir := m.dir
		if i != a.sel[1] {
			dir = dim(dir)
		}
		line := prefix + pad(m.name, 10) + " " + pad(status, 8) + " " +
			pad(truncate(version, 26), 26) + " " + pad(fmt.Sprint(m.backups), 6) + " " + dir
		if i == a.sel[1] {
			line = selected(pad(line, a.width))
		}
		lines = append(lines, line)
	}

	if count > 0 && a.sel[1] < count {
		m := a.managers[a.sel[1]]
		state := green("已安装")
		if !m.installed {
			state = yellow("未安装")
		}
		latest := m.latest
		if latest == "" {
			latest = dim("无备份")
		}
		lines = append(lines,
			"",
			bold("▸ "+m.name)+"  "+state,
			dim("  路径  ")+m.dir,
			dim("  备份  ")+fmt.Sprintf("%s（共 %d 个）", latest, m.backups),
		)
	}
	return lines
}

func (a *app) renderDevs(h int) []string {
	lines := []string{
		bold("开发工具") + "  " + dim("Enter 执行 · a 默认全部 · A 含 cargo"),
		dim(fmt.Sprintf("  %-38s %s", "分组", "规模")),
	}
	view := max(1, h-7)
	count := len(a.devs)
	start := clamp(a.scroll[2], 0, max(0, count-view))
	end := min(count, start+view)
	for i := start; i < end; i++ {
		row := a.devs[i]
		prefix := "  "
		if i == a.sel[2] {
			prefix = "▸ "
		}
		line := prefix + pad(truncate(row.title, 38), 38) + " "
		if i == a.sel[2] {
			line += row.summary
		} else {
			line += dim(row.summary)
		}
		if i == a.sel[2] {
			lines = append(lines, selected(pad(line, a.width)))
		} else {
			lines = append(lines, line)
		}
	}

	if count > 0 && a.sel[2] < count {
		row := a.devs[a.sel[2]]
		lines = append(lines, "", bold("▸ "+row.title))
		shown := 0
		for _, item := range row.items {
			if shown >= 2 {
				break
			}
			lines = append(lines, "  "+truncate(item, max(10, a.width-4)))
			shown++
		}
		if remaining := len(row.items) - shown; remaining > 0 {
			lines = append(lines, dim(fmt.Sprintf("  … 另有 %d 项", remaining)))
		} else {
			lines = append(lines, dim("  Enter 执行该分组"))
		}
	}
	return lines
}

func (a *app) renderRepos(h int) []string {
	tool := a.cfg.Repos.Tool
	if tool == "" {
		tool = "auto"
	}
	lines := []string{
		bold("仓库") + "  " + dim("目录 "+a.cfg.Repos.Dir+" · 工具 "+tool),
		dim(fmt.Sprintf("  %-22s %-8s %s", "仓库", "状态", "地址")),
	}
	view := max(1, h-4)
	count := len(a.repos)
	start := clamp(a.scroll[3], 0, max(0, count-view))
	end := min(count, start+view)
	for i := start; i < end; i++ {
		r := a.repos[i]
		prefix := "  "
		state := green("已克隆")
		if !r.exists {
			state = yellow("未克隆")
		}
		if i == a.sel[3] {
			prefix = "▸ "
			state = "已克隆"
			if !r.exists {
				state = "未克隆"
			}
		}
		url := r.url
		if i != a.sel[3] {
			url = dim(url)
		}
		line := prefix + pad(truncate(r.name, 22), 22) + " " + pad(state, 8) + " " + url
		if i == a.sel[3] {
			line = selected(pad(line, a.width))
		}
		lines = append(lines, line)
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	existing := 0
	for _, r := range a.repos {
		if r.exists {
			existing++
		}
	}
	lines = append(lines, dim(fmt.Sprintf("  共 %d 个仓库 · 已克隆 %d · 未克隆 %d", len(a.repos), existing, len(a.repos)-existing)))
	return lines
}

func (a *app) renderReleases(h int) []string {
	tool := a.cfg.Releases.Tool
	if tool == "" {
		tool = "auto"
	}
	force := "关闭"
	if a.releaseForce {
		force = "开启"
	}
	lines := []string{
		bold("Release") + "  " + dim("目录 "+releasesDir(a.cfg)+" · 工具 "+tool+" · 覆盖 "+force),
		dim(fmt.Sprintf("  %-30s %-24s %s", "仓库", "文件匹配", "本地")),
	}
	view := max(1, h-4)
	count := len(a.releases)
	start := clamp(a.scroll[4], 0, max(0, count-view))
	end := min(count, start+view)
	for i := start; i < end; i++ {
		r := a.releases[i]
		prefix := "  "
		state := dim("未下载")
		if r.matches > 0 {
			state = green(fmt.Sprintf("已有 %d", r.matches))
		}
		if i == a.sel[4] {
			prefix = "▸ "
			state = "未下载"
			if r.matches > 0 {
				state = fmt.Sprintf("已有 %d", r.matches)
			}
		}
		pattern := r.pattern
		if r.tag != "" {
			pattern += "@" + r.tag
		}
		line := prefix + pad(truncate(r.repo, 30), 30) + " " + pad(truncate(pattern, 24), 24) + " " + state
		if i == a.sel[4] {
			line = selected(pad(line, a.width))
		}
		lines = append(lines, line)
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	have := 0
	for _, r := range a.releases {
		if r.matches > 0 {
			have++
		}
	}
	lines = append(lines, dim(fmt.Sprintf("  共 %d 项 · 本地已有 %d · 下载目录 %s", len(a.releases), have, releasesDir(a.cfg))))
	return lines
}

func (a *app) renderConfig(h int) []string {
	lines := []string{bold("配置文件") + "  " + dim(a.cfg.File)}
	if a.loadError != "" {
		lines = append(lines, red("  "+a.loadError))
	}
	view := max(1, h-2)
	count := len(a.cfgText)
	start := clamp(a.scroll[5], 0, max(0, count-view))
	end := min(count, start+view)
	if count == 0 {
		lines = append(lines, dim("  (配置为空)"))
	}
	for i := start; i < end; i++ {
		text := a.cfgText[i]
		lineNo := dim(fmt.Sprintf("%4d ", i+1))
		lines = append(lines, lineNo+colorYAML(text))
	}
	return lines
}

func colorYAML(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") {
		return dim(line)
	}
	if i := strings.Index(line, " #"); i >= 0 {
		return line[:i] + dim(line[i:])
	}
	return line
}

func (a *app) renderLogBody(h int) []string {
	state := "运行中"
	stateLine := yellow(a.spinner() + " " + state)
	if !a.busy {
		if a.opSummary == "失败" {
			stateLine = red("● 失败")
		} else {
			stateLine = green("● 完成")
		}
	}
	title := bold("操作日志") + "  " + a.opName + "  " + stateLine
	lines := []string{title, dim(strings.Repeat("─", a.width))}
	view := max(1, h-3)
	tail := a.log.tail(view + a.logOffset)
	if a.logOffset > 0 {
		if len(tail) > a.logOffset {
			tail = tail[:len(tail)-a.logOffset]
		} else {
			tail = nil
		}
	}
	if len(tail) == 0 {
		if a.logOffset > 0 || a.log.count() > 0 {
			tail = []string{dim("(已到日志开头)")}
		} else {
			tail = []string{dim("等待输出…")}
		}
	}
	lines = append(lines, tail...)
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	lines = append(lines, dim("  Esc 返回 · ↑↓ 滚动 · End 最新 · x 取消运行中的操作"))
	return lines
}

func (a *app) renderHelpBody(h int) []string {
	content := []string{
		bold("udev 交互界面"),
		"",
		pad("1-6 / Tab", 18) + "切换页面",
		pad("↑ ↓ / j k", 18) + "移动选择 / 滚动",
		pad("Enter", 18) + "执行或打开操作菜单",
		pad("r", 18) + "刷新数据",
		pad("?", 18) + "关闭帮助",
		pad("q", 18) + "退出",
		"",
		bold("工具页"),
		pad("i / b / s / u", 18) + "安装 / 备份 / 恢复 / 卸载",
		pad("a", 18) + "安装所有缺失工具",
		bold("开发工具页"),
		pad("a / A", 18) + "默认全部 / 含 cargo",
		bold("仓库页"),
		pad("a / c", 18) + "全部更新 / 检查更新",
		bold("Release 页"),
		pad("a / f", 18) + "全部下载 / 切换覆盖",
		bold("日志页"),
		pad("Esc / x", 18) + "返回 / 取消操作",
		pad("↑ ↓ / End", 18) + "滚动日志 / 回到最新",
	}
	return a.centerBox("帮助", content, h)
}

func (a *app) renderMenuBody(h int) []string {
	content := make([]string, 0, len(a.menu.items)+2)
	for i, item := range a.menu.items {
		line := "  " + pad(item.label, 22) + dim(item.hint)
		if i == a.menu.sel {
			line = selected(pad("▸ "+item.label, 24) + item.hint)
		}
		content = append(content, line)
	}
	content = append(content, "", dim("Enter 选择 · Esc 关闭"))
	return a.centerBox(a.menu.title, content, h)
}

func (a *app) renderConfirmBody(h int) []string {
	width := min(max(30, a.width-8), 60)
	content := wrapText(a.confirm.message, width-6)
	content = append(content, "", green("Enter / y 确认")+"    "+dim("Esc / n 取消"))
	return a.centerBox(a.confirm.title, content, h)
}

func (a *app) centerBox(title string, content []string, h int) []string {
	width := min(max(30, a.width-8), 68)
	box := boxLines(title, content, width)
	top := max(0, (h-len(box))/2)
	left := max(0, (a.width-width)/2)
	out := make([]string, 0, h)
	for i := 0; i < top; i++ {
		out = append(out, "")
	}
	for _, line := range box {
		out = append(out, strings.Repeat(" ", left)+line)
	}
	for len(out) < h {
		out = append(out, "")
	}
	if len(out) > h {
		out = out[:h]
	}
	return out
}

func boxLines(title string, content []string, width int) []string {
	if width < 8 {
		width = 8
	}
	inner := width - 4
	top := ""
	if title != "" {
		title = truncate(title, max(0, width-6))
		fill := width - 5 - visibleWidth(title)
		top = "┌─ " + title + " " + strings.Repeat("─", max(0, fill)) + "┐"
	} else {
		top = "┌" + strings.Repeat("─", width-2) + "┐"
	}
	lines := []string{top}
	for _, line := range content {
		lines = append(lines, "│ "+pad(line, inner)+" │")
	}
	lines = append(lines, "└"+strings.Repeat("─", width-2)+"┘")
	return lines
}

func wrapText(s string, width int) []string {
	if width <= 1 {
		width = 1
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		var line strings.Builder
		w := 0
		for _, r := range para {
			rw := runeWidth(r)
			if w+rw > width && w > 0 {
				out = append(out, line.String())
				line.Reset()
				w = 0
			}
			line.WriteRune(r)
			w += rw
		}
		out = append(out, line.String())
	}
	return out
}
