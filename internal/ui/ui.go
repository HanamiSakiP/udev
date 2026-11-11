// Package ui 负责终端颜色、日志与进度输出。
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const (
	codeRed    = "\033[0;31m"
	codeGreen  = "\033[0;32m"
	codeYellow = "\033[0;33m"
	codeBlue   = "\033[0;34m"
	codeCyan   = "\033[0;36m"
	codeBold   = "\033[1m"
	codeDim    = "\033[2m"
	codeReset  = "\033[0m"
)

// UI 封装所有终端输出。
type UI struct {
	Out   io.Writer
	Err   io.Writer
	Color bool
	TTY   bool
}

// New 创建面向当前进程标准输出的 UI。
func New() *UI {
	tty := isTerminal(os.Stdout)
	color := tty && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	return &UI{Out: os.Stdout, Err: os.Stderr, Color: color, TTY: tty}
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

func (u *UI) paint(code, s string) string {
	if !u.Color {
		return s
	}
	return code + s + codeReset
}

// Bold 加粗。
func (u *UI) Bold(s string) string { return u.paint(codeBold, s) }

// Dim 变暗。
func (u *UI) Dim(s string) string { return u.paint(codeDim, s) }

// Green 绿色。
func (u *UI) Green(s string) string { return u.paint(codeGreen, s) }

// Red 红色。
func (u *UI) Red(s string) string { return u.paint(codeRed, s) }

// Cyan 青色。
func (u *UI) Cyan(s string) string { return u.paint(codeCyan, s) }

// Yellow 黄色。
func (u *UI) Yellow(s string) string { return u.paint(codeYellow, s) }

// Blue 蓝色。
func (u *UI) Blue(s string) string { return u.paint(codeBlue, s) }

// Header 打印命令标题。
func (u *UI) Header(title string) {
	fmt.Fprintf(u.Out, "\n%s\n", u.Bold("=== "+title+" ==="))
}

// Infof 打印信息。
func (u *UI) Infof(format string, a ...any) {
	fmt.Fprintf(u.Out, "%s %s\n", u.Cyan("[INFO]"), fmt.Sprintf(format, a...))
}

// OKf 打印成功信息。
func (u *UI) OKf(format string, a ...any) {
	fmt.Fprintf(u.Out, "%s %s\n", u.Green("[ OK ]"), fmt.Sprintf(format, a...))
}

// Warnf 打印警告。
func (u *UI) Warnf(format string, a ...any) {
	fmt.Fprintf(u.Err, "%s %s\n", u.Yellow("[WARN]"), fmt.Sprintf(format, a...))
}

// Errorf 打印错误。
func (u *UI) Errorf(format string, a ...any) {
	fmt.Fprintf(u.Err, "%s %s\n", u.Red("[ERR ]"), fmt.Sprintf(format, a...))
}

// Subst 打印子步骤。
func (u *UI) Subst(format string, a ...any) {
	fmt.Fprintf(u.Out, "      %s %s\n", u.Dim("→"), fmt.Sprintf(format, a...))
}

// ProgressBegin 打印 [cur/total] 形式的步骤头。
func (u *UI) ProgressBegin(cur, total int, desc string) {
	fmt.Fprintf(u.Out, "\n%s %s\n", u.Cyan(fmt.Sprintf("[%02d/%02d]", cur, total)), desc)
}

// ProgressOK 打印步骤完成。
func (u *UI) ProgressOK(d time.Duration) {
	fmt.Fprintf(u.Out, "%s\n", u.Green(fmt.Sprintf("   ✓ 完成 (耗时 %ds)", int(d.Seconds()))))
}

// ProgressSkip 打印步骤跳过。
func (u *UI) ProgressSkip() {
	fmt.Fprintf(u.Out, "%s\n", u.Yellow("   ↷ 已安装，跳过"))
}

// ProgressFail 打印步骤失败。
func (u *UI) ProgressFail(code int) {
	fmt.Fprintf(u.Out, "%s\n", u.Red(fmt.Sprintf("   ✗ 失败 (退出码 %d)", code)))
}

// ProgressStep 打印带进度条的步骤（用于卸载）。
func (u *UI) ProgressStep(cur, total int, desc string) {
	fmt.Fprintf(u.Out, "%s %s\n", u.Cyan(progressBar(cur, total, u.TTY)), desc)
}

func progressBar(cur, total int, tty bool) string {
	const width = 20
	if total <= 0 {
		return fmt.Sprintf("[%d/0]", cur)
	}
	pct := cur * 100 / total
	if !tty {
		return fmt.Sprintf("[%d/%d] %3d%%", cur, total, pct)
	}
	filled := cur * width / total
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return fmt.Sprintf("[%d/%d] %3d%% [%s]", cur, total, pct, bar)
}

// HintShellReload 提示用户手动刷新 shell。
func (u *UI) HintShellReload() {
	fmt.Fprintln(u.Out)
	fmt.Fprintln(u.Out, u.Dim("提示: 程序内的环境设置仅影响子进程，请在当前终端执行:"))
	fmt.Fprintln(u.Out, "    source ~/.bashrc  "+u.Dim("# 或者 exec bash / 重开终端"))
}
