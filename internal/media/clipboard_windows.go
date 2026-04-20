//go:build windows

package media

import (
	"encoding/base64"
	"errors"
	osexec "os/exec"
	"strings"
)

func clipboardImageBytes() ([]byte, error) {
	script := strings.Join([]string{
		"Add-Type -AssemblyName System.Windows.Forms",
		"Add-Type -AssemblyName System.Drawing",
		"$img = [System.Windows.Forms.Clipboard]::GetImage()",
		"if ($img -eq $null) { exit 1 }",
		"$ms = New-Object System.IO.MemoryStream",
		"$img.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)",
		"[Convert]::ToBase64String($ms.ToArray())",
	}, "; ")

	output, err := osexec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil || len(output) == 0 {
		return nil, errors.New("no image found in clipboard")
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(output)))
	if err != nil {
		return nil, err
	}
	return data, nil
}
