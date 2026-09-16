package nasm

import (
	"github.com/tarekwasfy01/Code-Transpiler/v2/internal/backend/x86encode"
)

// EncodeNativeWitness routes a concrete assembly witness through the
// repository's own x86 encoder. NASM remains external oracle only.
func EncodeNativeWitness(assembly string) ([]byte, error) {
	instructions, err := x86encode.ParseAssembly(assembly)
	if err != nil {
		return nil, err
	}
	return x86encode.EncodeProgram(instructions)
}
