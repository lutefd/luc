package media

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/provider"
)

type Attachment struct {
	ID        string
	Name      string
	Type      string
	MediaType string
	Data      string
	Width     int
	Height    int
}

func (a Attachment) ToHistory() history.AttachmentPayload {
	return history.AttachmentPayload{
		ID:        a.ID,
		Name:      a.Name,
		Type:      a.Type,
		MediaType: a.MediaType,
		Data:      a.Data,
		Width:     a.Width,
		Height:    a.Height,
	}
}

func (a Attachment) ToContentPart() provider.ContentPart {
	return provider.ContentPart{
		Type:      a.Type,
		MediaType: a.MediaType,
		Data:      a.Data,
	}
}

func FromHistory(payload history.AttachmentPayload) Attachment {
	return Attachment{
		ID:        payload.ID,
		Name:      payload.Name,
		Type:      payload.Type,
		MediaType: payload.MediaType,
		Data:      payload.Data,
		Width:     payload.Width,
		Height:    payload.Height,
	}
}

func ToHistoryPayloads(attachments []Attachment) []history.AttachmentPayload {
	if len(attachments) == 0 {
		return nil
	}
	out := make([]history.AttachmentPayload, 0, len(attachments))
	for _, attachment := range attachments {
		out = append(out, attachment.ToHistory())
	}
	return out
}

func FromHistoryPayloads(payloads []history.AttachmentPayload) []Attachment {
	if len(payloads) == 0 {
		return nil
	}
	out := make([]Attachment, 0, len(payloads))
	for _, payload := range payloads {
		out = append(out, FromHistory(payload))
	}
	return out
}

func FromContentParts(parts []provider.ContentPart) []Attachment {
	var out []Attachment
	for _, part := range parts {
		if strings.TrimSpace(part.Type) != "image" {
			continue
		}
		out = append(out, Attachment{
			Type:      "image",
			MediaType: part.MediaType,
			Data:      part.Data,
		})
	}
	return out
}

func MessageParts(text string, attachments []Attachment) []provider.ContentPart {
	parts := make([]provider.ContentPart, 0, len(attachments)+1)
	if trimmed := strings.TrimSpace(text); trimmed != "" {
		parts = append(parts, provider.ContentPart{Type: "text", Text: trimmed})
	}
	for _, attachment := range attachments {
		parts = append(parts, attachment.ToContentPart())
	}
	if len(parts) == 0 {
		return nil
	}
	return parts
}

func BuildImageAttachment(id, name, mediaType, base64Data string) (Attachment, error) {
	mediaType = strings.TrimSpace(mediaType)
	base64Data = strings.TrimSpace(base64Data)
	if mediaType == "" {
		return Attachment{}, errors.New("image media type is required")
	}
	if base64Data == "" {
		return Attachment{}, errors.New("image data is required")
	}
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return Attachment{}, err
	}
	return buildImageAttachmentFromBytes(id, name, mediaType, base64Data, data)
}

func BuildImageAttachmentFromBytes(id, name, mediaType string, data []byte) (Attachment, error) {
	mediaType = detectMediaType(data, mediaType)
	if !strings.HasPrefix(mediaType, "image/") {
		return Attachment{}, fmt.Errorf("unsupported media type %q", mediaType)
	}
	return buildImageAttachmentFromBytes(id, name, mediaType, base64.StdEncoding.EncodeToString(data), data)
}

func BuildImageAttachmentFromFile(id, path string) (Attachment, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Attachment{}, errors.New("image path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Attachment{}, err
	}
	return BuildImageAttachmentFromBytes(id, filepath.Base(path), "", data)
}

func BuildImageAttachmentFromClipboard(id string) (Attachment, error) {
	data, err := clipboardImageBytes()
	if err != nil {
		return Attachment{}, err
	}
	return BuildImageAttachmentFromBytes(id, "", "image/png", data)
}

func buildImageAttachmentFromBytes(id, name, mediaType, base64Data string, data []byte) (Attachment, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Attachment{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = defaultImageName(mediaType)
	}
	return Attachment{
		ID:        id,
		Name:      name,
		Type:      "image",
		MediaType: mediaType,
		Data:      base64Data,
		Width:     cfg.Width,
		Height:    cfg.Height,
	}, nil
}

