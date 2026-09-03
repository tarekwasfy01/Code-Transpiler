package backend

import (
	"bytes"
	"fmt"
	"math/big"
	"math/rand"
	"strings"
	"testing"
)

func integerOracle(value *big.Int, bits int, signed bool) *big.Int {
	modulus := new(big.Int).Lsh(big.NewInt(1), uint(bits))
	result := new(big.Int).Mod(value, modulus)
	if signed && result.Bit(bits-1) != 0 {
		result.Sub(result, modulus)
	}
	return result
}

// Rows are eight integer domains; columns are operations/conversions. Each
// cell is exercised with boundaries and seeded values against a big.Int oracle.
// Each row is a separate compilation shard to bound compiler/graph memory.
func TestNativeIntegerDifferentialMatrix(t *testing.T) {
	rng := rand.New(rand.NewSource(81264))
	for _, bits := range []int{8, 16, 32, 64} {
		for _, signed := range []bool{false, true} {
			typ := "uint"
			if signed {
				typ = "int"
			}
			typ += fmt.Sprint(bits)
			t.Run(typ, func(t *testing.T) {
				var source, want strings.Builder
				source.WriteString("package main\nimport \"fmt\"\nfunc main(){\n")
				for sample := 0; sample < 8; sample++ {
					raw := new(big.Int).SetUint64(rng.Uint64())
					other := new(big.Int).SetUint64(rng.Uint64())
					switch sample {
					case 0:
						raw.Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
						other.SetInt64(1)
					case 1:
						raw.Lsh(big.NewInt(1), uint(bits-1))
						other.SetInt64(-1)
					case 2:
						raw.SetInt64(0)
						other.SetInt64(0)
					case 3:
						raw.SetInt64(1)
						other.SetInt64(2)
					}
					a, b := integerOracle(raw, bits, signed), integerOracle(other, bits, signed)
					fmt.Fprintf(&source, "{ var a %s = %s; var b %s = %s\n", typ, a, typ, b)
					for _, op := range []string{"+", "-", "*", "&", "|", "^", "&^"} {
						result := new(big.Int)
						switch op {
						case "+":
							result.Add(a, b)
						case "-":
							result.Sub(a, b)
						case "*":
							result.Mul(a, b)
						case "&":
							result.And(a, b)
						case "|":
							result.Or(a, b)
						case "^":
							result.Xor(a, b)
						case "&^":
							result.AndNot(a, b)
						}
						fmt.Fprintf(&source, "fmt.Println(a %s b)\n", op)
						fmt.Fprintln(&want, integerOracle(result, bits, signed))
					}
					fmt.Fprintln(&source, "fmt.Println(-a); fmt.Println(^a)")
					fmt.Fprintln(&want, integerOracle(new(big.Int).Neg(a), bits, signed))
					fmt.Fprintln(&want, integerOracle(new(big.Int).Not(a), bits, signed))
					cmp := a.Cmp(b)
					for _, op := range []string{"==", "!=", "<", "<=", ">", ">="} {
						result := false
						switch op {
						case "==":
							result = cmp == 0
						case "!=":
							result = cmp != 0
						case "<":
							result = cmp < 0
						case "<=":
							result = cmp <= 0
						case ">":
							result = cmp > 0
						case ">=":
							result = cmp >= 0
						}
						fmt.Fprintf(&source, "if a %s b {fmt.Println(\"true\")}else{fmt.Println(\"false\")}\n", op)
						fmt.Fprintln(&want, result)
					}
					for _, toBits := range []int{8, 16, 32, 64} {
						for _, toSigned := range []bool{false, true} {
							to := "uint"
							if toSigned {
								to = "int"
							}
							to += fmt.Sprint(toBits)
							fmt.Fprintf(&source, "fmt.Println(%s(a))\n", to)
							fmt.Fprintln(&want, integerOracle(a, toBits, toSigned))
						}
					}
					source.WriteString("}\n")
				}
				source.WriteString("}\n")
				p, err := LowerNativeGo(typ+".go", source.String())
				if err != nil {
					t.Fatal(err)
				}
				data, err := p.MarshalSemanticJSON()
				if err != nil {
					t.Fatal(err)
				}
				p, err = ParseSemanticJSON(data)
				if err != nil {
					t.Fatal(err)
				}
				again, err := p.MarshalSemanticJSON()
				if err != nil || !bytes.Equal(data, again) {
					t.Fatal("matrix JSON mismatch", err)
				}
				result := ObserveSemantic(p)
				if result.Error != "" || result.Stdout != want.String() {
					t.Fatalf("matrix runtime: %s\ngot %s\nwant %s", result.Error, result.Stdout, want.String())
				}
				checkIntegerTargets(t, []byte(source.String()), want.String(), p)
			})
		}
	}
	t.Log("1472 native operation/conversion cases across 8 integer domains")
}

