package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var lockFile = kernel32.NewProc("LockFileEx")
var unlockFile = kernel32.NewProc("UnlockFileEx")

func lockChat(id string) (func(), error) {
	dir := filepath.Join(dataHome(), "locks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, id+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	overlap := &syscall.Overlapped{}
	ok, _, callErr := lockFile.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(overlap)))
	if ok == 0 {
		f.Close()
		return nil, fmt.Errorf("another update is running: %w", callErr)
	}
	return func() { unlockFile.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(overlap))); f.Close() }, nil
}
