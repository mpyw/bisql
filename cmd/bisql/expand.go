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
		Usage:     "resolve @include directives and print the expanded two-way SQL",
		ArgsUsage: "[template.sql ... | directory | glob | -]",
		Description: "Resolves every /*%! @include ... */ directive in each input template and\n" +
			"emits the expanded text. All other directives are left intact, so the result is\n" +
			"still a runnable two-way template — suitable for committing snapshots or running\n" +
			"through EXPLAIN. Include names and input paths resolve under --root.\n\n" +
			"With no input (or -), the template is read from stdin and written to stdout.\n" +
			"A directory input is walked recursively for *.sql files.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "root", Value: ".", Usage: "base directory for templates and @include resolution"},
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "output `path`: a file (single input) or a directory (multiple inputs)"},
			&cli.BoolFlag{Name: "write", Aliases: []string{"w"}, Usage: "write each result back to its input file in place"},
			&cli.BoolFlag{Name: "check", Usage: "write nothing; exit non-zero if any output would differ (for CI / go generate)"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return runExpand(expandOptions{
				root:   cmd.String("root"),
				output: cmd.String("output"),
				write:  cmd.Bool("write"),
				check:  cmd.Bool("check"),
				inputs: cmd.Args().Slice(),
			}, os.Stdin, os.Stdout, os.Stderr)
		},
	}
}

type expandOptions struct {
	root   string
	output string
	write  bool
	check  bool
	inputs []string
}

// runExpand executes the expand command. It is decoupled from urfave/cli (its streams are
// parameters) so it can be unit-tested directly.
func runExpand(opts expandOptions, stdin io.Reader, stdout, stderr io.Writer) error {
	if opts.write && opts.check {
		return fmt.Errorf("-w and --check are mutually exclusive")
	}

	fsys := os.DirFS(opts.root)

	// stdin mode: no path inputs, or a single "-".
	if len(opts.inputs) == 0 || (len(opts.inputs) == 1 && opts.inputs[0] == "-") {
		return expandStdin(opts, fsys, stdin, stdout)
	}
	for _, in := range opts.inputs {
		if in == "-" {
			return fmt.Errorf("cannot mix stdin (-) with file inputs")
		}
	}

	files, err := collectInputs(opts.root, opts.inputs)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no .sql templates matched the given inputs")
	}
	return expandFiles(opts, fsys, files, stdout, stderr)
}

func expandStdin(opts expandOptions, fsys fs.FS, stdin io.Reader, stdout io.Writer) error {
	if opts.write {
		return fmt.Errorf("-w cannot be used with stdin")
	}
	if opts.check {
		return fmt.Errorf("--check needs file inputs, not stdin")
	}
	src, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}
	expanded, err := bisql.Expand(string(src), bisql.WithLoader(bisql.NewFSLoader(fsys)))
	if err != nil {
		return err
	}
	return writeTo(opts.output, stdout, expanded)
}

// expandFiles expands each input (a slash path relative to root) and routes the output
// according to opts (stdout, -o file, -o dir, -w, or --check).
func expandFiles(opts expandOptions, fsys fs.FS, files []string, stdout, stderr io.Writer) error {
	outIsDir := opts.output != "" && (len(files) > 1 || looksLikeDir(opts.output))
	if opts.output != "" && !outIsDir && len(files) > 1 {
		return fmt.Errorf("-o must name a directory when there are multiple inputs")
	}
	if opts.output == "" && !opts.write && !opts.check && len(files) > 1 {
		return fmt.Errorf("multiple inputs require -w (in place) or -o DIR")
	}

	var drifted []string
	for _, rel := range files {
		expanded, err := bisql.ExpandFile(fsys, rel)
		if err != nil {
			return err
		}

		switch {
		case opts.check:
			target := destPath(opts, rel, false)
			same, err := fileHasContent(target, expanded)
			if err != nil {
				return err
			}
			if !same {
				drifted = append(drifted, target)
			}
		case opts.write:
			if err := writeFile(filepath.Join(opts.root, filepath.FromSlash(rel)), expanded); err != nil {
				return err
			}
		case opts.output != "":
			if err := writeFile(destPath(opts, rel, outIsDir), expanded); err != nil {
				return err
			}
		default: // single input to stdout
			if _, err := io.WriteString(stdout, expanded); err != nil {
				return err
			}
		}
	}

	if len(drifted) > 0 {
		// Diagnostics to a sink; a write failure here is not worth masking the drift result.
		_, _ = fmt.Fprintln(stderr, "out of date (re-run bisql expand):")
		for _, p := range drifted {
			_, _ = fmt.Fprintln(stderr, "  "+p)
		}
		return quietExit{}
	}
	return nil
}

// destPath returns where the expanded output for input rel should go. For --check and -o DIR
// it mirrors rel under output; for -o FILE (single input) it is output itself; for -w it is
// the source path under root.
func destPath(opts expandOptions, rel string, outIsDir bool) string {
	switch {
	case opts.write:
		return filepath.Join(opts.root, filepath.FromSlash(rel))
	case opts.output != "" && outIsDir, opts.check && opts.output != "":
		return filepath.Join(opts.output, filepath.FromSlash(rel))
	case opts.output != "":
		return opts.output
	default:
		return filepath.Join(opts.root, filepath.FromSlash(rel))
	}
}

// collectInputs resolves the given inputs (files, directories, or globs) into a sorted,
// de-duplicated list of slash paths relative to root. A directory is walked recursively for
// *.sql files. Every resolved file must live under root (so @include and mirroring resolve).
func collectInputs(root string, inputs []string) ([]string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(abs string) error {
		rel, err := filepath.Rel(absRoot, abs)
		if err != nil {
			return err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("%s is outside --root %s", abs, root)
		}
		slash := filepath.ToSlash(rel)
		if _, ok := seen[slash]; !ok {
			seen[slash] = struct{}{}
			out = append(out, slash)
		}
		return nil
	}

	for _, in := range inputs {
		matches, err := resolveInput(root, in)
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			abs, err := filepath.Abs(m)
			if err != nil {
				return nil, err
			}
			if err := add(abs); err != nil {
				return nil, err
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// resolveInput expands a single input token into concrete file paths: a directory becomes its
// *.sql files (recursively), a glob its matches, and a plain path itself (verified to exist).
// A relative input is interpreted under root (an absolute input is used as-is).
func resolveInput(root, in string) ([]string, error) {
	if !filepath.IsAbs(in) {
		in = filepath.Join(root, in)
	}
	if hasGlobMeta(in) {
		matches, err := filepath.Glob(in)
		if err != nil {
			return nil, fmt.Errorf("bad glob %q: %w", in, err)
		}
		var files []string
		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil {
				return nil, err
			}
			if !info.IsDir() {
				files = append(files, m)
			}
		}
		return files, nil
	}

	info, err := os.Stat(in)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{in}, nil
	}
	var files []string
	err = filepath.WalkDir(in, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".sql") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func hasGlobMeta(s string) bool { return strings.ContainsAny(s, "*?[") }

// looksLikeDir reports whether p should be treated as an output directory: it ends with a
// path separator, or it exists and is a directory.
func looksLikeDir(p string) bool {
	if strings.HasSuffix(p, "/") || strings.HasSuffix(p, string(os.PathSeparator)) {
		return true
	}
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// writeTo writes s to the named file, or to fallback (stdout) when name is empty.
func writeTo(name string, fallback io.Writer, s string) error {
	if name == "" {
		_, err := io.WriteString(fallback, s)
		return err
	}
	return writeFile(name, s)
}

func writeFile(path, content string) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(content), 0o644)
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
