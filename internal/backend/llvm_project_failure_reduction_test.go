// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestReduceLLVMProjectFailureMatrix(t *testing.T) {
	out := filepath.Join("..", "..", "outputs", "llvm-project-174-2026-09-10")
	input := filepath.Join(out, "error-matrix.csv")
	file, err := os.Open(input)
	if err != nil {
		t.Skipf("measurement matrix unavailable: %v", err)
	}
	defer file.Close()
	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) < 2 {
		t.Skip("measurement matrix contains no failure rows")
	}
	type family struct {
		family, primitive, contract string
		count                       int
		units                       []string
	}
	families := map[string]*family{}
	for _, row := range records[1:] {
		if len(row) < 7 || strings.TrimSpace(row[0]) == "" {
			continue
		}
		key := strings.Join([]string{row[3], row[4], row[5]}, "|")
		f := families[key]
		if f == nil {
			f = &family{family: row[3], primitive: row[4], contract: row[5]}
			families[key] = f
		}
		f.count++
		f.units = append(f.units, row[0])
	}
	keys := make([]string, 0, len(families))
	for key := range families {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	outFile, err := os.Create(filepath.Join(out, "error-families.csv"))
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(outFile)
	_ = w.Write([]string{"family", "primitive", "contract", "occurrences", "units", "implementation_status"})
	for _, key := range keys {
		f := families[key]
		sort.Strings(f.units)
		_ = w.Write([]string{f.family, f.primitive, f.contract, fmt.Sprint(f.count), strings.Join(f.units, "|"), llvmFailureFamilyStatus(f.family, f.primitive)})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		_ = outFile.Close()
		t.Fatal(err)
	}
	if err := outFile.Close(); err != nil {
		t.Fatal(err)
	}
	primitiveFile, err := os.Create(filepath.Join(out, "error-primitives.csv"))
	if err != nil {
		t.Fatal(err)
	}
	type primitiveRow struct {
		family, primitive, contract string
		count                       int
		units                       []string
	}
	primitives := map[string]*primitiveRow{}
	for _, row := range records[1:] {
		if len(row) < 7 || strings.TrimSpace(row[0]) == "" {
			continue
		}
		primitive, contract := reduceLLVMFailurePrimitive(row[3], row[4], row[5], row[6])
		key := strings.Join([]string{row[3], primitive, contract}, "|")
		p := primitives[key]
		if p == nil {
			p = &primitiveRow{family: row[3], primitive: primitive, contract: contract}
			primitives[key] = p
		}
		p.count++
		p.units = append(p.units, row[0])
	}
	primitiveKeys := make([]string, 0, len(primitives))
	for key := range primitives {
		primitiveKeys = append(primitiveKeys, key)
	}
	sort.Strings(primitiveKeys)
	pw := csv.NewWriter(primitiveFile)
	_ = pw.Write([]string{"family", "primitive", "contract", "occurrences", "units", "implementation_status"})
	for _, key := range primitiveKeys {
		p := primitives[key]
		sort.Strings(p.units)
		_ = pw.Write([]string{p.family, p.primitive, p.contract, fmt.Sprint(p.count), strings.Join(p.units, "|"), llvmFailurePrimitiveStatus(p.family, p.primitive)})
	}
	pw.Flush()
	if err := pw.Error(); err != nil {
		_ = primitiveFile.Close()
		t.Fatal(err)
	}
	if err := primitiveFile.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("LLVM_FAILURE_FAMILIES=%d LLVM_FAILURE_PRIMITIVES=%d MATRIX_ROWS=%d OUTPUT=%s", len(families), len(primitives), len(records)-1, filepath.Join(out, "error-primitives.csv"))
}

func reduceLLVMFailurePrimitive(family, primitive, contract, detail string) (string, string) {
	detail = strings.ToLower(detail)
	if strings.Contains(detail, "deref node") {
		return "deref-pointee-resolution", "pointer-pointee-contract"
	}
	if strings.Contains(detail, "index node") {
		return "bounded-index-access", "aggregate-length-contract"
	}
	if strings.Contains(detail, "for node") {
		return "bounded-iteration", "aggregate-length-contract"
	}
	if strings.Contains(detail, "structural=return") || strings.Contains(detail, "structural=returnstmt") {
		return "return-statement-dispatch", "uast-return-primitive"
	}
	if strings.Contains(detail, "structural=assign") || strings.Contains(detail, "structural=assignstmt") {
		return "assignment-statement-dispatch", "uast-assignment-primitive"
	}
	if strings.Contains(detail, "structural=callexpr") || strings.Contains(detail, "call node") {
		return "call-expression-dispatch", "callable-signature"
	}
	if strings.Contains(detail, "cannot coerce") && strings.Contains(detail, " to ptr") {
		return "pointer-representation-conversion", "aggregate-element-pointer-layout"
	}
	if strings.Contains(detail, "requires a bounded aggregate") || strings.Contains(detail, "proven length contract") {
		return "bounded-aggregate-access", "aggregate-length-contract"
	}
	if strings.Contains(detail, "no llvm element layout") {
		return "aggregate-element-layout", "aggregate-element-type-contract"
	}
	if strings.Contains(detail, "named aggregate layout") || strings.Contains(detail, "named product layout") {
		return "named-product-layout", "aggregate-field-contract"
	}
	if strings.Contains(detail, "binary operator") || strings.Contains(detail, "scalar-expression") {
		return "scalar-operation", "llvm-primitive-operation"
	}
	if strings.Contains(detail, "structural=") || strings.Contains(detail, "no llvm statement primitive") {
		return "statement-structural-dispatch", "uast-statement-primitive"
	}
	if strings.Contains(detail, "parameter") && strings.Contains(detail, "no canonical type") {
		return "callable-parameter-type", "callable-signature"
	}
	if strings.Contains(detail, "result contract") {
		return "callable-result-type", "callable-signature"
	}
	if strings.Contains(detail, "has ") && strings.Contains(detail, "arguments") {
		return "callable-arity", "callable-signature"
	}
	if family == "UNIT_COMPILE" {
		return "unit-contract", "canonical-unit"
	}
	return primitive, contract
}

func llvmFailurePrimitiveStatus(family, primitive string) string {
	if family == "CALL_ABI" && (primitive == "callable-parameter-type" || primitive == "callable-result-type" || primitive == "callable-arity") {
		return "FAIL_CLOSED_REQUIRES_COMPLETE_CALLABLE_CONTRACT"
	}
	return "MEASURED_NOT_IMPLEMENTED"
}

func llvmFailureFamilyStatus(family, primitive string) string {
	if family == "CALL_ABI" && primitive == "contract-presence" {
		return "FAIL_CLOSED_REQUIRES_FRONTEND_CONTRACT"
	}
	return "MEASURED_NOT_IMPLEMENTED"
}
