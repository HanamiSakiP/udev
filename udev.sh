#!/usr/bin/env bash
# udev - 一体化开发工具管理器（纯 Bash 版，无需安装 Go）
#
# 与 Go 版共享同一份配置（~/.udev/config.yaml）、命令与备份布局，
# 只依赖 bash / tar / 常见 Unix 工具；下载需要 curl 或 wget。
set -uo pipefail

VERSION="1.0.0"

if ((BASH_VERSINFO[0] < 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] < 3))); then
  echo "udev 需要 bash 4.3 或更高版本（当前: ${BASH_VERSION:-unknown}）" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# 输出
# ---------------------------------------------------------------------------

if [[ -t 1 && -z ${NO_COLOR-} && ${TERM-} != dumb ]]; then
  C_RED=$'\033[0;31m' C_GREEN=$'\033[0;32m' C_YELLOW=$'\033[0;33m'
  C_BLUE=$'\033[0;34m' C_CYAN=$'\033[0;36m' C_BOLD=$'\033[1m' C_DIM=$'\033[2m'
  C_RESET=$'\033[0m' COLOR=1
else
  C_RED='' C_GREEN='' C_YELLOW='' C_BLUE='' C_CYAN='' C_BOLD='' C_DIM='' C_RESET='' COLOR=0
fi

_paint() {
  if ((COLOR)); then printf '%s%s%s' "$1" "$2" "$C_RESET"; else printf '%s' "$2"; fi
}
bold()   { _paint "$C_BOLD" "$*"; }
dim()    { _paint "$C_DIM" "$*"; }
red()    { _paint "$C_RED" "$*"; }
green()  { _paint "$C_GREEN" "$*"; }
yellow() { _paint "$C_YELLOW" "$*"; }
blue()   { _paint "$C_BLUE" "$*"; }
cyan()   { _paint "$C_CYAN" "$*"; }

header() { printf '\n%s\n' "$(bold "=== $* ===")"; }
info()   { printf '%s %s\n' "$(cyan '[INFO]')" "$*"; }
ok()     { printf '%s %s\n' "$(green '[ OK ]')" "$*"; }
warn()   { printf '%s %s\n' "$(yellow '[WARN]')" "$*" >&2; }
err()    { printf '%s %s\n' "$(red '[ERR ]')" "$*" >&2; }
subst()  { printf '      %s %s\n' "$(dim '→')" "$*"; }

hint_shell_reload() {
  printf '\n%s\n' "$(dim '提示: 程序内的环境设置仅影响子进程，请在当前终端执行:')"
  printf '    source ~/.bashrc  %s\n' "$(dim '# 或者 exec bash / 重开终端')"
}

# 安全转义为 POSIX shell 参数
_q() { printf "'%s'" "${1//\'/\'\\\'\'}"; }

# ---------------------------------------------------------------------------
# 配置：默认模板 + YAML 子集解析
# ---------------------------------------------------------------------------

declare -gA _CFG=() _DEF=() _SEQ=() _DSEQ=()

# 解析我们使用的 YAML 子集：嵌套映射、标量列表、{key: value} 对象列表、行内列表。
# 输出 `key<TAB>value`，其中列表项写成 key[0]、key[1]；序列长度写成 __seq.key<TAB>n。
YAML_AWK=$(cat <<'AWK'
function ltrim(s) { sub(/^[ \t]+/, "", s); return s }
function rtrim(s) { sub(/[ \t]+$/, "", s); return s }
function trim(s) { return rtrim(ltrim(s)) }
function unq(s,  n, q) {
  q = sprintf("%c", 39)
  s = trim(s)
  n = length(s)
  if (n >= 2 && ((substr(s,1,1) == "\"" && substr(s,n,1) == "\"") || (substr(s,1,1) == q && substr(s,n,1) == q)))
    return substr(s, 2, n - 2)
  return s
}
function strip_comment(v,  i, q, n) {
  v = rtrim(v)
  if (v == "") return v
  q = substr(v, 1, 1)
  if (q == "\"" || q == sprintf("%c", 39)) {
    n = length(v)
    for (i = 2; i <= n; i++)
      if (substr(v, i, 1) == q && substr(v, i - 1, 1) != "\\") break
    if (i <= n) return substr(v, 1, i)
    return v
  }
  i = index(v, " #")
  if (i > 0) v = substr(v, 1, i - 1)
  return rtrim(v)
}
function leading(s,  i, c) {
  for (i = 1; i <= length(s); i++) {
    c = substr(s, i, 1)
    if (c != " " && c != "\t") return i - 1
  }
  return length(s)
}
function emit(k, v,  i, n, parts, inner) {
  v = strip_comment(trim(v))
  if (substr(v, 1, 1) == "[" && substr(v, length(v), 1) == "]") {
    inner = substr(v, 2, length(v) - 2)
    if (trim(inner) == "") { cnt[k] = 0; return }
    n = split(inner, parts, ",")
    cnt[k] = n
    for (i = 1; i <= n; i++) print k "[" (i - 1) "]\t" unq(parts[i])
    return
  }
  print k "\t" unq(v)
}
{
  line = $0
  sub(/\r$/, "", line)
  t = trim(line)
  if (t == "" || substr(t, 1, 1) == "#") next
  ind = leading(line)
  body = substr(line, ind + 1)
  isitem = (substr(body, 1, 2) == "- ")
  if (isitem) {
    # 序列项：允许与所属 key 同缩进（紧凑写法），但同级的上一个项要弹出
    while (sp > 0 && sind[sp] > ind) sp--
    if (sp > 0 && sind[sp] == ind && stype[sp] == "item") sp--
  } else {
    while (sp > 0 && sind[sp] >= ind) sp--
  }
  parent = (sp > 0) ? skey[sp] : ""
  if (isitem) {
    rest = trim(substr(body, 3))
    n = cnt[parent] + 0
    cnt[parent] = n + 1
    item = (parent == "") ? ("[" n "]") : (parent "[" n "]")
    if (rest == "") { sp++; sind[sp] = ind; skey[sp] = item; stype[sp] = "item"; next }
    p = index(rest, ":")
    if (p > 0 && substr(rest, 1, p - 1) !~ /[ \t]/ && (p == length(rest) || substr(rest, p + 1, 1) == " ")) {
      k = trim(substr(rest, 1, p - 1))
      v = trim(substr(rest, p + 1))
      sp++; sind[sp] = ind; skey[sp] = item; stype[sp] = "item"
      full = item "." k
      if (v == "") { sp++; sind[sp] = ind + 1; skey[sp] = full; stype[sp] = "key" } else emit(full, v)
      next
    }
    emit(item, rest)
    next
  }
  p = index(body, ": ")
  if (p > 0) {
    k = trim(substr(body, 1, p - 1))
    v = trim(substr(body, p + 2))
  } else if (substr(body, length(body), 1) == ":") {
    k = trim(substr(body, 1, length(body) - 1))
    v = ""
  } else {
    next
  }
  full = (parent == "") ? k : (parent "." k)
  if (v == "") { sp++; sind[sp] = ind; skey[sp] = full; stype[sp] = "key"; next }
  emit(full, v)
}
END { for (k in cnt) print "__seq." k "\t" cnt[k] }
AWK
)

load_yaml() { # file cfg_array seq_array
  local file=$1
  local -n _cfg_ref=$2 _seq_ref=$3
  local key val
  while IFS=$'\t' read -r key val; do
    [[ -z $key ]] && continue
    if [[ $key == __seq.* ]]; then
      _seq_ref["${key#__seq.}"]="$val"
    else
      _cfg_ref["$key"]="$val"
    fi
  done < <(awk "$YAML_AWK" "$file")
}

default_config() {
  cat <<EOF
# udev 配置文件
# 配置/存储目录: $HOME_DIR
# 修改后下次运行 udev 时生效。

mirror:
  use_cn: false
  github_proxy: https://ghfast.top/
  node: https://npmmirror.com/mirrors/node
  go: https://golang.google.cn/dl/
  python: https://registry.npmmirror.com/-/binary/python
paths:
  backup_dir: $HOME_DIR/backups
  pyenv_root: ~/.pyenv
  go_root: ~/.g
  gopath: ~/go
  nvm_dir: ~/.nvm
  rustup_home: ~/.rustup
  cargo_home: ~/.cargo
  fvm_cache: ~/fvm
  fvm_install: ~/fvm
shell:
  files:
    - ~/.bashrc
    - ~/.profile
    - ~/.zshrc
managers:
  pyenv:
    install_url: https://pyenv.run
  g:
    install_url: https://raw.githubusercontent.com/voidint/g/master/install.sh
  nvm:
    install_url: https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.2/install.sh
  rust:
    install_url: https://sh.rustup.rs
    default_toolchain: stable
    no_modify_path: false
  fvm:
    install_url: https://fvm.app/install.sh
dev_tools:
  node:
    nvm_version: lts
    npm:
      - nrm
      - yarn
      - bun
      - pnpm
      - fanyi
      - "@officecli/officecli"
      - "@fresh-editor/fresh-editor"
      - "@dbx-app/cli"
      - "@playwright/cli@latest"
      - "@jackwener/opencli"
      - microsandbox
      - cc-connect
      - "@bitwarden/cli"
      - "@earendil-works/pi-coding-agent"
      - "@anthropic-ai/claude-code"
      - "@deepseek-ai/dsh"
      - "@deepseek-harness-tui/dsh-tui"
      - rea-agents
      - "@opencode/cli"
      - "@openai/codex"
    commands:
      - dsh plugin --profile web add -w @xmanrui/dsh-im
      - dsh plugin --profile web add dsh-pet
    cleanup:
      - nvm cache clear
      - npm cache verify
  go:
    g_version: latest
    packages:
      - path: github.com/restic/restic/cmd/restic@latest
        bin: restic
      - path: github.com/rclone/rclone@latest
        bin: rclone
      - path: github.com/junegunn/fzf@latest
        bin: fzf
      - path: github.com/jesseduffield/lazygit@latest
        bin: lazygit
      - path: github.com/charmbracelet/glow@latest
        bin: glow
      - path: github.com/hybridgroup/yzma@latest
        bin: yzma
      - path: github.com/charmbracelet/crush@latest
        bin: crush
      - path: github.com/google/go-containerregistry/cmd/crane@latest
        bin: crane
      - path: github.com/regclient/regclient/cmd/regctl@latest
        bin: regctl
      - path: github.com/cli/cli/v2/cmd/gh@latest
        bin: gh
      - path: gitlab.com/gitlab-org/cli/cmd/glab@latest
        bin: glab
    cleanup:
      - g clean
      - go clean -cache -testcache -modcache
  python:
    pyenv_version: 3.12.3
    pip_upgrade: true
    packages:
      - code2prompt-rs
      - uv
      - cli-anything-hub
    commands: []
    cleanup:
      - pip cache purge
  cargo:
    packages:
      - name: starship
        args: [--locked]
      - name: navi
        args: [--locked]
      - name: zellij
        args: [--locked]
      - name: zoxide
        args: [--locked]
      - name: caligula
        args: []
      - name: bluetui
        args: []
      - name: ouch
        args: []
      - name: bookokrat
        args: []
      - name: abtop
        args: []
      - name: fd-find
        args: []
      - name: jj-cli
        args: []
      - name: lsd
        args: []
      - name: binsider
        args: []
      - name: microsandbox
        args: []
      - name: lychee
        args: []
      - name: code2prompt
        args: []
      - name: yazi-build
        args: [--force, --git, https://github.com/sxyazi/yazi.git]
      - name: openfang-cli
        args: [--force, --git, https://github.com/RightNow-AI/openfang]
    cleanup:
      - rm -rf ~/.cargo/git ~/.cargo/registry
uninstall:
  include_gopath: true
EOF
}

# 读取配置项，缺失时回落到内置默认值
cfg_key() {
  local k=$1
  if [[ -n ${_CFG[$k]+x} ]]; then printf '%s' "${_CFG[$k]}"; else printf '%s' "${_DEF[$k]-}"; fi
}

cfg_bool() {
  local v
  v=$(cfg_key "$1")
  case ${v,,} in
    1|true|yes|on|y) return 0 ;;
    *) return 1 ;;
  esac
}

# 列表长度（用户配置优先，缺失时使用默认清单）
seq_len() {
  local k=$1
  if [[ -n ${_SEQ[$k]+x} ]]; then printf '%s' "${_SEQ[$k]}"; else printf '%s' "${_DSEQ[$k]-0}"; fi
}

# 只读取用户配置（不回落到内置默认值），用于对象列表的字符串简写
cfg_user() {
  local k=$1
  if [[ -n ${_CFG[$k]+x} ]]; then printf '%s' "${_CFG[$k]}"; fi
}

# 输出列表的每一项
list_vals() {
  local k=$1 n i
  n=$(seq_len "$k")
  for ((i = 0; i < n; i++)); do cfg_key "$k[$i]"; printf '\n'; done
}

