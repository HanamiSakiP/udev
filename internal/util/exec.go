package util

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hanamisakip/udev/internal/ui"
)

// ExitCode 提取命令退出码。
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 1
}

// Output 执行命令并返回合并输出。
func Output(name string, args ...string) (string, error) {
	return OutputEnv(nil, "", name, args...)
}

// OutputEnv 以指定环境与工作目录执行命令并返回合并输出。
func OutputEnv(env []string, dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if len(env) > 0 {
		cmd.Env = env
	}
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// OutputShell 执行 shell 脚本并返回合并输出。
func OutputShell(script string, env []string) (string, error) {
	cmd := exec.Command("bash", "-c", "set -o pipefail\n"+script)
	if len(env) > 0 {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// RunStreaming 执行 shell 脚本，输出直接透传到终端。
func RunStreaming(ctx context.Context, script string, env []string) error {
	cmd := exec.CommandContext(ctx, "bash", "-c", "set -o pipefail\n"+script)
	if len(env) > 0 {
		cmd.Env = env
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunScript 执行下载到本地的安装脚本，并显示旋转进度条。
func RunScript(u *ui.UI, desc, interpreter, script string, args []string, env []string) error {
	cmdArgs := append([]string{script}, args...)
	cmd := exec.Command(interpreter, cmdArgs...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	return runWithSpinner(u, desc, cmd)
}

// RunCmd 执行命令并显示旋转进度条。
func RunCmd(u *ui.UI, desc string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	return runWithSpinner(u, desc, cmd)
}

func runWithSpinner(u *ui.UI, desc string, cmd *exec.Cmd) error {
	logFile, err := os.CreateTemp("", "udev-cmd-*.log")
	if err != nil {
		return err
	}
	logName := logFile.Name()
	defer os.Remove(logName)

	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return err
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	if u.TTY {
		wg.Add(1)
		go func() {
			defer wg.Done()
			frames := []string{"/", "-", "\\", "|"}
			ticker := time.NewTicker(120 * time.Millisecond)
			defer ticker.Stop()
			i := 0
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					fmt.Fprintf(u.Out, "\r  [%s] %s  ", frames[i%len(frames)], desc)
					i++
				}
			}
		}()
	}

	waitErr := cmd.Wait()
	close(done)
	wg.Wait()
	logFile.Close()

	if u.TTY {
		if waitErr == nil {
			fmt.Fprintf(u.Out, "\r  %s %-50s\n", u.Green("[✓]"), desc)
		} else {
			fmt.Fprintf(u.Out, "\r  %s %-50s\n", u.Red("[✗]"), desc)
		}
	}

	if waitErr != nil {
		u.Warnf("命令输出（末尾）:")
		printLogTail(logName, 30)
	}
	return waitErr
}

func printLogTail(path string, n int) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	for _, line := range lines {
		fmt.Fprintf(os.Stderr, "      | %s\n", line)
	}
}

// SetEnv 返回一份设置了指定键值的新环境变量列表，已存在的同名键会被替换。
func SetEnv(base []string, kv ...string) []string {
	keys := map[string]bool{}
	for _, pair := range kv {
		if i := strings.IndexByte(pair, '='); i > 0 {
			keys[pair[:i]] = true
		}
	}
	out := make([]string, 0, len(base)+len(kv))
	for _, pair := range base {
		name := pair
		if i := strings.IndexByte(pair, '='); i >= 0 {
			name = pair[:i]
		}
		if !keys[name] {
			out = append(out, pair)
		}
	}
	return append(out, kv...)
}

// GitMirrorEnv 返回镜像所需的 git 环境变量。
func GitMirrorEnv(proxy string) []string {
	return SetEnv(os.Environ(),
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=url."+proxy+"https://github.com/.insteadOf",
		"GIT_CONFIG_VALUE_0=https://github.com/",
	)
}

// GrepLines 从文件列表中筛选匹配正则的行，用于备份环境变量片段。
func GrepLines(pattern string, files []string) string {
	var buf bytes.Buffer
	re, err := regexp.Compile(pattern)
	if err != nil {
		return ""
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if re.MatchString(line) {
				buf.WriteString(line)
				buf.WriteByte('\n')
			}
		}
	}
	return buf.String()
}
