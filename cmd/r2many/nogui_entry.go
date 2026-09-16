//go:build !gui

package main

import "fmt"

// CLI builds intentionally exclude Gio and the GUI package. Build with
// -tags gui for the GUI executable.
func launchGUI() {
	fmt.Println("GUI is not included in this CLI build; build with -tags gui")
}