expand_path() {
  local p=$1 name
  if [[ $p == "~" ]]; then
    p=$HOME
  elif [[ $p == "~/"* ]]; then
    p="$HOME/${p#\~/}"
  fi
  while [[ $p =~ ^(.*)\$\{([A-Za-z_][A-Za-z0-9_]*)\}(.*)$ ]]; do
    name=${BASH_REMATCH[2]}
    p="${BASH_REMATCH[1]}${!name-}${BASH_REMATCH[3]}"
  done
  while [[ $p =~ ^(.*)\$([A-Za-z_][A-Za-z0-9_]*)(.*)$ ]]; do
    name=${BASH_REMATCH[2]}
    p="${BASH_REMATCH[1]}${!name-}${BASH_REMATCH[3]}"
  done
  case $p in
    /*) ;;
    *) p="$PWD/$p" ;;
  esac
  printf '%s' "$p"
}

resolve_home() {
  HOME_DIR=$(expand_path "${UDEV_HOME:-$HOME/.udev}")
  CONFIG_FILE="$HOME_DIR/config.yaml"
}

# 环境变量覆盖（优先级高于配置文件）
apply_env() {
  _env_pick() { # key names...
    local k=$1 n
    shift
    for n in "$@"; do
      if [[ -n ${!n-} ]]; then _CFG[$k]=${!n}; return 0; fi
    done
  }
  _env_pick paths.backup_dir UDEV_BACKUP_DIR
  _env_pick paths.pyenv_root PYENV_ROOT
  _env_pick paths.nvm_dir NVM_DIR
  _env_pick paths.gopath GOPATH
  _env_pick paths.rustup_home RUSTUP_HOME
  _env_pick paths.cargo_home CARGO_HOME
  _env_pick paths.fvm_cache FVM_CACHE_PATH FVM_HOME
  _env_pick paths.fvm_install FVM_INSTALL_DIR
  _env_pick mirror.github_proxy CN_GITHUB_PROXY
  _env_pick mirror.node CN_NODE_MIRROR
  _env_pick mirror.go CN_GO_MIRROR
  _env_pick mirror.python CN_PYTHON_MIRROR
  if [[ -n ${UDEV_USE_CN-} ]]; then
    _CFG[mirror.use_cn]=$UDEV_USE_CN
  elif [[ -n ${USE_CN_MIRROR-} ]]; then
    _CFG[mirror.use_cn]=$USE_CN_MIRROR
  fi
  unset -f _env_pick
}

init_paths() {
  BACKUP_DIR=$(expand_path "$(cfg_key paths.backup_dir)")
  PYENV_ROOT=$(expand_path "$(cfg_key paths.pyenv_root)")
  GO_ROOT=$(expand_path "$(cfg_key paths.go_root)")
  GO_PATH=$(expand_path "$(cfg_key paths.gopath)")
  NVM_DIR=$(expand_path "$(cfg_key paths.nvm_dir)")
  RUSTUP_HOME=$(expand_path "$(cfg_key paths.rustup_home)")
  CARGO_HOME=$(expand_path "$(cfg_key paths.cargo_home)")
  FVM_CACHE=$(expand_path "$(cfg_key paths.fvm_cache)")
  FVM_INSTALL=$(expand_path "$(cfg_key paths.fvm_install)")

  SHELL_FILES=()
  local n i
  n=$(seq_len shell.files)
  for ((i = 0; i < n; i++)); do
    SHELL_FILES+=("$(expand_path "$(cfg_key "shell.files[$i]")")")
  done

  PYENV_BIN="$PYENV_ROOT/bin/pyenv"
  G_BIN="$GO_ROOT/bin/g"
  GO_SDK_BIN="$GO_ROOT/go/bin"
  GO_BIN_DIR="$GO_PATH/bin"
  CARGO_BIN_DIR="$CARGO_HOME/bin"
  NVM_SCRIPT="$NVM_DIR/nvm.sh"
  RUSTUP_BIN="$CARGO_HOME/bin/rustup"
  FVM_BIN="$FVM_INSTALL/bin/fvm"

  USE_CN=$(cfg_key mirror.use_cn)
}

load_config() {
  resolve_home
  local dtmp
  dtmp=$(mktemp "${TMPDIR:-/tmp}/udev-default-XXXXXX") || return 1
  default_config >"$dtmp"
  load_yaml "$dtmp" _DEF _DSEQ
  rm -f "$dtmp"
  [[ -f $CONFIG_FILE ]] && load_yaml "$CONFIG_FILE" _CFG _SEQ
  apply_env
  init_paths
}

ensure_config() {
  mkdir -p "$HOME_DIR" || { err "无法创建配置目录: $HOME_DIR"; return 1; }
  mkdir -p "$BACKUP_DIR" || { err "无法创建备份目录: $BACKUP_DIR"; return 1; }
  if [[ ! -f $CONFIG_FILE ]]; then
    default_config >"$CONFIG_FILE" || return 1
    info "已生成默认配置: $CONFIG_FILE"
  fi
}

is_cn() {
  case ${USE_CN,,} in
    1|true|yes|on|y) return 0 ;;
    *) return 1 ;;
  esac
}

# ---------------------------------------------------------------------------
# 通用工具
# ---------------------------------------------------------------------------

is_dir() { [[ -d $1 ]]; }
is_exec() { [[ -f $1 && -x $1 ]]; }
command_exists() {
  if [[ $1 == */* ]]; then is_exec "$1"; else command -v "$1" >/dev/null 2>&1; fi
}

mirror_raw_url() {
  local u=$1 rest a b c d
  [[ $u == https://raw.githubusercontent.com/* ]] || { printf '%s' "$u"; return; }
  rest=${u#https://raw.githubusercontent.com/}
  a=${rest%%/*}; rest=${rest#*/}
  b=${rest%%/*}; rest=${rest#*/}
  c=${rest%%/*}; d=${rest#*/}
  if [[ -z $a || -z $b || -z $c || -z $d || $d == "$rest" ]]; then
    printf '%s' "$u"
    return
  fi
  printf 'https://fastly.jsdelivr.net/gh/%s/%s@%s/%s' "$a" "$b" "$c" "$d"
}

mirror_url() {
  local u=$1
  if is_cn; then mirror_raw_url "$u"; else printf '%s' "$u"; fi
}

download() { # url out
  local url=$1 out=$2 tmp="$2.part" i
  if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
    err "未找到 curl 或 wget，无法下载"
    return 1
  fi
  for i in 1 2 3; do
    ((i > 1)) && sleep 2
    rm -f "$tmp"
    if command -v curl >/dev/null 2>&1; then
      if curl -fsSL --retry 1 -o "$tmp" "$url" && [[ -f $tmp ]]; then
        mv -f "$tmp" "$out"
        return 0
      fi
    elif wget -q -O "$tmp" "$url" && [[ -f $tmp ]]; then
      mv -f "$tmp" "$out"
      return 0
    fi
  done
  rm -f "$tmp"
  return 1
}

SCRIPT_PATH=""
download_script() { # url -> 设置 SCRIPT_PATH
  local url f
  url=$(mirror_url "$1")
  subst "下载: $url"
  f=$(mktemp "${TMPDIR:-/tmp}/udev-install-XXXXXX.sh") || return 1
  if ! download "$url" "$f"; then
    rm -f "$f"
    return 1
  fi
  chmod 700 "$f"
  SCRIPT_PATH=$f
}

run_script() { # interpreter script args...
  local interp=$1 script=$2
  shift 2
  local -a envargs=()
  if is_cn; then
    envargs=(
      GIT_CONFIG_COUNT=1
      "GIT_CONFIG_KEY_0=url.$(cfg_key mirror.github_proxy)https://github.com/.insteadOf"
      GIT_CONFIG_VALUE_0=https://github.com/
    )
  fi
  env "${envargs[@]}" "$interp" "$script" "$@"
}

TAR_OPTS=()
TAR_DETECTED=0
detect_tar() {
  ((TAR_DETECTED)) && return
  TAR_DETECTED=1
  if tar --warning=no-file-changed -cf /dev/null /dev/null >/dev/null 2>&1; then
    TAR_OPTS+=(--warning=no-file-changed)
  fi
  if tar --ignore-failed-read -cf /dev/null /dev/null >/dev/null 2>&1; then
    TAR_OPTS+=(--ignore-failed-read)
  fi
}

tar_create() { # out base excludes_string entries...
  local out=$1 base=$2 excl=$3
  shift 3
  detect_tar
  local -a args=("${TAR_OPTS[@]}")
  local e
  while IFS= read -r e; do
    [[ -n $e ]] && args+=(--exclude="$e")
  done <<<"$excl"
  local rc
  tar "${args[@]}" -cf "$out" -C "$base" "$@" 2>/dev/null
  rc=$?
  # tar 退出码 1 表示“文件在读取过程中发生变化”，可容忍
  ((rc <= 1))
}

tar_create_gz() {
  local out=$1 base=$2 excl=$3
  shift 3
  detect_tar
  local -a args=("${TAR_OPTS[@]}")
  local e
  while IFS= read -r e; do
    [[ -n $e ]] && args+=(--exclude="$e")
  done <<<"$excl"
  local rc
  tar "${args[@]}" -czf "$out" -C "$base" "$@" 2>/dev/null
  rc=$?
  ((rc <= 1))
}

tar_extract() { tar -xpf "$1" -C "$2" 2>/dev/null; }
tar_validate() { [[ -f $1 ]] && tar -tzf "$1" >/dev/null 2>&1; }

BASE_EXCLUDES=(
  ".git" ".github" ".gitignore"
  ".gitmodules" ".gitattributes" ".cache"
  "test" "tests" "docs" "man"
  "*.md" "LICENSE" "LICENSE.*"
  "Makefile" "Dockerfile"
  "*.png" "*.gif" "*.jpg" "*.jpeg"
  "*.svg" "*.ico"
  "*.pyc" "__pycache__" "*.pyo" "*.log"
  "tmp" "temp"
  "rename_test.sh" "update_test_mocks.sh"
  "install.sh" "package.json" "package-lock.json"
)

base_excludes() { printf '%s\n' "${BASE_EXCLUDES[@]}"; }

excludes_without() { # item...
  local drop=" $* "
  local e
  for e in "${BASE_EXCLUDES[@]}"; do
    [[ $drop == *" $e "* ]] && continue
    printf '%s\n' "$e"
  done
}

copy_tree() { # src dst（合并复制，保留权限/时间/软链接）
  mkdir -p "$2" || return 1
  if cp -af "$1/." "$2/" 2>/dev/null; then return 0; fi
  # 只读缓存（如 Go module cache）无法直接覆盖时，先恢复可写再重试。
  find "$2" -xdev ! -writable -exec chmod u+w {} + 2>/dev/null
  cp -af "$1/." "$2/" 2>/dev/null
}

copy_path() { # src dst（先删除再复制）
  local src=$1 dst=$2
  rm -rf "$dst" || return 1
  mkdir -p "$(dirname "$dst")" || return 1
  cp -a "$src" "$dst" 2>/dev/null
}

human_size() {
  local f=$1 size
  size=$(stat -c %s "$f" 2>/dev/null) || { printf '?'; return; }
  awk -v s="$size" 'BEGIN{
    split("B K M G T", u, " "); i = 1
    while (s >= 1024 && i < 5) { s /= 1024; i++ }
    if (i == 1) printf "%.0f%s", s, u[i]; else printf "%.1f%s", s, u[i]
  }'
}

diskspace_warning() {
  local kb
  kb=$(df -Pk "$BACKUP_DIR" 2>/dev/null | awk 'NR==2 {print $4}')
  [[ -n ${kb:-} ]] || return 0
  if ((kb > 0 && kb < 204800)); then
    warn "备份目录剩余空间较小（${kb}KB），备份可能失败"
  fi
}

# 备份文件路径与查找
backup_path() {
  local tool=$1 dir
  dir="$BACKUP_DIR/$tool"
  mkdir -p "$dir" || return 1
  printf '%s/%s_backup_%s.tar.gz' "$dir" "$tool" "$(date +%Y%m%d%H%M%S)"
}

find_backups() {
  local tool=$1 dir f
  local -a dirs=(
    "$BACKUP_DIR/$tool" "$BACKUP_DIR"
    "$HOME_DIR/backups/$tool" "$HOME_DIR/backups"
    "$HOME_DIR/backup/$tool" "$HOME_DIR/backup"
  )
  local -A seen=()
  local -a found=()
  for dir in "${dirs[@]}"; do
    [[ -n $dir ]] || continue
    for f in "$dir/${tool}_backup_"*.tar.gz; do
      [[ -e $f ]] || continue
      [[ -n ${seen[$f]+x} ]] && continue
      seen[$f]=1
      found+=("$f")
    done
  done
  ((${#found[@]})) || return 0
  printf '%s\n' "${found[@]}" | sort
}

latest_backup() {
  local -a files=()
  local f latest=""
  mapfile -t files < <(find_backups "$1")
  for f in "${files[@]}"; do
    [[ -z $latest || ${f##*/} > ${latest##*/} ]] && latest=$f
  done
  [[ -n $latest ]] && printf '%s' "$latest"
}

count_backups() {
  local -a files=()
  mapfile -t files < <(find_backups "$1")
  printf '%s' "${#files[@]}"
}

# ---------------------------------------------------------------------------
# shell 配置写入 / 清理
# ---------------------------------------------------------------------------

_should_create() {
  case $(basename "$1") in
    .bashrc | .profile | .bash_profile) return 0 ;;
    *) return 1 ;;
  esac
}

_prepare_rc_file() { # file -> 0 可写，1 跳过
  local f=$1
  if [[ -e $f ]]; then return 0; fi
  _should_create "$f" || return 1
  mkdir -p "$(dirname "$f")" || return 1
  : >"$f" || return 1
  return 0
}

add_line_to() { # file line
  local f=$1 line=$2
  _prepare_rc_file "$f" || return 0
  grep -Fxq -- "$line" "$f" 2>/dev/null && return 0
  printf '%s\n' "$line" >>"$f" || return 1
  subst "写入 $f: $line"
}

add_block_to() { # file marker block
  local f=$1 marker=$2 block=$3
  _prepare_rc_file "$f" || return 0
  grep -Fq -- "$marker" "$f" 2>/dev/null && return 0
  printf '\n%s\n' "$block" >>"$f" || return 1
  subst "写入 $f: $marker"
}

add_shell_line() {
  local f
  for f in "${SHELL_FILES[@]}"; do
    add_line_to "$f" "$1" || warn "写入 $f 失败"
  done
}

add_shell_block() { # marker block
  local f
  for f in "${SHELL_FILES[@]}"; do
    add_block_to "$f" "$1" "$2" || warn "写入 $f 失败"
  done
}

# 删除匹配行（正则），并处理 g 的多行配置块
REMOVE_AWK=$(cat <<'AWK'
BEGIN {
  nb = split(ENVIRON["UDEV_BLOCK_END"], be, "\n")
  np = split(ENVIRON["UDEV_PATTERNS"], pats, "\n")
  bs = ENVIRON["UDEV_BLOCK_START"]
}
{
  line = $0
  if (inblock) {
    for (i = 1; i <= nb; i++) if (be[i] != "" && index(line, be[i]) > 0) { inblock = 0; next }
    t = line
    gsub(/^[ \t]+|[ \t]+$/, "", t)
    if (t == "" || substr(t,1,1) == "#" || substr(t,1,3) == "if " || t == "fi" ||
        substr(t,1,8) == "unalias " || substr(t,1,1) == "[" || index(line, ".g/go") > 0) next
    inblock = 0
    print line
    next
  }
  if (bs != "" && index(line, bs) == 1) { inblock = 1; next }
  for (i = 1; i <= np; i++) if (pats[i] != "" && line ~ pats[i]) next
  print line
}
AWK
)

remove_lines_in_file() { # file block_start block_end patterns... -> "before after"
  local f=$1 bs=$2 be=$3
  shift 3
  if [[ ! -f $f ]]; then printf '0 0'; return 0; fi
  local before after tmp mode rc
  before=$(wc -l <"$f")
  mode=$(stat -c '%a' "$f" 2>/dev/null || printf '644')
  tmp=$(mktemp "${TMPDIR:-/tmp}/udev-rc-XXXXXX") || return 1
  UDEV_PATTERNS=$(printf '%s\n' "$@") UDEV_BLOCK_START="$bs" UDEV_BLOCK_END="$be" \
    awk "$REMOVE_AWK" "$f" >"$tmp"
  rc=$?
  if ((rc != 0)); then
    rm -f "$tmp"
    return 1
  fi
  mv -f "$tmp" "$f" || return 1
  chmod "$mode" "$f" 2>/dev/null || true
  after=$(wc -l <"$f")
  printf '%s %s' "$before" "$after"
}

remove_shell_lines() { # block_start block_end patterns...
  local bs=$1 be=$2
  shift 2
  local f counts before after
  for f in "${SHELL_FILES[@]}"; do
    [[ -f $f ]] || continue
    counts=$(remove_lines_in_file "$f" "$bs" "$be" "$@") || {
      warn "清理 $f 失败"
      continue
    }
    read -r before after <<<"$counts"
    if [[ ${before:-0} != "${after:-0}" ]]; then
      subst "清理 $f（$before -> $after 行）"
    fi
  done
}

# 从 shell 配置中提取与工具相关的行（备份 env_config 用）
backup_tools_env() {
  local pat=$1 f
  for f in "${SHELL_FILES[@]}"; do
    [[ -f $f ]] && grep -E -- "$pat" "$f" 2>/dev/null
  done
}

write_platform_info() { # file KEY=VALUE...
  local f=$1
  shift
  {
    printf 'ARCH=%s\n' "$(uname -m)"
    printf 'OS=%s\n' "$(uname -s | tr '[:upper:]' '[:lower:]')"
    printf '%s\n' "$@"
  } >"$f"
}

# ---------------------------------------------------------------------------
# 版本管理器：pyenv / g / nvm / rust / fvm
# ---------------------------------------------------------------------------

canonical_tool() {
  case ${1,,} in
    pyenv|python) printf 'pyenv' ;;
    g|golang|go) printf 'g' ;;
    nvm|node|nodejs) printf 'nvm' ;;
    rust|rustup|cargo) printf 'rust' ;;
    fvm|flutter) printf 'fvm' ;;
    *) return 1 ;;
  esac
}

