// udev - 一体化开发工具管理器
//
// 安装:
//
//	go install github.com/hanamisakip/udev@latest
package main

import (
	"os"

	"github.com/hanamisakip/udev/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
