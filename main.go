// claude-scheduler
//
// A lightweight CLI that registers scheduled pings so that Claude Code's
// usage window (rolling window) starts at the times you choose. It uses
// crontab on Linux/macOS and schtasks on Windows. It has no runtime
// dependencies such as Node.js and runs as a single binary (intended to be
// cross-compiled with GOOS/GOARCH and distributed).
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

// pingModel is the model used for pings. A small model keeps usage low.
const pingModel = "haiku"

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

// runCommand runs an external command and returns its stdout. A non-empty stdin is
// passed to the command's standard input. On failure, the error includes stderr.
// It is a variable so that tests can replace it.
var runCommand = func(stdin, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			err = fmt.Errorf("%w: %s", err, msg)
		}
	}
	return out, err
}

// lookPath is a variable so that tests can replace it.
var lookPath = exec.LookPath

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
		if err := runRemove(rest); err != nil {
			fatal(err)
		}
	case "list":
		if err := runList(); err != nil {
			fatal(err)
		}
	case "-h", "--help", "help":
		printHelp()
	default:
		fmt.Fprintln(os.Stderr, "不明なコマンドです: "+cmd)
		printHelp()
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "エラー: "+err.Error())
	os.Exit(1)
}

// ---------- argument parsing ----------

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
			days, err := parseDays(args[i])
			if err != nil {
				return nil, opts, err
			}
			opts.days = days
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

	// Reject '"' and newlines because they cannot be safely embedded in a .bat file or crontab.
	if strings.ContainsAny(opts.prompt, "\"\r\n") {
		return nil, opts, fmt.Errorf(`--prompt に '"' や改行は使えません`)
	}
	if strings.ContainsAny(opts.log, "\"\r\n") {
		return nil, opts, fmt.Errorf(`--log に '"' や改行は使えません`)
	}
	return times, opts, nil
}

// parseDays parses comma-separated days into a lowercased, trimmed, deduplicated list.
func parseDays(s string) ([]string, error) {
	var days []string
	seen := map[string]bool{}
	for _, d := range strings.Split(strings.ToLower(s), ",") {
		d = strings.TrimSpace(d)
		if _, ok := dayCron[d]; !ok {
			return nil, fmt.Errorf("不正な曜日です: %s", d)
		}
		if !seen[d] {
			seen[d] = true
			days = append(days, d)
		}
	}
	return days, nil
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
	specs, err := parseTimes(times)
	if err != nil {
		return err
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

func runRemove(args []string) error {
	specs, err := parseRemoveArgs(args)
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return removeWindows(specs)
	}
	return removeCron(specs)
}

// parseRemoveArgs parses the arguments of remove. It returns nil (all jobs) for "all".
func parseRemoveArgs(args []string) ([]timeSpec, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("削除する時刻か all を指定してください (例: remove 07:00 / remove all)")
	}
	for _, a := range args {
		if a == "all" {
			if len(args) != 1 {
				return nil, fmt.Errorf("all と時刻は同時に指定できません")
			}
			return nil, nil
		}
	}
	return parseTimes(args)
}

// parseTimes parses time strings and returns them without duplicates.
func parseTimes(times []string) ([]timeSpec, error) {
	var specs []timeSpec
	seen := map[timeSpec]bool{}
	for _, t := range times {
		ts, err := parseTime(t)
		if err != nil {
			return nil, err
		}
		if !seen[ts] {
			seen[ts] = true
			specs = append(specs, ts)
		}
	}
	return specs, nil
}

func warnNotRegistered(ts timeSpec) {
	fmt.Fprintf(os.Stderr, "警告: %02d:%02d のジョブは登録されていません。\n", ts.hour, ts.minute)
}

// ---------- Linux / macOS (cron) ----------

func readCrontab() (string, error) {
	out, err := runCommand("", "crontab", "-l")
	if err != nil {
		// With no crontab set, this fails with "no crontab for <user>", so treat it as empty.
		// Any other failure is an error: treating it as empty would wipe the existing crontab.
		if strings.Contains(err.Error(), "no crontab") {
			return "", nil
		}
		return "", fmt.Errorf("crontabの読み込みに失敗しました: %w", err)
	}
	return string(out), nil
}

func writeCrontab(content string) error {
	if _, err := runCommand(content, "crontab", "-"); err != nil {
		return fmt.Errorf("crontabの書き込みに失敗しました: %w", err)
	}
	return nil
}

func isCronJobLine(l string) bool {
	return strings.HasSuffix(strings.TrimSpace(l), "# "+marker)
}

// cronLineTime extracts the time (minute and hour fields) from one of this tool's cron lines.
func cronLineTime(l string) (timeSpec, bool) {
	f := strings.Fields(l)
	if len(f) < 2 {
		return timeSpec{}, false
	}
	mi, err1 := strconv.Atoi(f[0])
	h, err2 := strconv.Atoi(f[1])
	if err1 != nil || err2 != nil {
		return timeSpec{}, false
	}
	return timeSpec{hour: h, minute: mi}, true
}

