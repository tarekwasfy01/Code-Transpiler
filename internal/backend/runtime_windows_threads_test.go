// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"sync/atomic"
	"testing"
)

func TestNativeWindowsThreadHandleJoinContract(t *testing.T) {
	var ran atomic.Int32
	control, err := startNativeTask(func() { ran.Store(1) })
	if err != nil {
		t.Fatalf("native thread creation failed: %v", err)
	}
	if control == nil {
		t.Fatal("native thread returned no handle control")
	}
	if err := control.join(); err != nil {
		t.Fatalf("native thread join failed: %v", err)
	}
	if ran.Load() != 1 {
		t.Fatal("joined native thread did not execute its callback")
	}
	if err := control.join(); err != nil {
		t.Fatalf("native thread handle was not idempotently closed: %v", err)
	}
}
