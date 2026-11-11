# udev

一体化开发工具管理器

配置、备份与卸载备份统一放在 `~/.udev`（可用 `UDEV_HOME` 覆盖）。

## 安装

```bash
go install github.com/hanamisakip/udev@latest
```

或在本目录直接构建：

```bash
go build -o udev .
```

确保 `$(go env GOPATH)/bin` 或 `$(go env GOBIN)` 在 `PATH` 中。

## 无需 Go：纯 Bash 版

`udev.sh` 是同一套命令的纯 Bash 实现，只依赖 `bash`、`tar`、`git`，下载需要 `curl` 或 `wget`，适合还没装 Go 的新机器。它复用同一份 `~/.udev/config.yaml` 与备份目录，可与 Go 版交替使用。

```bash
chmod +x udev.sh
./udev.sh status                            # 直接运行
install -m 755 udev.sh ~/.local/bin/udev    # 或安装到 PATH 后直接 udev
```

支持的命令与参数和 Go 版一致（含 Tab 补全）：

```bash
udev install --cn pyenv g nvm
udev backup all
udev tools node go
udev uninstall -y -k
```

一键运行（不下载、不安装，直接执行）：

```bash
# 查看状态
bash <(curl -fsSL https://raw.githubusercontent.com/HanamiSakiP/udev/main/udev.sh) status

# 安装全部工具链（参数与本地运行完全相同）
bash <(curl -fsSL https://raw.githubusercontent.com/HanamiSakiP/udev/main/udev.sh) install --cn all

# 没有 curl 时可用 wget
bash <(wget -qO- https://raw.githubusercontent.com/HanamiSakiP/udev/main/udev.sh) status
```

一键安装到 `PATH`：

```bash
mkdir -p ~/.local/bin && curl -fsSL https://raw.githubusercontent.com/HanamiSakiP/udev/main/udev.sh -o ~/.local/bin/udev && chmod +x ~/.local/bin/udev
```

> 用 `bash <(curl ...)`（进程替换）而不是 `curl ... | bash`，这样 `uninstall` 等需要确认的命令仍能从终端读取输入。

差异说明：

- 需要 bash 4.3+（macOS 自带 3.2，可 `brew install bash` 后用 `bash udev.sh`），以及 GNU tar
- `config show` 直接打印配置文件原文；Go 版输出的是与默认值合并后的结果
- 未安装 `curl` 时会自动回退到 `wget`
- 备份文件、目录布局、shell 配置规则与 Go 版一致，两个版本可以互相恢复

## Tab 补全

生成补全脚本：

```bash
udev completion bash
udev completion zsh
udev completion fish
```

自动写入当前 shell 的配置：

```bash
udev completion install          # 根据 $SHELL 自动判断
udev completion install zsh      # 也可显式指定
```

或手动启用：

```bash
# bash
echo 'source <(udev completion bash)' >> ~/.bashrc

# zsh（需要已加载 compinit）
echo 'source <(udev completion zsh)' >> ~/.zshrc

# fish
udev completion fish > ~/.config/fish/completions/udev.fish
```

启用后输入 `udev` 后按 Tab 可补全命令、工具名、工具分组与参数，例如：

```bash
udev ins<Tab>              # install
udev install py<Tab>       # pyenv
udev install --c<Tab>      # --cn
udev tools p<Tab>          # python
udev config i<Tab>         # init
```

## 首次运行

任意命令都会自动生成 `~/.udev/config.yaml`：

```bash
udev status
udev config path     # 打印配置文件位置
udev config show     # 查看当前生效配置
udev config edit     # 用 $EDITOR 编辑配置
udev config init -f  # 重置为默认配置
```

## 常用命令

```bash
# 版本管理器：pyenv / g / nvm / rust / fvm
udev status
udev install                 # 安装全部（默认 all）
udev install --cn pyenv g nvm
udev install --force rust
udev backup all
udev restore --force nvm
udev list

# 批量安装开发工具
udev tools                   # 默认 node go python
udev tools node go
udev tools all

# 卸载
udev uninstall -y -k         # 跳过确认，保留 GOPATH
udev uninstall -y pyenv nvm
```

`udev install` 会先检查是否已安装，已安装则跳过；随后优先尝试从最新备份恢复，没有备份才在线安装。`--force` 会先清理再重装。

## 自定义工具清单

所有下载/安装项都在 `~/.udev/config.yaml` 的 `dev_tools` 下，直接编辑即可，无需重新编译：

```yaml
dev_tools:
  node:
    nvm_version: lts
    npm:
      - nrm
      - yarn
      - "@playwright/cli@latest"
    commands:                    # 额外命令，失败不中断
      - dsh plugin --profile web add dsh-pet
    cleanup:
      - nvm cache clear
      - npm cache verify

  go:
    g_version: latest            # 为空则不执行 g install
    packages:
      - github.com/junegunn/fzf@latest
      # 当二进制名无法从路径推断时，可写成对象：
      - path: github.com/cli/cli/v2/cmd/gh@latest
        bin: gh
    cleanup:
      - g clean
      - go clean -cache -testcache -modcache

  python:
    pyenv_version: "3.12.3"
    pip_upgrade: true             # 是否执行 python -m pip install --upgrade pip
    packages:
      - uv
    cleanup:
      - pip cache purge

  cargo:
    packages:
      - starship
      - name: yazi-build
        args: ["--force", "--git", "https://github.com/sxyazi/yazi.git"]
    cleanup:
      - rm -rf ~/.cargo/git ~/.cargo/registry
```

跳过规则：

- npm：`npm list -g --depth=0 <包名>` 已存在则跳过
- go：`$GOBIN` 或 `$GOPATH/bin` 中已有对应二进制则跳过
- python：`pyenv versions --bare` 已包含该版本则跳过版本安装
- cargo：`cargo install --list` 已包含该包则跳过
- `commands` 与 `cleanup` 为容错步骤，失败只提示不中断主流程

## 配置文件结构

```yaml
mirror:        # 镜像开关与地址
paths:         # 备份目录、各工具安装路径、GOPATH
shell:         # 要写入环境变量的 shell 文件
managers:      # 各版本管理器的安装脚本地址
dev_tools:     # 开发工具清单
uninstall:     # 卸载行为（是否默认删除 GOPATH）
```

常用环境变量（优先级高于配置文件）：

- `UDEV_HOME`：配置/存储目录，默认 `~/.udev`
- `UDEV_BACKUP_DIR`：备份目录
- `PYENV_ROOT`、`NVM_DIR`、`GOPATH`、`RUSTUP_HOME`、`CARGO_HOME`
- `FVM_CACHE_PATH`、`FVM_INSTALL_DIR`
- `USE_CN_MIRROR=1` 或 `UDEV_USE_CN=1`
- `CN_GITHUB_PROXY`、`CN_NODE_MIRROR`、`CN_GO_MIRROR`、`CN_PYTHON_MIRROR`

## 备份布局

```text
~/.udev/
├── config.yaml
└── backups/
    ├── pyenv/pyenv_backup_20260101120000.tar.gz
    ├── g/g_backup_20260101120000.tar.gz
    ├── nvm/nvm_backup_20260101120000.tar.gz
    ├── rust/rust_backup_20260101120000.tar.gz
    ├── fvm/fvm_backup_20260101120000.tar.gz
    └── uninstall/20260101-120000/.bashrc.bak
```
