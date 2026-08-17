// Command bisql is a command-line front end to the bisql two-way SQL template engine.
//
// It currently provides one subcommand:
//
//	bisql expand   resolve /*%! @include ... */ directives, printing the expanded two-way SQL
//
// A future release will add `bisql render`, which additionally evaluates the SQL directives
// against a set of parameters to produce the final statement.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
)

func main() {
	cmd := &cli.Command{
		Name:  "bisql",
		Usage: "Two-way SQL template tool",
		Commands: []*cli.Command{
			expandCommand(),
		},
	}
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "bisql:", err)
		os.Exit(1)
	}
}
