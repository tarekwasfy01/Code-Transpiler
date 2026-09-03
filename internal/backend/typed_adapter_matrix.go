package backend

import "fmt"

// typedAdapter describes the target-local boundary of one universal typed
// operation. The operation name, bit width and signedness remain identical in
// every column; only the concrete target syntax changes. This prevents an
// adapter from accidentally falling back to Python syntax.
type typedAdapter struct {
	Target      string
	Runtime     string
	True, False string
	Arguments   func([]string) string
	Call        func(name string, bits int, signed string, text string, args []string) string
}

func typedAdapterFor(target string) (typedAdapter, bool) {
	vector := func(open, close string) func([]string) string {
		return func(values []string) string { return open + joinComma(values) + close }
	}
	call := func(runtime string, arguments func([]string) string) func(string, int, string, string, []string) string {
		return func(name string, bits int, signed, text string, args []string) string {
			return fmt.Sprintf("%s(%q, %d, %s, %q, %s)", runtime, name, bits, signed, text, arguments(args))
		}
	}
	adapters := map[string]typedAdapter{
		"python": {"python", "r_exact", "True", "False", vector("[", "]"), call("r_exact", vector("[", "]"))},
		"r":      {"r", "r_exact", "TRUE", "FALSE", vector("list(", ")"), call("r_exact", vector("list(", ")"))},
		"julia":  {"julia", "r_exact", "true", "false", vector("Any[", "]"), call("r_exact", vector("Any[", "]"))},
		"nim":    {"nim", "rExact", "true", "false", vector("@[", "]"), call("rExact", vector("@[", "]"))},
		"zig":    {"zig", "rExact", "true", "false", vector("&[_]RValue{", "}"), call("rExact", vector("&[_]RValue{", "}"))},
		"kotlin": {"kotlin", "rExact", "true", "false", vector("arrayOf(", ")"), call("rExact", vector("arrayOf(", ")"))},
		"swift":  {"swift", "rExact", "true", "false", vector("[", "]"), call("rExact", vector("[", "]"))},
	}
	a, ok := adapters[target]
	return a, ok
}

func joinComma(values []string) string {
	if len(values) == 0 {
		return ""
	}
	out := values[0]
	for _, value := range values[1:] {
		out += ", " + value
	}
	return out
}
