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

# 更新 udev 自身（从 GitHub 获取最新代码，同时同步本地源码）
udev update
udev update --check          # 只检查
udev update --source-only    # 只更新 ~/.udev/source

# 一键配置 gh 登录、云存储、restic 本地仓库
udev setup
udev setup gh                # 仅 gh 登录
udev setup cloud onedrive    # 仅创建 rclone 远程
udev setup restic            # 仅初始化本地 restic 仓库
udev setup status            # 查看配置状态
```

`udev install` 会先检查是否已安装，已安装则跳过；随后优先尝试从最新备份恢复，没有备份才在线安装。`--force` 会先清理再重装。

## 自更新与本地源码

`udev update` 通过 GitHub API 获取最新版本：

- 优先使用 GitHub Release 资产（如果存在与当前系统匹配的二进制）
- 否则下载源码后使用本机 Go 工具链构建并替换当前可执行文件
- `--check` 只检查，不修改任何文件
- `--source-only` 只同步源码，不替换二进制
- `--force` 强制重新同步

从默认源构建时，udev 每次启动都会把当前版本源码同步到 `~/.udev/source`，内容是编译进二进制的快照；执行 `udev update` 时会用 GitHub 上的最新源码整体替换该目录。源码目录可以直接查看或修改，版本标记写在 `~/.udev/source/.udev-source-version`。

更新源可在配置文件中修改（方便 fork）：

```yaml
update:
  repo: HanamiSakiP/udev
  branch: main
```

修改源码后发布前，执行一次 `go generate ./...` 重新生成内嵌源码归档（`internal/selfupdate/embedded/source.tar.gz`）。

## 一键配置 gh / 云存储 / restic

`udev setup` 默认依次执行三件事，也可以单独执行某一项：

```bash
udev setup                    # gh + cloud + restic
udev setup gh                 # gh auth login
udev setup cloud pcloud       # 创建 rclone 远程（pcloud/onedrive/dropbox）
udev setup restic             # 初始化 restic 本地仓库
udev setup status
```

常用选项：

```bash
udev setup --name udev                       # 默认存储名
udev setup cloud dropbox --name mybackup     # 远程名 mybackup-dropbox
udev setup restic --repo /data/restic/udev   # 自定义本地仓库
udev setup gh --token-file ~/.udev/secrets/gh-token
udev setup --secrets-dir ~/.udev/secrets
```

默认值全部保存在本地目录，且密码/token 文件权限为 `0600`：

```text
~/.udev/
├── secrets/
│   ├── gh-token                    # 可选：gh token（--token 时保存）
│   └── restic-udev.pass            # restic 密码
├── rclone/
│   └── rclone.conf                 # rclone 配置（云存储 token 在这里）
├── restic/
│   ├── udev/                       # restic 本地仓库
│   └── udev.env                    # source 后可直接使用 restic
└── source/                         # 工具源码
```

远程存储与 restic 仓库的默认名字都来自配置：

```yaml
storage:
  default_name: udev                # 默认名字
  provider: pcloud                  # pcloud | onedrive | dropbox
  remote_name: ""                   # 空则 <name>-<provider>
  restic_repo: ""                   # 空则 ~/.udev/restic/<name>
  secrets_dir: ~/.udev/secrets
  rclone_config: ~/.udev/rclone/rclone.conf
  source_dir: ~/.udev/source
```

说明：

- pCloud/OneDrive/Dropbox 首次授权需要浏览器或设备码，rclone 会给出提示
- pCloud 可选传入 `--user` 和 `--pcloud-password`，密码会保存到本地密码文件
- restic 默认创建在本地目录，使用前可执行 `source ~/.udev/restic/udev.env`
- `udev.sh` 纯 Bash 版暂不包含 `update`、`setup` 与源码同步功能

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
storage:       # 云存储、凭据、restic 仓库与源码目录
update:        # 自更新来源（GitHub repo / branch）
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
├── backups/
│   ├── pyenv/pyenv_backup_20260101120000.tar.gz
│   ├── g/g_backup_20260101120000.tar.gz
│   ├── nvm/nvm_backup_20260101120000.tar.gz
│   ├── rust/rust_backup_20260101120000.tar.gz
│   ├── fvm/fvm_backup_20260101120000.tar.gz
│   └── uninstall/20260101-120000/.bashrc.bak
├── source/                 # 当前版本工具源码（启动时同步）
├── secrets/                # 密码/token 文件（0600）
├── rclone/rclone.conf      # 云存储配置
└── restic/<name>/          # restic 本地仓库
```
