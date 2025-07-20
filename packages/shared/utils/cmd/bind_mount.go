package main

import (
	"flag"
	"fmt"
	"syscall"

	"golang.org/x/sys/unix"
)

func main() {
	var recursive, umount bool
	flag.BoolVar(&recursive, "r", false, "Mount recursively")
	flag.BoolVar(&umount, "u", false, "Unmount instead of bind mount")
	flag.Parse()

	if umount {
		if flag.NArg() != 1 {
			panic("Usage: bind_mount -u <mountpoint>")
		}
		if err := unix.Unmount(flag.Arg(0), syscall.MNT_DETACH); err != nil {
			msg := fmt.Sprintf("Error unmounting %s: %v\n", flag.Arg(0), err)
			panic(msg)
		}
	} else {
		if flag.NArg() != 2 {
			panic("Usage: bind_mount <old_dir> <new_dir>")
		}
		src := flag.Arg(0)
		dst := flag.Arg(1)
		var f uintptr = syscall.MS_BIND
		if recursive {
			f |= syscall.MS_REC
		}
		if err := unix.Mount(src, dst, "", f, ""); err != nil {
			msg := fmt.Sprintf("Error mounting %s to %s: %v\n", src, dst, err)
			panic(msg)
		}
	}
}