// shellQuote wraps s in single quotes for POSIX sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// buildShellCommand builds the command part of a crontab line.
// cron runs jobs with a minimal PATH, so claudeDir (if known) is prepended to PATH.
func buildShellCommand(opts options, claudeDir string) string {
	cmd := "claude --model " + pingModel + " -p " + shellQuote(opts.prompt)
	if claudeDir != "" {
		cmd = "PATH=" + shellQuote(claudeDir) + `:"$PATH" ` + cmd
	}
	if opts.log != "" {
		cmd += " >> " + shellQuote(opts.log) + " 2>&1"
	}
	// Escape '%' because crontab interprets it as a newline.
	return strings.ReplaceAll(cmd, "%", `\%`)
}

func buildCronLines(specs []timeSpec, opts options, claudeDir string) []string {
	var dayNums []string
	for _, d := range opts.days {
		dayNums = append(dayNums, dayCron[d])
	}
	dayStr := strings.Join(dayNums, ",")
	shellCmd := buildShellCommand(opts, claudeDir)

	var lines []string
	for _, ts := range specs {
		lines = append(lines, fmt.Sprintf("%d %d * * %s %s # %s", ts.minute, ts.hour, dayStr, shellCmd, marker))
	}
	return lines
}

// mergeCrontab appends new lines to the existing crontab.
// Existing jobs at the same times are replaced; all other lines (jobs at other times,
// blank lines and comments) are kept.
func mergeCrontab(existing string, specs []timeSpec, newLines []string) string {
	kept, _, _ := removeCronJobs(existing, specs)
	return joinLines(append(trimTrailingBlank(strings.Split(kept, "\n")), newLines...))
}

// removeCronJobs removes this tool's jobs from the existing crontab.
// If specs is nil, all jobs are removed; otherwise only jobs at the given times.
// It returns the new content, the number of removed jobs, and the times actually found.
func removeCronJobs(existing string, specs []timeSpec) (string, int, map[timeSpec]bool) {
	target := map[timeSpec]bool{}
	for _, ts := range specs {
		target[ts] = true
	}
	var kept []string
	removed := 0
	found := map[timeSpec]bool{}
	for _, l := range strings.Split(existing, "\n") {
		if isCronJobLine(l) {
			ts, ok := cronLineTime(l)
			if specs == nil || (ok && target[ts]) {
				removed++
				found[ts] = true
				continue
			}
		}
		kept = append(kept, l)
	}
	return joinLines(trimTrailingBlank(kept)), removed, found
}

func trimTrailingBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func addCron(specs []timeSpec, opts options) error {
	existing, err := readCrontab()
	if err != nil {
		return err
	}

	claudeDir := ""
	if p, err := lookPath("claude"); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			claudeDir = filepath.Dir(abs)
		}
	} else {
		fmt.Fprintln(os.Stderr, "警告: claude コマンドがPATHに見つかりません。cron実行時に失敗する可能性があります。")
	}

	newLines := buildCronLines(specs, opts, claudeDir)
	if err := writeCrontab(mergeCrontab(existing, specs, newLines)); err != nil {
		return err
	}

	fmt.Printf("%d件のcronジョブを登録しました:\n", len(newLines))
	for _, l := range newLines {
		fmt.Println("  " + l)
	}
	return nil
}

func removeCron(specs []timeSpec) error {
	existing, err := readCrontab()
	if err != nil {
		return err
	}
	content, removed, found := removeCronJobs(existing, specs)
	for _, ts := range specs {
		if !found[ts] {
			warnNotRegistered(ts)
		}
	}
	if removed == 0 {
		if specs == nil {
			fmt.Println("登録されているジョブはありません。")
		} else {
			fmt.Println("削除対象のジョブはありません。")
		}
		return nil
	}
	if err := writeCrontab(content); err != nil {
		return err
	}
	fmt.Printf("%d件のcronジョブを削除しました。\n", removed)
	return nil
}