func TestIntegerImplementationMatrixValidation(t *testing.T) {
	m := TypedImplementationMatrix()
	for row, name := range m.Operations {
		rejected, err := m.Reject([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		for col := range m.Stages {
			if m.Implemented.At(row, col)+m.Unsupported.At(row, col) != 1 || rejected.At(0, col) != m.Unsupported.At(row, col) {
				t.Fatalf("inconsistent implementation cell %d,%d", row, col)
			}
		}
	}
	if _, err := m.Reject([]string{"integer.future"}); err == nil {
		t.Fatal("unknown operation accepted")
	}
	for _, op := range []SemanticOperation{
		{Name: "integer.literal", Type: integerType(64, false), Text: "18446744073709551616"},
		{Name: "integer.literal", Type: integerType(8, true), Text: "128"},
		{Name: "integer.literal", Type: integerType(64, true), Text: "01"},
		{Name: "integer.literal", Type: integerType(7, true), Text: "1"},
		{Name: "integer.divide", Type: integerType(64, true)},
	} {
		if err := op.validate(0); err == nil {
			t.Errorf("invalid operation accepted: %+v", op)
		}
	}
	p, err := LowerNativeGo("a.go", `package main; import "fmt"; func main(){fmt.Println(uint64(9007199254740993))}`)
	if err != nil {
		t.Fatal(err)
	}
	// Matrix guards are derived from the executable operations, not optional
	// producer-supplied requirement strings that can be removed from JSON.
	p.Contracts.Requires = nil
	if _, err := EmitSemantic("zig", p); err == nil {
		t.Fatal("stripped requirements bypassed implementation matrix")
	}
	for _, source := range []string{
		`package main;func main(){var a uint64=1;a=a/uint64(0)}`,
		`package main;func main(){var a uint64=1;a=a<<uint64(2)}`,
		`package main;func main(){var a float64=1;a=a+1}`,
	} {
		if _, err := LowerNativeGo("unsupported.go", source); err == nil {
			t.Fatal("unsupported integer semantics accepted", source)
		}
	}
}

func TestExactIntegerCannotUseImplicitLegacySemantics(t *testing.T) {
	literal := func() Expr {
		return &OperationExpr{Operation: SemanticOperation{Name: "integer.literal", Type: integerType(64, false), Text: "9007199254740993"}}
	}
	for name, statement := range map[string]Stmt{
		"truth":      &IfStmt{Cond: &IdentExpr{Name: "x"}, Then: &BlockStmt{}},
		"arithmetic": &ExprStmt{X: &BinaryExpr{Op: "+", L: &IdentExpr{Name: "x"}, R: &LiteralExpr{Kind: "number", Text: "1"}}},
		"print":      &ExprStmt{X: &CallExpr{Fun: &IdentExpr{Name: "print"}, Args: []Arg{{Value: &IdentExpr{Name: "x"}}}}},
		"index":      &ExprStmt{X: &IndexExpr{X: &LiteralExpr{Kind: "string", Text: "\"a\""}, Args: []Arg{{Value: &IdentExpr{Name: "x"}}}}},
	} {
		t.Run(name, func(t *testing.T) {
			p := NewSemanticProgram(&BlockStmt{List: []Stmt{&AssignStmt{Name: "x", Op: "<-", Value: literal()}, statement}}, "eager_left_to_right")
			if err := ValidateSemanticProgram(p); err == nil {
				t.Fatal("exact integer entered unmodeled legacy semantics")
			}
		})
	}
}
