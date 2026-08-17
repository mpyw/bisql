package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExpandCommand_RejectsPositionalArgs exercises the guard in expandCommand's Action:
// expand reads its template from standard input, so any positional argument is an error.
func TestExpandCommand_RejectsPositionalArgs(t *testing.T) {
	cmd := expandCommand()
	err := cmd.Run(context.Background(), []string{"expand", "unexpected.sql"})
	if err == nil {
		t.Fatalf("expected an error when a positional argument is passed")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no arguments") && !strings.Contains(msg, "standard input") {
		t.Errorf("err = %v, want it to mention that no arguments are accepted / standard input", err)
	}
}

// TestExpandCommand_HappyPath is a smoke test: with no positional argument and a simple
// template (no @include) on standard input, the command writes the template unchanged to
// standard output and returns no error. os.Stdin/os.Stdout are swapped for temp files so the
// wired-in streams can be driven and inspected without changing production behavior.
func TestExpandCommand_HappyPath(t *testing.T) {
	const src = "select 1"

	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.sql")
	outPath := filepath.Join(dir, "out.sql")
	if err := os.WriteFile(inPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	in, err := os.Open(inPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Close() }()

	origIn, origOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = in, out
	defer func() { os.Stdin, os.Stdout = origIn, origOut }()

	cmd := expandCommand()
	if err := cmd.Run(context.Background(), []string{"expand"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != src {
		t.Errorf("out = %q, want %q", string(got), src)
	}
}
