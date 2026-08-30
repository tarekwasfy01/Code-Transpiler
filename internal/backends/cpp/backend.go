package cpp

import _ "embed"

//go:embed runtime.hpp.txt
var runtimeSource string

func Transpile(src string) (string, error) {
	ast, err := parse(src)
	if err != nil {
		return "", err
	}
	return generate(ast)
}
