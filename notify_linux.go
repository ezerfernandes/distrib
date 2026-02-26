//go:build linux

package main

import "os/exec"

func sendNotification(title, body string) error {
	path, err := exec.LookPath("notify-send")
	if err != nil {
		return nil // silently skip if not available
	}
	return exec.Command(path, title, body).Run()
}
