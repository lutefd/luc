package tui

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/lutefd/luc/internal/media"
)

type clipboardPasteMsg struct {
	attached media.Attachment
	text     string
	err      error
}

var buildClipboardImageAttachment = func() (media.Attachment, error) {
	return media.BuildImageAttachmentFromClipboard(newAttachmentID())
}

var readClipboardText = clipboard.ReadAll

func attachmentFromPasteContent(content string) (media.Attachment, bool, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return media.Attachment{}, false, nil
	}

	if mediaType, base64Data, ok := media.ParseDataURL(trimmed); ok {
		attachment, err := media.BuildImageAttachment(newAttachmentID(), "", mediaType, base64Data)
		return attachment, true, err
	}

	path := normalizePastedPath(trimmed)
	if path == "" || !media.IsImagePath(path) {
		return media.Attachment{}, false, nil
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return media.Attachment{}, false, nil
	}
	attachment, err := media.BuildImageAttachmentFromFile(newAttachmentID(), path)
	return attachment, true, err
}

func normalizePastedPath(content string) string {
	candidate := strings.TrimSpace(content)
	if strings.Count(candidate, "\n") > 0 {
		return ""
	}
	candidate = strings.Trim(candidate, `"'`)
	if strings.HasPrefix(candidate, "file://") {
		if parsed, err := url.Parse(candidate); err == nil {
			candidate = parsed.Path
		}
	}
	if candidate == "" {
		return ""
	}
	if !filepath.IsAbs(candidate) {
		return ""
	}
	return candidate
}

func readClipboardImageCmd() tea.Cmd {
	return readClipboardCmd(true)
}

func readClipboardCmd(imageOnly bool) tea.Cmd {
	return func() tea.Msg {
		attachment, err := buildClipboardImageAttachment()
		if err == nil && attachment.ID != "" {
			return clipboardPasteMsg{attached: attachment}
		}
		if imageOnly {
			return clipboardPasteMsg{err: err}
		}
		text, textErr := readClipboardText()
		if textErr == nil && text != "" {
			return clipboardPasteMsg{text: text}
		}
		if err != nil {
			return clipboardPasteMsg{err: err}
		}
		return clipboardPasteMsg{err: textErr}
	}
}

func newAttachmentID() string {
	return fmt.Sprintf("image_%d", time.Now().UnixNano())
}

func (m Model) renderPendingImages() string {
	width := max(24, m.transcriptWidth())
	title := m.theme.Footer.Render(fmt.Sprintf("Pending images (%d) • ctrl+d drops the last one", len(m.pendingImages)))
	items := make([]string, 0, len(m.pendingImages))

	for _, attachment := range m.pendingImages {
		body := renderAttachmentCardBody(m, attachment)
		items = append(items, m.theme.InputFrame.Copy().Width(width).Render(body))
	}

	return lipgloss.JoinVertical(lipgloss.Left, append([]string{title}, items...)...)
}

func renderAttachmentCardBody(m Model, attachment media.Attachment) string {
	label := m.theme.UserLabel.Render("image")
	name := strings.TrimSpace(attachment.Name)
	if name == "" {
		name = "attachment"
	}

	meta := []string{}
	if attachment.Width > 0 && attachment.Height > 0 {
		meta = append(meta, fmt.Sprintf("%dx%d", attachment.Width, attachment.Height))
	}
	if mediaType := strings.TrimSpace(attachment.MediaType); mediaType != "" {
		meta = append(meta, mediaType)
	}

	lines := []string{
		lipgloss.JoinHorizontal(lipgloss.Left, label, " ", name),
	}
	if len(meta) > 0 {
		lines = append(lines, m.theme.Muted.Render(strings.Join(meta, " • ")))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func pendingImagesCacheKey(attachments []media.Attachment, width int) string {
	parts := make([]string, 0, len(attachments)+1)
	parts = append(parts, fmt.Sprintf("w:%d", width))
	for _, attachment := range attachments {
		parts = append(parts, fmt.Sprintf(
			"%s:%s:%s:%d:%d",
			attachment.ID,
			attachment.Name,
			attachment.MediaType,
			attachment.Width,
			attachment.Height,
		))
	}
	return strings.Join(parts, "|")
}
