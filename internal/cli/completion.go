package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hanamisakip/udev/internal/config"
	"github.com/hanamisakip/udev/internal/ui"
	"github.com/hanamisakip/udev/internal/util"
)

var topLevelCandidates = []string{
	"install", "backup", "restore", "status", "list",
	"tools", "uninstall", "config", "completion", "version", "help",
	"--help", "--version", "-h", "-v",
}

var toolCandidates = []string{"pyenv", "g", "nvm", "rust", "fvm", "all"}
var sectionCandidates = []string{"node", "go", "python", "cargo", "all"}
var configCandidates = []string{"show", "path", "init", "edit", "--help", "-h"}
var shellCandidates = []string{"bash", "zsh", "fish"}

var installFlags = []string{"--cn", "-c", "--force", "-f", "--no-restore", "--no-modify-path", "--help", "-h"}
var backupFlags = []string{"--help", "-h"}
var restoreFlags = []string{"--force", "-f", "--help", "-h"}
var uninstallFlags = []string{"-y", "--yes", "-k", "--keep-gopath", "--help", "-h"}
var toolsFlags = []string{"--help", "-h"}

// completeWords 根据已输入内容返回候选列表。
// words 的最后一个元素是当前正在补全的词（可能为空）。
func completeWords(words []string) []string {
	if len(words) > 0 && (words[0] == "udev" || filepath.Base(words[0]) == "udev") {
		words = words[1:]
	}
	if len(words) == 0 {
		return topLevelCandidates
	}
	used := words[:len(words)-1]
	sub := findCommand(used)

	switch sub {
	case "":
		return filterUsed(topLevelCandidates, used)
	case "install", "i":
		return filterUsed(append(append([]string{}, toolCandidates...), installFlags...), used)
	case "backup", "b", "list", "ls", "l":
		return filterUsed(append(append([]string{}, toolCandidates...), backupFlags...), used)
	case "restore", "r", "recover":
		return filterUsed(append(append([]string{}, toolCandidates...), restoreFlags...), used)
	case "uninstall", "remove", "rm":
		return filterUsed(append(append([]string{}, toolCandidates...), uninstallFlags...), used)
	case "tools", "dev", "dev-tool":
		return filterUsed(append(append([]string{}, sectionCandidates...), toolsFlags...), used)
	case "config", "cfg":
		if len(used) >= 2 && !strings.HasPrefix(used[1], "-") {
			if used[1] == "init" {
				return filterUsed([]string{"--force", "-f", "--help", "-h"}, used)
			}
			return filterUsed([]string{"--help", "-h"}, used)
		}
		return filterUsed(configCandidates, used)
	case "completion":
		if len(used) >= 2 && !strings.HasPrefix(used[1], "-") {
			if used[1] == "install" {
				return filterUsed(shellCandidates, used)
			}
			return nil
		}
		return filterUsed(append(append([]string{}, shellCandidates...), "install", "--help", "-h"), used)
	case "status", "st", "s", "version", "help":
		return filterUsed([]string{"--help", "-h"}, used)
	}
	return filterUsed(topLevelCandidates, used)
}

func findCommand(used []string) string {
	for _, w := range used {
		if strings.HasPrefix(w, "-") {
			continue
		}
		switch w {
		case "install", "i", "backup", "b", "restore", "r", "recover",
			"status", "st", "s", "list", "ls", "l", "tools", "dev", "dev-tool",
			"uninstall", "remove", "rm", "config", "cfg", "completion":
			return w
		}
		return ""
	}
	return ""
}

func filterUsed(candidates, used []string) []string {
	seen := map[string]bool{}
	for _, u := range used {
		seen[u] = true
	}
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if !seen[c] {
			out = append(out, c)
		}
	}
	return out
}

