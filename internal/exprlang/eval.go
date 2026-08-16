package exprlang

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/mpyw/bisql/pkg/expr"
)

// eval is a recursive-descent evaluator over the token slice.
//
// Precedence (low to high): || , && , comparison, unary !, primary.
type evaluator struct {
	toks  []tok
	pos   int
	scope expr.Scope
}

func (p *evaluator) cur() tok { return p.toks[p.pos] }
func (p *evaluator) advance() { p.pos++ }

func (p *evaluator) parseExpr() (any, error) { return p.parseOr() }

func (p *evaluator) parseOr() (any, error) {
	v, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.cur().kind == tOr {
		p.advance()
		lb, err := toBool(v)
		if err != nil {
			return nil, err
		}
		r, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		rb, err := toBool(r)
		if err != nil {
			return nil, err
		}
		v = lb || rb
	}
	return v, nil
}

func (p *evaluator) parseAnd() (any, error) {
	v, err := p.parseCmp()
	if err != nil {
		return nil, err
	}
	for p.cur().kind == tAnd {
		p.advance()
		lb, err := toBool(v)
		if err != nil {
			return nil, err
		}
		r, err := p.parseCmp()
		if err != nil {
			return nil, err
		}
		rb, err := toBool(r)
		if err != nil {
			return nil, err
		}
		v = lb && rb
	}
	return v, nil
}

func (p *evaluator) parseCmp() (any, error) {
	v, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	switch p.cur().kind {
	case tEq, tNe, tGt, tLt, tGe, tLe:
		op := p.cur().kind
		p.advance()
		r, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return compare(op, v, r)
	}
	return v, nil
}

func (p *evaluator) parseUnary() (any, error) {
	if p.cur().kind == tNot {
		p.advance()
		v, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		b, err := toBool(v)
		if err != nil {
			return nil, err
		}
		return !b, nil
	}
	return p.parsePrimary()
}

func (p *evaluator) parsePrimary() (any, error) {
	t := p.cur()
	switch t.kind {
	case tNull:
		p.advance()
		return nil, nil
	case tTrue:
		p.advance()
		return true, nil
	case tFalse:
		p.advance()
		return false, nil
	case tNumber:
		p.advance()
		return t.num, nil
	case tString:
		p.advance()
		return t.num, nil
	case tLParen:
		p.advance()
		v, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if p.cur().kind != tRParen {
			return nil, fmt.Errorf("bisql/exprlang: expected ')'")
		}
		p.advance()
		return v, nil
	case tIdent:
		return p.parseIdentChain()
	default:
		return nil, fmt.Errorf("bisql/exprlang: unexpected token %q", t.text)
	}
}

// parseIdentChain parses ident, optional top-level call, then a chain of .member / ?.member
// with optional method calls.
func (p *evaluator) parseIdentChain() (any, error) {
	name := p.cur().text
	p.advance()
	var recv any
	if p.cur().kind == tLParen {
		// top-level function call: look up a func in scope
		args, err := p.parseArgs()
		if err != nil {
			return nil, err
		}
		fn, ok := p.scope[name]
		if !ok {
			return nil, fmt.Errorf("bisql/exprlang: unknown function %q", name)
		}
		v, err := callFunc(fn, args)
		if err != nil {
			return nil, err
		}
		recv = v
	} else {
		v, ok := p.scope[name]
		if !ok {
			return nil, fmt.Errorf("bisql/exprlang: unknown identifier %q", name)
		}
		recv = v
	}
	return p.parsePostfix(recv)
}

func (p *evaluator) parsePostfix(recv any) (any, error) {
	for {
		switch p.cur().kind {
		case tDot, tSafeDot:
			safe := p.cur().kind == tSafeDot
			p.advance()
			if p.cur().kind != tIdent {
				return nil, fmt.Errorf("bisql/exprlang: expected a name after '.'")
			}
			name := p.cur().text
			p.advance()
			if safe && recv == nil {
				// short-circuit: skip a trailing call's args if present
				if p.cur().kind == tLParen {
					if _, err := p.parseArgs(); err != nil {
						return nil, err
					}
				}
				recv = nil
				continue
			}
			if p.cur().kind == tLParen {
				args, err := p.parseArgs()
				if err != nil {
					return nil, err
				}
				v, err := callMethod(recv, name, args)
				if err != nil {
					return nil, err
				}
				recv = v
				continue
			}
			v, err := member(recv, name)
			if err != nil {
				return nil, err
			}
			recv = v
		default:
			return recv, nil
		}
	}
}

func (p *evaluator) parseArgs() ([]any, error) {
	// cur is '('
	p.advance()
	var args []any
	if p.cur().kind == tRParen {
		p.advance()
		return args, nil
	}
	for {
		v, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, v)
		switch p.cur().kind {
		case tComma:
			p.advance()
		case tRParen:
			p.advance()
			return args, nil
		default:
			return nil, fmt.Errorf("bisql/exprlang: expected ',' or ')' in argument list")
		}
	}
}

