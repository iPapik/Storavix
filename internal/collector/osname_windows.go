package collector

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/registry"
)

func OSName() string {
	root := os.Getenv("SystemRoot")
	if filepath.IsAbs(root) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		powershell := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		output, err := runStorageCLI(ctx, powershell, "-NoProfile", "-NonInteractive", "-Command",
			`$ErrorActionPreference = 'Stop'; [Console]::OutputEncoding = New-Object System.Text.UTF8Encoding -ArgumentList $false; (Get-CimInstance Win32_OperatingSystem).Caption`)
		if err == nil {
			if name := clean(string(output)); name != "" {
				return name
			}
		}
	}
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()
	name, _, _ := key.GetStringValue("ProductName")
	return clean(name)
}
