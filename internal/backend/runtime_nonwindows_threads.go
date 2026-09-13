// Copyright (c) 2026 Tarek Wasfy
//go:build !windows

package backend

type nativeTaskControl struct{}

func startNativeTask(fn func()) (*nativeTaskControl, error) {
	go fn()
	return &nativeTaskControl{}, nil
}

func (t *nativeTaskControl) join() error { return nil }
