// signature-bind exposes the shared binder for differential matrix verification.
package main

import (
	"encoding/json"
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"io"
	"os"
)

type request struct {
	Parameters []backend.SignatureParameter `json:"parameters"`
	Arguments  []backend.SignatureArgument  `json:"arguments"`
}
type result struct {
	Binding *backend.SignatureBinding `json:"binding,omitempty"`
	Error   string                    `json:"error,omitempty"`
}

func run() error {
	var cases []request
	d := json.NewDecoder(os.Stdin)
	d.DisallowUnknownFields()
	if err := d.Decode(&cases); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected one JSON request array")
	}
	results := make([]result, len(cases))
	for i, c := range cases {
		var err error
		results[i].Binding, err = backend.BindSignature(c.Parameters, c.Arguments)
		if err != nil {
			results[i].Error = err.Error()
		}
	}
	return json.NewEncoder(os.Stdout).Encode(results)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
