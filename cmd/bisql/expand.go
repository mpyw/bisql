package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/mpyw/bisql"
	"github.com/urfave/cli/v3"
)

func expandCommand() *cli.Command {
	return &cli.Command{
		Name:  "expand",
		Usage: "Resolve @include directives and write the expanded two-way SQL",
		UsageText: "Filter mode — one template to standard output or a file:\n" +
			"  bisql expand [--include-root DIR] [--output FILE] [template.sql|-]\n" +
			"\n" +
			"Tree mode — expand every *.sql under the root into a directory:\n" +
			"  bisql expand [--include-root DIR] [--exclude GLOB]... --out-dir DIR",
		Description: "Resolves /*%! @include ... */ directives and writes the expanded, still-two-way\n" +
			"SQL. Include names resolve under --include-root, as the library's FSLoader does;\n" +
			"expansion exits non-zero on an unresolved include, so a run also validates.\n\n" +
			"Filter mode reads one template (a file or standard input) to standard output, or\n" +
			"to --output. Tree mode (--out-dir) expands every *.sql under --include-root in one\n" +
			"process, mirroring the tree — the form for go generate. --exclude omits fragment\n" +
			"files from that output while still allowing them to be @included.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "include-root", Aliases: []string{"r"}, Value: ".", Usage: "Base `directory` for @include resolution (and the tree-mode source)"},
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "Filter mode: write to `file` instead of standard output"},
			&cli.StringFlag{Name: "out-dir", Aliases: []string{"O"}, Usage: "Tree mode: mirror the expanded tree into `directory`"},
			&cli.StringSliceFlag{Name: "exclude", Aliases: []string{"x"}, Usage: "Tree mode: omit files matching `glob` from the output (repeatable; still @includable). A slashless pattern matches the base name at any depth."},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return runExpand(expandOptions{
				root:    cmd.String("include-root"),
				inputs:  cmd.Args().Slice(),
				output:  cmd.String("output"),
				outDir:  cmd.String("out-dir"),
				exclude: cmd.StringSlice("exclude"),
			}, os.Stdin, os.Stdout)
		},
	}
}

type expandOptions struct {
	root    string   // base directory for @include resolution (and the tree-mode source root)
	inputs  []string // filter mode: zero or one template path ("" / "-" => stdin)
	output  string   // filter mode: -o target file ("" => stdout)
	outDir  string   // tree mode: --out-dir destination ("" => filter mode)
	exclude []string // tree mode: globs whose matches are omitted from the output
}

func runExpand(opts expandOptions, stdin io.Reader, stdout io.Writer) error {
	if opts.output != "" && opts.outDir != "" {
		return fmt.Errorf("--output (a single file) and --out-dir (a tree) are mutually exclusive")
	}
	if opts.outDir != "" {
		return runExpandTree(opts)
	}
	return runExpandFilter(opts, stdin, stdout)
}

// runExpandFilter handles the one-in/one-out case.
func runExpandFilter(opts expandOptions, stdin io.Reader, stdout io.Writer) error {
	if len(opts.inputs) > 1 {
		return fmt.Errorf("filter mode accepts at most one template (received %d); use --out-dir to expand a tree", len(opts.inputs))
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
		return fmt.Errorf("--out-dir expands the entire --include-root tree and accepts no file arguments")
	}
	rels, err := sqlFilesUnder(opts.root)
	if err != nil {
		return err
	}
	if len(rels) == 0 {
		return fmt.Errorf("no .sql templates found under %s", opts.root)
	}
	for _, rel := range rels {
		// An excluded file is skipped from the output but still resolvable as an @include,
		// since fragments are loaded from --include-root, not from this walk.
		skip, err := matchesAny(rel, opts.exclude)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
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

// matchesAny reports whether the slash path rel matches any of the glob patterns. A pattern
// containing a slash is matched against the whole path (with ** spanning directories); a
// slashless pattern is matched against the base name at any depth (the gitignore convention).
func matchesAny(rel string, patterns []string) (bool, error) {
	for _, p := range patterns {
		name := rel
		if !strings.Contains(p, "/") {
			name = path.Base(rel)
		}
		ok, err := doublestar.Match(p, name)
		if err != nil {
			return false, fmt.Errorf("invalid --exclude pattern %q: %w", p, err)
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
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
