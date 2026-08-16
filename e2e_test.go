package bisql_test

import (
	"reflect"
	"testing"

	"github.com/mpyw/bisql"
	"github.com/mpyw/bisql/dialect"
)

// Complex, realistic end-to-end scenarios: CTEs, dynamic WHERE/ORDER BY, recursive
// includes, and an all-in-one query — the shapes a real application produces.

// --- CTE (WITH ... AS (...)): WITH/RECURSIVE/AS pass through as plain words ---

func TestE2ECTE(t *testing.T) {
	runBuild(t, nil, []buildCase{
		{
			name:     "simple cte",
			tmpl:     "with cte as (select id from users where active = /*active*/true) select id from cte order by id",
			params:   map[string]any{"active": true},
			sql:      "with cte as (select id from users where active = ?) select id from cte order by id",
			args:     []any{true},
			withArgs: "with cte as (select id from users where active = true) select id from cte order by id",
		},
		{
			name:   "recursive cte",
			tmpl:   "with recursive t(n) as (select 1 union all select n+1 from t where n < /*max*/10) select n from t",
			params: map[string]any{"max": 10},
			sql:    "with recursive t(n) as (select 1 union all select n+1 from t where n < ?) select n from t",
			args:   []any{10},
		},
		{
			name:   "cte with dynamic inner where removed",
			tmpl:   "with cte as (select id, name from users where /*%if name != null*/name = /*name*/'x'/*%end*/) select * from cte",
			params: map[string]any{"name": nil},
			sql:    "with cte as (select id, name from users ) select * from cte",
		},
	})
}

// --- dynamic WHERE ---

func TestE2EDynamicWhere(t *testing.T) {
	const idiom = "select * from person where 1 = 1 /*%if name != null*/and name = /*name*/'x'/*%end*/ /*%if age != null*/and age > /*age*/0/*%end*/ /*%if ids != null*/and id in /*ids*/(0)/*%end*/"
	const noIdiom = "select * from person where /*%if name != null*/name = /*name*/'x'/*%end*/ /*%if age != null*/and age > /*age*/0/*%end*/"
	runBuild(t, nil, []buildCase{
		{
			name:     "idiom all set",
			tmpl:     idiom,
			params:   map[string]any{"name": "SCOTT", "age": 20, "ids": []any{1, 2, 3}},
			sql:      "select * from person where 1 = 1 and name = ? and age > ? and id in (?, ?, ?)",
			args:     []any{"SCOTT", 20, 1, 2, 3},
			withArgs: "select * from person where 1 = 1 and name = 'SCOTT' and age > 20 and id in (1, 2, 3)",
		},
		{
			name:   "idiom none set",
			tmpl:   idiom,
			params: map[string]any{},
			sql:    "select * from person where 1 = 1   ",
		},
		{
			name:   "no-idiom only second set drops leading and",
			tmpl:   noIdiom,
			params: map[string]any{"age": 20},
			sql:    "select * from person where   age > ?",
			args:   []any{20},
		},
		{
			name:   "no-idiom none set removes where",
			tmpl:   noIdiom,
			params: map[string]any{},
			sql:    "select * from person ",
		},
	})
}

// --- dynamic ORDER BY built with a for-loop ---

func TestE2EDynamicOrderBy(t *testing.T) {
	const tmpl = "select * from t /*%if sorts != null*/order by /*%for s in sorts*//*# s */ /*# s_next_comma */ /*%end*//*%end*/"
	runBuild(t, nil, []buildCase{
		{
			name:   "with sorts",
			tmpl:   tmpl,
			params: map[string]any{"sorts": []any{"name asc", "age desc"}},
			sql:    "select * from t order by name asc , age desc  ",
		},
		{
			name:   "no sorts removes order by",
			tmpl:   tmpl,
			params: map[string]any{},
			sql:    "select * from t ",
		},
	})
}

func TestE2EForAndJoin(t *testing.T) {
	runBuild(t, nil, []buildCase{
		{
			name:   "and-joined conditions",
			tmpl:   "select * from t /*%if conds != null*/where /*%for c in conds*/x = /*c*/0 /*# c_next_and */ /*%end*//*%end*/",
			params: map[string]any{"conds": []any{1, 2, 3}},
			sql:    "select * from t where x = ? and x = ? and x = ?  ",
			args:   []any{1, 2, 3},
		},
	})
}

// --- recursive includes ---

