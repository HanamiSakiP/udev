package util

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var (
	tarOnce      sync.Once
	tarWarnOpts  []string
	tarExtraOpts []string
)

func detectTar() {
	tarOnce.Do(func() {
		if exec.Command("tar", "--warning=no-file-changed", "-cf", os.DevNull, os.DevNull).Run() == nil {
			tarWarnOpts = []string{"--warning=no-file-changed"}
		}
		if exec.Command("tar", "--ignore-failed-read", "-cf", os.DevNull, os.DevNull).Run() == nil {
			tarExtraOpts = []string{"--ignore-failed-read"}
		}
	})
}

func tarArgs(excludes []string) []string {
	detectTar()
	args := append([]string{}, tarWarnOpts...)
	args = append(args, tarExtraOpts...)
	for _, e := range excludes {
		args = append(args, "--exclude="+e)
	}
	return args
}

// CreateTarGz 创建 .tar.gz。返回是否容忍了 tar 的警告（rc=1）。
func CreateTarGz(out, base string, entries, excludes []string) (bool, error) {
	args := tarArgs(excludes)
	args = append(args, "-czf", out, "-C", base)
	args = append(args, entries...)
	return runTar(args...)
}

// CreateTar 创建普通 .tar（用于分阶段打包）。
func CreateTar(out, base string, entries, excludes []string) (bool, error) {
	args := tarArgs(excludes)
	args = append(args, "-cf", out, "-C", base)
	args = append(args, entries...)
	return runTar(args...)
}

// ExtractTar 将 tar/tar.gz 解压到 dest。preserve 为 true 时使用 -p。
func ExtractTar(path, dest string, preserve bool) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	flag := "-xf"
	if preserve {
		flag = "-xpf"
	}
	_, err := runTar(flag, path, "-C", dest)
	return err
}

// ValidateTarGz 校验 tar.gz 是否可读。
func ValidateTarGz(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("备份文件不存在: %s", path)
		}
		return err
	}
	_, err := runTar("-tzf", path)
	if err != nil {
		return fmt.Errorf("非有效 tar.gz: %s", path)
	}
	return nil
}

// PackDirStage 将 src 打包并解压到 stageDir/dst，实现带排除规则的目录暂存。
// 返回 src 是否存在。
func PackDirStage(stageTmp, src, dst string, excludes []string, strict bool) (bool, error) {
	if !IsDir(src) {
		return false, nil
	}
	abs, err := filepath.Abs(src)
	if err != nil {
		return false, err
	}
	partial := filepath.Join(stageTmp, "pack.tar")
	defer os.Remove(partial)
	if _, err := CreateTar(partial, abs, []string{"."}, excludes); err != nil {
		if strict {
			return true, fmt.Errorf("打包 %s 失败: %w", src, err)
		}
		return true, nil
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return true, err
	}
	if err := ExtractTar(partial, dst, true); err != nil {
		_ = os.RemoveAll(dst)
		if strict {
			return true, fmt.Errorf("解压 %s 失败: %w", src, err)
		}
		return true, nil
	}
	return true, nil
}

func runTar(args ...string) (bool, error) {
	cmd := exec.Command("tar", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return false, nil
	}
	code := 1
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	// tar 退出码 1 表示“文件在读取过程中发生变化”，可容忍。
	if code == 1 {
		return true, nil
	}
	msg := strings.TrimSpace(stderr.String())
	if msg == "" {
		msg = err.Error()
	}
	return false, fmt.Errorf("%s", msg)
}
