package transcript

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/theme"
)

type Block struct {
	ID      string
	Kind    string
	Content string
	State   string
	Meta    map[string]string
}

type Model struct {
	viewport viewport.Model
	width    int
	height   int
	blocks   []Block
	cache    map[string]string
	theme    theme.Theme
	renderer RenderFunc
}

type RenderFunc func(width int, text string) (string, error)

func New(th theme.Theme, variant string) Model {
	vp := viewport.New(0, 0)
	return Model{
		viewport: vp,
		cache:    make(map[string]string),
		theme:    th,
		renderer: func(width int, text string) (string, error) {
			renderer, err := theme.NewMarkdownRenderer(width, variant)
			if err != nil {
				return "", err
			}
			return renderer.Render(text)
		},
	}
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.viewport.Width = width
	m.viewport.Height = height
	m.render()
}

func (m *Model) UpdateViewport(msg any) {
	m.viewport, _ = m.viewport.Update(msg)
}

func (m *Model) Apply(ev history.EventEnvelope) {
	switch ev.Kind {
	case "message.user":
		payload := decode[history.MessagePayload](ev.Payload)
		m.blocks = append(m.blocks, Block{ID: payload.ID, Kind: "user", Content: cleanText(payload.Content), State: "done"})
	case "message.assistant.delta":
		payload := decode[history.MessageDeltaPayload](ev.Payload)
		block := m.findOrAdd(payload.ID, "assistant")
		block.Content += cleanText(payload.Delta)
		block.State = "streaming"
	case "message.assistant.final":
		payload := decode[history.MessagePayload](ev.Payload)
		block := m.findOrAdd(payload.ID, "assistant")
		block.Content = cleanText(payload.Content)
		block.State = "done"
	case "tool.requested":
		payload := decode[history.ToolCallPayload](ev.Payload)
		m.blocks = append(m.blocks, Block{
			ID:      payload.ID,
			Kind:    "tool",
			Content: cleanText(fmt.Sprintf("%s %s", payload.Name, payload.Arguments)),
			State:   "pending",
			Meta:    map[string]string{"name": payload.Name},
		})
	case "tool.finished":
		payload := decode[history.ToolResultPayload](ev.Payload)
		block := m.findOrAdd(payload.ID, "tool")
		block.State = "done"
		if payload.Error != "" {
			block.State = "error"
		}
		block.Content = cleanText(payload.Content)
		if diff, ok := payload.Metadata["diff"].(string); ok && diff != "" {
			block.Content = block.Content + "\n\n" + diffPreview(cleanText(diff), 12)
		}
		if block.Meta == nil {
			block.Meta = map[string]string{}
		}
		block.Meta["name"] = payload.Name
	case "system.note", "system.error":
		payload := decode[history.MessagePayload](ev.Payload)
		kind := "note"
		if ev.Kind == "system.error" {
			kind = "error"
		}
		m.blocks = append(m.blocks, Block{ID: payload.ID, Kind: kind, Content: cleanText(payload.Content), State: "done"})
	case "reload.finished":
		payload := decode[history.ReloadPayload](ev.Payload)
		m.blocks = append(m.blocks, Block{
			ID:      fmt.Sprintf("reload_%d", payload.Version),
			Kind:    "note",
			Content: fmt.Sprintf("reloaded runtime to version %d", payload.Version),
			State:   "done",
		})
	case "reload.failed":
		payload := decode[history.ReloadPayload](ev.Payload)
		m.blocks = append(m.blocks, Block{
			ID:      fmt.Sprintf("reload_failed_%d", payload.Version),
			Kind:    "error",
			Content: fmt.Sprintf("reload failed: %s", payload.Error),
			State:   "done",
		})
	}
	m.render()
}

func (m Model) View() string {
	return m.viewport.View()
}

func (m *Model) findOrAdd(id, kind string) *Block {
	for i := range m.blocks {
		if m.blocks[i].ID == id {
			return &m.blocks[i]
		}
	}
	m.blocks = append(m.blocks, Block{ID: id, Kind: kind})
	return &m.blocks[len(m.blocks)-1]
}

func (m *Model) render() {
	if m.width <= 0 || m.height <= 0 {
		return
	}

	var views []string
	for _, block := range m.blocks {
		key := fmt.Sprintf("%s:%s:%s:%d", block.Kind, block.State, block.Content, m.width)
		if block.State == "done" {
			if cached, ok := m.cache[key]; ok {
				views = append(views, cached)
				continue
			}
		}

		rendered := m.safeRenderBlock(block)
		if block.State == "done" {
			m.cache[key] = rendered
		}
		views = append(views, rendered)
	}

	m.viewport.SetContent(strings.Join(views, "\n\n"))
	m.viewport.GotoBottom()
}

func (m Model) safeRenderBlock(block Block) (rendered string) {
	defer func() {
		if r := recover(); r != nil {
			rendered = lipgloss.NewStyle().Width(max(20, m.width-4)).Render(cleanText(block.Content))
		}
	}()
	return m.renderBlock(block)
}

func (m Model) renderBlock(block Block) string {
	width := max(20, m.width-4)
	switch block.Kind {
	case "user":
		body := lipgloss.NewStyle().Width(width).Render(block.Content)
		return lipgloss.JoinVertical(lipgloss.Left, m.theme.UserLabel.Render("You"), body)
	case "assistant":
		content := block.Content
		if block.State == "done" {
			if rendered, err := m.renderer(width, content); err == nil {
				if strings.TrimSpace(rendered) != "" {
					content = rendered
				}
			}
		}
		body := lipgloss.NewStyle().Width(width).Render(content)
		return lipgloss.JoinVertical(lipgloss.Left, m.theme.AssistantLabel.Render("Luc"), m.theme.AssistantBody.Render(body))
	case "tool":
		title := block.Meta["name"]
		if title == "" {
			title = "tool"
		}
		content := strings.TrimSpace(block.Content)
		if content == "" {
			content = block.State
		}
		style := m.theme.ToolCard
		if block.State == "error" {
			style = m.theme.ErrorCard
		}
		card := lipgloss.JoinVertical(
			lipgloss.Left,
			m.theme.ToolTitle.Render("Tool "+title),
			lipgloss.NewStyle().Width(width-2).Render(content),
		)
		return style.Width(width).Render(card)
	case "error":
		card := lipgloss.JoinVertical(lipgloss.Left, m.theme.ToolTitle.Render("Error"), block.Content)
		return m.theme.ErrorCard.Width(width).Render(card)
	default:
		return lipgloss.NewStyle().Width(width).Render(m.theme.Muted.Render(block.Content))
	}
}

func cleanText(s string) string {
	clean := ansi.Strip(s)
	clean = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, clean)
	return strings.TrimRight(clean, "\r")
}

func diffPreview(diff string, maxLines int) string {
	lines := strings.Split(strings.TrimSpace(diff), "\n")
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:maxLines], "\n") + "\n..."
}

func decode[T any](payload any) T {
	var out T
	data, _ := json.Marshal(payload)
	_ = json.Unmarshal(data, &out)
	return out
}
