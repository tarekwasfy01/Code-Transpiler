package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	codetranspiler "github.com/tarekwasfy01/Code-Transpiler"
	"os"
)

func compileNative(args []string) error {
	fs := flag.NewFlagSet("compile", flag.ContinueOnError)
	source := fs.String("source", "go", "source language")
	target := fs.String("target", "native-x86_64-windows", "native-x86_64-windows, machine-x86_64, object-x86_64-windows, asm-x86_64")
	out := fs.String("o", "", "output file (required for binary output)")
	inputKind := fs.String("input", "source", "source|assembly|machine|object|executable")
	outputKind := fs.String("output", "", "source|assembly|machine|object|executable")
	arch := fs.String("arch", "x86_64", "target architecture")
	osName := fs.String("os", "windows", "target operating system")
	abi := fs.String("abi", "win64", "target ABI")
	hexOutput := fs.Bool("hex", false, "print machine code as hexadecimal")
	entry := fs.String("entry", "", "entry function (default main, otherwise module)")
	via := fs.Bool("via-assembly", false, "explicitly use NASM instead of the internal encoder")
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{"-source": true, "-target": true, "-o": true, "-entry": true, "-input": true, "-output": true, "-arch": true, "-os": true, "-abi": true, "-via-assembly": false, "--via-assembly": false, "--hex": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: compile -source <language> -target native-x86_64-windows input -o program.exe")
	}
	kinds := map[string]codetranspiler.CompileOutputKind{"native-x86_64-windows": codetranspiler.Executable, "object-x86_64-windows": codetranspiler.Object, "machine-x86_64": codetranspiler.MachineCode, "asm-x86_64": codetranspiler.Assembly}
	kind, ok := kinds[*target]
	if *outputKind != "" {
		for alias, candidate := range map[string]codetranspiler.CompileOutputKind{"source": codetranspiler.Source, "assembly": codetranspiler.Assembly, "machine": codetranspiler.MachineCode, "object": codetranspiler.Object, "executable": codetranspiler.Executable} {
			if *outputKind == alias {
				kind, ok = candidate, true
				break
			}
		}
	}
	if !ok {
		if *outputKind != "" {
			for alias, candidate := range map[string]codetranspiler.CompileOutputKind{"source": codetranspiler.Source, "assembly": codetranspiler.Assembly, "machine": codetranspiler.MachineCode, "object": codetranspiler.Object, "executable": codetranspiler.Executable} {
				if *outputKind == alias {
					kind, ok = candidate, true
					break
				}
			}
		}
	}
	if !ok {
		return fmt.Errorf("unsupported compile target %q", *target)
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	input := codetranspiler.InputSource
	switch *inputKind {
	case "assembly":
		input = codetranspiler.InputAssembly
	case "machine", "machine_code":
		input = codetranspiler.InputMachine
	case "object":
		input = codetranspiler.InputObject
	case "executable":
		input = codetranspiler.InputExecutable
	case "source":
	default:
		return fmt.Errorf("unsupported input kind %q", *inputKind)
	}
	result, err := codetranspiler.Compile(string(data), codetranspiler.CompileOptions{InputKind: input, SourceLanguage: *source, SourceArch: *arch, SourceAsmSyntax: "intel", TargetArch: *arch, TargetOS: *osName, ABI: *abi, OutputKind: kind, EntryPoint: *entry, ViaAssembly: *via})
	if err != nil {
		return err
	}
	output := result.Bytes
	if kind == codetranspiler.Assembly {
		output = []byte(result.Text)
	}
	if *hexOutput && kind == codetranspiler.MachineCode {
		if *out == "" {
			_, _ = fmt.Fprintln(os.Stdout, hex.EncodeToString(result.Bytes))
			return nil
		}
	}
	if *out == "" {
		return fmt.Errorf("-o is required for binary output")
	}
	return os.WriteFile(*out, output, 0644)
}