ALL_TOOLS=(pyenv g nvm rust fvm)

resolve_tools() { # names... -> 设置 TOOLS 数组
  local -a out=()
  local -A seen=()
  local a t
  _add_tool() {
    [[ -n ${seen[$1]+x} ]] && return 0
    seen[$1]=1
    out+=("$1")
  }
  if (($# == 0)); then
    for t in "${ALL_TOOLS[@]}"; do _add_tool "$t"; done
  else
    for a in "$@"; do
      if [[ ${a,,} == all ]]; then
        for t in "${ALL_TOOLS[@]}"; do _add_tool "$t"; done
        continue
      fi
      if ! t=$(canonical_tool "$a"); then
        err "未知工具: $a（可选: pyenv g nvm rust fvm all）"
        unset -f _add_tool
        return 1
      fi
      _add_tool "$t"
    done
  fi
  unset -f _add_tool
  TOOLS=("${out[@]}")
}

tool_dir() {
  case $1 in
    pyenv) printf '%s' "$PYENV_ROOT" ;;
    g) printf '%s' "$GO_ROOT" ;;
    nvm) printf '%s' "$NVM_DIR" ;;
    rust) printf '%s' "$CARGO_HOME" ;;
    fvm) printf '%s' "$FVM_INSTALL" ;;
  esac
}

tool_installed() {
  case $1 in
    pyenv) is_exec "$PYENV_BIN" || command_exists pyenv ;;
    g) is_exec "$G_BIN" || command_exists g ;;
    nvm) [[ -s $NVM_SCRIPT ]] ;;
    rust)
      is_exec "$CARGO_BIN_DIR/rustup" || is_exec "$CARGO_BIN_DIR/cargo" ||
        command_exists rustup || command_exists cargo
      ;;
    fvm) is_exec "$FVM_BIN" || command_exists fvm ;;
    *) return 1 ;;
  esac
}

tool_version() {
  local bin out
  case $1 in
    pyenv)
      bin=$PYENV_BIN
      is_exec "$bin" || bin=$(command -v pyenv 2>/dev/null) || return 0
      out=$("$bin" --version 2>/dev/null)
      ;;
    g)
      bin=$G_BIN
      is_exec "$bin" || bin=$(command -v g 2>/dev/null) || return 0
      out=$("$bin" --version 2>/dev/null)
      ;;
    nvm)
      [[ -s $NVM_SCRIPT ]] || return 0
      out=$(bash -c "export NVM_DIR=$(_q "$NVM_DIR")
. \"\$NVM_DIR/nvm.sh\" >/dev/null 2>&1
nvm --version 2>/dev/null")
      ;;
    rust)
      bin=$CARGO_BIN_DIR/rustc
      is_exec "$bin" || bin=$(command -v rustc 2>/dev/null) || return 0
      out=$("$bin" --version 2>/dev/null)
      ;;
    fvm)
      bin=$FVM_BIN
      is_exec "$bin" || bin=$(command -v fvm 2>/dev/null) || return 0
      out=$("$bin" --version 2>/dev/null)
      ;;
  esac
  printf '%s' "${out%%$'\n'*}"
}

BK_RESULT=""

pack_dir_stage() { # src dst excludes_string
  local src=$1 dst=$2 excl=$3 partial
  [[ -d $src ]] || return 1
  partial=$(mktemp "${TMPDIR:-/tmp}/udev-pack-XXXXXX.tar") || return 1
  if ! tar_create "$partial" "$src" "$excl" "."; then
    rm -f "$partial"
    return 1
  fi
  mkdir -p "$dst" || { rm -f "$partial"; return 1; }
  if ! tar_extract "$partial" "$dst"; then
    rm -rf "$dst" "$partial"
    return 1
  fi
  rm -f "$partial"
  return 0
}

path_writable() {
  local p=$1 d t
  d=$p
  is_dir "$p" || d=$(dirname "$p")
  t=$(mktemp "$d/.udev-write-test-XXXXXX" 2>/dev/null) || return 1
  rm -f "$t"
  return 0
}

# ---- pyenv ----------------------------------------------------------------

pyenv_backup() {
  is_dir "$PYENV_ROOT" || { err "pyenv 根目录不存在: $PYENV_ROOT"; return 1; }
  local out
  out=$(backup_path pyenv) || return 1
  diskspace_warning
  subst "打包 $PYENV_ROOT"
  if ! tar_create_gz "$out" "$(dirname "$PYENV_ROOT")" "$(base_excludes)" "$(basename "$PYENV_ROOT")"; then
    rm -f "$out"
    err "打包 pyenv 失败"
    return 1
  fi
  BK_RESULT=$out
}

