// Package tui 提供 udev 的交互式终端界面。
package tui

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// key 表示一次按键。普通字符放在 ch 中，特殊键放在 name 中。
type key struct {
	name string
	ch   rune
}

type terminal struct {
	saved string
	raw   bool
}

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// openTerminal 保存当前终端状态并进入 raw 模式。
func openTerminal() (*terminal, error) {
	if !isTTY(os.Stdin) || !isTTY(os.Stdout) {
		return nil, errors.New("tui 需要交互式终端（stdin/stdout 均为 TTY）")
	}
	saved, err := stty("-g")
	if err != nil {
		return nil, fmt.Errorf("读取终端模式失败（需要 stty）: %w", err)
	}
	if _, err := stty("raw", "-echo"); err != nil {
		return nil, fmt.Errorf("进入终端原始模式失败: %w", err)
	}
	return &terminal{saved: saved, raw: true}, nil
}

func (t *terminal) restore() {
	if t == nil || !t.raw {
		return
	}
	_, _ = stty(t.saved)
	t.raw = false
}

func stty(args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// terminalSize 通过 stty size 获取终端行列数。
func terminalSize() (width, height int) {
	out, err := stty("size")
	if err != nil {
		return 80, 24
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 80, 24
	}
	height, _ = strconv.Atoi(fields[0])
	width, _ = strconv.Atoi(fields[1])
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	return width, height
}

// readKeys 持续读取 stdin，并把字节流解析成按键。
func readKeys() <-chan key {
	keys := make(chan key, 32)
	raw := make(chan byte, 256)

	go func() {
		br := bufio.NewReader(os.Stdin)
		for {
			b, err := br.ReadByte()
			if err != nil {
				close(raw)
				return
			}
			raw <- b
		}
	}()

	go func() {
		defer close(keys)
		for {
			b, ok := <-raw
			if !ok {
				return
			}

			switch {
			case b == 0x1b:
				seq := []byte{b}
				timer := time.NewTimer(35 * time.Millisecond)
			collect:
				for len(seq) < 9 {
					select {
					case nb, ok := <-raw:
						if !ok {
							break collect
						}
						seq = append(seq, nb)
						if len(seq) == 2 && nb != '[' && nb != 'O' {
							break collect
						}
						if len(seq) > 2 && ((nb >= 'A' && nb <= 'Z') || (nb >= 'a' && nb <= 'z') || nb == '~') {
							break collect
						}
					case <-timer.C:
						break collect
					}
				}
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				if k, ok := decodeSeq(seq); ok {
					keys <- k
				}

			case b >= 0x80:
				n := utf8Len(b)
				buf := []byte{b}
				deadline := time.NewTimer(20 * time.Millisecond)
			more:
				for len(buf) < n {
					select {
					case nb, ok := <-raw:
						if !ok {
							break more
						}
						buf = append(buf, nb)
					case <-deadline.C:
						break more
					}
				}
				if !deadline.Stop() {
					select {
					case <-deadline.C:
					default:
					}
				}
				if r, size := utf8.DecodeRune(buf); r != utf8.RuneError && size == len(buf) {
					keys <- key{ch: r}
				}

			default:
				if k, ok := decodeByte(b); ok {
					keys <- k
				}
			}
		}
	}()

	return keys
}

func utf8Len(b byte) int {
	switch {
	case b&0xE0 == 0xC0:
		return 2
	case b&0xF0 == 0xE0:
		return 3
	case b&0xF8 == 0xF0:
		return 4
	default:
		return 1
	}
}

func decodeByte(b byte) (key, bool) {
	switch b {
	case 0x03:
		return key{name: "ctrl-c"}, true
	case 0x0d, 0x0a:
		return key{name: "enter"}, true
	case 0x09:
		return key{name: "tab"}, true
	case 0x7f, 0x08:
		return key{name: "backspace"}, true
	}
	if b >= 0x20 && b <= 0x7e {
		return key{ch: rune(b)}, true
	}
	return key{}, false
}

func decodeSeq(seq []byte) (key, bool) {
	switch string(seq) {
	case "\x1b":
		return key{name: "esc"}, true
	case "\x1b[A":
		return key{name: "up"}, true
	case "\x1b[B":
		return key{name: "down"}, true
	case "\x1b[C":
		return key{name: "right"}, true
	case "\x1b[D":
		return key{name: "left"}, true
	case "\x1b[H", "\x1b[1~", "\x1bOH":
		return key{name: "home"}, true
	case "\x1b[F", "\x1b[4~", "\x1bOF":
		return key{name: "end"}, true
	case "\x1b[5~":
		return key{name: "pgup"}, true
	case "\x1b[6~":
		return key{name: "pgdn"}, true
	case "\x1b[3~":
		return key{name: "delete"}, true
	case "\x1b[Z":
		return key{name: "shift-tab"}, true
	case "\x1bOP":
		return key{name: "f1"}, true
	}
	return key{}, false
}
