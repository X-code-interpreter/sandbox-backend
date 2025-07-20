package utils

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func CheckFileExists(path string) bool {
	if info, err := os.Stat(path); err == nil {
		return !info.IsDir()
	}
	return false
}

func CreateDirAllIfNotExists(dir string, perm fs.FileMode) error {
	stat, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(dir, perm)
		}
		return err
	}
	if !stat.IsDir() {
		return fmt.Errorf("%s exists but not dir", dir)
	}
	return nil
}

func CreateFileAndDirIfNotExists(filePath string, filePerm, dirPerm fs.FileMode) error {
	stat, err := os.Stat(filePath)
	if err == nil {
		// file exists
		if !stat.Mode().IsRegular() {
			return fmt.Errorf("%s exists but not regular file", filePath)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	if err := CreateDirAllIfNotExists(filepath.Dir(filePath), dirPerm); err != nil {
		return err
	}
	// After test, we confirm that only use O_CREATE | O_EXCL is fine.
	// The file handle is read only.
	osFile, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL, filePerm)
	if err != nil {
		return err
	}
	osFile.Close()
	return nil
}

func CheckDirExists(path string) bool {
	if info, err := os.Stat(path); err == nil {
		return info.IsDir()
	}
	return false
}

func DeleteDirWithRetry(path string) (err error) {
	// NOTE(huang-jl): maybe process has not been clean completely by kernel, so:
	// (1) retry rm cgroup dir for 3 times
	// (2) make remove cgroup at final step.
	sleepTimes := [3]time.Duration{
		200 * time.Millisecond,
		500 * time.Millisecond,
		1500 * time.Millisecond,
	}
	for _, sleepTime := range sleepTimes {
		if err = syscall.Rmdir(path); err == nil {
			break
		}
		time.Sleep(sleepTime)
	}
	return err
}
