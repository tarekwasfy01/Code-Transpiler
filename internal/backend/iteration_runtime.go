package backend

import "fmt"

// snapshotIteration projects one value onto a private outer iteration vector.
// The projection preserves element types and nesting: NULL -> [], scalar ->
// [scalar], vector -> a copy of its outer storage. In particular, it does not
// invoke user-visible c/list functions or flatten nested vectors.
func (g *targetGen) snapshotIteration(value string) (string, error) {
	helper := g.freshName("snapshot")
	var code string
	call := helper + "(" + value + ")"
	switch g.target {
	case "go":
		code = "func " + helper + "(value any) any { if value == nil { return []any{} }; if items, ok := value.([]any); ok { copied := make([]any, len(items)); copy(copied, items); return copied }; return []any{value} }\n"
	case "python":
		code = "def " + helper + "(value):\n    if value is None: return []\n    if isinstance(value, list): return list(value)\n    return [value]\n"
	case "rust":
		// RValue owns all its storage; consuming the value transfers a private
		// vector. Expressions referring to state slots already clone their value.
		code = "fn " + helper + "(value: RValue) -> RValue { match value { RValue::Null => RValue::Vec(Vec::new()), RValue::Vec(items) => RValue::Vec(items), scalar => RValue::Vec(vec![scalar]) } }\n"
	case "cpp":
		code = "static RValue " + helper + "(const RValue& value) { if (std::holds_alternative<std::monostate>(value.v)) return RValue(std::vector<RValue>{}); if (auto items = std::get_if<std::vector<RValue>>(&value.v)) return RValue(*items); return RValue(std::vector<RValue>{value}); }\n"
	case "c":
		code = "static RValue " + helper + "(RValue value) {\n" +
			"    size_t count = value.t == R_NULL ? 0 : (value.t == R_VEC ? value.len : 1);\n" +
			"    if (count > (size_t)-1 / sizeof(RValue)) { fprintf(stderr, \"iteration snapshot size overflow\\n\"); exit(1); }\n" +
			"    if (value.t == R_VEC && count && !value.v) { fprintf(stderr, \"invalid iteration vector\\n\"); exit(1); }\n" +
			"    RValue* items = count ? (RValue*)malloc(count * sizeof(RValue)) : NULL;\n" +
			"    if (count && !items) { fprintf(stderr, \"iteration snapshot allocation failed\\n\"); exit(1); }\n" +
			"    if (count) { if (value.t == R_VEC) memcpy(items, value.v, count * sizeof(RValue)); else items[0] = value; }\n" +
			"    RValue result = {R_VEC, 0, NULL, items, count}; return result;\n}\n"
	case "nim":
		code = "proc " + helper + "(value: RValue): RValue =\n" +
			"    let source = rIter(value)\n" +
			"    var copied = newSeq[RValue](source.len)\n" +
			"    for i in 0..<source.len: copied[i] = source[i]\n" +
			"    return rVec(copied)\n"
	case "java":
		// Helpers are emitted beside R2 and Main, not inside either class.
		code = "final class " + helper + " { static Object call(Object value) { if (value == null) return new Object[0]; if (value instanceof Object[]) return ((Object[])value).clone(); return new Object[]{value}; } }\n"
		call = helper + ".call(" + value + ")"
	case "csharp":
		code = "static class " + helper + " { public static object Call(object value) { if (value == null) return new object[0]; if (value is object[] items) return (object[])items.Clone(); return new object[]{value}; } }\n"
		call = helper + ".Call(" + value + ")"
	case "julia":
		code = "function " + helper + "(value)\n    value === nothing && return Any[]\n    value isa AbstractArray && return Any[item for item in value]\n    return Any[value]\nend\n"
	case "kotlin":
		code = "fun " + helper + "(value: Any?): Any { if (value == null || value === RValue.Null) return emptyArray<Any?>(); if (value is Array<*>) return value.copyOf(); return arrayOf(value) }\n"
	case "swift":
		code = "func " + helper + "(_ value: Any) -> Any { if case RValue.null = value { return [Any]() }; if let items = value as? [Any] { return Array(items) }; return [value] }\n"
	case "zig":
		code = "fn " + helper + "(value: RValue) RValue {\n" +
			"    switch (value) {\n" +
			"        .null => return .{ .vec = &[_]RValue{} },\n" +
			"        .vec => |items| return .{ .vec = std.heap.page_allocator.dupe(RValue, items) catch @panic(\"iteration snapshot allocation failed\") },\n" +
			"        else => { const items = std.heap.page_allocator.alloc(RValue, 1) catch @panic(\"iteration snapshot allocation failed\"); items[0] = value; return .{ .vec = items }; },\n" +
			"    }\n}\n"
	default:
		return "", fmt.Errorf("target %s has no iteration snapshot projection", g.target)
	}
	// Snapshot semantics were established from UAST before this renderer.  The
	// target fragment is registered by purpose, never used as semantic state.
	g.requireHelper("helper.iteration.snapshot", code)
	return call, nil
}
