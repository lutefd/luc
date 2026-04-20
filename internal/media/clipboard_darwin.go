//go:build darwin

package media

import (
	"encoding/base64"
	"errors"
	osexec "os/exec"
	"strings"
)

func clipboardImageBytes() ([]byte, error) {
	script := strings.Join([]string{
		"import AppKit",
		"import Foundation",
		"let pb = NSPasteboard.general",
		"func emit(_ data: Data) { FileHandle.standardOutput.write(data.base64EncodedData()) }",
		"if let data = pb.data(forType: .png) { emit(data); exit(0) }",
		"if let tiff = pb.data(forType: .tiff), let image = NSImage(data: tiff), let imageTIFF = image.tiffRepresentation, let rep = NSBitmapImageRep(data: imageTIFF), let png = rep.representation(using: .png, properties: [:]) { emit(png); exit(0) }",
		"exit(1)",
	}, "\n")

	output, err := osexec.Command("swift", "-e", script).Output()
	if err != nil || len(output) == 0 {
		return nil, errors.New("no image found in clipboard")
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(output)))
	if err != nil {
		return nil, err
	}
	return data, nil
}
