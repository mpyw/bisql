package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mpyw/bisql"
	"github.com/urfave/cli/v3"
)

func expandCommand() *cli.Command {
	return &cli.Command{
		Name:      "expand",
		Usage:     "resolve @include directives and print the expanded two-way SQL",
		ArgsUsage: "[template.sql]",
		Description: "Resolves every /*%! @include ... */ directive in the template and writes the\n" +
			"expanded text. All other directives are left intact, so the result is still a\n" +
			"runnable two-way template — suitable for committing snapshots or running through\n" +
			"EXPLAIN. Include names resolve under --root, exactly as the library's FSLoader\n" +
			"resolves them, so the output matches what the application sees.\n\n" +
			"The template is read from the given file, or from stdin when no file (or -) is\n" +
			"given. This is a one-in/one-out filter; to expand many files, invoke it per file\n" +
			"(a shell loop, or one //go:generate line each).",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "root", Value: ".", Usage: "base `directory` for @include resolution"},
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "write the result to this `file` instead of stdout"},
			&cli.StringFlag{Name: "check", Usage: "compare the result to this `file`; write nothing, and exit non-zero on any difference"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) > 1 {
				return fmt.Errorf("expand takes at most one template (got %d); expand one file at a time", len(args))
			}
			var input string
			if len(args) == 1 {
				input = args[0]
			}
			return runExpand(expandOptions{
				root:   cmd.String("root"),
				input:  input,
				output: cmd.String("output"),
				check:  cmd.String("check"),
			}, os.Stdin, os.Stdout)
		},
	}
}

type expandOptions struct {
	root   string // base directory for @include resolution
	input  string // template path; empty or "-" means stdin
	output string // -o target file; empty means stdout
	check  string // --check target file; empty means no check
}

// runExpand executes the expand command as a one-in/one-out filter. It is decoupled from
// urfave/cli (its streams are parameters) so it can be unit-tested directly.
func runExpand(opts expandOptions, stdin io.Reader, stdout io.Writer) error {
	if opts.output != "" && opts.check != "" {
		return fmt.Errorf("-o and --check are mutually exclusive")
	}

	src, err := readInput(opts.input, stdin)
	if err != nil {
		return err
	}
	expanded, err := bisql.Expand(string(src), bisql.WithLoader(bisql.NewFSLoader(os.DirFS(opts.root))))
	if err != nil {
		return err
	}

	switch {
	case opts.check != "":
		return checkAgainst(opts.check, expanded)
	case opts.output != "":
		return writeFile(opts.output, expanded)
	default:
		_, err := io.WriteString(stdout, expanded)
		return err
	}
}

func readInput(path string, stdin io.Reader) ([]byte, error) {
	if path == "" || path == "-" {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
		return b, nil
	}
	return os.ReadFile(path)
}

// checkAgainst compares want against the current contents of path, returning an error (for a
// non-zero exit) when they differ or path is absent. It writes nothing.
func checkAgainst(path, want string) error {
	got, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("out of date: %s does not exist (run: bisql expand -o %s ...)", path, path)
		}
		return err
	}
	if string(got) != want {
		return fmt.Errorf("out of date: %s (re-run: bisql expand -o %s ...)", path, path)
	}
	return nil
}

func writeFile(path, content string) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
