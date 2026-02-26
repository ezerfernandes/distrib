//go:build windows

package main

import (
	"fmt"
	"os/exec"
)

func sendNotification(title, body string) error {
	ps := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
$n = New-Object System.Windows.Forms.NotifyIcon
$n.Icon = [System.Drawing.SystemIcons]::Information
$n.Visible = $true
$n.ShowBalloonTip(5000, %q, %q, 'Info')
Start-Sleep -Seconds 6
$n.Dispose()
`, title, body)
	return exec.Command("powershell", "-NoProfile", "-Command", ps).Run()
}
