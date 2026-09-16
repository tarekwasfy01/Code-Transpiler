//go:build gui

package main

import (
	"fmt"
	"os"

	"gioui.org/app"
	"github.com/tarekwasfy01/Code-Transpiler/internal/ui"
)

func launchGUI() {
	go func() {
		a := ui.New()
		if err := a.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}
