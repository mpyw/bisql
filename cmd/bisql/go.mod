module github.com/mpyw/bisql/cmd/bisql

go 1.25.0

require (
	github.com/mpyw/bisql v0.0.0
	github.com/urfave/cli/v3 v3.11.0
)

require github.com/expr-lang/expr v1.17.8 // indirect

replace github.com/mpyw/bisql => ../..
