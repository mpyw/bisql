package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mpyw/bisql/cmd/bisql/internal/expand"
	"github.com/urfave/cli/v3"
)

func expandCommand() *cli.Command {
	return &cli.Command{
		Name:      "expand",
		Usage:     "Resolve @include directives and write the expanded two-way SQL",
		UsageText: "bisql expand [--include-root DIR] < template.sql > expanded.sql",
		Description: "Resolves the /*%! @include ... */ directives in a two-way SQL template and writes\n" +
			"the expanded, still-two-way SQL. It reads from standard input and writes to standard\n" +
			"output — a plain filter. @include names resolve under --include-root, exactly as the\n" +
			"library's FSLoader does; an unresolved include exits non-zero, so a run also validates.\n\n" +
			"To expand many files, drive it from the shell (a loop, or find ... | xargs) or call\n" +
			"bisql.ExpandFile from Go, where you control output layout, naming, and atomicity.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "include-root", Aliases: []string{"r"}, Value: ".", Usage: "Base `directory` for @include resolution"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() > 0 {
				return fmt.Errorf("expand reads the template from standard input and takes no arguments (got %q); use: bisql expand < %s", cmd.Args().First(), cmd.Args().First())
			}
			return expand.Run(cmd.String("include-root"), os.Stdin, os.Stdout)
		},
	}
}