pyenv_restore() {
  local archive=$1 tmp source
  local -a entries=()
  tar_validate "$archive" || { err "非有效 tar.gz: $archive"; return 1; }
  tmp=$(mktemp -d "${TMPDIR:-/tmp}/udev-pyenv-restore-XXXXXX") || return 1
  subst "解压 pyenv 备份"
  if ! tar_extract "$archive" "$tmp"; then rm -rf "$tmp"; return 1; fi
  source=$tmp
  if is_dir "$tmp/.pyenv"; then
    source="$tmp/.pyenv"
  else
    entries=("$tmp"/*)
    if ((${#entries[@]} == 1)) && is_dir "${entries[0]}"; then source="${entries[0]}"; fi
  fi
  subst "复制到 $PYENV_ROOT"
  mkdir -p "$PYENV_ROOT" || { rm -rf "$tmp"; return 1; }
  copy_tree "$source" "$PYENV_ROOT" || { rm -rf "$tmp"; return 1; }
  rm -rf "$tmp"
  add_shell_line "export PYENV_ROOT=$(_q "$PYENV_ROOT")"
  add_shell_line '[[ -d $PYENV_ROOT/bin ]] && export PATH="$PYENV_ROOT/bin:$PATH"'
  add_shell_line 'eval "$(pyenv init - bash)"'
}

pyenv_install() {
  local url script
  url=$(cfg_key managers.pyenv.install_url)
  is_cn && url="https://raw.githubusercontent.com/pyenv/pyenv-installer/master/bin/pyenv-installer"
  download_script "$url" || { err "下载 pyenv 安装脚本失败"; return 1; }
  script=$SCRIPT_PATH
  subst "执行 pyenv 安装脚本"
  if ! PYENV_ROOT="$PYENV_ROOT" run_script bash "$script"; then
    rm -f "$script"
    err "pyenv 安装脚本执行失败，可尝试 CN_GITHUB_PROXY=https://ghproxy.net/ udev install --cn pyenv"
    return 1
  fi
  rm -f "$script"
  add_shell_line "export PYENV_ROOT=$(_q "$PYENV_ROOT")"
  add_shell_line '[[ -d $PYENV_ROOT/bin ]] && export PATH="$PYENV_ROOT/bin:$PATH"'
  add_shell_line 'eval "$(pyenv init - bash)"'
  if is_cn; then
    add_shell_line "export PYTHON_BUILD_MIRROR_URL=$(_q "$(cfg_key mirror.python)")"
    add_shell_line "export PYTHON_BUILD_MIRROR_URL_SKIP_CHECKSUM=1"
  fi
}

pyenv_clean() {
  subst "保留已有 pyenv 目录（避免误删 Python 版本）"
}

# ---- g (Go 版本管理器) -----------------------------------------------------

gopath_arc_name() {
  local gp=$1
  if [[ $gp == "$HOME/"* ]]; then
    printf '%s' "${gp#"$HOME/"}"
  elif [[ $gp == /go ]]; then
    printf '_go'
  else
    printf '_abs%s' "${gp//\//_}"
  fi
}

G_TMP=""
G_FOUND=0
_g_pack() { # src dst strict
  local src=$1 dst=$2 strict=$3
  [[ -d $src ]] || return 0
  subst "打包 $src"
  G_FOUND=1
  if ! pack_dir_stage "$src" "$G_TMP/$dst" "$(base_excludes)"; then
    ((strict)) && return 1
  fi
  return 0
}

g_backup() {
  local out
  out=$(backup_path g) || return 1
  G_TMP=$(mktemp -d "${TMPDIR:-/tmp}/udev-g-backup-XXXXXX") || return 1
  diskspace_warning
  G_FOUND=0

  if ! _g_pack "$GO_ROOT" ".g" 1; then
    rm -rf "$G_TMP" "$out"
    err "打包 $GO_ROOT 失败"
    return 1
  fi
  if ! _g_pack "$HOME/.goenv" ".goenv" 1; then
    rm -rf "$G_TMP" "$out"
    err "打包 $HOME/.goenv 失败"
    return 1
  fi
  if ! _g_pack "$HOME/.config/g" ".config/g" 1; then
    rm -rf "$G_TMP" "$out"
    err "打包 $HOME/.config/g 失败"
    return 1
  fi
  if is_dir "$GO_PATH"; then
    if ! _g_pack "$GO_PATH" "$(gopath_arc_name "$GO_PATH")" 1; then
      rm -rf "$G_TMP" "$out"
      err "打包 GOPATH 失败"
      return 1
    fi
  fi
  if is_dir /go; then
    _g_pack /go "_go" 0 || true
  fi

  if ((G_FOUND == 0)); then
    rm -rf "$G_TMP" "$out"
    err "未找到 g/go 相关目录"
    return 1
  fi

  local envcfg
  envcfg=$(backup_tools_env '(PATH.*\.g/bin|GOROOT|GOPATH|/go/bin)')
  [[ -n $envcfg ]] && printf '%s\n' "$envcfg" >"$G_TMP/env_config"
  write_platform_info "$G_TMP/platform_info" "GOPATH=$GO_PATH"

  if ! tar_create_gz "$out" "$G_TMP" "" "."; then
    rm -rf "$G_TMP" "$out"
    err "生成 g 备份失败"
    return 1
  fi
  rm -rf "$G_TMP"
  BK_RESULT=$out
}

g_restore() {
  local archive=$1 tmp f base target child line
  local -a items=()
  tar_validate "$archive" || { err "非有效 tar.gz: $archive"; return 1; }
  tmp=$(mktemp -d "${TMPDIR:-/tmp}/udev-g-restore-XXXXXX") || return 1
  subst "解压 g 备份"
  if ! tar_extract "$archive" "$tmp"; then rm -rf "$tmp"; return 1; fi
  if [[ -f $tmp/platform_info ]]; then
    subst "备份平台信息:"
    while IFS= read -r line; do printf '         %s\n' "$line"; done <"$tmp/platform_info"
  fi
  # 包含隐藏目录（.g / .goenv / .config），因此用 find 而不是通配符
  mapfile -d '' items < <(find "$tmp" -mindepth 1 -maxdepth 1 -print0)
  for f in "${items[@]}"; do
    base=$(basename "$f")
    case $base in
      platform_info | .platform_info | env_config) continue ;;
    esac
    is_dir "$f" || continue
    if [[ $base == .config ]]; then
      # 只合并 .config 下的子项，避免覆盖用户已有的 ~/.config
      mkdir -p "$HOME/.config" || { rm -rf "$tmp"; return 1; }
      for child in "$f"/*; do
        [[ -e $child ]] || continue
        target="$HOME/.config/$(basename "$child")"
        subst "恢复 .config/$(basename "$child") → $target"
        if ! copy_tree "$child" "$target"; then rm -rf "$tmp"; return 1; fi
      done
      continue
    fi
    case $base in
      _go)
        if path_writable /go; then
          target=/go
        else
          target="$HOME/_go"
          warn "无权限写入 /go，恢复为 $target"
        fi
        ;;
      _abs*)
        target="$HOME/$base"
        warn "外部绝对路径（编码有损），恢复为 $target"
        ;;
      *) target="$HOME/$base" ;;
    esac
    subst "恢复 $base → $target"
    if ! copy_tree "$f" "$target"; then rm -rf "$tmp"; return 1; fi
  done
  rm -rf "$tmp"

  remove_shell_lines "" "" 'GOROOT=.*versions/current/go'
  block="# g (Go Version Manager) configuration
if [[ -n \$(alias g 2>/dev/null) ]]; then
    unalias g
fi
[ -s $(_q "$GO_ROOT/env") ] && \. $(_q "$GO_ROOT/env")  # g shell setup"
  add_shell_block "g shell setup" "$block"
}

g_install() {
  local script go_root
  go_root="$GO_ROOT"
  download_script "$(cfg_key managers.g.install_url)" || { err "下载 g 安装脚本失败"; return 1; }
  script=$SCRIPT_PATH
  if is_cn; then
    subst "重写脚本 github 地址为镜像"
    sed -i \
      -e "s#https://api.github.com#$(cfg_key mirror.github_proxy)https://api.github.com#g" \
      -e "s#https://github.com#$(cfg_key mirror.github_proxy)https://github.com#g" \
      "$script" 2>/dev/null || {
      rm -f "$script"
      err "重写 g 安装脚本失败"
      return 1
    }
  fi
  subst "执行 g 安装脚本"
  if ! run_script bash "$script"; then
    rm -f "$script"
    err "g 安装失败，可尝试 CN_GITHUB_PROXY=https://ghproxy.net/ udev install --cn g"
    return 1
  fi
  rm -f "$script"

  add_shell_line "export GOROOT=$(_q "$go_root/go")"
  add_shell_line "export PATH=\"$go_root/bin:\$GOROOT/bin:\$PATH\""
  if is_cn; then
    add_shell_line "export G_MIRROR=$(_q "$(cfg_key mirror.go)")"
  fi
  local d
  for d in bin src pkg; do
    mkdir -p "$GO_PATH/$d" || return 1
  done
}

g_clean() {
  rm -rf "$GO_ROOT"
  remove_shell_lines "" "" 'g shell setup' '\.g/' 'GOROOT=.*\.g/go' 'G_MIRROR'
}

# ---- nvm ------------------------------------------------------------------

nvm_backup() {
  is_dir "$NVM_DIR" || { err "$NVM_DIR 不存在，跳过 nvm 备份"; return 1; }
  local out excl
  out=$(backup_path nvm) || return 1
  diskspace_warning
  excl=$(excludes_without "package.json" "package-lock.json")
  subst "打包 $NVM_DIR"
  if ! tar_create_gz "$out" "$(dirname "$NVM_DIR")" "$excl" "$(basename "$NVM_DIR")"; then
    rm -f "$out"
    err "打包 nvm 失败"
    return 1
  fi
  BK_RESULT=$out
}

nvm_restore() {
  local archive=$1 tmp source block
  local -a entries=()
  tar_validate "$archive" || { err "非有效 tar.gz: $archive"; return 1; }
  tmp=$(mktemp -d "${TMPDIR:-/tmp}/udev-nvm-restore-XXXXXX") || return 1
  subst "解压 nvm 备份"
  if ! tar_extract "$archive" "$tmp"; then rm -rf "$tmp"; return 1; fi
  source="$tmp/.nvm"
  if ! is_dir "$source"; then
    entries=("$tmp"/*)
    if ((${#entries[@]} == 1)) && is_dir "${entries[0]}"; then source="${entries[0]}"; fi
  fi
  if ! is_dir "$source"; then
    rm -rf "$tmp"
    err "备份中未找到 nvm 目录"
    return 1
  fi
  subst "恢复 $NVM_DIR"
  if ! copy_path "$source" "$NVM_DIR"; then rm -rf "$tmp"; return 1; fi
  rm -rf "$tmp"
  block="export NVM_DIR=$(_q "$NVM_DIR")
[ -s \"\$NVM_DIR/nvm.sh\" ] && \. \"\$NVM_DIR/nvm.sh\" # This loads nvm"
  add_shell_block "NVM_DIR" "$block"
}

nvm_install() {
  local script
  download_script "$(cfg_key managers.nvm.install_url)" || { err "下载 nvm 安装脚本失败"; return 1; }
  script=$SCRIPT_PATH
  subst "执行 nvm 安装脚本"
  if ! NVM_DIR="$NVM_DIR" run_script bash "$script"; then rm -f "$script"; err "nvm 安装失败"; return 1; fi
  rm -f "$script"
  add_shell_line "export NVM_DIR=$(_q "$NVM_DIR")"
  add_shell_line '[ -s "$NVM_DIR/nvm.sh" ] && \. "$NVM_DIR/nvm.sh"'
  add_shell_line '[ -s "$NVM_DIR/bash_completion" ] && \. "$NVM_DIR/bash_completion"'
  if is_cn; then
    add_shell_line "export NVM_NODEJS_ORG_MIRROR=$(_q "$(cfg_key mirror.node)")"
  fi
}

nvm_clean() {
  rm -rf "$NVM_DIR"
  remove_shell_lines "" "" 'NVM_DIR' 'nvm\.sh' 'nvm/bash_completion' 'NVM_NODEJS_ORG_MIRROR'
}

# ---- rust -----------------------------------------------------------------

rust_backup() {
  local out tmp found=0 excl
  out=$(backup_path rust) || return 1
  tmp=$(mktemp -d "${TMPDIR:-/tmp}/udev-rust-backup-XXXXXX") || return 1
  diskspace_warning

  if is_dir "$RUSTUP_HOME"; then
    subst "打包 $RUSTUP_HOME"
    if ! pack_dir_stage "$RUSTUP_HOME" "$tmp/rustup" "$(base_excludes)"; then
      rm -rf "$tmp" "$out"
      err "打包 Rustup 目录失败"
      return 1
    fi
    found=1
  fi
  if is_dir "$CARGO_HOME"; then
    subst "打包 $CARGO_HOME（排除 registry / git 缓存）"
    excl=$(printf '%s\n%s\n%s' "$(base_excludes)" "registry" "git")
    if ! pack_dir_stage "$CARGO_HOME" "$tmp/cargo" "$excl"; then
      rm -rf "$tmp" "$out"
      err "打包 CARGO_HOME 失败"
      return 1
    fi
    found=1
  fi
  if ((found == 0)); then
    rm -rf "$tmp" "$out"
    err "未找到 Rust 相关目录（$RUSTUP_HOME / $CARGO_HOME）"
    return 1
  fi

  local envcfg
  envcfg=$(backup_tools_env '(RUSTUP_HOME|CARGO_HOME|\.cargo/env|\.cargo/bin)')
  [[ -n $envcfg ]] && printf '%s\n' "$envcfg" >"$tmp/env_config"
  write_platform_info "$tmp/platform_info" "RUSTUP_HOME=$RUSTUP_HOME" "CARGO_HOME=$CARGO_HOME"

  if ! tar_create_gz "$out" "$tmp" "" "."; then
    rm -rf "$tmp" "$out"
    err "生成 Rust 备份失败"
    return 1
  fi
  rm -rf "$tmp"
  BK_RESULT=$out
}

rust_restore() {
  local archive=$1 tmp restored=0
  tar_validate "$archive" || { err "非有效 tar.gz: $archive"; return 1; }
  tmp=$(mktemp -d "${TMPDIR:-/tmp}/udev-rust-restore-XXXXXX") || return 1
  subst "解压 Rust 备份"
  if ! tar_extract "$archive" "$tmp"; then rm -rf "$tmp"; return 1; fi
  if [[ -f $tmp/platform_info ]]; then
    subst "备份平台信息:"
    while IFS= read -r line; do printf '         %s\n' "$line"; done <"$tmp/platform_info"
  fi
  if is_dir "$tmp/rustup"; then
    subst "恢复 $RUSTUP_HOME"
    if ! copy_tree "$tmp/rustup" "$RUSTUP_HOME"; then rm -rf "$tmp"; return 1; fi
    restored=1
  fi
  if is_dir "$tmp/cargo"; then
    subst "恢复 $CARGO_HOME"
    if ! copy_tree "$tmp/cargo" "$CARGO_HOME"; then rm -rf "$tmp"; return 1; fi
    restored=1
  fi
  rm -rf "$tmp"
  if ((restored == 0)); then
    err "备份中未找到可恢复的 Rust 数据"
    return 1
  fi
  add_shell_line "export RUSTUP_HOME=$(_q "$RUSTUP_HOME")"
  add_shell_line "export CARGO_HOME=$(_q "$CARGO_HOME")"
  add_shell_line ". $(_q "$CARGO_HOME/env")"
  add_shell_line "export PATH=\"$CARGO_BIN_DIR:\$PATH\""
}

rust_install() {
  local script tc
  download_script "$(cfg_key managers.rust.install_url)" || { err "下载 rustup 安装脚本失败"; return 1; }
  script=$SCRIPT_PATH
  tc=$(cfg_key managers.rust.default_toolchain)
  [[ -n $tc ]] || tc=stable
  local -a args=(-y --default-toolchain "$tc")
  if [[ ${NO_MODIFY_PATH:-0} == 1 ]] || cfg_bool managers.rust.no_modify_path; then
    args+=(--no-modify-path)
  fi
  subst "执行 rustup 安装脚本"
  if ! RUSTUP_HOME="$RUSTUP_HOME" CARGO_HOME="$CARGO_HOME" sh "$script" "${args[@]}"; then
    rm -f "$script"
    err "rustup 安装失败"
    return 1
  fi
  rm -f "$script"
  add_shell_line "export RUSTUP_HOME=$(_q "$RUSTUP_HOME")"
  add_shell_line "export CARGO_HOME=$(_q "$CARGO_HOME")"
  if [[ ${NO_MODIFY_PATH:-0} == 1 ]] || cfg_bool managers.rust.no_modify_path; then
    add_shell_line ". $(_q "$CARGO_HOME/env")"
  fi
}

rust_clean() {
  rm -rf "$RUSTUP_HOME" "$CARGO_HOME/registry" "$CARGO_HOME/git"
}

# ---- fvm ------------------------------------------------------------------

fvm_cache_dir() {
  local bin=$FVM_BIN out line
  is_exec "$bin" || bin=$(command -v fvm 2>/dev/null) || { printf '%s' "$FVM_CACHE"; return; }
  out=$("$bin" list 2>/dev/null) || { printf '%s' "$FVM_CACHE"; return; }
  while IFS= read -r line; do
    if [[ $line == "Cache directory:"* ]]; then
      line=${line#Cache directory:}
      line=${line#"${line%%[![:space:]]*}"}
      if [[ -n $line ]]; then printf '%s' "$line"; return; fi
    fi
  done <<<"$out"
  printf '%s' "$FVM_CACHE"
}

fvm_backup() {
  local out tmp cache found=0 excl
  out=$(backup_path fvm) || return 1
  tmp=$(mktemp -d "${TMPDIR:-/tmp}/udev-fvm-backup-XXXXXX") || return 1
  diskspace_warning
  cache=$(fvm_cache_dir)

  excl=$(printf '%s\n%s\n%s' "$(base_excludes)" "versions/*/bin/cache/artifacts" "versions/*/bin/cache/engine")
  if is_dir "$cache"; then
    subst "打包 $cache（FVM 缓存，排除引擎产物）"
    if ! pack_dir_stage "$cache" "$tmp/fvm_cache" "$excl"; then
      rm -rf "$tmp" "$out"
      err "打包 FVM 缓存失败"
      return 1
    fi
    found=1
  fi
  if is_dir "$FVM_INSTALL" && [[ $(expand_path "$FVM_INSTALL") != "$(expand_path "$cache")" ]]; then
    subst "打包 FVM 安装目录 $FVM_INSTALL"
    if ! pack_dir_stage "$FVM_INSTALL" "$tmp/fvm_install" "$(base_excludes)"; then
      rm -rf "$tmp" "$out"
      err "打包 FVM 安装目录失败"
      return 1
    fi
    found=1
  fi
  if ((found == 0)); then
    rm -rf "$tmp" "$out"
    err "未找到 FVM 相关目录（缓存: $cache / 安装: $FVM_INSTALL）"
    return 1
  fi

  local envcfg
  envcfg=$(backup_tools_env '(FVM_CACHE_PATH|FVM_HOME|fvm/bin|fvm/default)')
  [[ -n $envcfg ]] && printf '%s\n' "$envcfg" >"$tmp/env_config"
  write_platform_info "$tmp/platform_info" "FVM_CACHE_PATH=$cache" "FVM_INSTALL_DIR=$FVM_INSTALL"

  if ! tar_create_gz "$out" "$tmp" "" "."; then
    rm -rf "$tmp" "$out"
    err "生成 FVM 备份失败"
    return 1
  fi
  rm -rf "$tmp"
  BK_RESULT=$out
}

fvm_restore() {
  local archive=$1 tmp cache restored=0
  tar_validate "$archive" || { err "非有效 tar.gz: $archive"; return 1; }
  tmp=$(mktemp -d "${TMPDIR:-/tmp}/udev-fvm-restore-XXXXXX") || return 1
  subst "解压 FVM 备份"
  if ! tar_extract "$archive" "$tmp"; then rm -rf "$tmp"; return 1; fi
  if [[ -f $tmp/platform_info ]]; then
    subst "备份平台信息:"
    while IFS= read -r line; do printf '         %s\n' "$line"; done <"$tmp/platform_info"
  fi
  cache=$FVM_CACHE
  if is_dir "$tmp/fvm_cache"; then
    subst "恢复 FVM 缓存到 $cache"
    if ! copy_tree "$tmp/fvm_cache" "$cache"; then rm -rf "$tmp"; return 1; fi
    restored=1
  fi
  if is_dir "$tmp/fvm_install"; then
    subst "恢复 FVM 安装目录到 $FVM_INSTALL"
    if ! copy_tree "$tmp/fvm_install" "$FVM_INSTALL"; then rm -rf "$tmp"; return 1; fi
    restored=1
  fi
  rm -rf "$tmp"
  if ((restored == 0)); then
    err "备份中未找到可恢复的 FVM 数据"
    return 1
  fi
  add_shell_line "export PATH=\"$FVM_INSTALL/bin:\$PATH\""
  if [[ $cache != "$HOME/fvm" ]]; then
    add_shell_line "export FVM_CACHE_PATH=$(_q "$cache")"
  fi
}

fvm_install() {
  local script
  download_script "$(cfg_key managers.fvm.install_url)" || { err "下载 FVM 安装脚本失败"; return 1; }
  script=$SCRIPT_PATH
  subst "执行 FVM 安装脚本"
  if ! FVM_INSTALL_DIR="$FVM_INSTALL" FVM_CACHE_PATH="$FVM_CACHE" bash "$script"; then
    rm -f "$script"
    err "FVM 安装失败"
    return 1
  fi
  rm -f "$script"
  add_shell_line "export PATH=\"$FVM_INSTALL/bin:\$PATH\""
  if [[ $FVM_CACHE != "$HOME/fvm" ]]; then
    add_shell_line "export FVM_CACHE_PATH=$(_q "$FVM_CACHE")"
  fi
}

fvm_clean() {
  local cache
  cache=$(fvm_cache_dir)
  if [[ $cache == "$HOME" || $cache == "/" ]]; then
    err "拒绝删除异常缓存路径: $cache"
    return 1
  fi
  rm -rf "$cache"
}

# ---- 分发 -----------------------------------------------------------------

tool_backup() {
  case $1 in
    pyenv) pyenv_backup ;;
    g) g_backup ;;
    nvm) nvm_backup ;;
    rust) rust_backup ;;
    fvm) fvm_backup ;;
  esac
}

tool_restore() {
  local archive=$2
  case $1 in
    pyenv) pyenv_restore "$archive" ;;
    g) g_restore "$archive" ;;
    nvm) nvm_restore "$archive" ;;
    rust) rust_restore "$archive" ;;
    fvm) fvm_restore "$archive" ;;
  esac
}

tool_install() {
  case $1 in
    pyenv) pyenv_install ;;
    g) g_install ;;
    nvm) nvm_install ;;
    rust) rust_install ;;
    fvm) fvm_install ;;
  esac
}

tool_clean() {
  case $1 in
    pyenv) pyenv_clean ;;
    g) g_clean ;;
    nvm) nvm_clean ;;
    rust) rust_clean ;;
    fvm) fvm_clean ;;
  esac
}

# ---------------------------------------------------------------------------
# 命令：install / backup / restore / status / list / config
# ---------------------------------------------------------------------------

check_deps() { # install?
  local -a missing=()
  local d
  for d in bash tar; do
    command -v "$d" >/dev/null 2>&1 || missing+=("$d")
  done
  if [[ ${1-} == install ]]; then
    command -v git >/dev/null 2>&1 || missing+=("git")
    if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
      missing+=("curl/wget")
    fi
  fi
  if ((${#missing[@]})); then
    err "缺少必需依赖: ${missing[*]}"
    return 1
  fi
}

cmd_install() {
  local -a names=() failed=()
  local cn=0 force=0 no_restore=0 no_modify_path=0
  local a m b i restored
  for a in "$@"; do
    case $a in
      --cn | -c) cn=1 ;;
      --force | -f) force=1 ;;
      --no-restore) no_restore=1 ;;
      --no-modify-path) no_modify_path=1 ;;
      -h | --help) print_help; return 0 ;;
      -*)
        err "未知参数: $a"
        return 1
        ;;
      *) names+=("$a") ;;
    esac
  done

  if ((cn)); then
    USE_CN=true
    _CFG[mirror.use_cn]=true
  fi
  ((no_modify_path)) && NO_MODIFY_PATH=1

  resolve_tools "${names[@]}" || return 1

  header "udev install"
  info "目标工具: ${TOOLS[*]}"
  is_cn && info "启用镜像源"
  ((force)) && warn "启用 --force，将重新安装已存在的工具"
  check_deps install || return 1

  for ((i = 0; i < ${#TOOLS[@]}; i++)); do
    m=${TOOLS[$i]}
    printf '\n%s ==> [%d/%d] %s\n' "$(bold '')" "$((i + 1))" "${#TOOLS[@]}" "$m"

    if tool_installed "$m" && ((force == 0)); then
      warn "$m 已安装，跳过（如需强制重装请加 --force）"
      continue
    fi
    if ((force)) && tool_installed "$m"; then
      subst "清理已有的 $m"
      tool_clean "$m" || warn "清理 $m 失败"
    fi

    restored=0
    if ((no_restore == 0)); then
      b=$(latest_backup "$m")
      if [[ -n $b ]]; then
        subst "发现本地备份: $(basename "$b")"
        if tool_restore "$m" "$b"; then
          restored=1
          ok "$m 已从备份恢复"
        else
          warn "$m 从备份恢复失败，将回退到在线安装"
        fi
      fi
    else
      subst "已禁用本地恢复 (--no-restore)"
    fi

    if ((restored == 0)); then
      subst "在线安装 $m"
      if ! tool_install "$m"; then
        err "$m 安装失败"
        failed+=("$m")
        continue
      fi
    fi

    if tool_installed "$m"; then
      ok "$m 验证通过"
    else
      err "$m 验证失败"
      failed+=("$m")
    fi
  done

  printf '\n'
  if ((${#failed[@]})); then
    err "以下工具处理失败: ${failed[*]}"
    return 1
  fi
  ok "=== 全部完成 ==="
  hint_shell_reload
}

collect_tool_args() { # 与 Go 版一致：只接受工具名与 -h
  local a
  TOOL_ARGS=()
  for a in "$@"; do
    case $a in
      -h | --help)
        print_help
        return 1
        ;;
      -*)
        err "未知参数: $a"
        return 2
        ;;
      *) TOOL_ARGS+=("$a") ;;
    esac
  done
  return 0
}

cmd_backup() {
  local rc
  collect_tool_args "$@"
  rc=$?
  ((rc == 1)) && return 0
  ((rc != 0)) && return 1
  resolve_tools "${TOOL_ARGS[@]}" || return 1

  header "udev backup"
  info "备份目录: $BACKUP_DIR"

  local -a failed=() succeeded=()
  local i m
  for ((i = 0; i < ${#TOOLS[@]}; i++)); do
    m=${TOOLS[$i]}
    printf '\n%s ==> [%d/%d] 备份 %s\n' "$(bold '')" "$((i + 1))" "${#TOOLS[@]}" "$m"
    BK_RESULT=""
    if ! tool_backup "$m"; then
      err "$m 备份失败"
      failed+=("$m")
      continue
    fi
    ok "已创建: $BK_RESULT"
    succeeded+=("$m")
  done

  printf '\n'
  ((${#succeeded[@]})) && ok "已备份: ${succeeded[*]}"
  if ((${#failed[@]})); then
    err "备份失败: ${failed[*]}"
    return 1
  fi
}

cmd_restore() {
  local -a names=() failed=() restored_list=()
  local a force=0 m b
  for a in "$@"; do
    case $a in
      --force | -f) force=1 ;;
      -h | --help) print_help; return 0 ;;
      -*)
        err "未知参数: $a"
        return 1
        ;;
      *) names+=("$a") ;;
    esac
  done
  resolve_tools "${names[@]}" || return 1

  header "udev restore"
  local i
  for ((i = 0; i < ${#TOOLS[@]}; i++)); do
    m=${TOOLS[$i]}
    printf '\n%s ==> [%d/%d] 恢复 %s\n' "$(bold '')" "$((i + 1))" "${#TOOLS[@]}" "$m"
    if tool_installed "$m" && ((force == 0)); then
      warn "$m 已安装，跳过（如需覆盖恢复请加 --force）"
      continue
    fi
    b=$(latest_backup "$m")
    if [[ -z $b ]]; then
      warn "未找到 $m 的备份，跳过"
      continue
    fi
    subst "使用备份: $(basename "$b")"
    if ! tool_restore "$m" "$b"; then
      err "$m 恢复失败"
      failed+=("$m")
      continue
    fi
    if ! tool_installed "$m"; then
      err "$m 恢复后验证失败"
      failed+=("$m")
      continue
    fi
    restored_list+=("$m")
  done

  printf '\n'
  ((${#restored_list[@]})) && ok "已恢复: ${restored_list[*]}"
  if ((${#failed[@]})); then
    err "恢复失败: ${failed[*]}"
    return 1
  fi
  hint_shell_reload
}

cmd_status() {
  local m status version v
  header "udev status"
  printf '%-8s %-12s %-38s %s\n' "TOOL" "STATUS" "LOCATION" "VERSION"
  printf -- '-%.0s' {1..78}
  printf '\n'

  for m in "${ALL_TOOLS[@]}"; do
    status=missing
    version=-
    if tool_installed "$m"; then
      status=installed
      v=$(tool_version "$m")
      [[ -n $v ]] && version=$v
    fi
    printf '%-8s %-12s %-38s %s\n' "$m" "$status" "$(tool_dir "$m")" "$version"
    printf '         %sbackups: %d%s\n' "$(dim '')" "$(count_backups "$m")" ""
  done

  printf '\n%s\n' "$(bold '开发工具清单')"
  printf '  node   : npm %d 个，命令 %d 条\n' \
    "$(seq_len dev_tools.node.npm)" "$(seq_len dev_tools.node.commands)"
  printf '  go     : go install %d 个\n' "$(seq_len dev_tools.go.packages)"
  printf '  python : pip %d 个\n' "$(seq_len dev_tools.python.packages)"
  printf '  cargo  : %d 个\n' "$(seq_len dev_tools.cargo.packages)"

  printf '\n配置目录: %s\n' "$HOME_DIR"
  printf '配置文件: %s\n' "$CONFIG_FILE"
  printf '备份目录: %s\n' "$BACKUP_DIR"
}

cmd_list() {
  local rc
  collect_tool_args "$@"
  rc=$?
  ((rc == 1)) && return 0
  ((rc != 0)) && return 1
  resolve_tools "${TOOL_ARGS[@]}" || return 1

  header "udev list"
  local m f
  local -a files=()
  for m in "${TOOLS[@]}"; do
    printf '\n%s\n' "$(bold "[$m]")"
    files=()
    mapfile -t files < <(find_backups "$m")
    if ((${#files[@]} == 0)); then
      printf '  %s\n' "$(dim '(无备份)')"
      continue
    fi
    for f in "${files[@]}"; do
      printf '  %s  %s\n' "$(basename "$f")" "$(dim "($(human_size "$f"))")"
    done
  done
  printf '\n备份目录: %s\n' "$BACKUP_DIR"
}

cmd_config() {
  local sub=${1:-show}
  case $sub in
    -h | --help)
      print_help
      return 0
      ;;
    path)
      printf '%s\n' "$CONFIG_FILE"
      ;;
    show | "")
      printf '# udev 当前生效配置\n# 配置文件: %s\n\n' "$CONFIG_FILE"
      cat "$CONFIG_FILE"
      ;;
    init)
      local force=0 a
      shift || true
      for a in "$@"; do
        case $a in
          --force | -f) force=1 ;;
          -h | --help) print_help; return 0 ;;
        esac
      done
      if ((force == 0)) && [[ -f $CONFIG_FILE ]]; then
        info "配置文件已存在: $CONFIG_FILE（使用 --force 重置为默认值）"
        return 0
      fi
      default_config >"$CONFIG_FILE" || return 1
      ok "已写入默认配置: $CONFIG_FILE"
      ;;
    edit)
      local editor=${VISUAL:-${EDITOR:-vi}}
      "$editor" "$CONFIG_FILE"
      ;;
    *)
      err "未知 config 子命令: $sub（可选: show path init edit）"
      return 1
      ;;
  esac
}

# ---------------------------------------------------------------------------
# 命令：tools（按配置批量安装开发工具）
# ---------------------------------------------------------------------------

STEP=0
TOTAL=0
PRELUDE=""

build_prelude() {
  local bins
  bins="$PYENV_ROOT/bin:$GO_ROOT/bin:$GO_ROOT/go/bin:$GO_BIN_DIR:$CARGO_BIN_DIR:$FVM_INSTALL/bin"
  PRELUDE="export NVM_DIR=$(_q "$NVM_DIR")"
  PRELUDE+=$'\n''if [ -s "$NVM_DIR/nvm.sh" ]; then . "$NVM_DIR/nvm.sh" >/dev/null 2>&1 || true; fi'
  PRELUDE+=$'\n'"export PYENV_ROOT=$(_q "$PYENV_ROOT")"
  PRELUDE+=$'\n'"if [ -x $(_q "$PYENV_BIN") ]; then eval \"\$($(_q "$PYENV_BIN") init - bash)\" 2>/dev/null || true; elif command -v pyenv >/dev/null 2>&1; then eval \"\$(pyenv init - bash)\" 2>/dev/null || true; fi"
  PRELUDE+=$'\n'"export GOPATH=$(_q "$GO_PATH")"
  PRELUDE+=$'\n'"export GOBIN=$(_q "$GO_BIN_DIR")"
  PRELUDE+=$'\n'"export RUSTUP_HOME=$(_q "$RUSTUP_HOME")"
  PRELUDE+=$'\n'"export CARGO_HOME=$(_q "$CARGO_HOME")"
  PRELUDE+=$'\n'"if [ -s $(_q "$GO_ROOT/env") ]; then . $(_q "$GO_ROOT/env"); fi"
  PRELUDE+=$'\n'"if [ -s $(_q "$CARGO_HOME/env") ]; then . $(_q "$CARGO_HOME/env"); fi"
  PRELUDE+=$'\n'"export PATH=\"$bins:\$PATH\""
}

stream() { bash -c "set -o pipefail"$'\n'"$PRELUDE"$'\n'"$1"; }
quiet_cmd() { bash -c "set -o pipefail"$'\n'"$PRELUDE"$'\n'"$1" 2>&1; }

step_begin() {
  STEP=$((STEP + 1))
  printf '\n%s %s\n' "$(cyan "$(printf '[%02d/%02d]' "$STEP" "$TOTAL")")" "$1"
}
step_ok() { printf '%s\n' "$(green "   ✓ 完成 (耗时 ${1}s)")"; }
step_skip() { printf '%s\n' "$(yellow '   ↷ 已安装，跳过')"; }
step_fail() { printf '%s\n' "$(red "   ✗ 失败 (退出码 $1)")"; }

run_step() { # desc optional cmd
  local desc=$1 optional=$2 cmd=$3 start rc
  step_begin "$desc"
  start=$SECONDS
  if stream "$cmd"; then
    step_ok $((SECONDS - start))
    return 0
  else
    rc=$?
  fi
  step_fail "$rc"
  ((optional)) && return 0
  err "$desc: 命令执行失败（退出码 $rc）"
  return "$rc"
}

count_section() {
  local n=0 v
  case $1 in
    node)
      v=$(cfg_key dev_tools.node.nvm_version)
      [[ -n $v ]] && n=$((n + 1))
      n=$((n + $(seq_len dev_tools.node.npm) + $(seq_len dev_tools.node.commands) + $(seq_len dev_tools.node.cleanup)))
      ;;
    go)
      v=$(cfg_key dev_tools.go.g_version)
      [[ -n $v ]] && n=$((n + 1))
      n=$((n + $(seq_len dev_tools.go.packages) + $(seq_len dev_tools.go.cleanup)))
      ;;
    python)
      v=$(cfg_key dev_tools.python.pyenv_version)
      [[ -n $v ]] && n=$((n + 2))
      cfg_bool dev_tools.python.pip_upgrade && n=$((n + 1))
      n=$((n + $(seq_len dev_tools.python.packages) + $(seq_len dev_tools.python.commands) + $(seq_len dev_tools.python.cleanup)))
      ;;
    cargo)
      n=$(($(seq_len dev_tools.cargo.packages) + $(seq_len dev_tools.cargo.cleanup)))
      ;;
  esac
  printf '%s' "$n"
}

normalize_sections() {
  local -A seen=()
  local a s
  SECTIONS=()
  _add_sec() {
    [[ -n ${seen[$1]+x} ]] || { seen[$1]=1; SECTIONS+=("$1"); }
  }
  if (($# == 0)); then
    for s in node go python; do _add_sec "$s"; done
  else
    for a in "$@"; do
      case ${a,,} in
        all)
          for s in node go python cargo; do _add_sec "$s"; done
          ;;
        node | npm | nodejs | js) _add_sec node ;;
        go | golang) _add_sec go ;;
        python | pip | py) _add_sec python ;;
        cargo | rust) _add_sec cargo ;;
        *)
          err "未知参数: $a（可选: node go python cargo all）"
          unset -f _add_sec
          return 1
          ;;
      esac
    done
  fi
  unset -f _add_sec
  return 0
}

preflight_sections() {
  local s
  for s in "${SECTIONS[@]}"; do
    (($(count_section "$s") == 0)) && continue
    case $s in
      node)
        [[ -s $NVM_SCRIPT ]] || { err "未找到 nvm（$NVM_SCRIPT），请先运行: udev install nvm"; return 1; }
        ;;
      go)
        if [[ -n $(cfg_key dev_tools.go.g_version) ]] && ! command_exists "$G_BIN" && ! command_exists g; then
          err "未找到 g（Go 版本管理器），请先运行: udev install g"
          return 1
        fi
        if ! command_exists "$GO_SDK_BIN/go" && ! command_exists go; then
          err "未找到 go，请先运行: udev install g"
          return 1
        fi
        ;;
      python)
        command_exists "$PYENV_BIN" || command_exists pyenv || {
          err "未找到 pyenv，请先运行: udev install pyenv"
          return 1
        }
        ;;
      cargo)
        command_exists "$CARGO_BIN_DIR/cargo" || command_exists cargo || {
          err "未找到 cargo，请先运行: udev install rust"
          return 1
        }
        ;;
    esac
  done
}

npm_package_name() {
  local spec=$1 rest
  if [[ $spec == @* ]]; then
    rest=${spec:1}
    if [[ $rest == *"@"* ]]; then
      printf '@%s' "${rest%%@*}"
    else
      printf '%s' "$spec"
    fi
    return
  fi
  printf '%s' "${spec%%@*}"
}

go_pkg_default_bin() {
  local p=$1
  if [[ $p == *"@"* ]]; then p=${p%@*}; fi
  p=${p%/}
  printf '%s' "${p##*/}"
}

