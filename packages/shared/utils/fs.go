package utils

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

func CreateDirAllIfNotExists(dir string) error {
	stat, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			err = os.MkdirAll(dir, 0o777)
			return nil
		}
		return err
	}
	if !stat.IsDir() {
		return fmt.Errorf("%s exists but not dir", dir)
	}
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
