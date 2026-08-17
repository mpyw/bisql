// Package expand implements the `bisql expand` subcommand: resolving /*%! @include ... */
// directives in two-way SQL templates, as a one-in/one-out filter or a whole-tree batch. It
// is decoupled from the CLI framework — Run takes plain Options and streams — so it is
// directly testable. Input selection lives in the selection package and output naming in the
// outname package.
package expand

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mpyw/bisql/cmd/bisql/internal/outname"
	"github.com/mpyw/bisql/cmd/bisql/internal/selection"
)

// Options configures Run.
//
// Filter mode (OutDir == "") expands a single template — Inputs[0], or stdin when Inputs is
// empty — to Output, or to stdout when Output is empty.
//
// Tree mode (OutDir != "") expands the *.sql files under Root selected by the input globs in
// Inputs (all of them when Inputs is empty), minus any Exclude matches, and writes each under
// OutDir at the path produced by the OutName template (default: mirror the input tree).
type Options struct {
	Root    string
	Inputs  []string
	Output  string
	OutDir  string
	OutName string
	Exclude []string
}

// Run executes the expand command against the given streams.
func Run(opts Options, stdin io.Reader, stdout io.Writer) error {
	if opts.Output != "" && opts.OutDir != "" {
		return fmt.Errorf("--output (a single file) and --out-dir (a tree) are mutually exclusive")
	}
	if opts.OutDir != "" {
		return runTree(opts)
	}
	// Filter mode: the tree-only flags have no meaning here, so reject them rather than
	// silently ignoring them.
	if len(opts.Exclude) > 0 {
		return fmt.Errorf("--exclude is a tree-mode flag; add --out-dir to expand a tree")
	}
	if opts.OutName != "" {
		return fmt.Errorf("--out-name-format is a tree-mode flag; add --out-dir to expand a tree")
	}
	return runFilter(opts, stdin, stdout)
}

func runFilter(opts Options, stdin io.Reader, stdout io.Writer) error {
	if len(opts.Inputs) > 1 {
		return fmt.Errorf("filter mode accepts at most one template (received %d); use --out-dir to expand a tree", len(opts.Inputs))
	}
	var input string
	if len(opts.Inputs) == 1 {
		input = opts.Inputs[0]
	}
	src, err := readInput(input, stdin)
	if err != nil {
		return err
	}
	expanded, err := expandText(string(src), opts.Root)
	if err != nil {
		return err
	}
	if opts.Output != "" {
		return writeFile(opts.Output, expanded)
	}
	_, err = io.WriteString(stdout, expanded)
	return err
}

func runTree(opts Options) error {
	name, err := outname.Parse(opts.OutName)
	if err != nil {
		return err
	}
	rels, err := selection.SelectInputs(opts.Root, opts.Inputs)
	if err != nil {
		return err
	}

	// Resolve every output path first, so an --out-name-format collision is reported before
	// anything is written.
	type job struct{ rel, target string }
	var jobs []job
	byTarget := map[string]string{}
	for _, rel := range rels {
		// An excluded file is dropped from the output but stays resolvable as an @include,
		// since fragments load from --include-root independently of this selection.
		skip, err := selection.Match(rel, opts.Exclude)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
		outRel, err := name.Render(rel)
		if err != nil {
			return err
		}
		target := filepath.Join(opts.OutDir, filepath.FromSlash(outRel))
		if prev, dup := byTarget[target]; dup {
			return fmt.Errorf("output collision: %s and %s both map to %s", prev, rel, outRel)
		}
		byTarget[target] = rel
		jobs = append(jobs, job{rel: rel, target: target})
	}
	if len(jobs) == 0 {
		return fmt.Errorf("no .sql templates to expand under %s", opts.Root)
	}

	for _, j := range jobs {
		src, err := os.ReadFile(filepath.Join(opts.Root, filepath.FromSlash(j.rel)))
		if err != nil {
			return err
		}
		expanded, err := expandText(string(src), opts.Root)
		if err != nil {
			return fmt.Errorf("%s: %w", j.rel, err)
		}
		if err := writeFile(j.target, expanded); err != nil {
			return err
		}
	}
	return nil
}
