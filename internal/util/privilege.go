package util

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"runtime"
)

type SysInfo struct {
	OS     string
	Arch   string
	User   string
	IsRoot bool
}

func IsRoot() bool {
	if runtime.GOOS == "windows" {
		return false
	}

	return os.Geteuid() == 0
}

func HasSudo() bool {

	if runtime.GOOS == "windows" {
		return false
	}

	cmd := exec.Command(
		"sudo",
		"-n",
		"true",
	)

	err := cmd.Run()

	return err == nil
}

func RelaunchWithSudo() error {

	if runtime.GOOS == "windows" {
		return errors.New(
			"sudo relaunch unsupported on windows",
		)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	args := os.Args[1:]

	cmdArgs := append(
		[]string{exe},
		args...,
	)

	cmd := exec.Command(
		"sudo",
		cmdArgs...,
	)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		return err
	}

	os.Exit(0)

	return errors.New("unreachable")
}

func GetSysInfo() SysInfo {

	currentUser := "unknown"

	u, err := user.Current()
	if err == nil {
		currentUser = u.Username
	}

	return SysInfo{
		OS:     runtime.GOOS,
		Arch:   runtime.GOARCH,
		User:   currentUser,
		IsRoot: IsRoot(),
	}
}
