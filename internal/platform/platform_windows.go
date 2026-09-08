// Copyright (c) 2026 Tarek Wasfy
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
	"unsafe"
)

const (
	attachParentProcess       = uintptr(0xFFFFFFFF)
	threadPriorityAboveNormal = uintptr(1)
)

// SetPath elevates once and adds the executable directory to the machine PATH.
func SetPath(exe string) error {
	dir, err := filepath.Abs(filepath.Dir(exe))
	if err != nil {
		return err
	}
	// Install the documented `sp` command beside the executable.  PATH then
	// exposes both codetranspiler.exe and this stable command alias.
	name := filepath.Base(exe)
	shim := filepath.Join(dir, "sp.cmd")
	shimText := "@echo off\r\n\"%~dp0" + name + "\" %*\r\n"
	// The install directory may require elevation, so a local write is only a
	// fast path; the elevated command below writes it again when necessary.
	_ = os.WriteFile(shim, []byte(shimText), 0644)
	q := strings.ReplaceAll(dir, "'", "''")
	qe := strings.ReplaceAll(exe, "'", "''")
	qs := strings.ReplaceAll(shim, "'", "''")
	script := "$d='" + q + "';$e='" + qe + "';$s='" + qs + "';Set-Content -LiteralPath $s -Value ('@echo off`r`n\"%~dp0' + (Split-Path -Leaf $e) + '\" %*`r`n') -Encoding ASCII;$p=[Environment]::GetEnvironmentVariable('Path','Machine');if(-not (($p -split ';') -contains $d)){[Environment]::SetEnvironmentVariable('Path',(($p.TrimEnd(';')+';'+$d).Trim(';')),'Machine')}"
	arg := "-NoProfile -Command \"" + strings.ReplaceAll(script, "\"", "\\\"") + "\""
	return exec.Command("powershell.exe", "-NoProfile", "-Command", "Start-Process powershell.exe -Verb RunAs -ArgumentList '"+strings.ReplaceAll(arg, "'", "''")+"'").Run()
}

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	shell32               = syscall.NewLazyDLL("shell32.dll")
	procShellExecute      = shell32.NewProc("ShellExecuteW")
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

	// Use ShellExecuteW like the working launcher.  /K keeps the prompt open.
	// Start in the executable directory first, then invoke the executable with
	// a separately quoted command. This avoids cmd.exe treating a spaced path
	// as its optional window-title argument.
	verb := syscall.StringToUTF16Ptr("open")
	app := syscall.StringToUTF16Ptr(comspec)
	params := syscall.StringToUTF16Ptr(`/K cd /d "` + dir + `" && "` + exe + `" help`)
	work := syscall.StringToUTF16Ptr(dir)
	result, _, callErr := procShellExecute.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(app)), uintptr(unsafe.Pointer(params)), uintptr(unsafe.Pointer(work)), 1)
	if result <= 32 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return callErr
		}
		return fmt.Errorf("ShellExecuteW failed: %d", result)
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
