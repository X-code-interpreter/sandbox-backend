package utils

import "os"

func MakeSureDir(dir string) error {
	_, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			err = os.MkdirAll(dir, 0o644)
		}
	}
	return err
}