func listCron() error {
	existing, err := readCrontab()
	if err != nil {
		return err
	}
	var lines []string
	for _, l := range strings.Split(existing, "\n") {
		if isCronJobLine(l) {
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

// buildBatContent builds the batch file run by the task.
// The file is written as UTF-8, so it switches the code page to 65001 (UTF-8) first.
func buildBatContent(opts options) string {
	// Escape '%' as '%%' because batch files expand it as a variable.
	esc := func(s string) string { return strings.ReplaceAll(s, "%", "%%") }
	content := "@echo off\r\nchcp 65001 >nul\r\nclaude --model " + pingModel + " -p \"" + esc(opts.prompt) + "\""
	if opts.log != "" {
		content += " >> \"" + esc(opts.log) + "\" 2>&1"
	}
	return content + "\r\n"
}

func addWindows(specs []timeSpec, opts options) error {
	dir, err := winScriptDir()
	if err != nil {
		return err
	}

	var dayNames []string
	for _, d := range opts.days {
		dayNames = append(dayNames, dayWin[d])
	}
	dayStr := strings.Join(dayNames, ",")

	for _, ts := range specs {
		name := taskName(ts)
		batPath := filepath.Join(dir, name+".bat")

		if err := os.WriteFile(batPath, []byte(buildBatContent(opts)), 0o644); err != nil {
			return fmt.Errorf("バッチファイルの作成に失敗しました: %w", err)
		}

		timeStr := fmt.Sprintf("%02d:%02d", ts.hour, ts.minute)
		// Quote the path so it is treated as a single command even if it contains spaces.
		// /f overwrites an existing task with the same name.
		createArgs := []string{
			"/create", "/tn", name, "/tr", `"` + batPath + `"`,
			"/sc", "weekly", "/d", dayStr, "/st", timeStr, "/f",
		}
		if _, err := runCommand("", "schtasks", createArgs...); err != nil {
			return fmt.Errorf("schtasksの実行に失敗しました (%s): %w", name, err)
		}
		fmt.Printf("  %s (%s %s)\n", name, dayStr, timeStr)
	}

	fmt.Printf("%d件のタスクを登録しました。(バッチファイル格納先: %s)\n", len(specs), dir)
	return nil
}

// taskNameRe matches the first column (task name) of `schtasks /query /fo CSV /nh`.
// CSV is used because LIST-format labels (e.g. "TaskName:") change with the OS display language.
var taskNameRe = regexp.MustCompile(`(?m)^"\\?(ClaudePing_\d{4})"`)

func queryClaudePingTasks() ([]string, error) {
	out, err := runCommand("", "schtasks", "/query", "/fo", "CSV", "/nh")
	// schtasks may exit non-zero if any single task is corrupt, but stdout still
	// contains the tasks it could read, so parse it anyway.
	if err != nil && len(bytes.TrimSpace(out)) == 0 {
		return nil, fmt.Errorf("タスク一覧の取得に失敗しました: %w", err)
	}
	return parseTaskNames(string(out)), nil
}

func parseTaskNames(out string) []string {
	matches := taskNameRe.FindAllStringSubmatch(out, -1)
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

// removeWindows deletes this tool's tasks. If specs is nil, all tasks are deleted.
func removeWindows(specs []timeSpec) error {
	names, err := queryClaudePingTasks()
	if err != nil {
		return err
	}
	if specs != nil {
		registered := map[string]bool{}
		for _, n := range names {
			registered[n] = true
		}
		names = nil
		for _, ts := range specs {
			if n := taskName(ts); registered[n] {
				names = append(names, n)
			} else {
				warnNotRegistered(ts)
			}
		}
	}
	if len(names) == 0 {
		if specs == nil {
			fmt.Println("登録されているジョブはありません。")
		} else {
			fmt.Println("削除対象のジョブはありません。")
		}
		return nil
	}
	dir, _ := winScriptDir()
	var failed []string
	for _, name := range names {
		if _, err := runCommand("", "schtasks", "/delete", "/tn", name, "/f"); err != nil {
			fmt.Fprintf(os.Stderr, "削除に失敗しました: %s: %v\n", name, err)
			failed = append(failed, name)
			continue
		}
		fmt.Println("削除しました: " + name)
		if dir != "" {
			_ = os.Remove(filepath.Join(dir, name+".bat"))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d件のタスクを削除できませんでした: %s", len(failed), strings.Join(failed, ", "))
	}
	return nil
}

func listWindows() error {
	names, err := queryClaudePingTasks()
	if err != nil {
		return err
	}
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

// ---------- help ----------

func printHelp() {
	fmt.Println(`claude-scheduler - Claude Codeの利用枠を狙ったタイミングで開始させるスケジューラ

使い方:
  claude-scheduler add <時刻...> [オプション]
  claude-scheduler remove <時刻...> | all
  claude-scheduler list

例:
  claude-scheduler add 07:00 12:00 17:00
  claude-scheduler add 07:00 12:00 17:00 --days mon,tue,wed,thu,fri
  claude-scheduler add 07:00 --prompt "hello" --log C:\Users\you\claude-ping.log
  claude-scheduler remove 07:00 12:00
  claude-scheduler remove all
  claude-scheduler list

オプション:
  --days <list>    カンマ区切りの曜日 (sun,mon,tue,wed,thu,fri,sat)
                   デフォルト: mon,tue,wed,thu,fri
  --prompt <text>  送信するプロンプト文字列。デフォルト: "ok"
  --log <path>     実行結果を追記するログファイルのパス

add は同じ時刻のジョブを上書きし、それ以外の時刻のジョブは残します。
remove は指定した時刻のジョブを削除します。すべて削除するには remove all を使ってください。

注意:
  - 事前に 'claude' コマンドがPATHに通っている必要があります。
  - 各pingは実際にAPIを消費するため利用量にわずかに反映されます。
  - 利用枠の起算/リセットの正確な仕様は変更される可能性があるため、
    最新情報は公式ドキュメントで確認してください。
    https://code.claude.com/docs/ja/overview`)
}
