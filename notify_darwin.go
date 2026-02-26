//go:build darwin

package main

import (
	"fmt"
	"os/exec"
)

func sendNotification(title, body string) error {
	script := fmt.Sprintf(`display notification %q with title %q`, body, title)
	return exec.Command("osascript", "-e", script).Run()
}
