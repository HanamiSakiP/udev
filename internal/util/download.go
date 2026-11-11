package util

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// MirrorRawURL 将 raw.githubusercontent.com 地址转换为 jsdelivr 镜像地址。
func MirrorRawURL(rawURL string) string {
	const prefix = "https://raw.githubusercontent.com/"
	if !strings.HasPrefix(rawURL, prefix) {
		return rawURL
	}
	rest := strings.TrimPrefix(rawURL, prefix)
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) != 4 {
		return rawURL
	}
	return fmt.Sprintf("https://fastly.jsdelivr.net/gh/%s/%s@%s/%s", parts[0], parts[1], parts[2], parts[3])
}

var httpClient = &http.Client{Timeout: 10 * time.Minute}

// Download 下载 url 到 out，失败自动重试 3 次。
func Download(ctx context.Context, url, out string) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
		lastErr = downloadOnce(ctx, url, out)
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

func downloadOnce(ctx context.Context, url, out string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "udev")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("下载 %s 失败: HTTP %s", url, resp.Status)
	}
	tmp := out + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, out)
}

// Fetch 下载并返回内容。
func Fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "udev")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("下载 %s 失败: HTTP %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}
