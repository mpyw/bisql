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
			"resolves them, so the output matches what the application sees. Expansion fails\n" +
			"(non-zero exit) if an @include cannot be resolved, so a plain run doubles as a check.\n\n" +
			"Filter mode (default): read one template (a file, or stdin) and write the result to\n" +
			"stdout or, with -o, a single file.\n\n" +
			"Tree mode (--out-dir): expand every *.sql under --root in a single process and\n" +
			"mirror the results into the output directory (same relative paths). This is the\n" +
			"form to use from go generate, so a whole directory costs one process, not one per\n" +
			"file. To gate CI on the committed output, regenerate and let git detect drift\n" +
			"(bisql expand --out-dir gen && git diff --exit-code gen).",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "root", Value: ".", Usage: "base `directory` for @include resolution (and, in tree mode, the source tree)"},
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "filter mode: write the result to this `file` instead of stdout"},
			&cli.StringFlag{Name: "out-dir", Usage: "tree mode: mirror the expanded --root tree into this `directory`"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return runExpand(expandOptions{
				root:   cmd.String("root"),
				inputs: cmd.Args().Slice(),
				output: cmd.String("output"),
				outDir: cmd.String("out-dir"),
			}, os.Stdin, os.Stdout)
		},
	}
}

type expandOptions struct {
	root   string   // base directory for @include resolution (and the tree-mode source root)
	inputs []string // filter mode: zero or one template path ("" / "-" => stdin)
	output string   // filter mode: -o target file ("" => stdout)
	outDir string   // tree mode: --out-dir destination ("" => filter mode)
}

func runExpand(opts expandOptions, stdin io.Reader, stdout io.Writer) error {
	if opts.output != "" && opts.outDir != "" {
		return fmt.Errorf("-o (a single file) and --out-dir (a tree) are mutually exclusive")
	}
	if opts.outDir != "" {
		return runExpandTree(opts)
	}
	return runExpandFilter(opts, stdin, stdout)
}

// runExpandFilter handles the one-in/one-out case.
func runExpandFilter(opts expandOptions, stdin io.Reader, stdout io.Writer) error {
	if len(opts.inputs) > 1 {
		return fmt.Errorf("filter mode takes at most one template (got %d); use --out-dir for a tree", len(opts.inputs))
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
	if opts.output != "" {
		return writeFile(opts.output, expanded)
	}
	_, err = io.WriteString(stdout, expanded)
	return err
}

// runExpandTree expands every *.sql under root in one process and mirrors the results into
// out-dir, preserving relative paths.
func runExpandTree(opts expandOptions) error {
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
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(opts.root, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		expanded, err := expandText(string(src), opts.root)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		if err := writeFile(filepath.Join(opts.outDir, filepath.FromSlash(rel)), expanded); err != nil {
			return err
		}
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

func writeFile(path, content string) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
