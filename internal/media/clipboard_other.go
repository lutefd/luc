//go:build !darwin && !windows && !linux

package media

import "errors"

func clipboardImageBytes() ([]byte, error) {
	return nil, errors.New("clipboard image paste is not supported on this platform")
}