run_node() {
  local nvmver n i pkg name cmd start rc
  header "Node.js"

  nvmver=$(cfg_key dev_tools.node.nvm_version)
  if [[ -n $nvmver ]]; then
    if [[ ${nvmver,,} == lts ]]; then
      run_step "nvm install --lts" 0 "nvm install --lts" || return 1
    else
      run_step "nvm install $nvmver" 0 "nvm install $(_q "$nvmver")" || return 1
    fi
  fi

  n=$(seq_len dev_tools.node.npm)
  for ((i = 0; i < n; i++)); do
    pkg=$(cfg_key "dev_tools.node.npm[$i]")
    name=$(npm_package_name "$pkg")
    cmd="npm install -g $(_q "$pkg")"
    step_begin "$cmd"
    start=$SECONDS
    if quiet_cmd "npm list -g --depth=0 $(_q "$name") >/dev/null 2>&1" >/dev/null 2>&1; then
      step_skip
    elif stream "$cmd"; then
      step_ok $((SECONDS - start))
    else
      rc=$?
      step_fail "$rc"
      err "$cmd: 命令执行失败（退出码 $rc）"
      return "$rc"
    fi
  done

  while IFS= read -r cmd; do
    [[ -z $cmd ]] && continue
    run_step "$cmd" 1 "$cmd" || return 1
  done < <(list_vals dev_tools.node.commands)

  while IFS= read -r cmd; do
    [[ -z $cmd ]] && continue
    run_step "$cmd" 1 "$cmd" || return 1
  done < <(list_vals dev_tools.node.cleanup)
}

