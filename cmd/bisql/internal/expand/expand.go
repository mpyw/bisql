// Package expand implements the `bisql expand` subcommand: a one-in/one-out filter that
// resolves /*%! @include ... */ directives in a two-way SQL template. It reads the template
// from stdin and writes the expanded, still-two-way SQL to stdout; fragments are resolved
// under an include root, exactly as the library's FSLoader does. It is decoupled from the CLI
// framework (Run takes plain streams) so it is directly testable.
package expand

import (
	"fmt"
	"io"
	"os"

	"github.com/mpyw/bisql"
)

// Run reads a two-way SQL template from stdin, resolves its @include directives (fragments
// loaded under root), and writes the expanded, still-two-way SQL to stdout. An unresolved
// include returns an error.
func Run(root string, stdin io.Reader, stdout io.Writer) error {
	src, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}
	expanded, err := bisql.Expand(string(src), bisql.WithLoader(bisql.NewFSLoader(os.DirFS(root))))
	if err != nil {
		return err
	}
	_, err = io.WriteString(stdout, expanded)
	return err
}
