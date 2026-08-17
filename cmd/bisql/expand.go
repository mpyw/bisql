package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mpyw/bisql"
	"github.com/urfave/cli/v3"
)

func expandCommand() *cli.Command {
	return &cli.Command{
		Name:      "expand",
		Usage:     "resolve @include directives and write the expanded two-way SQL",
		ArgsUsage: "[template.sql]",
		Description: "Resolves every /*%! @include ... */ directive and writes the expanded text.\n" +
			"All other directives are left intact, so the result is still a runnable two-way\n" +
			"template. Include names resolve under --root, exactly as the library's FSLoader\n" +
			"resolves them, so the output matches what the application sees.\n\n" +
			"Filter mode (default): read one template (a file, or stdin) and write the result to\n" +
			"stdout or, with -o, a single file.\n\n" +
			"Tree mode (--out-dir): expand every *.sql under --root in a single process and\n" +
			"mirror the results into the output directory (same relative paths). This is the\n" +
			"form to use from go generate, so a whole directory costs one process, not one per\n" +
			"file.\n\n" +
			"With --check nothing is written; the command exits non-zero if the target (the -o\n" +
			"file, or the --out-dir tree) is out of date.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "root", Value: ".", Usage: "base `directory` for @include resolution (and, in tree mode, the source tree)"},
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "filter mode: write the result to this `file` instead of stdout"},
			&cli.StringFlag{Name: "out-dir", Usage: "tree mode: mirror the expanded --root tree into this `directory`"},
			&cli.BoolFlag{Name: "check", Usage: "write nothing; exit non-zero if the output target is out of date"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return runExpand(expandOptions{
				root:   cmd.String("root"),
				inputs: cmd.Args().Slice(),
				output: cmd.String("output"),
				outDir: cmd.String("out-dir"),
				check:  cmd.Bool("check"),
			}, os.Stdin, os.Stdout, os.Stderr)
		},
	}
}

type expandOptions struct {
	root   string   // base directory for @include resolution (and the tree-mode source root)
	inputs []string // filter mode: zero or one template path ("" / "-" => stdin)
	output string   // filter mode: -o target file ("" => stdout)
	outDir string   // tree mode: --out-dir destination ("" => filter mode)
	check  bool     // write nothing; verify the target instead
}

func runExpand(opts expandOptions, stdin io.Reader, stdout, stderr io.Writer) error {
	if opts.output != "" && opts.outDir != "" {
		return fmt.Errorf("-o (a single file) and --out-dir (a tree) are mutually exclusive")
	}
	if opts.outDir != "" {
		return runExpandTree(opts, stderr)
	}
	return runExpandFilter(opts, stdin, stdout)
}

// runExpandFilter handles the one-in/one-out case.
func runExpandFilter(opts expandOptions, stdin io.Reader, stdout io.Writer) error {
	if len(opts.inputs) > 1 {
		return fmt.Errorf("filter mode takes at most one template (got %d); use --out-dir for a tree", len(opts.inputs))
	}
	if opts.check && opts.output == "" {
		return fmt.Errorf("--check needs a target: pass -o FILE (or use --out-dir for a tree)")
	}

	var input string
	if len(opts.inputs) == 1 {
		input = opts.inputs[0]
	}
	src, err := readInput(input, stdin)
	if err != nil {
		return err
	}
	expanded, err := expandText(string(src), opts.root)
	if err != nil {
		return err
	}

	switch {
	case opts.check:
		ok, err := fileHasContent(opts.output, expanded)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("out of date: %s (re-run without --check)", opts.output)
		}
		return nil
	case opts.output != "":
		return writeFile(opts.output, expanded)
	default:
		_, err := io.WriteString(stdout, expanded)
		return err
	}
}

// runExpandTree expands every *.sql under root in one process and mirrors the results into
// out-dir (or, with --check, verifies them).
func runExpandTree(opts expandOptions, stderr io.Writer) error {
	if len(opts.inputs) > 0 {
		return fmt.Errorf("--out-dir expands the whole --root tree and takes no file arguments")
	}
	rels, err := sqlFilesUnder(opts.root)
	if err != nil {
		return err
	}
	if len(rels) == 0 {
		return fmt.Errorf("no .sql templates found under %s", opts.root)
	}

	var drifted []string
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(opts.root, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		expanded, err := expandText(string(src), opts.root)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		target := filepath.Join(opts.outDir, filepath.FromSlash(rel))
		if opts.check {
			ok, err := fileHasContent(target, expanded)
			if err != nil {
				return err
			}
			if !ok {
				drifted = append(drifted, target)
			}
			continue
		}
		if err := writeFile(target, expanded); err != nil {
			return err
		}
	}

	if len(drifted) > 0 {
		_, _ = fmt.Fprintf(stderr, "out of date (re-run: bisql expand -root %s --out-dir %s):\n", opts.root, opts.outDir)
		for _, p := range drifted {
			_, _ = fmt.Fprintln(stderr, "  "+p)
		}
		return fmt.Errorf("%d file(s) out of date", len(drifted))
	}
	return nil
}

// expandText resolves @include in src, with fragments loaded from root (matching the library).
func expandText(src, root string) (string, error) {
	return bisql.Expand(src, bisql.WithLoader(bisql.NewFSLoader(os.DirFS(root))))
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

// sqlFilesUnder returns the *.sql files below root as sorted slash paths relative to root.
func sqlFilesUnder(root string) ([]string, error) {
	var rels []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".sql") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rels = append(rels, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(rels)
	return rels, nil
}

// fileHasContent reports whether the file at path exists and already equals want.
func fileHasContent(path, want string) (bool, error) {
	got, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return string(got) == want, nil
}

func writeFile(path, content string) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
