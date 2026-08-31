package backend

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type runEnv struct {
	parent *runEnv
	vars   map[string]any
}

func newRunEnv(parent *runEnv) *runEnv { return &runEnv{parent: parent, vars: map[string]any{}} }
func (e *runEnv) get(name string) (any, bool) {
	for p := e; p != nil; p = p.parent {
		if v, ok := p.vars[name]; ok {
			return v, true
		}
	}
	return nil, false
}
func (e *runEnv) set(name string, v any) { e.vars[name] = v }

type runFunction struct {
	params []Param
	body   *BlockStmt
	env    *runEnv
}

type runSignal int

const (
	runNormal runSignal = iota
	runReturn
	runBreak
	runNext
)

type runState struct {
	out      strings.Builder
	rng      *rand.Rand
	steps    int
	maxSteps int
}

func Run(src string) (string, error) {
	ast, err := parse(src)
	if err != nil {
		return "", err
	}
	st := &runState{rng: rand.New(rand.NewSource(1)), maxSteps: 1_000_000}
	env := newRunEnv(nil)
	_, sig, err := st.block(env, ast)
	if err != nil {
		return st.out.String(), err
	}
	if sig == runBreak || sig == runNext {
		return st.out.String(), fmt.Errorf("loop control used outside loop")
	}
	return st.out.String(), nil
}

// RunSemantic executes the semantic tree directly. It is used for IR
// round-trip equivalence checks and never reconstructs source text.
func RunSemantic(program *SemanticProgram) (string, error) {
	if program == nil || program.Body == nil {
		return "", fmt.Errorf("missing semantic program body")
	}
	st := &runState{rng: rand.New(rand.NewSource(1)), maxSteps: 1_000_000}
	env := newRunEnv(nil)
	_, sig, err := st.block(env, program.Body)
	if err != nil {
		return st.out.String(), err
	}
	if sig == runBreak || sig == runNext {
		return st.out.String(), fmt.Errorf("loop control used outside loop")
	}
	return st.out.String(), nil
}

func (st *runState) tick() error {
	st.steps++
	if st.steps > st.maxSteps {
		return fmt.Errorf("embedded R runtime stopped after %d evaluation steps", st.maxSteps)
	}
	return nil
}

func (st *runState) block(env *runEnv, b *BlockStmt) (any, runSignal, error) {
	var last any
	for _, s := range b.List {
		if err := st.tick(); err != nil {
			return nil, runNormal, err
		}
		v, sig, err := st.stmt(env, s)
		if err != nil {
			return nil, runNormal, err
		}
		last = v
		if sig != runNormal {
			return v, sig, nil
		}
	}
	return last, runNormal, nil
}

func (st *runState) stmt(env *runEnv, s Stmt) (any, runSignal, error) {
	switch x := s.(type) {
	case *BlockStmt:
		return st.block(env, x)
	case *ExprStmt:
		v, err := st.expr(env, x.X)
		return v, runNormal, err
	case *AssignStmt:
		v, err := st.expr(env, x.Value)
		if err != nil {
			return nil, runNormal, err
		}
		env.set(x.Name, v)
		return v, runNormal, nil
	case *IfStmt:
		c, err := st.expr(env, x.Cond)
		if err != nil {
			return nil, runNormal, err
		}
		ok, err := runTruth(c)
		if err != nil {
			return nil, runNormal, err
		}
		if ok {
			return st.stmt(env, x.Then)
		}
		if x.Else != nil {
			return st.stmt(env, x.Else)
		}
		return nil, runNormal, nil
	case *WhileStmt:
		var last any
		for {
			if err := st.tick(); err != nil {
				return nil, runNormal, err
			}
			c, err := st.expr(env, x.Cond)
			if err != nil {
				return nil, runNormal, err
			}
			ok, err := runTruth(c)
			if err != nil {
				return nil, runNormal, err
			}
			if !ok {
				return last, runNormal, nil
			}
			v, sig, err := st.stmt(env, x.Body)
			if err != nil {
				return nil, runNormal, err
			}
			last = v
			if sig == runBreak {
				return last, runNormal, nil
			}
			if sig == runReturn {
				return v, sig, nil
			}
		}
	case *ForStmt:
		seq, err := st.expr(env, x.Seq)
		if err != nil {
			return nil, runNormal, err
		}
		var last any
		for _, item := range runVec(seq) {
			env.set(x.Name, item)
			v, sig, err := st.stmt(env, x.Body)
			if err != nil {
				return nil, runNormal, err
			}
			last = v
			if sig == runBreak {
				return last, runNormal, nil
			}
			if sig == runReturn {
				return v, sig, nil
			}
		}
		return last, runNormal, nil
	case *RepeatStmt:
		for {
			if err := st.tick(); err != nil {
				return nil, runNormal, err
			}
			v, sig, err := st.stmt(env, x.Body)
			if err != nil {
				return nil, runNormal, err
			}
			if sig == runBreak {
				return v, runNormal, nil
			}
			if sig == runReturn {
				return v, sig, nil
			}
		}
	case *ReturnStmt:
		if x.X == nil {
			return nil, runReturn, nil
		}
		v, err := st.expr(env, x.X)
		return v, runReturn, err
	case *BreakStmt:
		return nil, runBreak, nil
	case *NextStmt:
		return nil, runNext, nil
	default:
		return nil, runNormal, fmt.Errorf("embedded runtime: unsupported statement %T", s)
	}
}

