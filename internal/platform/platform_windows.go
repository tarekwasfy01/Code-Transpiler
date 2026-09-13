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

// SetPath installs the sp.cmd alias and adds its directory to the per-user
// PATH synchronously.  A user PATH is sufficient for the CLI and avoids an
// asynchronous elevation race where `sp` was unavailable immediately after
// the command returned.
func SetPath(exe string) error {
	var err error
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}
	// Keep the shim writable and stable even when the executable is installed
	// under a protected directory.  It points at the concrete EXE path rather
	// than relying on the caller's working directory.
	shimDir := filepath.Join(os.Getenv("LOCALAPPDATA"), "CodeTranspiler")
	if shimDir == "CodeTranspiler" {
		shimDir, _ = os.UserConfigDir()
		shimDir = filepath.Join(shimDir, "CodeTranspiler")
	}
	if err := os.MkdirAll(shimDir, 0755); err != nil {
		return err
	}
	shim := filepath.Join(shimDir, "sp.cmd")
	quotedExe := strings.ReplaceAll(exe, "\"", "\"\"")
	shimText := "@echo off\r\n\"" + quotedExe + "\" %*\r\n"
	if err := os.WriteFile(shim, []byte(shimText), 0644); err != nil {
		return err
	}
	q := strings.ReplaceAll(shimDir, "'", "''")
	script := "$d='" + q + "';$p=[Environment]::GetEnvironmentVariable('Path','User');if(-not (($p -split ';') -contains $d)){[Environment]::SetEnvironmentVariable('Path',(($p.TrimEnd(';')+';'+$d).Trim(';')),'User')}"
	if err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Run(); err != nil {
		return err
	}
	// Make the alias available to commands launched from this process too.
	if current := os.Getenv("Path"); !strings.Contains(";"+strings.ToLower(current)+";", ";"+strings.ToLower(shimDir)+";") {
		_ = os.Setenv("Path", strings.Trim(current, ";")+";"+shimDir)
	}
	return nil
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

func SelectFolderDialog(title string) (string, error) {
	safe := strings.ReplaceAll(title, "'", "''")
	// OpenFileDialog uses the full Explorer shell UI (address bar, editable
	// paths, breadcrumbs and copy/paste) while ValidateNames=false turns the
	// selection into a folder picker. FolderBrowserDialog intentionally is not
	// used because its compact tree view cannot accept or copy full paths.
	script := `$ErrorActionPreference='Stop'
Add-Type -AssemblyName System.Windows.Forms
$d=New-Object System.Windows.Forms.OpenFileDialog
$d.Title='` + safe + `'
$d.CheckFileExists=$false
$d.CheckPathExists=$true
$d.ValidateNames=$false
$d.DereferenceLinks=$true
$d.AddExtension=$false
$d.FileName='Select this folder'
$d.Filter='Folders|*'
try {
 if($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK){
  $p=$d.FileName
  $leaf=[System.IO.Path]::GetFileName($p)
  if($leaf -eq 'Select this folder' -or $leaf -eq 'Select this folder.folder'){ $p=[System.IO.Path]::GetDirectoryName($p) }
  [Console]::Out.Write($p)
 }
} finally { $d.Dispose() }`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-STA", "-WindowStyle", "Hidden", "-EncodedCommand", encodePowerShell(script))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("open folder dialog: %w", err)
	}
	return filepath.Clean(strings.TrimSpace(string(out))), nil
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
