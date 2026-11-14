package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSafeRelPath(t *testing.T) {
	ok := map[string]string{
		"main.go":             "main.go",
		"./internal/cli/a.go": filepath.FromSlash("internal/cli/a.go"),
	}
	for in, want := range ok {
		got, valid := safeRelPath(in)
		if !valid || got != want {
			t.Errorf("safeRelPath(%q) = %q, %v", in, got, valid)
		}
	}
	for _, in := range []string{"", ".", "..", "../evil", "/etc/passwd"} {
		if _, valid := safeRelPath(in); valid {
			t.Errorf("safeRelPath(%q) accepted", in)
		}
	}
}

func TestMatchAsset(t *testing.T) {
	arch := runtime.GOARCH
	osName := runtime.GOOS
	assets := []githubAsset{
		{Name: "checksums.txt"},
		{Name: "udev_" + osName + "_" + arch + ".tar.gz"},
	}
	got, ok := matchAsset(assets)
	if !ok || got.Name != "udev_"+osName+"_"+arch+".tar.gz" {
		t.Fatalf("matchAsset = %+v, %v", got, ok)
	}
}

func TestExtractTarGzAvoidsTraversal(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	write := func(name, body string) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package main")
	write("../evil", "bad")
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if _, err := extractTarGz(buf.Bytes(), dest); err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(dest, "main.go")); err != nil || string(data) != "package main" {
		t.Fatalf("main.go = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "evil")); !os.IsNotExist(err) {
		t.Fatalf("path traversal wrote outside destination: %v", err)
	}
}