func detectMediaType(data []byte, fallback string) string {
	mediaType := strings.TrimSpace(fallback)
	if mediaType != "" {
		return mediaType
	}
	if len(data) == 0 {
		return mediaType
	}
	detected := http.DetectContentType(data)
	if detected == "application/octet-stream" {
		return mediaType
	}
	return detected
}

func defaultImageName(mediaType string) string {
	ext := ".img"
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image/png":
		ext = ".png"
	case "image/jpeg":
		ext = ".jpg"
	case "image/gif":
		ext = ".gif"
	}
	return "clipboard" + ext
}

func ParseDataURL(value string) (mediaType string, base64Data string, ok bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(strings.ToLower(value), "data:image/") {
		return "", "", false
	}
	head, body, found := strings.Cut(value, ",")
	if !found || body == "" || !strings.Contains(strings.ToLower(head), ";base64") {
		return "", "", false
	}
	mediaType = head[5:]
	if idx := strings.Index(mediaType, ";"); idx >= 0 {
		mediaType = mediaType[:idx]
	}
	return mediaType, strings.TrimSpace(body), true
}

func IsImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".png", ".jpg", ".jpeg", ".gif":
		return true
	default:
		return false
	}
}

func DecodePreview(base64Data string) (image.Image, error) {
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(base64Data))
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

func RenderThumbnail(base64Data string, maxWidth, maxHeight int) (string, error) {
	img, err := DecodePreview(base64Data)
	if err != nil {
		return "", err
	}
	return renderImageANSI(img, maxWidth, maxHeight), nil
}

func renderImageANSI(img image.Image, maxWidth, maxHeight int) string {
	bounds := img.Bounds()
	srcW := max(1, bounds.Dx())
	srcH := max(1, bounds.Dy())
	width := min(maxWidth, max(1, srcW))
	height := min(maxHeight*2, max(2, srcH))

	if srcW > width {
		height = max(2, int(float64(srcH)*float64(width)/float64(srcW)))
	}
	if height > maxHeight*2 {
		scale := float64(maxHeight*2) / float64(height)
		height = max(2, int(float64(height)*scale))
		width = max(1, int(float64(width)*scale))
	}
	if height%2 != 0 {
		height++
	}

	var lines []string
	for y := 0; y < height; y += 2 {
		var line strings.Builder
		for x := 0; x < width; x++ {
			top := samplePixel(img, bounds, x, y, width, height)
			bottom := samplePixel(img, bounds, x, y+1, width, height)
			fmt.Fprintf(
				&line,
				"\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀",
				top.R, top.G, top.B,
				bottom.R, bottom.G, bottom.B,
			)
		}
		line.WriteString("\x1b[0m")
		lines = append(lines, line.String())
	}
	return strings.Join(lines, "\n")
}

func samplePixel(img image.Image, bounds image.Rectangle, x, y, outW, outH int) color.NRGBA {
	srcX := bounds.Min.X + x*bounds.Dx()/max(1, outW)
	srcY := bounds.Min.Y + y*bounds.Dy()/max(1, outH)
	return color.NRGBAModel.Convert(img.At(srcX, srcY)).(color.NRGBA)
}

func AttachmentsSummary(attachments []Attachment) string {
	if len(attachments) == 0 {
		return ""
	}
	if len(attachments) == 1 {
		attachment := attachments[0]
		name := strings.TrimSpace(attachment.Name)
		if name == "" {
			name = "image"
		}
		if attachment.Width > 0 && attachment.Height > 0 {
			return fmt.Sprintf("%s (%dx%d)", name, attachment.Width, attachment.Height)
		}
		return name
	}
	return fmt.Sprintf("%d images", len(attachments))
}

func clipboardImageBytes() ([]byte, error) {
	if runtime.GOOS != "darwin" {
		return nil, errors.New("clipboard image paste is currently supported on macOS only")
	}

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
	if err != nil {
		return nil, errors.New("no image found in clipboard")
	}
	if len(output) == 0 {
		return nil, errors.New("no image found in clipboard")
	}

	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(output)))
	if err != nil {
		return nil, err
	}
	return data, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
