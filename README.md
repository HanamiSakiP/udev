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
udev repos
udev release
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
- 交互式界面 `udev tui` 仅 Go 版提供
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
# 交互式仪表盘
udev tui

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

# 一键批量克隆/更新仓库（优先 jj，其次 git、gh）
udev repos
udev repos --list            # 只查看将处理的仓库
udev repos --check           # 检查哪些仓库有远端更新
udev repos --tool git        # 强制使用 git
udev repos vaultwarden u-ani # 只处理匹配的仓库

# 一键下载 GitHub Release 文件（gh 已登录时优先 gh）
udev release
udev release --list          # 只查看将下载的文件
udev release --dir ~/Downloads iina
udev release someone/repo#*.zip

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

## 交互式界面（TUI）

```bash
udev tui
```

直接在终端中打开仪表盘，集中查看和操作工具链、开发工具、仓库、Release 与配置。界面使用备用屏幕，按 `q` 退出后终端内容会恢复。

| 按键 | 功能 |
| --- | --- |
| `1`-`6` / `Tab` | 切换页面（概览 / 工具 / 开发工具 / 仓库 / Release / 配置） |
| `↑` `↓` / `j` `k` | 移动选择或滚动 |
| `Enter` | 执行当前项，或打开工具操作菜单 |
| `r` | 重新读取配置与状态 |
| `?` | 显示/关闭快捷键帮助 |
| `q` / `Ctrl-C` | 退出 |

页面内快捷键：

```text
工具页      i 安装（已安装时重装）  b 备份  s 恢复  u 卸载  a 安装所有缺失工具
开发工具页  Enter 执行选中分组  a 默认全部（node/go/python）  A 全部（含 cargo）
仓库页      Enter 更新选中  a 全部更新  c 只检查更新
Release 页  Enter 下载选中  a 全部下载  f 切换是否覆盖已有文件
配置页      ↑↓ 滚动查看合并后的配置；编辑请退出后运行 udev config edit
日志页      Esc 返回  ↑↓ 滚动  End 回到最新  x 取消运行中的操作
```

所有安装、备份、恢复、卸载、克隆与下载操作都在后台执行，日志会显示在操作面板中。运行期间可以按 `Esc` 返回列表，或用 `x` 取消支持取消的操作；完成后会自动刷新状态。

> TUI 依赖 Unix 终端与 `stty`，目前支持 Linux/macOS。Windows 或非交互环境请继续使用命令行子命令。

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

## 批量克隆/更新仓库

`udev repos` 会按配置中的清单逐个处理：目录不存在时克隆，已存在时更新，默认克隆到 `~/.udev/git`。执行时会自动检测 `jj`、`git`、`gh` 是否存在，默认优先使用 `jj`；更新时按仓库自身类型选择 `jj git fetch` 或 `git pull --ff-only`。

默认清单：

```text
dani-garcia/vaultwarden
h4ckf0r0day/obscura
localsend/localsend
HanamiSakiP/u-ani
HanamiSakiP/u-comic
HanamiSakiP/u-game
HanamiSakiP/udev
```

在 `~/.udev/config.yaml` 中修改目录、工具与清单，直接在 `list` 下新增仓库即可。已有配置文件缺少 `repos` 段时，运行任意 `udev` 命令会自动把默认仓库清单追加进去：

```yaml
repos:
  dir: ~/.udev/git        # 克隆根目录，默认 ~/.udev/git
  tool: auto              # auto | jj | git | gh，auto 时优先 jj
  protocol: https         # https | ssh
  list:
    - dani-garcia/vaultwarden
    - h4ckf0r0day/obscura
    - localsend/localsend
    - HanamiSakiP/u-ani
    - HanamiSakiP/u-comic
    - HanamiSakiP/u-game
    - HanamiSakiP/udev
    # 追加新仓库只需加一行
    - owner/new-repo
    # 也支持完整地址或自带主机名
    - https://gitlab.com/owner/repo.git
    - gitlab.com/owner/repo
```

常用用法：

