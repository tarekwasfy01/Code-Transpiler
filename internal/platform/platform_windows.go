//go:build windows

package platform

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
)

const (
	attachParentProcess       = uintptr(0xFFFFFFFF)
	threadPriorityAboveNormal = uintptr(1)
)

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole     = kernel32.NewProc("AttachConsole")
	procGetCurrentThread  = kernel32.NewProc("GetCurrentThread")
	procSetThreadPriority = kernel32.NewProc("SetThreadPriority")
)

func EnsureCLIConsole() {
	if _, err := os.Stdout.Stat(); err == nil {
		return
	}
	_, _, _ = procAttachConsole.Call(attachParentProcess)
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout = f
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stderr = f
	}
	if f, err := os.OpenFile("CONIN$", os.O_RDONLY, 0); err == nil {
		os.Stdin = f
	}
}
func BoostGUIThread() {
	h, _, _ := procGetCurrentThread.Call()
	if h == 0 {
		return
	}
	_, _, _ = procSetThreadPriority.Call(h, threadPriorityAboveNormal)
}
func OpenCMD(exe string) error {
	comspec := os.Getenv("ComSpec")
	if comspec == "" {
		comspec = `C:\\Windows\\System32\\cmd.exe`
	}
	exe, err := filepath.Abs(exe)
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)

	// Build a tiny batch bootstrap. cmd.exe is started with /K so it remains
	// interactive after the help command has completed.
	f, err := os.CreateTemp("", "uct-cli-*.cmd")
	if err != nil {
		return fmt.Errorf("create CLI bootstrap: %w", err)
	}
	scriptPath := f.Name()
	script := "@echo off\r\n" +
		"cd /d \"" + dir + "\"\r\n" +
		"title Code Transpiler CLI\r\n" +
		"echo.\r\n" +
		"start \"\" /wait /b \"" + exe + "\" help\r\n" +
		"echo.\r\n" +
		"echo Code Transpiler CLI ready.\r\n" +
		"echo Current directory: %CD%\r\n" +
		"echo Type CodeTranspiler.exe help to show the commands again.\r\n"
	if _, err := f.WriteString(script); err != nil {
		_ = f.Close()
		_ = os.Remove(scriptPath)
		return fmt.Errorf("write CLI bootstrap: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(scriptPath)
		return fmt.Errorf("close CLI bootstrap: %w", err)
	}

	command := `call "` + scriptPath + `"`
	cmd := exec.Command(comspec, "/D", "/K", command)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x00000010, // CREATE_NEW_CONSOLE
	}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(scriptPath)
		return err
	}
	return nil
}
func SaveSourceFileDialog(defaultName, ext, label string) (string, error) {
	if defaultName == "" {
		defaultName = "output" + ext
	}
	if ext == "" {
		ext = filepath.Ext(defaultName)
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	safeName := strings.ReplaceAll(defaultName, "'", "''")
	desc := strings.ReplaceAll(label, "'", "''")
	if desc == "" {
		desc = "Source"
	}
	plain := strings.TrimPrefix(ext, ".")
	script := `$ErrorActionPreference='Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
Add-Type -AssemblyName System.Windows.Forms
$dialog = New-Object System.Windows.Forms.SaveFileDialog
$dialog.Filter = '` + desc + ` files (*` + ext + `)|*` + ext + `|All files (*.*)|*.*'
$dialog.DefaultExt = '` + plain + `'
$dialog.AddExtension = $true
$dialog.OverwritePrompt = $true
$dialog.FileName = '` + safeName + `'
try {
 if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
  [Console]::Out.Write($dialog.FileName)
 }
} finally { $dialog.Dispose() }`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-STA", "-WindowStyle", "Hidden", "-EncodedCommand", encodePowerShell(script))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("open Save As dialog: %w", err)
	}
	p := strings.TrimSpace(string(out))
	if p == "" {
		return "", nil
	}
	if filepath.Ext(p) == "" {
		p += ext
	}
	return filepath.Clean(p), nil
}
func encodePowerShell(script string) string {
	words := utf16.Encode([]rune(script))
	data := make([]byte, len(words)*2)
	for i, w := range words {
		data[i*2] = byte(w)
		data[i*2+1] = byte(w >> 8)
	}
	return base64.StdEncoding.EncodeToString(data)
}
