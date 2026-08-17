package main

import (
	"context"
	"os"

	"github.com/mpyw/bisql/cmd/bisql/internal/expand"
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
			"SQL; an unresolved include exits non-zero, so a run also validates.\n\n" +
			"Filter mode: one template (a file or standard input) to standard output or --output.\n" +
			"Tree mode (--out-dir): expand the *.sql files under --include-root in one process,\n" +
			"the form for go generate; positional GLOBs select the inputs (default: all).",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "include-root", Aliases: []string{"r"}, Value: ".", Usage: "Base `directory` for @include resolution, and the tree-mode source tree"},
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "Filter mode: write to `file` instead of standard output"},
			&cli.StringFlag{Name: "out-dir", Aliases: []string{"O"}, Usage: "Tree mode: write the expanded tree into `directory`"},
			&cli.StringFlag{Name: "out-name-format", Usage: "Tree mode: Go `template` for each output path, relative to --out-dir. Fields for employees/search.sql:\n" +
				"  .Path = employees/search.sql\n" +
				"  .Dir  = employees\n" +
				"  .Base = search.sql\n" +
				"  .Name = search\n" +
				"  .Ext  = .sql\n" +
				"The default mirrors the input tree."},
			&cli.StringSliceFlag{Name: "exclude", Aliases: []string{"x"}, Usage: "Tree mode: omit files matching `glob` from the output (repeatable; still @includable). A slashless glob matches the base name at any depth"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return expand.Run(expand.Options{
				Root:    cmd.String("include-root"),
				Inputs:  cmd.Args().Slice(),
				Output:  cmd.String("output"),
				OutDir:  cmd.String("out-dir"),
				OutName: cmd.String("out-name-format"),
				Exclude: cmd.StringSlice("exclude"),
			}, os.Stdin, os.Stdout)
		},
	}
}