func TestE2ERecursiveIncludes(t *testing.T) {
	t.Run("nested partials a->b->c", func(t *testing.T) {
		ld := bisql.NewLoader()
		ld.Register("a", "x = 1 /*> b */")
		ld.Register("b", "and y = 2 /*> c */")
		ld.Register("c", "and z = 3")
		tmpl, err := ld.Parse("select * from t where /*> a */")
		if err != nil {
			t.Fatal(err)
		}
		stmt, _ := tmpl.Build(nil)
		if stmt.SQL != "select * from t where x = 1 and y = 2 and z = 3" {
			t.Errorf("got %q", stmt.SQL)
		}
	})

	t.Run("partial containing an embed", func(t *testing.T) {
		ld := bisql.NewLoader()
		ld.Register("frag", "col = /*# raw */")
		tmpl, err := ld.Parse("select /*> frag */ from t")
		if err != nil {
			t.Fatal(err)
		}
		stmt, _ := tmpl.Build(map[string]any{"raw": "computed"})
		if stmt.SQL != "select col = computed from t" {
			t.Errorf("got %q", stmt.SQL)
		}
	})

	t.Run("embed producing a partial reference", func(t *testing.T) {
		ld := bisql.NewLoader()
		ld.Register("cond", "active = /*a*/true")
		tmpl, err := ld.Parse("select * from t where /*# dyn */")
		if err != nil {
			t.Fatal(err)
		}
		stmt, err := tmpl.Build(map[string]any{"dyn": "/*> cond */", "a": true})
		if err != nil {
			t.Fatal(err)
		}
		if stmt.SQL != "select * from t where active = ?" || !reflect.DeepEqual(stmt.Args, []any{true}) {
			t.Errorf("SQL=%q Args=%#v", stmt.SQL, stmt.Args)
		}
	})

	t.Run("embed producing a bind", func(t *testing.T) {
		tmpl, err := bisql.Parse("select * from t where /*# dyn */")
		if err != nil {
			t.Fatal(err)
		}
		stmt, err := tmpl.Build(map[string]any{"dyn": "a = /*x*/1", "x": 99})
		if err != nil {
			t.Fatal(err)
		}
		if stmt.SQL != "select * from t where a = ?" || !reflect.DeepEqual(stmt.Args, []any{99}) {
			t.Errorf("SQL=%q Args=%#v", stmt.SQL, stmt.Args)
		}
	})
}

// Known authoring gotchas, pinned so a change surfaces here. These stem from the shallow
// structural model (bisql does not parse SQL grammar) and match Komapper.
func TestE2EKnownGotchas(t *testing.T) {
	runBuild(t, nil, []buildCase{
		{
			// Empty grouping parens are NOT removed: at render time a dropped grouping
			// group is indistinguishable from a function call like my_function(). Guard a
			// dynamic group with an outer /*%if*/ instead of relying on removal.
			name:   "empty grouping parens are kept (guard with outer if)",
			tmpl:   "select * from t where (/*%if a*/a = 1/*%end*/)",
			params: map[string]any{"a": false},
			sql:    "select * from t where ()",
		},
	})
}

// --- all-in-one: CTE + dynamic select columns + dynamic WHERE + IN + dynamic ORDER BY ---

const allInOne = "with active as (select id from acct where flag = /*flag*/true) " +
	"select /*%for c in cols*//*# c *//*# c_next_comma */ /*%end*/ from person p join active a on a.id = p.id " +
	"where 1 = 1 /*%if name != null*/and name = /*name*/'x'/*%end*/ /*%if ids != null*/and p.id in /*ids*/(0)/*%end*/ " +
	"/*%if sorts != null*/order by /*%for s in sorts*//*# s *//*# s_next_comma */ /*%end*//*%end*/"

func TestE2EAllInOneMySQL(t *testing.T) {
	runBuild(t, nil, []buildCase{
		{
			name:     "full params",
			tmpl:     allInOne,
			params:   map[string]any{"flag": true, "cols": []any{"p.id", "p.name"}, "name": "SCOTT", "ids": []any{1, 2}, "sorts": []any{"name", "id desc"}},
			sql:      "with active as (select id from acct where flag = ?) select p.id, p.name  from person p join active a on a.id = p.id where 1 = 1 and name = ? and p.id in (?, ?) order by name, id desc ",
			args:     []any{true, "SCOTT", 1, 2},
			withArgs: "with active as (select id from acct where flag = true) select p.id, p.name  from person p join active a on a.id = p.id where 1 = 1 and name = 'SCOTT' and p.id in (1, 2) order by name, id desc ",
		},
		{
			name:   "minimal params",
			tmpl:   allInOne,
			params: map[string]any{"flag": false, "cols": []any{"p.id"}},
			sql:    "with active as (select id from acct where flag = ?) select p.id  from person p join active a on a.id = p.id where 1 = 1   ",
			args:   []any{false},
		},
	})
}

// The all-in-one across index-based dialects: this is the shape that the placeholder-
// numbering bug corrupted; it must number monotonically across the CTE, WHERE, and IN.
func TestE2EAllInOneDialects(t *testing.T) {
	params := map[string]any{"flag": true, "cols": []any{"p.id"}, "name": "SCOTT", "ids": []any{1, 2}}
	cases := []struct {
		name string
		d    dialect.Dialect
		sql  string
	}{
		{"postgres", dialect.PostgreSQL, "with active as (select id from acct where flag = $1) select p.id  from person p join active a on a.id = p.id where 1 = 1 and name = $2 and p.id in ($3, $4) "},
		{"oracle", dialect.Oracle, "with active as (select id from acct where flag = :1) select p.id  from person p join active a on a.id = p.id where 1 = 1 and name = :2 and p.id in (:3, :4) "},
		{"sqlserver", dialect.SQLServer, "with active as (select id from acct where flag = @p1) select p.id  from person p join active a on a.id = p.id where 1 = 1 and name = @p2 and p.id in (@p3, @p4) "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpl, err := bisql.Parse(allInOne, bisql.WithDialect(c.d))
			if err != nil {
				t.Fatal(err)
			}
			stmt, err := tmpl.Build(params)
			if err != nil {
				t.Fatal(err)
			}
			if stmt.SQL != c.sql {
				t.Errorf("SQL\n got: %q\nwant: %q", stmt.SQL, c.sql)
			}
			if !reflect.DeepEqual(stmt.Args, []any{true, "SCOTT", 1, 2}) {
				t.Errorf("Args got %#v", stmt.Args)
			}
		})
	}
}
