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
	"text/template"

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
			"Tree mode — expand the *.sql files under the root into a directory:\n" +
			"  bisql expand [--include-root DIR] [--out-name-format TMPL] [--exclude GLOB]... --out-dir DIR [GLOB...]",
		Description: "Resolves /*%! @include ... */ directives and writes the expanded, still-two-way\n" +
			"SQL. Include names resolve under --include-root, as the library's FSLoader does; an\n" +
			"unresolved include exits non-zero, so a run also validates.\n\n" +
			"Filter mode: one template (a file or standard input) to standard output or --output.\n\n" +
			"Tree mode (--out-dir): expand the *.sql files under --include-root in one process.\n" +
			"Positional GLOBs select inputs (default: all); --exclude removes files from the\n" +
			"output (they stay @includable). --out-name-format is a Go template naming each\n" +
			"output relative to --out-dir; its fields, for input employees/search.sql, are:\n\n" +
			"    {{.Path}}   employees/search.sql\n" +
			"    {{.Dir}}    employees\n" +
			"    {{.Base}}   search.sql\n" +
			"    {{.Name}}   search\n" +
			"    {{.Ext}}    .sql",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "include-root", Aliases: []string{"r"}, Value: ".", Usage: "Base `directory` for @include resolution (and the tree-mode source)"},
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "Filter mode: write to `file` instead of standard output"},
			&cli.StringFlag{Name: "out-dir", Aliases: []string{"O"}, Usage: "Tree mode: write the expanded tree into `directory`"},
			&cli.StringFlag{Name: "out-name-format", Value: "{{.Path}}", Usage: "Tree mode: Go `template` for each output path, relative to --out-dir (default mirrors the input)"},
			&cli.StringSliceFlag{Name: "exclude", Aliases: []string{"x"}, Usage: "Tree mode: omit files matching `glob` from the output (repeatable; still @includable)"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return runExpand(expandOptions{
				root:    cmd.String("include-root"),
				inputs:  cmd.Args().Slice(),
				output:  cmd.String("output"),
				outDir:  cmd.String("out-dir"),
				outName: cmd.String("out-name-format"),
				exclude: cmd.StringSlice("exclude"),
			}, os.Stdin, os.Stdout)
		},
	}
}

type expandOptions struct {
	root    string   // base directory for @include resolution (and the tree-mode source root)
	inputs  []string // filter mode: a template path ("" / "-" => stdin); tree mode: input globs
	output  string   // filter mode: -o target file ("" => stdout)
	outDir  string   // tree mode: --out-dir destination ("" => filter mode)
	outName string   // tree mode: Go template for each output path relative to out-dir
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

// runExpandTree expands the selected *.sql files under root in one process and writes the
// results into out-dir, naming each output with the --out-name template.
func runExpandTree(opts expandOptions) error {
	if opts.outName == "" {
		opts.outName = "{{.Path}}"
	}
	nameTmpl, err := template.New("out-name").Parse(opts.outName)
	if err != nil {
		return fmt.Errorf("invalid --out-name template: %w", err)
	}

	rels, err := selectInputs(opts.root, opts.inputs)
	if err != nil {
		return err
	}

	// Resolve every output path first, so an --out-name collision is reported before anything
	// is written.
	type job struct{ rel, target string }
	var jobs []job
	byTarget := map[string]string{}
	for _, rel := range rels {
		// An excluded file is dropped from the output but stays resolvable as an @include,
		// since fragments load from --include-root independently of this selection.
		skip, err := matchesAny(rel, opts.exclude)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
		outRel, err := renderOutName(nameTmpl, rel)
		if err != nil {
			return err
		}
		target := filepath.Join(opts.outDir, filepath.FromSlash(outRel))
		if prev, dup := byTarget[target]; dup {
			return fmt.Errorf("output collision: %s and %s both map to %s", prev, rel, outRel)
		}
		byTarget[target] = rel
		jobs = append(jobs, job{rel: rel, target: target})
	}
	if len(jobs) == 0 {
		return fmt.Errorf("no .sql templates to expand under %s", opts.root)
	}

	for _, j := range jobs {
		src, err := os.ReadFile(filepath.Join(opts.root, filepath.FromSlash(j.rel)))
		if err != nil {
			return err
		}
		expanded, err := expandText(string(src), opts.root)
		if err != nil {
			return fmt.Errorf("%s: %w", j.rel, err)
		}
		if err := writeFile(j.target, expanded); err != nil {
			return err
		}
	}
	return nil
}

// selectInputs returns the *.sql files under root (as sorted slash paths) that match any of
// the input globs; with no globs, every *.sql file is selected.
func selectInputs(root string, globs []string) ([]string, error) {
	all, err := sqlFilesUnder(root)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no .sql templates found under %s", root)
	}
	if len(globs) == 0 {
		return all, nil
	}
	var sel []string
	for _, rel := range all {
		ok, err := matchesAny(rel, globs)
		if err != nil {
			return nil, err
		}
		if ok {
			sel = append(sel, rel)
		}
	}
	if len(sel) == 0 {
		return nil, fmt.Errorf("no .sql files matched: %s", strings.Join(globs, ", "))
	}
	return sel, nil
}

// pathFields are the fields exposed to the --out-name template for one input path.
type pathFields struct {
	Path string // full relative slash path, e.g. "employees/search.sql"
	Dir  string // directory, e.g. "employees" ("." at the root)
	Base string // file name with extension, e.g. "search.sql"
	Name string // file name without the final extension, e.g. "search"
	Ext  string // final extension including the dot, e.g. ".sql"
}

// renderOutName evaluates the --out-name template for rel and returns the cleaned output path
// relative to out-dir, rejecting an empty or escaping (absolute or "..") result.
func renderOutName(t *template.Template, rel string) (string, error) {
	ext := path.Ext(rel)
	base := path.Base(rel)
	var b strings.Builder
	if err := t.Execute(&b, pathFields{
		Path: rel,
		Dir:  path.Dir(rel),
		Base: base,
		Name: strings.TrimSuffix(base, ext),
		Ext:  ext,
	}); err != nil {
		return "", fmt.Errorf("--out-name for %s: %w", rel, err)
	}
	out := path.Clean(strings.TrimSpace(b.String()))
	if out == "" || out == "." {
		return "", fmt.Errorf("--out-name produced an empty path for %s", rel)
	}
	if path.IsAbs(out) || out == ".." || strings.HasPrefix(out, "../") {
		return "", fmt.Errorf("--out-name produced a path outside --out-dir for %s: %q", rel, out)
	}
	return out, nil
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
