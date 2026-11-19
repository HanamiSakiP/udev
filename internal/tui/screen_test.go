package tui

import (
	"strings"
	"testing"

	"github.com/hanamisakip/udev/internal/config"
)

func TestVisibleWidthAndPad(t *testing.T) {
	if got := visibleWidth("工具链"); got != 6 {
		t.Fatalf("visibleWidth(工具链) = %d, want 6", got)
	}
	if got := visibleWidth("\x1b[31m工具\x1b[0m"); got != 4 {
		t.Fatalf("visibleWidth(ANSI) = %d, want 4", got)
	}
	if got := truncate("工具链", 4); got != "工具" {
		t.Fatalf("truncate(工具链, 4) = %q", got)
	}
	if got := visibleWidth(pad("工具", 6)); got != 6 {
		t.Fatalf("pad(工具, 6) width = %d, want 6", got)
	}
}

func TestLogBuffer(t *testing.T) {
	l := newLogBuffer(10)
	_, _ = l.Write([]byte("hello\nwor"))
	_, _ = l.Write([]byte("ld\n\x1b[31m红色\x1b[0m\n"))
	got := l.tail(10)
	want := []string{"hello", "world", "红色"}
	if len(got) != len(want) {
		t.Fatalf("tail = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tail[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRenderAllTabs(t *testing.T) {
	oldColor := colorOn
	colorOn = false
	defer func() { colorOn = oldColor }()

	cfg := config.Defaults("/tmp/tui-test")
	cfg.Home = "/tmp/tui-test"
	cfg.File = "/tmp/tui-test/config.yaml"
	a := &app{
		cfg:     cfg,
		version: "test",
		width:   80,
		height:  24,
		log:     newLogBuffer(10),
	}
	a.managers = []managerRow{{
		name: "pyenv", installed: true, version: "pyenv 2.0", dir: "/tmp/pyenv", backups: 1, latest: "pyenv.tar.gz",
	}}
	a.devs = a.buildDevRows()
	a.repos = []repoRow{{name: "demo", url: "https://example.com/a/b.git", exists: true}}
	a.releases = []releaseRow{{repo: "a/b", pattern: "*.zip", matches: 1}}
	a.cfgText = []string{"mirror:", "    use_cn: false", "paths:", "    backup_dir: /tmp/backups"}

	for tab := 0; tab < tabCount; tab++ {
		a.tab = tab
		checkFrame(t, a.render(), a.width, a.height)
	}

	a.showHelp = true
	checkFrame(t, a.render(), a.width, a.height)
	a.showHelp = false
	a.menu = &menuState{title: "工具操作 · pyenv", items: []menuItem{
		{label: "安装 / 重装", hint: "i"},
		{label: "备份", hint: "b"},
	}}
	checkFrame(t, a.render(), a.width, a.height)
	a.menu = nil
	a.confirm = &confirmState{title: "卸载 pyenv", message: "确定继续？"}
	checkFrame(t, a.render(), a.width, a.height)
	a.confirm = nil
	a.showLog = true
	_, _ = a.log.Write([]byte("正在执行测试操作\n完成\n"))
	checkFrame(t, a.render(), a.width, a.height)
}

func checkFrame(t *testing.T, out string, width, height int) {
	t.Helper()
	out = strings.TrimPrefix(out, "\x1b[H")
	lines := strings.Split(out, "\r\n")
	if len(lines) != height {
		t.Fatalf("rendered %d lines, want %d", len(lines), height)
	}
	for i, line := range lines {
		if got := visibleWidth(line); got != width {
			t.Fatalf("line %d width = %d, want %d: %q", i, got, width, line)
		}
	}
}
