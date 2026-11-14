package storage

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hanamisakip/udev/internal/ui"
)

func prompt(u *ui.UI, label string) (string, error) {
	fmt.Fprint(u.Out, label)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return "", fmt.Errorf("读取输入失败: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func promptSecret(u *ui.UI, label string) (string, error) {
	if !isTerminal(os.Stdin) {
		return prompt(u, label)
	}
	fmt.Fprint(u.Out, label)
	cmd := exec.Command("stty", "-echo")
	cmd.Stdin = os.Stdin
	_ = cmd.Run()
	defer func() {
		c := exec.Command("stty", "echo")
		c.Stdin = os.Stdin
		_ = c.Run()
		fmt.Fprintln(u.Out)
	}()
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return "", fmt.Errorf("读取输入失败: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// generatePassword 生成适合放入密码文件的随机串。
func generatePassword() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func writeSecret(path, value string) error {
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(value), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func readSecret(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