func (st *runState) expr(env *runEnv, e Expr) (any, error) {
	switch x := e.(type) {
	case *IdentExpr:
		switch x.Name {
		case "TRUE", "T":
			return true, nil
		case "FALSE", "F":
			return false, nil
		case "NULL":
			return nil, nil
		case "NA", "NA_real_", "NA_integer_", "NA_complex_", "NaN":
			return math.NaN(), nil
		case "NA_character_":
			return nil, nil
		case "Inf":
			return math.Inf(1), nil
		case "pi":
			return math.Pi, nil
		}
		if v, ok := env.get(x.Name); ok {
			return v, nil
		}
		return nil, fmt.Errorf("object %q not found", x.Name)
	case *LiteralExpr:
		if x.Kind == "string" {
			return unquote(x.Text), nil
		}
		t := strings.TrimSuffix(x.Text, "L")
		if strings.HasSuffix(t, "i") {
			return complex(0, runNum(strings.TrimSuffix(t, "i"))), nil
		}
		v, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return nil, err
		}
		return v, nil
	case *UnaryExpr:
		v, err := st.expr(env, x.X)
		if err != nil {
			return nil, err
		}
		switch x.Op {
		case "+":
			return runMap(v, func(q any) any { return runNumber(q) }), nil
		case "-":
			return runMap(v, func(q any) any { return -runNumber(q) }), nil
		case "!":
			return runMap(v, func(q any) any { b, _ := runTruth(q); return !b }), nil
		case "~":
			return v, nil
		}
	case *BinaryExpr:
		a, err := st.expr(env, x.L)
		if err != nil {
			return nil, err
		}
		b, err := st.expr(env, x.R)
		if err != nil {
			return nil, err
		}
		return runBinary(x.Op, a, b)
	case *IndexExpr:
		v, err := st.expr(env, x.X)
		if err != nil {
			return nil, err
		}
		if len(x.Args) == 0 {
			return v, nil
		}
		idx, err := st.expr(env, x.Args[0].Value)
		if err != nil {
			return nil, err
		}
		return runSubset(v, idx, x.Double), nil
	case *FunctionExpr:
		return &runFunction{params: x.Params, body: x.Body, env: env}, nil
	case *CallExpr:
		args := make([]any, 0, len(x.Args))
		names := make([]string, 0, len(x.Args))
		for _, a := range x.Args {
			names = append(names, a.Name)
			if a.Missing {
				args = append(args, nil)
				continue
			}
			v, err := st.expr(env, a.Value)
			if err != nil {
				return nil, err
			}
			args = append(args, v)
		}
		if id, ok := x.Fun.(*IdentExpr); ok {
			if fn, ok := env.get(id.Name); ok {
				if f, ok := fn.(*runFunction); ok {
					return st.callFunction(f, args, names)
				}
			}
			return st.primitive(id.Name, args, names)
		}
		fv, err := st.expr(env, x.Fun)
		if err != nil {
			return nil, err
		}
		if f, ok := fv.(*runFunction); ok {
			return st.callFunction(f, args, names)
		}
		return nil, fmt.Errorf("attempt to call non-function")
	}
	return nil, fmt.Errorf("embedded runtime: unsupported expression %T", e)
}