run_go() {
  local gver n i pkg bin cmd start rc out ulist=0
  header "Go"

  gver=$(cfg_key dev_tools.go.g_version)
  if [[ -n $gver ]]; then
    cmd="g install $(_q "$gver")"
    step_begin "$cmd"
    start=$SECONDS
    out=$(quiet_cmd "$cmd")
    rc=$?
    printf '%s' "$out"
    if ((rc != 0)) && [[ $out == *"has been installed"* ]]; then rc=0; fi
    if ((rc == 0)); then
      step_ok $((SECONDS - start))
    else
      step_fail "$rc"
      err "$cmd: 命令执行失败（退出码 $rc）"
      return "$rc"
    fi
  fi

  n=$(seq_len dev_tools.go.packages)
  [[ -n ${_SEQ[dev_tools.go.packages]+x} ]] && ulist=1
  for ((i = 0; i < n; i++)); do
    if ((ulist)); then
      pkg=$(cfg_user "dev_tools.go.packages[$i].path")
      [[ -n $pkg ]] || pkg=$(cfg_user "dev_tools.go.packages[$i]")
      bin=$(cfg_user "dev_tools.go.packages[$i].bin")
    else
      pkg=$(cfg_key "dev_tools.go.packages[$i].path")
      bin=$(cfg_key "dev_tools.go.packages[$i].bin")
    fi
    [[ -n $pkg ]] || continue
    [[ -n $bin ]] || bin=$(go_pkg_default_bin "$pkg")
    cmd="go install $(_q "$pkg")"
    step_begin "$cmd"
    start=$SECONDS
    if is_exec "$GO_BIN_DIR/$bin" || { [[ -n ${GOBIN-} ]] && is_exec "$GOBIN/$bin"; }; then
      step_skip
    elif stream "$cmd"; then
      step_ok $((SECONDS - start))
    else
      rc=$?
      step_fail "$rc"
      err "$cmd: 命令执行失败（退出码 $rc）"
      return "$rc"
    fi
  done

  while IFS= read -r cmd; do
    [[ -z $cmd ]] && continue
    run_step "$cmd" 1 "$cmd" || return 1
  done < <(list_vals dev_tools.go.cleanup)
}