```bash
udev repos                     # 一键克隆/更新全部
udev repos --list              # 只列出，不执行
udev repos --check             # 检查哪些仓库有远端更新
udev repos --jj                # 强制 jj 克隆
udev repos --tool git          # 强制 git
udev repos --dir ~/code        # 临时更换目录
udev repos vaultwarden u-ani   # 只处理匹配的仓库
udev repos someone/newrepo     # 清单外的仓库也可临时克隆
```

说明：

- `--list` 只读本地目录状态；`--check` 会访问远端检查是否有更新
- `--check`：git 仓库使用 `git fetch --dry-run`，不改动任何引用；jj 仓库使用 `jj git fetch` 更新远端 bookmark，但不修改工作副本
- 未克隆的仓库在 `--check` 时只报告“未克隆”，不会自动克隆
- `--jj`、`--git`、`--gh` 是 `--tool` 的快捷写法
- `jj` 仓库更新只执行 `jj git fetch`，不会自动改动工作副本
- `git` 仓库使用 `git pull --ff-only`，存在本地分叉时会报错而不会合并
- 环境变量 `UDEV_REPOS_DIR`、`UDEV_REPOS_TOOL` 可覆盖配置

## 一键下载 Release 文件

`udev release` 会按配置下载 GitHub Release 中的文件。默认清单：

```text
iina/iina                       IINA.v*.dmg
gauthierpiarrette/highball      Highball.dmg
shinchiro/mpv-winbuild-cmake    *mpv-x86_64-v3-*.7z
```

工具选择规则：

- 安装并已登录 `gh` 时，优先执行 `gh release download --repo <repo> --pattern <pattern> --dir <dir>`
- 否则使用 GitHub API 查找 Release 资产并直接下载（curl/wget 仅 Bash 版需要，Go 版使用内置 HTTP）
- 可用 `--tool gh` 强制 gh，或用 `--tool api` 强制 API
- 仅下载与 pattern 匹配的文件；同名文件已存在时跳过，`--force` 覆盖

常用用法：

```bash
udev release                       # 下载默认清单到 ~/.udev/git/release
udev release --list                # 只列出将下载的文件
udev release --dir ~/Downloads     # 指定下载目录
udev release iina highball         # 只下载匹配的仓库
udev release someone/repo#*.zip    # 临时下载清单外的仓库
udev release --gh                  # 强制使用 gh
udev release --api                 # 强制使用 GitHub API
udev release --force               # 覆盖已存在的文件
```

默认目录为 `~/.udev/git/release`，也可以在 `~/.udev/config.yaml` 中修改；已有配置文件缺少 `releases` 段时，运行任意 `udev` 命令会自动追加默认清单：

```yaml
releases:
  dir: ~/.udev/git/release   # 下载目录，默认 ~/.udev/git/release
  tool: auto             # auto | gh | api，auto 时 gh 已登录优先 gh
  list:
    - repo: iina/iina
      pattern: IINA.v*.dmg
      # tag: 1.4.0       # 可选，默认最新 Release
    - repo: gauthierpiarrette/highball
      pattern: Highball.dmg
    - repo: shinchiro/mpv-winbuild-cmake
      pattern: "*mpv-x86_64-v3-*.7z"
```

环境变量 `UDEV_RELEASES_DIR`、`UDEV_RELEASES_TOOL` 可覆盖配置；启用镜像（`USE_CN_MIRROR=1`）时，API 下载会尝试使用配置中的 `mirror.github_proxy`。

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
repos:         # 批量克隆/更新仓库的目录、工具与清单
releases:      # 一键下载 Release 的目录、工具与文件清单
```

常用环境变量（优先级高于配置文件）：

- `UDEV_HOME`：配置/存储目录，默认 `~/.udev`
- `UDEV_BACKUP_DIR`：备份目录
- `PYENV_ROOT`、`NVM_DIR`、`GOPATH`、`RUSTUP_HOME`、`CARGO_HOME`
- `FVM_CACHE_PATH`、`FVM_INSTALL_DIR`
- `UDEV_REPOS_DIR`、`UDEV_REPOS_TOOL`
- `UDEV_RELEASES_DIR`、`UDEV_RELEASES_TOOL`
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
