package bisql_test

import (
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/mpyw/bisql"
	"github.com/mpyw/bisql/dialect"
)

// FuzzBuild fuzzes the whole pipeline (Parse + Build) and, beyond "never panics", asserts a
// set of input-independent properties that must hold for every template that parses and builds:
//
//   - Determinism / immutability: building the same Template twice with the same params yields
//     DeepEqual SQL, Args, and SQLWithArgs().
//   - Cross-dialect Args invariance: a dialect only changes placeholder spelling, never the
//     bind arguments — so the same src built under MySQL and PostgreSQL yields DeepEqual Args.
//   - SQLWithArgs never panics and equals SQL when there are no binds.
//   - Postgres placeholder numbering: the distinct $N tokens are exactly $1..$len(Args).
func FuzzBuild(f *testing.F) {
	// A fixed loader so the /*%! @include x */ seed (and any @include the fuzzer stumbles onto)
	// actually resolves, exercising the include path rather than short-circuiting at Parse.
	loader := bisql.NewRegistry().Register("x", "1 = /*a*/0")

	seeds := []string{
		"select 1",
		"select * from t where 1 = 1 /*%if a != null*/and a = /*a*/0/*%end*/",
		"select 1 /*%if a*/x/*%elseif b*/y/*%else*/z/*%end*/",
		"where 1 = 0 /*%for kw in kws*/or x like /*kw*/'y'/*%end*/",
		"where id in /*ids*/(1, 2)",
		"select /*a*/1::int, ANY(/*b*/'{}'::int[])",
		"'{}' \"id\" `x` -- c\n/** c2 */",
		// Enriched seeds:
		"where a = /*^v*/'x' and b > 1",                  // literal embed
		"where (a, b) in /*p*/((0, 0))",                  // tuple-IN expansion
		"select /*%for c in cols : ', '*//*c*/0/*%end*/", // for-loop with separator
		"where /*%! @include x */ and 1 = 1",             // @include via the fixed loader
	}
	for _, s := range seeds {
		f.Add(s)
	}

	params := map[string]any{
		"a": true, "b": false, "kws": []any{"%a%"}, "ids": []any{1, 2},
		"u":    map[string]any{"a": 1},
		"v":    42,
		"p":    []any{[]any{1, 2}, []any{3, 4}},
		"cols": []any{1, 2, 3},
	}

	f.Fuzz(func(t *testing.T, src string) {
		my, err := bisql.Parse(src, bisql.WithLoader(loader))
		if err != nil {
			return
		}
		// Parse errors are handled independently per dialect: the Postgres parse may succeed or
		// fail independently, and we only cross-check when it succeeds.
		pg, pgErr := bisql.Parse(src, bisql.WithDialect(dialect.PostgreSQL), bisql.WithLoader(loader))

		// Also run once with nil params (the case nil branch of toScope), alongside the seeded
		// params. Each property is guarded so a Build error simply skips that params value.
		for _, p := range []any{params, nil} {
			s1, err := my.Build(p)
			if err != nil {
				continue
			}
			s2, err := my.Build(p)
			if err != nil {
				continue
			}

			// Determinism / immutability.
			if !reflect.DeepEqual(s1.SQL, s2.SQL) {
				t.Fatalf("non-deterministic SQL:\n a: %q\n b: %q", s1.SQL, s2.SQL)
			}
			if !argsEqual(s1.Args, s2.Args) {
				t.Fatalf("non-deterministic Args:\n a: %#v\n b: %#v", s1.Args, s2.Args)
			}
			// SQLWithArgs must never panic and must be deterministic.
			w1, w2 := s1.SQLWithArgs(), s2.SQLWithArgs()
			if !reflect.DeepEqual(w1, w2) {
				t.Fatalf("non-deterministic SQLWithArgs:\n a: %q\n b: %q", w1, w2)
			}
			// With no binds, SQLWithArgs is exactly SQL.
			if len(s1.Args) == 0 && w1 != s1.SQL {
				t.Fatalf("no-bind SQLWithArgs = %q, want SQL %q", w1, s1.SQL)
			}

			if pgErr == nil {
				ps, err := pg.Build(p)
				if err != nil {
					continue
				}
				// A dialect changes placeholders, never the args.
				if !argsEqual(s1.Args, ps.Args) {
					t.Fatalf("cross-dialect Args diverged:\n mysql: %#v\n pg:    %#v", s1.Args, ps.Args)
				}
				assertPostgresPlaceholders(t, s1.SQL, ps.SQL, len(ps.Args))
			}
		}
	})
}