func cmdCompletion(cfg *config.Config, u *ui.UI, args []string) error {
	shell := ""
	if len(args) > 0 {
		shell = args[0]
	}
	if shell == "" || shell == "-h" || shell == "--help" || shell == "help" {
		printCompletionHelp(u)
		return nil
	}
	if shell == "install" {
		target := ""
		if len(args) > 1 {
			target = args[1]
		}
		if target == "" {
			target = detectShell()
		}
		return installCompletion(cfg, u, target)
	}
	script, ok := completionScript(shell)
	if !ok {
		return fmt.Errorf("不支持 shell: %s（可选: bash zsh fish）", shell)
	}
	fmt.Fprint(u.Out, script)
	return nil
}

func printCompletionHelp(u *ui.UI) {
	fmt.Fprint(u.Out, `用法: udev completion <bash|zsh|fish|install> [shell]

  udev completion bash     输出 bash 补全脚本
  udev completion zsh      输出 zsh 补全脚本
  udev completion fish     输出 fish 补全脚本
  udev completion install  自动写入当前 shell 的配置（可选指定 bash/zsh/fish）

手动启用:
  bash:  echo 'source <(udev completion bash)' >> ~/.bashrc
  zsh:   echo 'source <(udev completion zsh)'  >> ~/.zshrc
  fish:  udev completion fish > ~/.config/fish/completions/udev.fish

启用后输入 udev 后按 Tab 即可补全命令、工具名、分组与参数。
`)
}

func completionScript(shell string) (string, bool) {
	switch shell {
	case "bash":
		return `# bash completion for udev
_udev_completion() {
    local cur candidates
    cur="${COMP_WORDS[COMP_CWORD]}"
    candidates=$(command udev __complete "${COMP_WORDS[@]:1}" 2>/dev/null)
    COMPREPLY=( $(compgen -W "$candidates" -- "$cur") )
}
complete -F _udev_completion udev
`, true
	case "zsh":
		return `#compdef udev
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
`, true
	case "fish":
		return `# fish completion for udev
function __udev_candidates
    set -l tokens (commandline -opc)
    set -l cur (commandline -ct)
    command udev __complete $tokens "$cur" 2>/dev/null
end
complete -c udev -f -a '(__udev_candidates)'
`, true
	}
	return "", false
}

func detectShell() string {
	sh := filepath.Base(os.Getenv("SHELL"))
	switch {
	case strings.Contains(sh, "zsh"):
		return "zsh"
	case strings.Contains(sh, "fish"):
		return "fish"
	default:
		return "bash"
	}
}

func installCompletion(cfg *config.Config, u *ui.UI, shell string) error {
	switch shell {
	case "bash", "zsh":
		rc := rcFileFor(cfg, shell)
		line := fmt.Sprintf("source <(udev completion %s)", shell)
		added, err := util.AddLineIfMissing(rc, line, true)
		if err != nil {
			return err
		}
		if added {
			u.OKf("已写入 %s: %s", rc, line)
		} else {
			u.Infof("%s 中已存在补全配置，无需重复写入", rc)
		}
		fmt.Fprintf(u.Out, "请执行 source %s 或重开终端后生效。\n", rc)
		if shell == "zsh" {
			fmt.Fprintln(u.Out, u.Dim("提示: zsh 需要先加载 compinit（多数主题/框架已自动加载）。"))
		}
		return nil
	case "fish":
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path := filepath.Join(home, ".config", "fish", "completions", "udev.fish")
		script, _ := completionScript("fish")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
			return err
		}
		u.OKf("已写入 %s", path)
		fmt.Fprintln(u.Out, "重开 fish 后生效。")
		return nil
	default:
		return fmt.Errorf("不支持 shell: %s（可选: bash zsh fish）", shell)
	}
}

func rcFileFor(cfg *config.Config, shell string) string {
	name := ".bashrc"
	if shell == "zsh" {
		name = ".zshrc"
	}
	for _, f := range cfg.Shell.Files {
		if filepath.Base(f) == name {
			return f
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, name)
}