func (st *runState) callFunction(fn *runFunction, args []any, names []string) (any, error) {
	env := newRunEnv(fn.env)
	used := make([]bool, len(args))
	for i, p := range fn.params {
		var val any
		found := false
		for j, n := range names {
			if !used[j] && n == p.Name {
				val = args[j]
				used[j] = true
				found = true
				break
			}
		}
		if !found && i < len(args) && !used[i] && names[i] == "" {
			val = args[i]
			used[i] = true
			found = true
		}
		if !found && p.Default != nil {
			v, err := st.expr(env, p.Default)
			if err != nil {
				return nil, err
			}
			val = v
			found = true
		}
		if !found {
			val = nil
		}
		env.set(p.Name, val)
	}
	v, sig, err := st.block(env, fn.body)
	if err != nil {
		return nil, err
	}
	if sig == runBreak || sig == runNext {
		return nil, fmt.Errorf("loop control escaped function")
	}
	return v, nil
}

func runVec(v any) []any {
	if z, ok := v.([]any); ok {
		return z
	}
	return []any{v}
}
func runMap(v any, f func(any) any) any {
	z, ok := v.([]any)
	if !ok {
		return f(v)
	}
	o := make([]any, len(z))
	for i, q := range z {
		o[i] = f(q)
	}
	return o
}
func runNumber(v any) float64 {
	switch q := v.(type) {
	case float64:
		return q
	case int:
		return float64(q)
	case bool:
		if q {
			return 1
		}
		return 0
	case string:
		x, _ := strconv.ParseFloat(q, 64)
		return x
	}
	return math.NaN()
}
func runNum(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
func runTruth(v any) (bool, error) {
	if z, ok := v.([]any); ok {
		if len(z) != 1 {
			return false, fmt.Errorf("condition has length %d", len(z))
		}
		return runTruth(z[0])
	}
	switch q := v.(type) {
	case nil:
		return false, nil
	case bool:
		return q, nil
	case float64:
		return q != 0 && !math.IsNaN(q), nil
	case string:
		return q != "", nil
	}
	return true, nil
}
func runText(v any) string {
	switch q := v.(type) {
	case nil:
		return "NULL"
	case bool:
		if q {
			return "TRUE"
		}
		return "FALSE"
	case float64:
		if math.IsNaN(q) {
			return "NA"
		}
		if math.IsInf(q, 1) {
			return "Inf"
		}
		return strconv.FormatFloat(q, 'g', -1, 64)
	case string:
		return q
	case []any:
		ss := make([]string, len(q))
		for i, x := range q {
			ss[i] = runText(x)
		}
		return "[" + strings.Join(ss, ", ") + "]"
	}
	return fmt.Sprint(v)
}

func runBinary(op string, a, b any) (any, error) {
	if op == "%in%" {
		t := runVec(b)
		return runMap(a, func(x any) any {
			for _, y := range t {
				if runText(x) == runText(y) {
					return true
				}
			}
			return false
		}), nil
	}
	if op == ":" {
		x, y := int(runNumber(a)), int(runNumber(b))
		step := 1
		if x > y {
			step = -1
		}
		o := []any{}
		for i := x; ; i += step {
			o = append(o, float64(i))
			if i == y {
				break
			}
		}
		return o, nil
	}
	av, bv := runVec(a), runVec(b)
	if len(av) == 0 || len(bv) == 0 {
		return []any{}, nil
	}
	n := len(av)
	if len(bv) > n {
		n = len(bv)
	}
	one := func(x, y any) (any, error) {
		X, Y := runNumber(x), runNumber(y)
		switch op {
		case "+":
			return X + Y, nil
		case "-":
			return X - Y, nil
		case "*":
			return X * Y, nil
		case "/":
			return X / Y, nil
		case "^", "**":
			return math.Pow(X, Y), nil
		case "%%":
			return math.Mod(X, Y), nil
		case "%/%":
			return math.Floor(X / Y), nil
		case "==":
			return runText(x) == runText(y), nil
		case "!=":
			return runText(x) != runText(y), nil
		case "<":
			return X < Y, nil
		case "<=":
			return X <= Y, nil
		case ">":
			return X > Y, nil
		case ">=":
			return X >= Y, nil
		case "&", "&&":
			tx, _ := runTruth(x)
			ty, _ := runTruth(y)
			return tx && ty, nil
		case "|", "||":
			tx, _ := runTruth(x)
			ty, _ := runTruth(y)
			return tx || ty, nil
		case "$", "@":
			return nil, fmt.Errorf("%s access requires structured object", op)
		case "::", ":::":
			return runText(x) + "::" + runText(y), nil
		}
		return nil, fmt.Errorf("unsupported operator %q", op)
	}
	if _, aVec := a.([]any); !aVec {
		if _, bVec := b.([]any); !bVec {
			return one(a, b)
		}
	}
	o := make([]any, n)
	for i := 0; i < n; i++ {
		v, err := one(av[i%len(av)], bv[i%len(bv)])
		if err != nil {
			return nil, err
		}
		o[i] = v
	}
	return o, nil
}

func runSubset(v, idx any, double bool) any {
	z := runVec(v)
	ii := runVec(idx)
	o := []any{}
	for _, q := range ii {
		i := int(runNumber(q))
		if i >= 1 && i <= len(z) {
			o = append(o, z[i-1])
		}
	}
	if double && len(o) > 0 {
		return o[0]
	}
	return o
}

func (st *runState) primitive(name string, a []any, names []string) (any, error) {
	first := any(nil)
	if len(a) > 0 {
		first = a[0]
	}
	switch name {
	case "c", "list", "expression":
		return a, nil
	case "print", "show":
		st.out.WriteString(runText(first))
		st.out.WriteByte('\n')
		return first, nil
	case "cat":
		for _, v := range a {
			st.out.WriteString(runText(v))
		}
		return nil, nil
	case "identity", "invisible", "force":
		return first, nil
	case "length":
		return float64(len(runVec(first))), nil
	case "lengths":
		return runMap(first, func(v any) any { return float64(len(runVec(v))) }), nil
	case "sum", "prod", "mean", "min", "max":
		z := runVec(first)
		if len(z) == 0 {
			if name == "sum" {
				return 0.0, nil
			}
			if name == "prod" {
				return 1.0, nil
			}
			return math.NaN(), nil
		}
		s, p := 0.0, 1.0
		mn, mx := runNumber(z[0]), runNumber(z[0])
		for _, v := range z {
			q := runNumber(v)
			s += q
			p *= q
			if q < mn {
				mn = q
			}
			if q > mx {
				mx = q
			}
		}
		switch name {
		case "sum":
			return s, nil
		case "prod":
			return p, nil
		case "mean":
			return s / float64(len(z)), nil
		case "min":
			return mn, nil
		default:
			return mx, nil
		}
	case "range":
		mn, _ := st.primitive("min", a, names)
		mx, _ := st.primitive("max", a, names)
		return []any{mn, mx}, nil
	case "abs":
		return runMap(first, func(v any) any { return math.Abs(runNumber(v)) }), nil
	case "sqrt":
		return runMap(first, func(v any) any { return math.Sqrt(runNumber(v)) }), nil
	case "exp":
		return runMap(first, func(v any) any { return math.Exp(runNumber(v)) }), nil
	case "expm1":
		return runMap(first, func(v any) any { return math.Expm1(runNumber(v)) }), nil
	case "log":
		return runMap(first, func(v any) any { return math.Log(runNumber(v)) }), nil
	case "log10":
		return runMap(first, func(v any) any { return math.Log10(runNumber(v)) }), nil
	case "log2":
		return runMap(first, func(v any) any { return math.Log2(runNumber(v)) }), nil
	case "log1p":
		return runMap(first, func(v any) any { return math.Log1p(runNumber(v)) }), nil
	case "sin":
		return runMap(first, func(v any) any { return math.Sin(runNumber(v)) }), nil
	case "cos":
		return runMap(first, func(v any) any { return math.Cos(runNumber(v)) }), nil
	case "tan":
		return runMap(first, func(v any) any { return math.Tan(runNumber(v)) }), nil
	case "asin":
		return runMap(first, func(v any) any { return math.Asin(runNumber(v)) }), nil
	case "acos":
		return runMap(first, func(v any) any { return math.Acos(runNumber(v)) }), nil
	case "atan":
		return runMap(first, func(v any) any { return math.Atan(runNumber(v)) }), nil
	case "sinh":
		return runMap(first, func(v any) any { return math.Sinh(runNumber(v)) }), nil
	case "cosh":
		return runMap(first, func(v any) any { return math.Cosh(runNumber(v)) }), nil
	case "tanh":
		return runMap(first, func(v any) any { return math.Tanh(runNumber(v)) }), nil
	case "floor":
		return runMap(first, func(v any) any { return math.Floor(runNumber(v)) }), nil
	case "ceiling":
		return runMap(first, func(v any) any { return math.Ceil(runNumber(v)) }), nil
	case "trunc":
		return runMap(first, func(v any) any { return math.Trunc(runNumber(v)) }), nil
	case "round":
		return runMap(first, func(v any) any { return math.Round(runNumber(v)) }), nil
	case "sign":
		return runMap(first, func(v any) any {
			q := runNumber(v)
			if q < 0 {
				return -1.0
			}
			if q > 0 {
				return 1.0
			}
			return 0.0
		}), nil
	case "gamma":
		return runMap(first, func(v any) any { return math.Gamma(runNumber(v)) }), nil
	case "lgamma":
		return runMap(first, func(v any) any { x, _ := math.Lgamma(runNumber(v)); return x }), nil
	case "is.null":
		return first == nil, nil
	case "is.na", "is.nan":
		return runMap(first, func(v any) any { q, ok := v.(float64); return ok && math.IsNaN(q) }), nil
	case "is.finite":
		return runMap(first, func(v any) any { q := runNumber(v); return !math.IsNaN(q) && !math.IsInf(q, 0) }), nil
	case "is.infinite":
		return runMap(first, func(v any) any { return math.IsInf(runNumber(v), 0) }), nil
	case "is.logical":
		_, ok := first.(bool)
		return ok, nil
	case "is.integer", "is.double", "is.numeric":
		_, ok := first.(float64)
		return ok, nil
	case "is.character":
		_, ok := first.(string)
		return ok, nil
	case "is.list", "is.vector", "is.atomic":
		_, ok := first.([]any)
		return ok, nil
	case "as.double", "as.numeric", "as.real":
		return runMap(first, func(v any) any { return runNumber(v) }), nil
	case "as.integer":
		return runMap(first, func(v any) any { return math.Trunc(runNumber(v)) }), nil
	case "as.logical":
		return runMap(first, func(v any) any { q, _ := runTruth(v); return q }), nil
	case "as.character":
		return runMap(first, func(v any) any { return runText(v) }), nil
	case "as.list", "as.vector":
		return runVec(first), nil
	case "sort":
		z := append([]any{}, runVec(first)...)
		sort.SliceStable(z, func(i, j int) bool { return runNumber(z[i]) < runNumber(z[j]) })
		return z, nil
	case "rev":
		z := append([]any{}, runVec(first)...)
		for i, j := 0, len(z)-1; i < j; i, j = i+1, j-1 {
			z[i], z[j] = z[j], z[i]
		}
		return z, nil
	case "unique":
		seen := map[string]bool{}
		o := []any{}
		for _, v := range runVec(first) {
			k := runText(v)
			if !seen[k] {
				seen[k] = true
				o = append(o, v)
			}
		}
		return o, nil
	case "which":
		o := []any{}
		for i, v := range runVec(first) {
			b, _ := runTruth(v)
			if b {
				o = append(o, float64(i+1))
			}
		}
		return o, nil
	case "which.min":
		z := runVec(first)
		if len(z) == 0 {
			return 0.0, nil
		}
		best := 0
		for i := 1; i < len(z); i++ {
			if runNumber(z[i]) < runNumber(z[best]) {
				best = i
			}
		}
		return float64(best + 1), nil
	case "which.max":
		z := runVec(first)
		if len(z) == 0 {
			return 0.0, nil
		}
		best := 0
		for i := 1; i < len(z); i++ {
			if runNumber(z[i]) > runNumber(z[best]) {
				best = i
			}
		}
		return float64(best + 1), nil
	case "seq_len":
		n := int(runNumber(first))
		o := make([]any, 0, n)
		for i := 1; i <= n; i++ {
			o = append(o, float64(i))
		}
		return o, nil
	case "seq_along":
		z := runVec(first)
		o := make([]any, len(z))
		for i := range z {
			o[i] = float64(i + 1)
		}
		return o, nil
	case "seq", "seq.int":
		if len(a) == 1 {
			return st.primitive("seq_len", a, names)
		}
		from, to := runNumber(a[0]), runNumber(a[1])
		by := 1.0
		if len(a) > 2 && a[2] != nil {
			by = runNumber(a[2])
		}
		o := []any{}
		if by == 0 {
			return nil, fmt.Errorf("invalid (zero) 'by'")
		}
		if by > 0 {
			for x := from; x <= to+1e-12; x += by {
				o = append(o, x)
			}
		} else {
			for x := from; x >= to-1e-12; x += by {
				o = append(o, x)
			}
		}
		return o, nil
	case "rep":
		times := 1
		if len(a) > 1 {
			times = int(runNumber(a[1]))
		}
		z := runVec(first)
		o := []any{}
		for i := 0; i < times; i++ {
			o = append(o, z...)
		}
		return o, nil
	case "paste", "paste0":
		sep := " "
		if name == "paste0" {
			sep = ""
		}
		parts := []string{}
		for _, v := range a {
			for _, q := range runVec(v) {
				parts = append(parts, runText(q))
			}
		}
		return strings.Join(parts, sep), nil
	case "nchar":
		return runMap(first, func(v any) any { return float64(len([]rune(runText(v)))) }), nil
	case "toupper":
		return runMap(first, func(v any) any { return strings.ToUpper(runText(v)) }), nil
	case "tolower":
		return runMap(first, func(v any) any { return strings.ToLower(runText(v)) }), nil
	case "substr", "substring":
		if len(a) < 3 {
			return first, nil
		}
		s := runText(a[0])
		rr := []rune(s)
		stt := int(runNumber(a[1]))
		en := int(runNumber(a[2]))
		if stt < 1 {
			stt = 1
		}
		if en > len(rr) {
			en = len(rr)
		}
		if stt > en {
			return "", nil
		}
		return string(rr[stt-1 : en]), nil
	case "grepl":
		if len(a) < 2 {
			return false, nil
		}
		rx, err := regexp.Compile(runText(a[0]))
		if err != nil {
			return nil, err
		}
		return runMap(a[1], func(v any) any { return rx.MatchString(runText(v)) }), nil
	case "grep":
		if len(a) < 2 {
			return []any{}, nil
		}
		rx, err := regexp.Compile(runText(a[0]))
		if err != nil {
			return nil, err
		}
		o := []any{}
		for i, v := range runVec(a[1]) {
			if rx.MatchString(runText(v)) {
				o = append(o, float64(i+1))
			}
		}
		return o, nil
	case "sub", "gsub":
		if len(a) < 3 {
			return first, nil
		}
		rx, err := regexp.Compile(runText(a[0]))
		if err != nil {
			return nil, err
		}
		repl := runText(a[1])
		return runMap(a[2], func(v any) any {
			src := runText(v)
			if name == "gsub" {
				return rx.ReplaceAllString(src, repl)
			}
			loc := rx.FindStringIndex(src)
			if loc == nil {
				return src
			}
			return src[:loc[0]] + repl + src[loc[1]:]
		}), nil
	case "any":
		for _, v := range runVec(first) {
			b, _ := runTruth(v)
			if b {
				return true, nil
			}
		}
		return false, nil
	case "all":
		for _, v := range runVec(first) {
			b, _ := runTruth(v)
			if !b {
				return false, nil
			}
		}
		return true, nil
	case "set.seed":
		st.rng = rand.New(rand.NewSource(int64(runNumber(first))))
		return nil, nil
	case "runif":
		n := int(runNumber(first))
		o := make([]any, n)
		for i := range o {
			o[i] = st.rng.Float64()
		}
		return o, nil
	case "rnorm":
		n := int(runNumber(first))
		o := make([]any, n)
		for i := range o {
			o[i] = st.rng.NormFloat64()
		}
		return o, nil
	case "sample":
		z := append([]any{}, runVec(first)...)
		st.rng.Shuffle(len(z), func(i, j int) { z[i], z[j] = z[j], z[i] })
		if len(a) > 1 {
			n := int(runNumber(a[1]))
			if n < len(z) {
				z = z[:n]
			}
		}
		return z, nil
	case "getwd":
		return os.Getwd()
	case "setwd":
		return nil, os.Chdir(runText(first))
	case "file.exists":
		return runMap(first, func(v any) any { _, err := os.Stat(runText(v)); return err == nil }), nil
	case "dir.exists":
		return runMap(first, func(v any) any { q, err := os.Stat(runText(v)); return err == nil && q.IsDir() }), nil
	case "dir.create":
		return os.MkdirAll(runText(first), 0755) == nil, nil
	case "basename":
		return filepath.Base(runText(first)), nil
	case "dirname":
		return filepath.Dir(runText(first)), nil
	case "Sys.getenv":
		return os.Getenv(runText(first)), nil
	case "Sys.time":
		return float64(time.Now().UnixNano()) / 1e9, nil
	case "Sys.Date":
		return math.Floor(float64(time.Now().Unix()) / 86400), nil
	case "stop":
		return nil, fmt.Errorf("%s", runText(first))
	case "warning":
		st.out.WriteString("Warning: " + runText(first) + "\n")
		return nil, nil
	case "typeof":
		switch first.(type) {
		case nil:
			return "NULL", nil
		case bool:
			return "logical", nil
		case float64:
			return "double", nil
		case string:
			return "character", nil
		case []any:
			return "list", nil
		case *runFunction:
			return "closure", nil
		}
		return "unknown", nil
	case "class":
		return st.primitive("typeof", a, names)
	case "match":
		if len(a) < 2 {
			return []any{}, nil
		}
		tt := runVec(a[1])
		o := []any{}
		for _, v := range runVec(a[0]) {
			pos := math.NaN()
			for j, q := range tt {
				if runText(v) == runText(q) {
					pos = float64(j + 1)
					break
				}
			}
			o = append(o, pos)
		}
		return o, nil
	}
	if spec, ok := primitiveByName[name]; ok {
		return st.kernelFallback(spec.Kernel, name, a), nil
	}
	return nil, fmt.Errorf("could not find function %q", name)
}

func (st *runState) kernelFallback(kernel, name string, a []any) any {
	first := any(nil)
	if len(a) > 0 {
		first = a[0]
	}
	switch kernel {
	case "combine":
		o := []any{}
		for _, v := range a {
			o = append(o, runVec(v)...)
		}
		return o
	case "arithmetic", "numeric-binary", "relational", "logical":
		if len(a) >= 2 {
			v, _ := runBinary(name, a[0], a[1])
			return v
		}
		return first
	case "numeric-unary", "numeric-complex", "numeric-ternary":
		return first
	case "reduction":
		return first
	case "predicate", "numeric-predicate", "missingness":
		return runMap(first, func(any) any { return false })
	case "coercion-atomic", "coercion-mode", "ordering", "attribute", "iteration", "environment", "io", "system", "serialization", "language", "runtime":
		return first
	case "matching":
		if len(a) >= 2 {
			v, _ := st.primitive("match", a, nil)
			return v
		}
		return []any{}
	case "subset":
		if len(a) >= 2 {
			return runSubset(a[0], a[1], false)
		}
		return first
	case "replacement":
		if len(a) > 0 {
			return a[len(a)-1]
		}
		return nil
	case "matrix":
		o := []any{}
		for _, v := range a {
			o = append(o, runVec(v)...)
		}
		return o
	case "cumulative":
		z := runVec(first)
		o := make([]any, len(z))
		acc := 0.0
		if name == "cumprod" {
			acc = 1
		}
		for i, v := range z {
			q := runNumber(v)
			switch name {
			case "cumsum":
				acc += q
			case "cumprod":
				acc *= q
			case "cummin":
				if i == 0 || q < acc {
					acc = q
				}
			case "cummax":
				if i == 0 || q > acc {
					acc = q
				}
			}
			o[i] = acc
		}
		return o
	case "bitwise":
		if len(a) >= 2 {
			x, y := int64(runNumber(a[0])), int64(runNumber(a[1]))
			switch name {
			case "bitwAnd":
				return float64(x & y)
			case "bitwOr":
				return float64(x | y)
			case "bitwXor":
				return float64(x ^ y)
			case "bitwShiftL":
				return float64(x << uint64(y))
			case "bitwShiftR":
				return float64(x >> uint64(y))
			}
		}
		return first
	case "random":
		return st.rng.Float64()
	case "character":
		return runText(first)
	case "datetime":
		return float64(time.Now().Unix())
	case "logical-reduction", "logical-short-circuit":
		b, _ := runTruth(first)
		return b
	}
	return first
}
