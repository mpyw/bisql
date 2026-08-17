// Command bisql-expand resolves the /*%! @include ... */ directives in a two-way SQL
// template and writes the expanded, still-two-way SQL. It performs only the include
// preprocessing step (bisql.Expand); it does no dialect rendering and binds no arguments, so
// the output remains a runnable two-way template — useful for committing expanded snapshots
// or inspecting a query with EXPLAIN before execution.
//
// Usage:
//
//	bisql-expand [-root dir] [-o out.sql] <template.sql>
//	bisql-expand [-root dir] [-o out.sql] -        # read the template from stdin
//
// Both the template path and every @include name resolve as paths under -root (default: the
// current directory), matching bisql.ParseFile and FSLoader.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/mpyw/bisql"
)

const usage = `bisql-expand resolves @include directives in a two-way SQL template.

Usage:
  bisql-expand [-root dir] [-o out.sql] <template.sql>
  bisql-expand [-root dir] [-o out.sql] -        read the template from stdin

The template path and every @include name resolve as paths under -root.

Flags:
`

func main() {
	err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	if err == nil {
		return
	}
	if errors.Is(err, flag.ErrHelp) {
		return // -h / -help already printed usage; a help request is not a failure
	}
	fmt.Fprintln(os.Stderr, "bisql-expand:", err)
	os.Exit(1)
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("bisql-expand", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		_, _ = fmt.Fprint(stderr, usage) // usage output to a sink; a write error is not actionable
		flags.PrintDefaults()
	}
	root := flags.String("root", ".", "base directory for the template and its @include fragments")
	out := flags.String("o", "", "write output to this file instead of stdout")
	if err := flags.Parse(args); err != nil {
		return err // flag prints its own message; ErrHelp is handled in main
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return errors.New("exactly one template path is required (use - for stdin)")
	}
	name := flags.Arg(0)

	expanded, err := expand(os.DirFS(*root), name, stdin)
	if err != nil {
		return err
	}

	w := stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close() //nolint:errcheck // the WriteString error below covers a write failure
		w = f
	}
	if _, err := io.WriteString(w, expanded); err != nil {
		return err
	}
	return nil
}

// expand reads the template (from stdin when name is "-", else from fsys) and returns its
// include-expanded text, resolving @include fragments from fsys.
func expand(fsys fs.FS, name string, stdin io.Reader) (string, error) {
	if name == "-" {
		src, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return bisql.Expand(string(src), bisql.WithLoader(bisql.NewFSLoader(fsys)))
	}
	return bisql.ExpandFile(fsys, name)
}