// --- runtime helpers ---

func toBool(v any) (bool, error) {
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("bisql/exprlang: expected a boolean, got %T", v)
	}
	return b, nil
}

func compare(op tkind, l, r any) (any, error) {
	switch op {
	case tEq:
		return equal(l, r), nil
	case tNe:
		return !equal(l, r), nil
	}
	// ordered comparisons
	if lf, rf, ok := asFloats(l, r); ok {
		switch op {
		case tGt:
			return lf > rf, nil
		case tLt:
			return lf < rf, nil
		case tGe:
			return lf >= rf, nil
		case tLe:
			return lf <= rf, nil
		}
	}
	if ls, ok := l.(string); ok {
		if rs, ok := r.(string); ok {
			switch op {
			case tGt:
				return ls > rs, nil
			case tLt:
				return ls < rs, nil
			case tGe:
				return ls >= rs, nil
			case tLe:
				return ls <= rs, nil
			}
		}
	}
	return nil, fmt.Errorf("bisql/exprlang: cannot order %T and %T", l, r)
}

func equal(l, r any) bool {
	if l == nil || r == nil {
		return l == nil && r == nil
	}
	if lf, rf, ok := asFloats(l, r); ok {
		return lf == rf
	}
	return reflect.DeepEqual(l, r)
}

// asFloats reports whether both l and r are numeric, returning them as float64.
func asFloats(l, r any) (float64, float64, bool) {
	lf, ok1 := toFloat(l)
	rf, ok2 := toFloat(r)
	return lf, rf, ok1 && ok2
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}

// member resolves a property: a map key or a struct field (exact, then case-insensitive).
func member(recv any, name string) (any, error) {
	if recv == nil {
		return nil, fmt.Errorf("bisql/exprlang: cannot read %q of nil", name)
	}
	rv := reflect.ValueOf(recv)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil, fmt.Errorf("bisql/exprlang: cannot read %q of nil pointer", name)
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Map:
		if rv.Type().Key().Kind() == reflect.String {
			mv := rv.MapIndex(reflect.ValueOf(name))
			if mv.IsValid() {
				return mv.Interface(), nil
			}
			return nil, nil
		}
	case reflect.Struct:
		if f := rv.FieldByName(name); f.IsValid() {
			return f.Interface(), nil
		}
		// case-insensitive fallback
		t := rv.Type()
		for i := 0; i < t.NumField(); i++ {
			if strings.EqualFold(t.Field(i).Name, name) {
				return rv.Field(i).Interface(), nil
			}
		}
	}
	return nil, fmt.Errorf("bisql/exprlang: no property %q on %T", name, recv)
}

func callMethod(recv any, name string, args []any) (any, error) {
	if recv == nil {
		return nil, fmt.Errorf("bisql/exprlang: cannot call %q on nil", name)
	}
	rv := reflect.ValueOf(recv)
	m := rv.MethodByName(name)
	if !m.IsValid() {
		// case-insensitive fallback
		t := rv.Type()
		for i := 0; i < t.NumMethod(); i++ {
			if strings.EqualFold(t.Method(i).Name, name) {
				m = rv.Method(i)
				break
			}
		}
	}
	if !m.IsValid() {
		return nil, fmt.Errorf("bisql/exprlang: no method %q on %T", name, recv)
	}
	return callValue(m, args)
}

func callFunc(fn any, args []any) (any, error) {
	rv := reflect.ValueOf(fn)
	if rv.Kind() != reflect.Func {
		return nil, fmt.Errorf("bisql/exprlang: %T is not callable", fn)
	}
	return callValue(rv, args)
}

func callValue(fn reflect.Value, args []any) (any, error) {
	ft := fn.Type()
	if !ft.IsVariadic() && ft.NumIn() != len(args) {
		return nil, fmt.Errorf("bisql/exprlang: expected %d args, got %d", ft.NumIn(), len(args))
	}
	in := make([]reflect.Value, len(args))
	for i, a := range args {
		if a == nil {
			in[i] = reflect.Zero(ft.In(min(i, ft.NumIn()-1)))
			continue
		}
		in[i] = reflect.ValueOf(a)
	}
	out := fn.Call(in)
	switch len(out) {
	case 0:
		return nil, nil
	case 1:
		return out[0].Interface(), nil
	case 2:
		if err, ok := out[1].Interface().(error); ok && err != nil {
			return nil, err
		}
		return out[0].Interface(), nil
	default:
		return nil, fmt.Errorf("bisql/exprlang: functions must return at most (value, error)")
	}
}
