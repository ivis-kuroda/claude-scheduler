package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

type call struct {
	stdin string
	args  string // "name arg1 arg2 ..."
}

// stubCommand replaces runCommand with f and returns the recorded calls.
func stubCommand(t *testing.T, f func(stdin, name string, args ...string) ([]byte, error)) *[]call {
	t.Helper()
	var calls []call
	orig := runCommand
	runCommand = func(stdin, name string, args ...string) ([]byte, error) {
		calls = append(calls, call{stdin, strings.Join(append([]string{name}, args...), " ")})
		return f(stdin, name, args...)
	}
	t.Cleanup(func() { runCommand = orig })
	return &calls
}

// fakeCrontab emulates `crontab -l` / `crontab -` in memory.
func fakeCrontab(t *testing.T, initial string, exists bool) (*string, *[]call) {
	t.Helper()
	content := initial
	calls := stubCommand(t, func(stdin, name string, args ...string) ([]byte, error) {
		if name != "crontab" {
			t.Fatalf("unexpected command: %s", name)
		}
		if args[0] == "-l" {
			if !exists {
				return nil, errString("exit status 1: no crontab for vscode")
			}
			return []byte(content), nil
		}
		content, exists = stdin, true
		return nil, nil
	})
	return &content, calls
}

type errString string

func (e errString) Error() string { return string(e) }

func stubLookPath(t *testing.T, path string, err error) {
	t.Helper()
	orig := lookPath
	lookPath = func(string) (string, error) { return path, err }
	t.Cleanup(func() { lookPath = orig })
}

func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	f()
	w.Close()
	b, _ := io.ReadAll(r)
	return string(b)
}