// argsEqual reports whether two Args slices are equal. reflect.DeepEqual is the fast path and
// the intended semantics; it only reports NaN != NaN, which would turn a perfectly
// deterministic NaN bind (possibly nested inside a slice/map) into a spurious failure. So when
// DeepEqual says "not equal" we fall back to valueEqual, a structural walk identical to
// DeepEqual except that two NaN floats compare equal.
func argsEqual(a, b []any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	return valueEqual(reflect.ValueOf(a), reflect.ValueOf(b))
}

// valueEqual is reflect.DeepEqual with one deviation: NaN == NaN. It recurses through the value
// shapes a bind can produce (scalars, []any nesting, map[string]any).
func valueEqual(a, b reflect.Value) bool {
	if !a.IsValid() || !b.IsValid() {
		return a.IsValid() == b.IsValid()
	}
	if a.Type() != b.Type() {
		return false
	}
	switch a.Kind() {
	case reflect.Float32, reflect.Float64:
		af, bf := a.Float(), b.Float()
		if math.IsNaN(af) && math.IsNaN(bf) {
			return true
		}
		return af == bf
	case reflect.Slice, reflect.Array:
		if a.Kind() == reflect.Slice && a.IsNil() != b.IsNil() {
			return false
		}
		if a.Len() != b.Len() {
			return false
		}
		for i := 0; i < a.Len(); i++ {
			if !valueEqual(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Interface, reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil()
		}
		return valueEqual(a.Elem(), b.Elem())
	case reflect.Map:
		if a.IsNil() != b.IsNil() || a.Len() != b.Len() {
			return false
		}
		for _, k := range a.MapKeys() {
			bv := b.MapIndex(k)
			if !bv.IsValid() || !valueEqual(a.MapIndex(k), bv) {
				return false
			}
		}
		return true
	default:
		// Comparable leaves (ints, strings, bools, ...): defer to DeepEqual.
		return reflect.DeepEqual(a.Interface(), b.Interface())
	}
}

// assertPostgresPlaceholders verifies that the Postgres build numbers its generated
// placeholders exactly $1..$n, in order, with no gaps or duplicates.
//
// It cannot reliably scan for `$\d+` in the Postgres SQL alone: a generated $1 immediately
// followed by a literal source digit (e.g. the template `/*a*/0`) yields the text "$10", which
// a regex would misread. Instead it diffs the Postgres SQL against the MySQL SQL, which is
// byte-for-byte identical except that each generated placeholder is `?` under MySQL and `$k`
// under Postgres. Walking both in lockstep: identical bytes advance together (so a *literal* `?`
// present in both is consumed as text); at a divergence the MySQL side must be a generated `?`
// facing a Postgres `$`, and — knowing the placeholder must be the next sequential number — the
// Postgres side is required to read exactly `$k`, leaving any abutting literal digit as text.
func assertPostgresPlaceholders(t *testing.T, mysql, pg string, n int) {
	t.Helper()
	i, j, counter := 0, 0, 0
	for i < len(mysql) && j < len(pg) {
		if mysql[i] == pg[j] {
			i++
			j++
			continue
		}
		if mysql[i] != '?' || pg[j] != '$' {
			t.Fatalf("SQL diverges outside a placeholder at mysql[%d]/pg[%d]:\n mysql=%q\n pg=   %q", i, j, mysql, pg)
		}
		counter++
		want := "$" + strconv.Itoa(counter)
		if !strings.HasPrefix(pg[j:], want) {
			t.Fatalf("postgres placeholder #%d = want %q at offset %d:\n pg=%q", counter, want, j, pg)
		}
		i++ // past '?'
		j += len(want)
	}
	if i != len(mysql) || j != len(pg) {
		t.Fatalf("SQL length/tail mismatch (mysql[%d:], pg[%d:]):\n mysql=%q\n pg=   %q", i, j, mysql, pg)
	}
	if counter != n {
		t.Fatalf("postgres placeholders: highest number $%d, want $%d for len(Args) (pg=%q)", counter, n, pg)
	}
}
