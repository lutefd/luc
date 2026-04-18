package transcript

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
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
}

func New(th theme.Theme) Model {
	vp := viewport.New(0, 0)
	return Model{
		viewport: vp,
		cache:    make(map[string]string),
		theme:    th,
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
		m.blocks = append(m.blocks, Block{ID: payload.ID, Kind: "user", Content: payload.Content, State: "done"})
	case "message.assistant.delta":
		payload := decode[history.MessageDeltaPayload](ev.Payload)
		block := m.findOrAdd(payload.ID, "assistant")
		block.Content += payload.Delta
		block.State = "streaming"
	case "message.assistant.final":
		payload := decode[history.MessagePayload](ev.Payload)
		block := m.findOrAdd(payload.ID, "assistant")
		block.Content = payload.Content
		block.State = "done"
	case "tool.requested":
		payload := decode[history.ToolCallPayload](ev.Payload)
		m.blocks = append(m.blocks, Block{
			ID:      payload.ID,
			Kind:    "tool",
			Content: fmt.Sprintf("%s %s", payload.Name, payload.Arguments),
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
		block.Content = payload.Content
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
		m.blocks = append(m.blocks, Block{ID: payload.ID, Kind: kind, Content: payload.Content, State: "done"})
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
		key := fmt.Sprintf("%s:%s:%d", block.Kind, block.Content, m.width)
		if block.State == "done" {
			if cached, ok := m.cache[key]; ok {
				views = append(views, cached)
				continue
			}
		}

		rendered := m.renderBlock(block)
		if block.State == "done" {
			m.cache[key] = rendered
		}
		views = append(views, rendered)
	}

	m.viewport.SetContent(strings.Join(views, "\n\n"))
	m.viewport.GotoBottom()
}

func (m Model) renderBlock(block Block) string {
	width := max(20, m.width-2)
	switch block.Kind {
	case "user":
		return lipgloss.NewStyle().Width(width).Render(m.theme.UserBubble.Render("You") + "\n" + block.Content)
	case "assistant":
		content := block.Content
		if block.State == "done" {
			renderer, err := theme.NewMarkdownRenderer(width)
			if err == nil {
				if rendered, renderErr := renderer.Render(content); renderErr == nil {
					content = rendered
				}
			}
		}
		return lipgloss.NewStyle().Width(width).Render(m.theme.AgentBubble.Render("Luc") + "\n" + content)
	case "tool":
		title := block.Meta["name"]
		if title == "" {
			title = "tool"
		}
		content := strings.TrimSpace(block.Content)
		if content == "" {
			content = block.State
		}
		style := m.theme.ToolBubble
		if block.State == "error" {
			style = m.theme.ErrorBubble
		}
		return lipgloss.NewStyle().Width(width).Render(style.Render("Tool: "+title) + "\n" + content)
	case "error":
		return lipgloss.NewStyle().Width(width).Render(m.theme.ErrorBubble.Render("Error") + "\n" + block.Content)
	default:
		return lipgloss.NewStyle().Width(width).Render(m.theme.Muted.Render(block.Content))
	}
}

func decode[T any](payload any) T {
	var out T
	data, _ := json.Marshal(payload)
	_ = json.Unmarshal(data, &out)
	return out
}
