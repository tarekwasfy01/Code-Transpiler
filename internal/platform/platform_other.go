//go:build !windows

package platform

import (
	"fmt"
	"os"
	"path/filepath"
)

func EnsureCLIConsole()    {}
func BoostGUIThread()      {}
func OpenCMD(string) error { return fmt.Errorf("Open CMD is Windows-only") }
func SaveSourceFileDialog(defaultName, ext, label string) (string, error) {
	if defaultName == "" {
		defaultName = "output" + ext
	}
	return filepath.Join(".", defaultName), nil
}

var _ = os.Stdout