run_python() {
  local ver n i pkg cmd start rc out line found
  header "Python"

  ver=$(cfg_key dev_tools.python.pyenv_version)
  if [[ -n $ver ]]; then
    cmd="pyenv install -s $(_q "$ver")"
    step_begin "pyenv install $ver"
    start=$SECONDS
    out=$(quiet_cmd "pyenv versions --bare 2>/dev/null")
    found=0
    while IFS= read -r line; do
      [[ ${line#"${line%%[![:space:]]*}"} == "$ver" ]] && found=1
    done <<<"$out"
    if ((found)); then
      step_skip
    elif stream "$cmd"; then
      step_ok $((SECONDS - start))
    else
      rc=$?
      step_fail "$rc"
      err "$cmd: 命令执行失败（退出码 $rc）"
      return "$rc"
    fi

    run_step "pyenv global $ver" 0 "pyenv global $(_q "$ver")" || return 1
  fi

  if cfg_bool dev_tools.python.pip_upgrade; then
    run_step "python -m pip install --upgrade pip" 0 "python -m pip install --upgrade pip" || return 1
  fi

  n=$(seq_len dev_tools.python.packages)
  for ((i = 0; i < n; i++)); do
    pkg=$(cfg_key "dev_tools.python.packages[$i]")
    run_step "pip install $pkg" 0 "pip install $(_q "$pkg")" || return 1
  done

  while IFS= read -r cmd; do
    [[ -z $cmd ]] && continue
    run_step "$cmd" 1 "$cmd" || return 1
  done < <(list_vals dev_tools.python.commands)

  while IFS= read -r cmd; do
    [[ -z $cmd ]] && continue
    run_step "$cmd" 1 "$cmd" || return 1
  done < <(list_vals dev_tools.python.cleanup)
}

CARGO_LIST=""
CARGO_LOADED=0
cargo_pkg_installed() {
  local name=$1 line
  if ((CARGO_LOADED == 0)); then
    CARGO_LIST=$(quiet_cmd "cargo install --list 2>/dev/null")
    CARGO_LOADED=1
  fi
  while IFS= read -r line; do
    [[ ${line#"${line%%[![:space:]]*}"} == "$name "* ]] && return 0
  done <<<"$CARGO_LIST"
  return 1
}

run_cargo() {
  local n i pkg cmd start rc j nargs arg ulist=0
  local -a args=()
  header "Cargo"

  n=$(seq_len dev_tools.cargo.packages)
  [[ -n ${_SEQ[dev_tools.cargo.packages]+x} ]] && ulist=1
  for ((i = 0; i < n; i++)); do
    if ((ulist)); then
      pkg=$(cfg_user "dev_tools.cargo.packages[$i].name")
      [[ -n $pkg ]] || pkg=$(cfg_user "dev_tools.cargo.packages[$i]")
      [[ -n $pkg ]] || continue
      nargs=0
      if [[ -n ${_SEQ["dev_tools.cargo.packages[$i].args"]+x} ]]; then
        nargs=${_SEQ["dev_tools.cargo.packages[$i].args"]}
      fi
    else
      pkg=$(cfg_key "dev_tools.cargo.packages[$i].name")
      nargs=$(seq_len "dev_tools.cargo.packages[$i].args")
    fi
    args=()
    for ((j = 0; j < nargs; j++)); do
      arg=$(cfg_key "dev_tools.cargo.packages[$i].args[$j]")
      args+=("$(_q "$arg")")
    done
    cmd="cargo install $(_q "$pkg")"
    ((${#args[@]})) && cmd+=" ${args[*]}"
    step_begin "$cmd"
    start=$SECONDS
    if cargo_pkg_installed "$pkg"; then
      step_skip
    elif stream "$cmd"; then
      step_ok $((SECONDS - start))
    else
      rc=$?
      step_fail "$rc"
      err "$cmd: 命令执行失败（退出码 $rc）"
      return "$rc"
    fi
  done

  while IFS= read -r cmd; do
    [[ -z $cmd ]] && continue
    run_step "$cmd" 1 "$cmd" || return 1
  done < <(list_vals dev_tools.cargo.cleanup)
}

cmd_tools() {
  local a s
  for a in "$@"; do
    case $a in
      -h | --help)
        print_help
        return 0
        ;;
    esac
  done

  normalize_sections "$@" || return 1
  STEP=0
  TOTAL=0
  for s in "${SECTIONS[@]}"; do
    TOTAL=$((TOTAL + $(count_section "$s")))
  done
  if ((TOTAL == 0)); then
    warn "配置中的工具清单为空，请检查 $CONFIG_FILE"
    return 0
  fi
  preflight_sections || return 1
  build_prelude

  printf '%s\n' "$(cyan "==> 开始执行，共 $TOTAL 步")"
  for s in "${SECTIONS[@]}"; do
    case $s in
      node) run_node || return 1 ;;
      go) run_go || return 1 ;;
      python) run_python || return 1 ;;
      cargo) run_cargo || return 1 ;;
    esac
  done
  ok "==> 全部完成。"
}

# ---------------------------------------------------------------------------
# 命令：uninstall
# ---------------------------------------------------------------------------

declare -gA UN_DIRS=() UN_PATTERNS=() UN_BLOCK_START=() UN_BLOCK_END=()
UN_TOOLS=()
UN_BACKUP_DIR=""

build_uninstall_targets() {
  UN_DIRS[pyenv]="$PYENV_ROOT"
  UN_PATTERNS[pyenv]=$(printf '%s\n' \
    'PYENV_ROOT' 'pyenv init' 'pyenv virtualenv-init' \
    'pyenv/bin' '^# pyenv' 'PYTHON_BUILD_MIRROR_URL')

  UN_DIRS[g]=$(printf '%s\n' "$GO_ROOT" "$HOME/.goenv" "$HOME/.config/g")
  UN_PATTERNS[g]=$(printf '%s\n' \
    'g shell setup' '\.g/' 'GOROOT=.*\.g/go' 'G_MIRROR' \
    '^[[:space:]]*unalias[[:space:]]+g[[:space:]]*$')
  UN_BLOCK_START[g]="# g (Go Version Manager) configuration"
  UN_BLOCK_END[g]=$(printf '%s\n' 'g shell setup' '.g/env')

  UN_DIRS[nvm]="$NVM_DIR"
  UN_PATTERNS[nvm]=$(printf '%s\n' \
    'NVM_DIR' 'nvm\.sh' 'nvm/bash_completion' \
    '^# nvm配置' '^# nvm$' 'NVM_NODEJS_ORG_MIRROR')

  UN_DIRS[rust]=$(printf '%s\n' "$RUSTUP_HOME" "$CARGO_HOME/registry" "$CARGO_HOME/git")
  UN_PATTERNS[rust]=$(printf '%s\n' 'RUSTUP_HOME' 'CARGO_HOME' '\.cargo/env' '\.cargo/bin')

  UN_DIRS[fvm]=$(printf '%s\n' "$FVM_CACHE" "$FVM_INSTALL")
  UN_PATTERNS[fvm]=$(printf '%s\n' 'FVM_CACHE_PATH' 'FVM_INSTALL_DIR' 'fvm/bin')
}

file_needs_clean() {
  local f=$1 t p bs
  [[ -f $f ]] || return 1
  for t in "${UN_TOOLS[@]}"; do
    bs=${UN_BLOCK_START[$t]-}
    if [[ -n $bs ]] && grep -Fq -- "$bs" "$f" 2>/dev/null; then return 0; fi
    while IFS= read -r p; do
      [[ -z $p ]] && continue
      if grep -Eq -- "$p" "$f" 2>/dev/null; then return 0; fi
    done <<<"${UN_PATTERNS[$t]}"
  done
  return 1
}

clean_shell_file() {
  local f=$1 t p bs="" be="" before after counts
  local -a pats=()
  if [[ ! -f $f ]]; then
    subst "$f 不存在，跳过"
    return 0
  fi
  file_needs_clean "$f" || { subst "$f 无需清理"; return 0; }

  mkdir -p "$UN_BACKUP_DIR" || return 1
  cp -p "$f" "$UN_BACKUP_DIR/$(basename "$f").bak" || return 1
  subst "已备份 $f -> $UN_BACKUP_DIR/$(basename "$f").bak"

  for t in "${UN_TOOLS[@]}"; do
    while IFS= read -r p; do
      [[ -n $p ]] && pats+=("$p")
    done <<<"${UN_PATTERNS[$t]}"
    if [[ -n ${UN_BLOCK_START[$t]-} ]]; then
      bs=${UN_BLOCK_START[$t]}
      be=${UN_BLOCK_END[$t]-}
    fi
  done

  counts=$(remove_lines_in_file "$f" "$bs" "$be" "${pats[@]}") || return 1
  read -r before after <<<"$counts"
  subst "已清理 $f（$before -> $after 行）"
  return 0
}

safe_remove() {
  local path=$1 abs
  [[ -n $path ]] || return 0
  abs=$(expand_path "$path")
  if [[ $abs == "/" || $abs == "$HOME" ]]; then
    err "拒绝删除异常路径: $abs"
    return 1
  fi
  if ! is_dir "$abs"; then
    subst "未发现 $abs，跳过"
    return 0
  fi
  subst "删除 $abs"
  rm -rf "$abs" || return 1
}

print_uninstall_plan() {
  local remove_gopath=$1 t d
  printf '即将卸载以下工具链:\n'
  for t in "${UN_TOOLS[@]}"; do
    while IFS= read -r d; do
      [[ -n $d ]] && printf '  - %-6s: %s\n' "$t" "$d"
    done <<<"${UN_DIRS[$t]}"
  done
  if ((remove_gopath)); then
    printf '  - %-6s: %s（包含项目代码与已编译二进制，将被删除）\n' "GOPATH" "$GO_PATH"
  else
    printf '  - %-6s: 保留（%s）\n' "GOPATH" "$GO_PATH"
  fi
  printf '\n并会备份后清理以下文件中的相关配置:\n'
  local f
  for f in "${SHELL_FILES[@]}"; do
    printf '  - %s\n' "$f"
  done
  printf '\n'
}

fill_str() { local n=$1 s=$2 out="" i; for ((i = 0; i < n; i++)); do out+=$s; done; printf '%s' "$out"; }

progress_step() { # cur total desc
  local cur=$1 total=$2 desc=$3 pct
  pct=$((cur * 100 / total))
  if [[ -t 1 ]]; then
    local fill bar
    fill=$((cur * 20 / total))
    bar="$(fill_str "$fill" '█')$(fill_str "$((20 - fill))" '░')"
    printf '%s [%d/%d] %3d%% [%s] %s\n' "$(cyan '')" "$cur" "$total" "$pct" "$bar" "$desc"
  else
    printf '[%d/%d] %3d%% %s\n' "$cur" "$total" "$pct" "$desc"
  fi
}

post_check() {
  local t d f found=0
  subst "检查残留..."
  for t in "${UN_TOOLS[@]}"; do
    while IFS= read -r d; do
      [[ -n $d ]] || continue
      if is_dir "$d"; then
        warn "残留目录: $d"
        found=1
      fi
    done <<<"${UN_DIRS[$t]}"
  done
  for f in "${SHELL_FILES[@]}"; do
    if file_needs_clean "$f"; then
      warn "残留配置: $f（可手动检查）"
      found=1
    fi
  done
  ((found == 0)) && subst "未发现残留"
}

cmd_uninstall() {
  local -a names=()
  local yes=0 keep=0 a t d remove_gopath=0
  for a in "$@"; do
    case $a in
      -y | --yes) yes=1 ;;
      -k | --keep-gopath) keep=1 ;;
      -h | --help) print_help; return 0 ;;
      -*)
        err "未知参数: $a"
        return 1
        ;;
      *) names+=("$a") ;;
    esac
  done

  header "udev uninstall"
  resolve_tools "${names[@]}" || return 1
  UN_TOOLS=("${TOOLS[@]}")
  build_uninstall_targets

  for t in "${UN_TOOLS[@]}"; do
    if [[ $t == g ]] && cfg_bool uninstall.include_gopath; then
      remove_gopath=1
    fi
  done
  ((keep)) && remove_gopath=0

  if ((yes == 0)); then
    if [[ ! -t 0 || ! -t 1 ]]; then
      err "非交互环境，请使用 -y/--yes 显式确认卸载"
      return 1
    fi
    print_uninstall_plan "$remove_gopath"
    printf '确认继续? [y/N] '
    local answer=""
    read -r answer || true
    case ${answer,,} in
      y | yes) ;;
      *)
        warn "已取消卸载"
        return 0
        ;;
    esac
  fi

  UN_BACKUP_DIR="$HOME_DIR/backups/uninstall/$(date +%Y%m%d-%H%M%S)"
  local total=$((${#SHELL_FILES[@]} + ${#UN_TOOLS[@]} + 1))
  ((remove_gopath)) && total=$((total + 1))
  local step=0 f

  for f in "${SHELL_FILES[@]}"; do
    step=$((step + 1))
    progress_step "$step" "$total" "清理 shell 配置: $f"
    clean_shell_file "$f" || return 1
  done

  for t in "${UN_TOOLS[@]}"; do
    step=$((step + 1))
    progress_step "$step" "$total" "删除 $t 相关目录"
    while IFS= read -r d; do
      [[ -n $d ]] || continue
      safe_remove "$d" || return 1
    done <<<"${UN_DIRS[$t]}"
  done

  if ((remove_gopath)); then
    step=$((step + 1))
    progress_step "$step" "$total" "删除 GOPATH: $GO_PATH"
    safe_remove "$GO_PATH" || return 1
  fi

  step=$((step + 1))
  progress_step "$step" "$total" "检查残留"
  post_check

  printf '\n'
  ok "======= 卸载完成 ======="
  if is_dir "$UN_BACKUP_DIR"; then
    printf '配置备份目录: %s\n' "$UN_BACKUP_DIR"
    printf '如需回滚，可将备份复制回原位置。\n'
  fi
  hint_shell_reload
}

# ---------------------------------------------------------------------------
# 补全
# ---------------------------------------------------------------------------

completion_script() {
  case $1 in
    bash)
      cat <<'EOF'
# bash completion for udev
_udev_completion() {
    local cur candidates
    cur="${COMP_WORDS[COMP_CWORD]}"
    candidates=$(command udev __complete "${COMP_WORDS[@]:1}" 2>/dev/null)
    COMPREPLY=( $(compgen -W "$candidates" -- "$cur") )
}
complete -F _udev_completion udev
EOF
      ;;
    zsh)
      cat <<'EOF'
#compdef udev
# zsh completion for udev
_udev() {
    local -a args candidates
    args=("${(@)words[2,-1]}")
    candidates=("${(@f)$(command udev __complete "${args[@]}" 2>/dev/null)}")
    if (( ${#candidates} )); then
        compadd -- "${candidates[@]}"
    fi
}
if (( $+functions[compdef] )); then
    compdef _udev udev
fi
EOF
      ;;
    fish)
      cat <<'EOF'
# fish completion for udev
function __udev_candidates
    set -l tokens (commandline -opc)
    set -l cur (commandline -ct)
    command udev __complete $tokens "$cur" 2>/dev/null
end
complete -c udev -f -a '(__udev_candidates)'
EOF
      ;;
    *) return 1 ;;
  esac
}

print_completion_help() {
  cat <<'EOF'
用法: udev completion <bash|zsh|fish|install> [shell]

  udev completion bash     输出 bash 补全脚本
  udev completion zsh      输出 zsh 补全脚本
  udev completion fish     输出 fish 补全脚本
  udev completion install  自动写入当前 shell 的配置（可选指定 bash/zsh/fish）

手动启用:
  bash:  echo 'source <(udev completion bash)' >> ~/.bashrc
  zsh:   echo 'source <(udev completion zsh)'  >> ~/.zshrc
  fish:  udev completion fish > ~/.config/fish/completions/udev.fish

启用后输入 udev 后按 Tab 即可补全命令、工具名、分组与参数。
EOF
}

detect_shell() {
  case $(basename "${SHELL:-bash}") in
    *zsh*) printf 'zsh' ;;
    *fish*) printf 'fish' ;;
    *) printf 'bash' ;;
  esac
}

