// Copyright (c) 2026 Tarek Wasfy
//go:build windows

package backend

import (
	"fmt"
	"syscall"
	"unsafe"
)

// nativeTaskControl is the target runtime's Windows thread handle contract.
// The callback address is retained until join so the OS cannot outlive the
// callback registration owned by the Go runtime.
type nativeTaskControl struct {
	handle   syscall.Handle
	callback uintptr
	joined   bool
}

var (
	kernel32CreateThread        = syscall.NewLazyDLL("kernel32.dll").NewProc("CreateThread")
	kernel32WaitForSingleObject = syscall.NewLazyDLL("kernel32.dll").NewProc("WaitForSingleObject")
	kernel32CloseHandle         = syscall.NewLazyDLL("kernel32.dll").NewProc("CloseHandle")
)

func startNativeTask(fn func()) (*nativeTaskControl, error) {
	callback := syscall.NewCallback(func(uintptr) uintptr {
		fn()
		return 0
	})
	var threadID uint32
	r1, _, callErr := kernel32CreateThread.Call(0, 0, callback, 0, 0, uintptr(unsafe.Pointer(&threadID)))
	if r1 == 0 {
		if callErr != syscall.Errno(0) {
			return nil, fmt.Errorf("CreateThread: %w", callErr)
		}
		return nil, fmt.Errorf("CreateThread failed")
	}
	return &nativeTaskControl{handle: syscall.Handle(r1), callback: callback}, nil
}

func (t *nativeTaskControl) join() error {
	if t == nil || t.joined {
		return nil
	}
	const infinite = ^uint32(0)
	waitResult, _, waitErr := kernel32WaitForSingleObject.Call(uintptr(t.handle), uintptr(infinite))
	if waitResult == 0xffffffff {
		return fmt.Errorf("WaitForSingleObject: %w", waitErr)
	}
	closeResult, _, closeErr := kernel32CloseHandle.Call(uintptr(t.handle))
	t.joined = true
	if closeResult == 0 {
		return fmt.Errorf("CloseHandle: %w", closeErr)
	}
	return nil
}
