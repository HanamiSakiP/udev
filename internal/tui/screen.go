package tui

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// runeWidth 估算一个字符在终端中占用的列数。
func runeWidth(r rune) int {
	switch {
	case r == 0 || r < 32 || r == 0x7f:
		return 0
	case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r), unicode.Is(unicode.Cf, r):
		return 0
	case r == 0x2329 || r == 0x232a:
		return 2
	case r >= 0x1100 && (r <= 0x115f ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe10 && r <= 0xfe19) ||
		(r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1f64f) ||
		(r >= 0x1f900 && r <= 0x1f9ff) ||
		(r >= 0x20000 && r <= 0x3fffd)):
		return 2
	}
	return 1
}

// ansiEnd 返回从 i 开始的 ANSI 转义序列结束位置（不含）。
func ansiEnd(s string, i int) int {
	if i >= len(s) || s[i] != 0x1b || i+1 >= len(s) {
		return i + 1
	}
	switch s[i+1] {
	case '[':
		j := i + 2
		for j < len(s) && !(s[j] >= '@' && s[j] <= '~') {
			j++
		}
		if j < len(s) {
			j++
		}
		return j
	case ']':
		j := i + 2
		for j < len(s) {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
			j++
		}
		return j
	default:
		return i + 2
	}
}

// visibleWidth 忽略 ANSI 转义序列后计算显示宽度。
func visibleWidth(s string) int {
	w := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i = ansiEnd(s, i)
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		w += runeWidth(r)
		i += size
	}
	return w
}

// truncate 按显示宽度截断字符串，保留其中的 ANSI 序列。
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	w := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			j := ansiEnd(s, i)
			b.WriteString(s[i:j])
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		rw := runeWidth(r)
		if w+rw > width {
			break
		}
		b.WriteRune(r)
		w += rw
		i += size
	}
	return b.String()
}

// pad 用空格把字符串补齐到指定显示宽度。
func pad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := visibleWidth(s)
	if w > width {
		return truncate(s, width)
	}
	return s + strings.Repeat(" ", width-w)
}

// cleanText 去掉 ANSI 转义并把制表符替换为空格。
func cleanText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i = ansiEnd(s, i)
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		switch r {
		case '\t':
			b.WriteString("    ")
		case '\r':
			// 忽略回车，避免日志行被覆盖。
		case '\n':
			b.WriteByte('\n')
		default:
			if r >= 32 && r != 0x7f {
				b.WriteRune(r)
			}
		}
		i += size
	}
	return b.String()
}

// logBuffer 是并发安全的日志缓冲区，用于接收命令输出。
type logBuffer struct {
	mu      sync.Mutex
	lines   []string
	partial string
	max     int
}

func newLogBuffer(max int) *logBuffer {
	if max <= 0 {
		max = 2000
	}
	return &logBuffer{max: max}
}

func (l *logBuffer) reset() {
	l.mu.Lock()
	l.lines = nil
	l.partial = ""
	l.mu.Unlock()
}

func (l *logBuffer) Write(p []byte) (int, error) {
	text := cleanText(string(p))
	l.mu.Lock()
	defer l.mu.Unlock()

	chunks := strings.Split(text, "\n")
	if len(chunks) == 1 {
		l.partial = capTail(l.partial+chunks[0], 8192)
	} else {
		l.partial = capTail(l.partial+chunks[0], 8192)
		l.lines = append(l.lines, l.partial)
		l.lines = append(l.lines, chunks[1:len(chunks)-1]...)
		l.partial = capTail(chunks[len(chunks)-1], 8192)
	}
	if len(l.lines) > l.max {
		l.lines = append([]string(nil), l.lines[len(l.lines)-l.max:]...)
	}
	return len(p), nil
}

func capTail(s string, max int) string {
	if len(s) > max {
		return s[len(s)-max:]
	}
	return s
}

func (l *logBuffer) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(l.lines)
	if l.partial != "" {
		n++
	}
	return n
}

// tail 返回最后 n 行（包括尚未换行的部分）。
func (l *logBuffer) tail(n int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := append([]string(nil), l.lines...)
	if l.partial != "" {
		out = append(out, l.partial)
	}
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}