rc_file_for() {
  local name=.bashrc f
  [[ $1 == zsh ]] && name=.zshrc
  for f in "${SHELL_FILES[@]}"; do
    [[ $(basename "$f") == "$name" ]] && { printf '%s' "$f"; return; }
  done
  printf '%s' "$HOME/$name"
}

install_completion() {
  local shell=$1
  case $shell in
    bash | zsh)
      local rc line
      rc=$(rc_file_for "$shell")
      line="source <(udev completion $shell)"
      add_line_to "$rc" "$line" || return 1
      printf '请执行 source %s 或重开终端后生效。\n' "$rc"
      [[ $shell == zsh ]] && printf '%s\n' "$(dim '提示: zsh 需要先加载 compinit（多数主题/框架已自动加载）。')"
      ;;
    fish)
      local path="$HOME/.config/fish/completions/udev.fish"
      mkdir -p "$(dirname "$path")" || return 1
      completion_script fish >"$path" || return 1
      ok "已写入 $path"
      printf '重开 fish 后生效。\n'
      ;;
    *)
      err "不支持 shell: $shell（可选: bash zsh fish）"
      return 1
      ;;
  esac
}

cmd_completion() {
  local shell=${1-}
  case $shell in
    "" | -h | --help | help)
      print_completion_help
      return 0
      ;;
    install)
      local target=${2-}
      [[ -n $target ]] || target=$(detect_shell)
      install_completion "$target"
      ;;
    bash | zsh | fish)
      completion_script "$shell"
      ;;
    *)
      err "不支持 shell: $shell（可选: bash zsh fish）"
      return 1
      ;;
  esac
}

comp_words() {
  local -a words=("$@")
  if ((${#words[@]})); then
    case $(basename "${words[0]}") in
      udev | udev.sh) words=("${words[@]:1}") ;;
    esac
  fi

  local cur="" sub="" w u c skip
  local -a used=() cands=()
  if ((${#words[@]})); then
    cur=${words[${#words[@]}-1]}
    used=("${words[@]:0:${#words[@]}-1}")
  fi

  for w in "${used[@]}"; do
    [[ $w == -* ]] && continue
    case $w in
      install | i | backup | b | restore | r | recover | status | st | s | list | ls | l | tools | dev | dev-tool | uninstall | remove | rm | config | cfg | completion) sub=$w ;;
    esac
    break
  done

  local -a tools=(pyenv g nvm rust fvm all)
  local -a sections=(node go python cargo all)
  local -a config_subs=(show path init edit --help -h)
  local -a shells=(bash zsh fish)
  local -a install_flags=(--cn -c --force -f --no-restore --no-modify-path --help -h)
  local -a restore_flags=(--force -f --help -h)
  local -a uninstall_flags=(-y --yes -k --keep-gopath --help -h)

  case $sub in
    "")
      cands=(install backup restore status list tools uninstall config completion version help --help --version -h -v)
      ;;
    install | i) cands=("${tools[@]}" "${install_flags[@]}") ;;
    backup | b | list | ls | l) cands=("${tools[@]}" --help -h) ;;
    restore | r | recover) cands=("${tools[@]}" "${restore_flags[@]}") ;;
    uninstall | remove | rm) cands=("${tools[@]}" "${uninstall_flags[@]}") ;;
    tools | dev | dev-tool) cands=("${sections[@]}" --help -h) ;;
    config | cfg)
      if ((${#used[@]} >= 2)) && [[ ${used[1]} != -* ]]; then
        if [[ ${used[1]} == init ]]; then
          cands=(--force -f --help -h)
        else
          cands=(--help -h)
        fi
      else
        cands=("${config_subs[@]}")
      fi
      ;;
    completion | comp)
      if ((${#used[@]} >= 2)) && [[ ${used[1]} != -* ]]; then
        if [[ ${used[1]} == install ]]; then cands=("${shells[@]}"); fi
      else
        cands=("${shells[@]}" install --help -h)
      fi
      ;;
    status | st | s | version | help) cands=(--help -h) ;;
    *) cands=(install backup restore status list tools uninstall config completion version help --help --version -h -v) ;;
  esac

  for c in "${cands[@]}"; do
    skip=0
    for u in "${used[@]}"; do
      [[ $u == "$c" ]] && { skip=1; break; }
    done
    ((skip)) || printf '%s\n' "$c"
  done
}

# ---------------------------------------------------------------------------
# 帮助与入口
# ---------------------------------------------------------------------------

print_help() {
  cat <<EOF
$(bold udev) v$VERSION - 一体化开发工具管理器（纯 Bash 版，无需 Go）

用法: udev <命令> [选项] [工具/分组...]

命令:
  install   安装工具（优先本地备份恢复，否则在线安装）
  backup    备份已安装的工具
  restore   从备份恢复工具（已安装时跳过，除非 --force）
  status    查看工具安装状态、版本与备份数量
  list      列出可用备份
  tools     按配置文件批量安装开发工具
  uninstall 卸载工具链并清理 shell 配置
  config    查看/编辑配置（show | path | init | edit）
  completion 生成或安装 shell Tab 补全（bash | zsh | fish | install）
  version   显示版本
  help      显示帮助

工具:
  pyenv | g | nvm | rust | fvm | all（默认 all）

install 选项:
  --cn, -c          使用镜像源
  --force, -f       强制重装
  --no-restore      跳过本地备份恢复
  --no-modify-path  传给 rustup：不自动修改 PATH

restore 选项:
  --force, -f       覆盖已安装的工具

uninstall 选项:
  -y, --yes         跳过确认
  -k, --keep-gopath 保留 GOPATH

tools 分组:
  node | go | python | cargo | all
  默认执行 node go python

配置/存储目录:
  ${HOME_DIR:-$HOME/.udev}
  配置文件: ${CONFIG_FILE:-$HOME/.udev/config.yaml}

示例:
  udev status
  udev install --cn all
  udev install --force pyenv
  udev backup all
  udev restore --force nvm
  udev tools node go
  udev tools all
  udev uninstall -y -k
  udev config edit

说明:
  - 与 Go 版共用同一份 config.yaml 与备份目录，可交替使用
  - npm/go/pip/cargo 工具清单全部位于配置文件中，直接编辑即可自定义
  - 环境变量可覆盖配置：UDEV_HOME、UDEV_BACKUP_DIR、PYENV_ROOT、NVM_DIR、
    GOPATH、RUSTUP_HOME、CARGO_HOME、FVM_CACHE_PATH、FVM_INSTALL_DIR、
    USE_CN_MIRROR=1
  - 依赖：bash 4.3+、tar、git；下载需要 curl 或 wget
EOF
}

main() {
  local cmd=${1-}
  case $cmd in
    help | -h | --help)
      print_help
      return 0
      ;;
    version | -v | --version)
      printf 'udev %s\n' "$VERSION"
      return 0
      ;;
    __complete)
      shift
      comp_words "$@"
      return 0
      ;;
    "")
      print_help
      return 1
      ;;
  esac

  load_config || return 1
  ensure_config || return 1

  case $cmd in
    install | i)
      shift
      cmd_install "$@"
      ;;
    backup | b)
      shift
      cmd_backup "$@"
      ;;
    restore | r | recover)
      shift
      cmd_restore "$@"
      ;;
    status | st | s)
      cmd_status
      ;;
    list | ls | l)
      shift
      cmd_list "$@"
      ;;
    tools | dev | dev-tool)
      shift
      cmd_tools "$@"
      ;;
    uninstall | remove | rm)
      shift
      cmd_uninstall "$@"
      ;;
    config | cfg)
      shift
      cmd_config "$@"
      ;;
    completion | comp)
      shift
      cmd_completion "$@"
      ;;
    *)
      err "未知命令: $cmd"
      print_help
      return 1
      ;;
  esac
}

main "$@"
exit $?
