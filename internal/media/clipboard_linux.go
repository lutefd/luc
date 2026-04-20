//go:build linux

package media

import (
	"errors"
	"os"
	osexec "os/exec"
)

func clipboardImageBytes() ([]byte, error) {
	// Wayland: wl-paste
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		out, err := osexec.Command("wl-paste", "--no-newline", "--type", "image/png").Output()
		if err == nil && len(out) > 0 {
			return out, nil
		}
	}
	// X11: xclip
	out, err := osexec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o").Output()
	if err == nil && len(out) > 0 {
		return out, nil
	}
	return nil, errors.New("no image found in clipboard (install wl-paste for Wayland or xclip for X11)")
}
