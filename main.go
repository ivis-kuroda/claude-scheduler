// claude-scheduler
//
// Claude Codeの利用枠(rolling window)を狙ったタイミングで開始させるための
// 軽量ping登録CLI。Linux/macOSでは crontab、Windowsでは schtasks を
// 自動で使い分ける。Node.js等のランタイムに依存せず、単一バイナリとして
// 動作する(GOOS/GOARCHでクロスコンパイルして配布する想定)。
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const marker = "claude-ping-scheduler"

var dayOrder = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

var dayCron = map[string]string{
	"sun": "0", "mon": "1", "tue": "2", "wed": "3", "thu": "4", "fri": "5", "sat": "6",
}

var dayWin = map[string]string{
	"sun": "SUN", "mon": "MON", "tue": "TUE", "wed": "WED", "thu": "THU", "fri": "FRI", "sat": "SAT",
}

type timeSpec struct {
	hour   int
	minute int
}

type options struct {
	days   []string
	prompt string
	log    string
}

func main() {
	if len(os.Args) < 2 {
		printHelp()
		return
	}

	cmd := os.Args[1]
	rest := os.Args[2:]

	switch cmd {
	case "add":
		if err := runAdd(rest); err != nil {
			fatal(err)
		}
	case "remove":
		if err := runRemove(); err != nil {
			fatal(err)
		}
	case "list":
		if err := runList(); err != nil {
			fatal(err)
		}
	case "-h", "--help", "help":
		printHelp()
	default:
		printHelp()
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "エラー: "+err.Error())
	os.Exit(1)
}

// ---------- 引数パース ----------

func parseOptions(args []string) ([]string, options, error) {
	opts := options{
		days:   []string{"mon", "tue", "wed", "thu", "fri"},
		prompt: "ok",
	}
	var times []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--days":
			i++
			if i >= len(args) {
				return nil, opts, fmt.Errorf("--days に値がありません")
			}
			opts.days = strings.Split(strings.ToLower(args[i]), ",")
			for _, d := range opts.days {
				if _, ok := dayCron[strings.TrimSpace(d)]; !ok {
					return nil, opts, fmt.Errorf("不正な曜日です: %s", d)
				}
			}
		case a == "--prompt":
			i++
			if i >= len(args) {
				return nil, opts, fmt.Errorf("--prompt に値がありません")
			}
			opts.prompt = args[i]
		case a == "--log":
			i++
			if i >= len(args) {
				return nil, opts, fmt.Errorf("--log に値がありません")
			}
			opts.log = args[i]
		case strings.HasPrefix(a, "--"):
			return nil, opts, fmt.Errorf("不明なオプションです: %s", a)
		default:
			times = append(times, a)
		}
	}
	return times, opts, nil
}

var timeRe = regexp.MustCompile(`^([01]?\d|2[0-3]):([0-5]\d)$`)

func parseTime(t string) (timeSpec, error) {
	m := timeRe.FindStringSubmatch(t)
	if m == nil {
		return timeSpec{}, fmt.Errorf(`不正な時刻形式です: "%s" (例: 07:00)`, t)
	}
	h, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	return timeSpec{hour: h, minute: mi}, nil
}

// ---------- add / list / remove ----------

func runAdd(args []string) error {
	times, opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	if len(times) == 0 {
		return fmt.Errorf("少なくとも1つの時刻を指定してください (例: 07:00)")
	}
	var specs []timeSpec
	for _, t := range times {
		ts, err := parseTime(t)
		if err != nil {
			return err
		}
		specs = append(specs, ts)
	}

	if runtime.GOOS == "windows" {
		return addWindows(specs, opts)
	}
	return addCron(specs, opts)
}

func runList() error {
	if runtime.GOOS == "windows" {
		return listWindows()
	}
	return listCron()
}

func runRemove() error {
	if runtime.GOOS == "windows" {
		return removeWindows()
	}
	return removeCron()
}

// ---------- Linux / macOS (cron) ----------

func readCrontab() string {
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		return "" // crontabが空/未設定の場合はエラーになるので無視
	}
	return string(out)
}

func writeCrontab(content string) error {
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = bytes.NewBufferString(content)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func buildShellCommand(opts options) string {
	cmd := fmt.Sprintf(`claude -p "%s"`, opts.prompt)
	if opts.log != "" {
		cmd += fmt.Sprintf(` >> "%s" 2>&1`, opts.log)
	}
	return cmd
}

func addCron(specs []timeSpec, opts options) error {
	existing := readCrontab()
	var kept []string
	for _, l := range strings.Split(existing, "\n") {
		if strings.TrimSpace(l) == "" || strings.Contains(l, marker) {
			continue
		}
		kept = append(kept, l)
	}

	var dayNums []string
	for _, d := range opts.days {
		dayNums = append(dayNums, dayCron[strings.TrimSpace(d)])
	}
	dayStr := strings.Join(dayNums, ",")
	shellCmd := buildShellCommand(opts)

	var newLines []string
	for _, ts := range specs {
		line := fmt.Sprintf("%d %d * * %s %s # %s", ts.minute, ts.hour, dayStr, shellCmd, marker)
		newLines = append(newLines, line)
	}

	final := strings.Join(append(kept, newLines...), "\n") + "\n"
	if err := writeCrontab(final); err != nil {
		return fmt.Errorf("crontabの書き込みに失敗しました: %w", err)
	}

	fmt.Printf("%d件のcronジョブを登録しました:\n", len(newLines))
	for _, l := range newLines {
		fmt.Println("  " + l)
	}
	return nil
}

func removeCron() error {
	existing := readCrontab()
	var kept []string
	removed := 0
	for _, l := range strings.Split(existing, "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if strings.Contains(l, marker) {
			removed++
			continue
		}
		kept = append(kept, l)
	}
	content := ""
	if len(kept) > 0 {
		content = strings.Join(kept, "\n") + "\n"
	}
	if err := writeCrontab(content); err != nil {
		return fmt.Errorf("crontabの書き込みに失敗しました: %w", err)
	}
	fmt.Printf("%d件のcronジョブを削除しました。\n", removed)
	return nil
}

