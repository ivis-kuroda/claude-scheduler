package main

import (
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// ---------- argument parsing ----------

func TestParseTime(t *testing.T) {
	valid := map[string]timeSpec{
		"07:00": {7, 0},
		"7:05":  {7, 5},
		"00:00": {0, 0},
		"23:59": {23, 59},
	}
	for in, want := range valid {
		got, err := parseTime(in)
		if err != nil || got != want {
			t.Errorf("parseTime(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"24:00", "12:60", "7", "07:0", "", "ab:cd", "07:00:00"} {
		if _, err := parseTime(in); err == nil {
			t.Errorf("parseTime(%q) should fail", in)
		}
	}
}

func TestParseOptionsDefaults(t *testing.T) {
	times, opts, err := parseOptions([]string{"07:00", "12:00"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(times, []string{"07:00", "12:00"}) {
		t.Errorf("times = %v", times)
	}
	if !reflect.DeepEqual(opts.days, []string{"mon", "tue", "wed", "thu", "fri"}) || opts.prompt != "ok" || opts.log != "" {
		t.Errorf("unexpected defaults: %+v", opts)
	}
}

func TestParseOptionsFlags(t *testing.T) {
	times, opts, err := parseOptions([]string{"--days", "SAT, Sun,sat", "07:00", "--prompt", "hi", "--log", `C:\log.txt`})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(times, []string{"07:00"}) {
		t.Errorf("times = %v", times)
	}
	if !reflect.DeepEqual(opts.days, []string{"sat", "sun"}) || opts.prompt != "hi" || opts.log != `C:\log.txt` {
		t.Errorf("unexpected opts: %+v", opts)
	}
}

func TestParseOptionsErrors(t *testing.T) {
	cases := [][]string{
		{"--days"},
		{"--prompt"},
		{"--log"},
		{"--days", "mon,xyz"},
		{"--days", "mon,"},
		{"--unknown"},
		{"--prompt", `say "hi"`},
		{"--prompt", "a\nb"},
		{"--log", "a\r\nb"},
	}
	for _, args := range cases {
		if _, _, err := parseOptions(args); err == nil {
			t.Errorf("parseOptions(%q) should fail", args)
		}
	}
}

// ---------- cron ----------

func TestBuildCronLines(t *testing.T) {
	got := buildCronLines(
		[]timeSpec{{7, 0}, {12, 30}},
		options{days: []string{"mon", "wed"}, prompt: "ok", log: "/tmp/p.log"},
		"/opt/claude/bin",
	)
	want := []string{
		`0 7 * * 1,3 PATH='/opt/claude/bin':"$PATH" claude -p 'ok' >> '/tmp/p.log' 2>&1 # claude-ping-scheduler`,
		`30 12 * * 1,3 PATH='/opt/claude/bin':"$PATH" claude -p 'ok' >> '/tmp/p.log' 2>&1 # claude-ping-scheduler`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestBuildShellCommandEscaping(t *testing.T) {
	got := buildShellCommand(options{prompt: "100% it's $(rm -rf ~)"}, "")
	want := `claude -p '100\% it'\''s $(rm -rf ~)'`
	if got != want {
		t.Errorf("got %s want %s", got, want)
	}
}

func TestMergeCrontabKeepsOtherLinesAndReplacesSameTime(t *testing.T) {
	existing := "# my jobs\n0 1 * * * backup\n\n0 7 * * 1 old # claude-ping-scheduler\n0 9 * * 1 keep # claude-ping-scheduler\n\n"
	got := mergeCrontab(existing, []timeSpec{{7, 0}}, []string{"0 7 * * 2 new # claude-ping-scheduler"})
	want := "# my jobs\n0 1 * * * backup\n\n0 9 * * 1 keep # claude-ping-scheduler\n0 7 * * 2 new # claude-ping-scheduler\n"
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

func TestRemoveCronJobsAll(t *testing.T) {
	existing := "0 1 * * * backup\n0 7 * * 1 x # claude-ping-scheduler\n# note mentions claude-ping-scheduler\n"
	got, n, _ := removeCronJobs(existing, nil)
	if n != 1 || got != "0 1 * * * backup\n# note mentions claude-ping-scheduler\n" {
		t.Errorf("got %q (%d)", got, n)
	}
	if got, n, _ := removeCronJobs("0 7 * * 1 x # claude-ping-scheduler\n", nil); got != "" || n != 1 {
		t.Errorf("got %q (%d)", got, n)
	}
}

func TestRemoveCronJobsByTime(t *testing.T) {
	existing := "0 7 * * * backup\n0 7 * * 1 a # claude-ping-scheduler\n30 12 * * 1 b # claude-ping-scheduler\n0 17 * * 1 c # claude-ping-scheduler\n"
	got, n, found := removeCronJobs(existing, []timeSpec{{7, 0}, {12, 30}, {9, 0}})
	want := "0 7 * * * backup\n0 17 * * 1 c # claude-ping-scheduler\n"
	if got != want || n != 2 {
		t.Errorf("got %q (%d)", got, n)
	}
	if !found[timeSpec{7, 0}] || !found[timeSpec{12, 30}] || found[timeSpec{9, 0}] {
		t.Errorf("found = %v", found)
	}
}

func TestParseRemoveArgs(t *testing.T) {
	if specs, err := parseRemoveArgs([]string{"all"}); err != nil || specs != nil {
		t.Errorf("all: %v, %v", specs, err)
	}
	specs, err := parseRemoveArgs([]string{"07:00", "12:30", "7:00"})
	if err != nil || !reflect.DeepEqual(specs, []timeSpec{{7, 0}, {12, 30}}) {
		t.Errorf("times: %v, %v", specs, err)
	}
	for _, args := range [][]string{{}, {"all", "07:00"}, {"07:00", "all"}, {"25:00"}, {"ALL"}} {
		if _, err := parseRemoveArgs(args); err == nil {
			t.Errorf("parseRemoveArgs(%q) should fail", args)
		}
	}
}

func TestAddCronEndToEnd(t *testing.T) {
	stubLookPath(t, "/opt/claude/bin/claude", nil)
	content, _ := fakeCrontab(t, "0 1 * * * backup\n", true)
	captureStdout(t, func() {
		if err := addCron([]timeSpec{{7, 0}}, options{days: []string{"mon"}, prompt: "ok"}); err != nil {
			t.Fatal(err)
		}
		if err := addCron([]timeSpec{{12, 0}}, options{days: []string{"mon"}, prompt: "ok"}); err != nil {
			t.Fatal(err)
		}
	})
	lines := strings.Split(strings.TrimSpace(*content), "\n")
	if len(lines) != 3 || lines[0] != "0 1 * * * backup" {
		t.Errorf("unexpected crontab:\n%s", *content)
	}
}

func TestAddCronWithoutExistingCrontab(t *testing.T) {
	stubLookPath(t, "", exec.ErrNotFound)
	content, _ := fakeCrontab(t, "", false)
	captureStdout(t, func() {
		if err := addCron([]timeSpec{{7, 0}}, options{days: []string{"mon"}, prompt: "ok"}); err != nil {
			t.Fatal(err)
		}
	})
	if *content != "0 7 * * 1 claude -p 'ok' # claude-ping-scheduler\n" {
		t.Errorf("unexpected crontab: %q", *content)
	}
}

// If `crontab -l` fails for a reason other than "no crontab", the existing crontab must not be overwritten.
func TestCronDoesNotOverwriteOnReadError(t *testing.T) {
	calls := stubCommand(t, func(stdin, name string, args ...string) ([]byte, error) {
		if args[0] == "-l" {
			return nil, errors.New("exit status 1: permission denied")
		}
		return nil, nil
	})
	stubLookPath(t, "", exec.ErrNotFound)
	if err := addCron([]timeSpec{{7, 0}}, options{days: []string{"mon"}, prompt: "ok"}); err == nil {
		t.Error("addCron should fail")
	}
	if err := removeCron(nil); err == nil {
		t.Error("removeCron should fail")
	}
	for _, c := range *calls {
		if c.args == "crontab -" {
			t.Errorf("crontab was written despite read error")
		}
	}
}

func TestRemoveCronEndToEnd(t *testing.T) {
	content, _ := fakeCrontab(t, "0 1 * * * backup\n0 7 * * 1 x # claude-ping-scheduler\n", true)
	out := captureStdout(t, func() {
		if err := removeCron(nil); err != nil {
			t.Fatal(err)
		}
	})
	if *content != "0 1 * * * backup\n" || !strings.Contains(out, "1件") {
		t.Errorf("content %q, out %q", *content, out)
	}
}

func TestRemoveCronByTimeEndToEnd(t *testing.T) {
	content, calls := fakeCrontab(t, "0 7 * * 1 a # claude-ping-scheduler\n0 12 * * 1 b # claude-ping-scheduler\n", true)
	captureStdout(t, func() {
		if err := removeCron([]timeSpec{{12, 0}}); err != nil {
			t.Fatal(err)
		}
	})
	if *content != "0 7 * * 1 a # claude-ping-scheduler\n" {
		t.Errorf("content %q", *content)
	}
	// When nothing matches, the crontab must not be rewritten.
	n := len(*calls)
	captureStdout(t, func() {
		if err := removeCron([]timeSpec{{9, 0}}); err != nil {
			t.Fatal(err)
		}
	})
	if len(*calls) != n+1 {
		t.Errorf("crontab should only be read: %v", (*calls)[n:])
	}
}

func TestListCronFiltersMarkerLines(t *testing.T) {
	fakeCrontab(t, "0 1 * * * other-job\n0 7 * * 1 claude -p 'ok' # claude-ping-scheduler\n", true)
	out := captureStdout(t, func() {
		if err := listCron(); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "claude-ping-scheduler") || strings.Contains(out, "other-job") {
		t.Errorf("unexpected list output:\n%s", out)
	}
}

// ---------- Windows (schtasks) ----------

func TestBuildBatContent(t *testing.T) {
	got := buildBatContent(options{prompt: "100% ok", log: `C:\Users\me\%TEMP%.log`})
	want := "@echo off\r\nchcp 65001 >nul\r\nclaude -p \"100%% ok\" >> \"C:\\Users\\me\\%%TEMP%%.log\" 2>&1\r\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// Excerpt of `schtasks /query /fo CSV /nh` output on English Windows.
const schtasksCSVEN = "\r\n" +
	`"\ClaudePing_1200","10/22/2026 12:00:00 PM","Ready"` + "\r\n" +
	`"\ClaudePing_0700","10/22/2026 7:00:00 AM","Ready"` + "\r\n" +
	`"\OtherTask","N/A","Disabled"` + "\r\n" +
	`"\Microsoft\Windows\ClaudePing_0900","N/A","Ready"` + "\r\n"

// Excerpt of `schtasks /query /fo CSV /nh` output on Japanese Windows.
const schtasksCSVJA = "\r\n" +
	`"\ClaudePing_0700","2026/10/22 7:00:00","準備完了"` + "\r\n" +
	`"\ClaudePing_1200","2026/10/22 12:00:00","準備完了"` + "\r\n"

func TestParseTaskNames(t *testing.T) {
	want := []string{"ClaudePing_0700", "ClaudePing_1200"}
	for lang, out := range map[string]string{"en": schtasksCSVEN, "ja": schtasksCSVJA} {
		if got := parseTaskNames(out); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v want %v", lang, got, want)
		}
	}
}

// schtasks /query may exit non-zero if any single task is corrupt,
// but stdout still contains the tasks it could read.
func TestQueryUsesOutputOnNonZeroExit(t *testing.T) {
	stubCommand(t, func(stdin, name string, args ...string) ([]byte, error) {
		return []byte(schtasksCSVEN), errors.New("exit status 1")
	})
	got, err := queryClaudePingTasks()
	if err != nil || len(got) != 2 {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestQueryFailsWithoutOutput(t *testing.T) {
	stubCommand(t, func(stdin, name string, args ...string) ([]byte, error) {
		return nil, errors.New("exit status 1")
	})
	if _, err := queryClaudePingTasks(); err == nil {
		t.Error("should fail")
	}
}

func TestListWindows(t *testing.T) {
	stubCommand(t, func(stdin, name string, args ...string) ([]byte, error) {
		return []byte(schtasksCSVJA), nil
	})
	out := captureStdout(t, func() {
		if err := listWindows(); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "ClaudePing_0700") || !strings.Contains(out, "ClaudePing_1200") {
		t.Errorf("unexpected list output:\n%s", out)
	}
}

func TestRemoveWindowsReportsFailures(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	calls := stubCommand(t, func(stdin, name string, args ...string) ([]byte, error) {
		if args[0] == "/query" {
			return []byte(schtasksCSVJA), nil
		}
		if args[2] == "ClaudePing_1200" {
			return nil, errors.New("access denied")
		}
		return nil, nil
	})
	var err error
	out := captureStdout(t, func() { err = removeWindows(nil) })
	if err == nil || !strings.Contains(err.Error(), "ClaudePing_1200") {
		t.Errorf("expected failure for ClaudePing_1200, got %v", err)
	}
	if !strings.Contains(out, "削除しました: ClaudePing_0700") {
		t.Errorf("unexpected output: %s", out)
	}
	if len(*calls) != 3 {
		t.Errorf("calls = %v", *calls)
	}
}

func TestAddWindowsQuotesTaskPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	calls := stubCommand(t, func(stdin, name string, args ...string) ([]byte, error) { return nil, nil })
	captureStdout(t, func() {
		if err := addWindows([]timeSpec{{7, 0}}, options{days: []string{"mon", "fri"}, prompt: "ok"}); err != nil {
			t.Fatal(err)
		}
	})
	if len(*calls) != 1 {
		t.Fatalf("calls = %v", *calls)
	}
	c := (*calls)[0].args
	if !strings.Contains(c, `/tr "`) || !strings.Contains(c, `ClaudePing_0700.bat"`) || !strings.Contains(c, "/d MON,FRI /st 07:00") {
		t.Errorf("unexpected schtasks args: %s", c)
	}
}

func TestRemoveWindowsByTime(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	calls := stubCommand(t, func(stdin, name string, args ...string) ([]byte, error) {
		if args[0] == "/query" {
			return []byte(schtasksCSVJA), nil
		}
		return nil, nil
	})
	captureStdout(t, func() {
		if err := removeWindows([]timeSpec{{12, 0}, {9, 0}}); err != nil {
			t.Fatal(err)
		}
	})
	if len(*calls) != 2 || (*calls)[1].args != "schtasks /delete /tn ClaudePing_1200 /f" {
		t.Errorf("calls = %v", *calls)
	}
}