func listCron() error {
	existing := readCrontab()
	var lines []string
	for _, l := range strings.Split(existing, "\n") {
		if strings.Contains(l, marker) {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		fmt.Println("登録されているジョブはありません。")
		return nil
	}
	fmt.Println("登録済みのcronジョブ:")
	for _, l := range lines {
		fmt.Println("  " + l)
	}
	return nil
}

// ---------- Windows (schtasks) ----------

func winScriptDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".claude-scheduler")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func taskName(ts timeSpec) string {
	return fmt.Sprintf("ClaudePing_%02d%02d", ts.hour, ts.minute)
}

func addWindows(specs []timeSpec, opts options) error {
	dir, err := winScriptDir()
	if err != nil {
		return err
	}

	var dayNames []string
	for _, d := range opts.days {
		dayNames = append(dayNames, dayWin[strings.TrimSpace(d)])
	}
	dayStr := strings.Join(dayNames, ",")

	for _, ts := range specs {
		name := taskName(ts)
		batPath := filepath.Join(dir, name+".bat")

		batContent := "@echo off\r\nclaude -p \"" + opts.prompt + "\""
		if opts.log != "" {
			batContent += " >> \"" + opts.log + "\" 2>&1"
		}
		batContent += "\r\n"
		if err := os.WriteFile(batPath, []byte(batContent), 0o644); err != nil {
			return fmt.Errorf("バッチファイルの作成に失敗しました: %w", err)
		}

		// 既存の同名タスクがあれば削除(冪等にするため。無ければ無視してよい)
		_ = exec.Command("schtasks", "/delete", "/tn", name, "/f").Run()

		timeStr := fmt.Sprintf("%02d:%02d", ts.hour, ts.minute)
		createArgs := []string{
			"/create", "/tn", name, "/tr", batPath,
			"/sc", "weekly", "/d", dayStr, "/st", timeStr, "/f",
		}
		cmd := exec.Command("schtasks", createArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("schtasksの実行に失敗しました: %w", err)
		}
	}

	fmt.Printf("%d件のタスクを登録しました。(バッチファイル格納先: %s)\n", len(specs), dir)
	return nil
}

var taskNameRe = regexp.MustCompile(`TaskName:\s+\\?(ClaudePing_\d{4})`)

func queryClaudePingTasks() []string {
	out, err := exec.Command("schtasks", "/query", "/fo", "LIST", "/v").Output()
	if err != nil {
		return nil
	}
	matches := taskNameRe.FindAllStringSubmatch(string(out), -1)
	seen := map[string]bool{}
	var names []string
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	sort.Strings(names)
	return names
}

func removeWindows() error {
	names := queryClaudePingTasks()
	if len(names) == 0 {
		fmt.Println("登録されているジョブはありません。")
		return nil
	}
	dir, _ := winScriptDir()
	for _, name := range names {
		if err := exec.Command("schtasks", "/delete", "/tn", name, "/f").Run(); err == nil {
			fmt.Println("削除しました: " + name)
		}
		if dir != "" {
			_ = os.Remove(filepath.Join(dir, name+".bat"))
		}
	}
	return nil
}

func listWindows() error {
	names := queryClaudePingTasks()
	if len(names) == 0 {
		fmt.Println("登録されているジョブはありません。")
		return nil
	}
	fmt.Println("登録済みのタスク:")
	for _, n := range names {
		fmt.Println("  " + n)
	}
	return nil
}

// ---------- ヘルプ ----------

func printHelp() {
	fmt.Println(`claude-scheduler - Claude Codeの利用枠を狙ったタイミングで開始させるスケジューラ

使い方:
  claude-scheduler add <時刻...> [オプション]
  claude-scheduler remove
  claude-scheduler list

例:
  claude-scheduler add 07:00 12:00 17:00
  claude-scheduler add 07:00 12:00 17:00 --days mon,tue,wed,thu,fri
  claude-scheduler add 07:00 --prompt "hello" --log C:\Users\you\claude-ping.log
  claude-scheduler remove
  claude-scheduler list

オプション:
  --days <list>    カンマ区切りの曜日 (sun,mon,tue,wed,thu,fri,sat)
                   デフォルト: mon,tue,wed,thu,fri
  --prompt <text>  送信するプロンプト文字列。デフォルト: "ok"
  --log <path>     実行結果を追記するログファイルのパス

注意:
  - 事前に 'claude' コマンドがPATHに通っている必要があります。
  - 各pingは実際にAPIを消費するため利用量にわずかに反映されます。
  - 利用枠の起算/リセットの正確な仕様は変更される可能性があるため、
    最新情報は公式ドキュメントで確認してください。
    https://docs.claude.com/en/docs/claude-code/overview`)
}
