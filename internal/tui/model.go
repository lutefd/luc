package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/lutefd/luc/internal/config"
	"github.com/lutefd/luc/internal/extensions"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/kernel"
	"github.com/lutefd/luc/internal/media"
	luruntime "github.com/lutefd/luc/internal/runtime"
	"github.com/lutefd/luc/internal/theme"
	"github.com/lutefd/luc/internal/tui/commands"
	"github.com/lutefd/luc/internal/tui/inspector"
	modelspicker "github.com/lutefd/luc/internal/tui/models"
	sessionpicker "github.com/lutefd/luc/internal/tui/sessions"
	themepicker "github.com/lutefd/luc/internal/tui/themes"
	"github.com/lutefd/luc/internal/tui/transcript"
)

type appEventsMsg []history.EventEnvelope
type submitDoneMsg struct{ err error }
type reloadDoneMsg struct{ err error }
type toggleInspectorMsg struct{}
type nextTabMsg struct{}
type openModelPickerMsg struct{}
type openSessionPickerMsg struct{}
type openThemePickerMsg struct{}
type resetThemeMsg struct{}
type newSessionMsg struct{}
type copySelectionMsg struct{}
type clearComposerMsg struct{}
type stopTurnMsg struct{}
type stopTurnDoneMsg struct{ stopped bool }

var writeClipboardText = clipboard.WriteAll

type keyMap struct {
	Send        key.Binding
	Newline     key.Binding
	ClearInput  key.Binding
	Stop        key.Binding
	TogglePane  key.Binding
	NextTab     key.Binding
	PrevTab     key.Binding
	ScrollUp    key.Binding
	ScrollDown  key.Binding
	Reload      key.Binding
	Palette     key.Binding
	ModelPick   key.Binding
	SessionPick key.Binding
	Paste       key.Binding
	RemoveImage key.Binding
	Copy        key.Binding
	Quit        key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Send, k.Newline, k.TogglePane, k.Reload, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Send, k.Newline, k.TogglePane, k.NextTab, k.PrevTab, k.ScrollUp, k.ScrollDown, k.ModelPick, k.SessionPick, k.Reload, k.Quit}}
}

type Model struct {
	controller     *kernel.Controller
	transcript     transcript.Model
	inspector      inspector.Model
	input          textarea.Model
	palette        commands.Model
	modelPicker    modelspicker.Model
	sessionPicker  sessionpicker.Model
	themePicker    themepicker.Model
	pendingImages  []media.Attachment
	registry       *commands.Registry
	keys           keyMap
	theme          theme.Theme
	width          int
	height         int
	inspectorOpen  bool
	status         string
	lastClickAt    time.Time
	lastClickID    string
	logsDirty      bool
	wheelQueued    bool
	wheelBody      int
	wheelSidebar   int
	submitInFlight bool
	runtimeBroker  *teaUIBroker
	runtimePage    runtimePageState
	runtimeDialog  runtimeDialogState
	agentStatus    agentStatusState
	chrome         *chromeCache
	composerAnchor int
	composerActive int
}

type chromeCache struct {
	headerDirty      bool
	header           string
	headerHeight     int
	footerDirty      bool
	footer           string
	footerHeight     int
	footerHintsKey   string
	footerHints      string
	footerPendingKey string
	footerPending    string
	bodyDirty        bool
	bodyKey          string
	body             string
}

type flushWheelMsg struct{}

const wheelBatchDelay = 8 * time.Millisecond

func New(controller *kernel.Controller) Model {
	th, variant, _ := theme.Load(controller.Config().UI.Theme, controller.Workspace().Root)

	input := textarea.New()
	input.Placeholder = "Tell luc what to inspect or change..."
	input.Focus()
	input.SetHeight(3)
	input.ShowLineNumbers = false
	input.Prompt = "> "
	input.CharLimit = 0
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter"), key.WithHelp("shift+enter", "newline"))
	applyInputTheme(&input, th, variant)

	registry := commands.NewRegistry()
	model := Model{
		controller:     controller,
		transcript:     transcript.New(th, variant),
		inspector:      inspector.New(controller.Workspace(), controller.Session(), th, variant),
		input:          input,
		palette:        commands.New(registry, th),
		modelPicker:    modelspicker.New(controller.Registry(), th),
		sessionPicker:  sessionpicker.New(th),
		themePicker:    themepicker.New(th),
		registry:       registry,
		theme:          th,
		inspectorOpen:  controller.Config().UI.InspectorOpen,
		composerAnchor: -1,
		composerActive: -1,
		chrome:         &chromeCache{headerDirty: true, footerDirty: true, bodyDirty: true},
		keys: keyMap{
			Send:        key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
			Newline:     key.NewBinding(key.WithKeys("shift+enter", "alt+enter"), key.WithHelp("shift+enter", "newline")),
			ClearInput:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear")),
			Stop:        key.NewBinding(key.WithKeys("ctrl+."), key.WithHelp("ctrl+.", "stop")),
			TogglePane:  key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", "details")),
			NextTab:     key.NewBinding(key.WithKeys("ctrl+]"), key.WithHelp("ctrl+]", "next tab")),
			PrevTab:     key.NewBinding(key.WithKeys("ctrl+\\"), key.WithHelp("ctrl+\\", "prev tab")),
			ScrollUp:    key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "scroll up")),
			ScrollDown:  key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdown", "scroll down")),
			Reload:      key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "reload")),
			Palette:     key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "commands")),
			ModelPick:   key.NewBinding(key.WithKeys("ctrl+m"), key.WithHelp("ctrl+m", "model")),
			SessionPick: key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("ctrl+l", "sessions")),
			Paste:       platformPasteBinding(),
			RemoveImage: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "drop image")),
			Copy:        platformCopyBinding(),
			Quit:        key.NewBinding(key.WithKeys("ctrl+c", "ctrl+q"), key.WithHelp("ctrl+c", "quit")),
		},
	}
	model.installRuntimeUI()

	events := controller.SessionEvents()
	model.transcript.ApplyBatch(events)
	model.inspector.ApplyBatch(events)
	model.logsDirty = true
	model.syncInspectorLogs(true)
	model.setStatus("Ready")
	return model
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, waitForEvent(m.controller.Events()), waitForUIBroker(m.runtimeBroker.actions))
}

func (m *Model) ensureChrome() *chromeCache {
	if m.chrome == nil {
		m.chrome = &chromeCache{headerDirty: true, footerDirty: true, bodyDirty: true}
	}
	return m.chrome
}

func (m *Model) invalidateHeader() {
	m.ensureChrome().headerDirty = true
}

func (m *Model) invalidateFooter() {
	m.ensureChrome().footerDirty = true
}

func (m *Model) invalidateChrome() {
	chrome := m.ensureChrome()
	chrome.headerDirty = true
	chrome.footerDirty = true
	chrome.bodyDirty = true
}

func (m *Model) invalidateBody() {
	m.ensureChrome().bodyDirty = true
}

func (m *Model) queueWheel(msg tea.MouseWheelMsg) tea.Cmd {
	delta := 0
	switch msg.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return nil
	}

	if m.inspectorOpen && m.hasSidebar() && msg.X >= m.transcriptWidth()+1 {
		m.wheelSidebar += delta
	} else {
		m.wheelBody += delta
	}
	if m.wheelQueued {
		return nil
	}
	m.wheelQueued = true
	return tea.Tick(wheelBatchDelay, func(time.Time) tea.Msg {
		return flushWheelMsg{}
	})
}

func (m *Model) flushQueuedWheel() tea.Cmd {
	m.wheelQueued = false
	bodyDelta := m.wheelBody
	sidebarDelta := m.wheelSidebar
	m.wheelBody = 0
	m.wheelSidebar = 0

	var cmds []tea.Cmd
	if bodyDelta != 0 {
		cmds = append(cmds, m.transcript.ScrollDeltaCmd(bodyDelta))
	}
	if sidebarDelta != 0 {
		cmds = append(cmds, m.inspector.ScrollDeltaCmd(sidebarDelta))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	handledTranscript, transcriptCmd := m.transcript.HandleMsg(msg)
	handledInspector, inspectorCmd := m.inspector.HandleMsg(msg)
	if handledTranscript || handledInspector || transcriptCmd != nil || inspectorCmd != nil {
		return m, tea.Batch(transcriptCmd, inspectorCmd)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.invalidateChrome()
		m.resize()
		return m, nil
	case tea.MouseWheelMsg:
		if m.sessionPicker.IsOpen() || m.modelPicker.IsOpen() || m.themePicker.IsOpen() || m.palette.IsOpen() || m.runtimeDialog.open || m.runtimePage.open {
			return m, nil
		}
		if !m.wheelInBody(msg) {
			return m, nil
		}
		return m, m.queueWheel(msg)
	case flushWheelMsg:
		return m, m.flushQueuedWheel()
	case agentStatusTickMsg:
		return m, m.updateAgentStatusTick()
	case tea.MouseClickMsg:
		if m.sessionPicker.IsOpen() || m.modelPicker.IsOpen() || m.themePicker.IsOpen() || m.palette.IsOpen() || m.runtimeDialog.open || m.runtimePage.open {
			return m, nil
		}
		if msg.Button == tea.MouseLeft {
			if row, ok := m.transcriptMouseRow(msg.X, msg.Y); ok {
				if blockID, ok := m.transcript.BlockIDAtRow(row); ok {
					now := time.Now()
					if blockID != "" && blockID == m.lastClickID && now.Sub(m.lastClickAt) <= 400*time.Millisecond {
						m.lastClickAt = time.Time{}
						m.lastClickID = ""
						m.transcript.ClearSelection()
						if m.transcript.ToggleBlockExpansionAtRow(row) {
							return m, nil
						}
					}
					m.lastClickAt = now
					m.lastClickID = blockID
				}
				m.transcript.BeginSelection(row)
			} else {
				m.lastClickAt = time.Time{}
				m.lastClickID = ""
				m.transcript.ClearSelection()
			}
		}
		return m, nil
	case tea.MouseMotionMsg:
		if m.sessionPicker.IsOpen() || m.modelPicker.IsOpen() || m.themePicker.IsOpen() || m.palette.IsOpen() || m.runtimeDialog.open || m.runtimePage.open {
			return m, nil
		}
		if msg.Button != tea.MouseLeft && !m.transcript.IsSelecting() {
			return m, nil
		}
		if row, ok := m.transcriptMouseRow(msg.X, msg.Y); ok {
			m.transcript.ExtendSelection(row)
		}
		return m, nil
	case tea.MouseReleaseMsg:
		if msg.Button == tea.MouseLeft {
			m.transcript.EndSelection()
		}
		return m, nil
	case tea.PasteMsg:
		if m.sessionPicker.IsOpen() || m.modelPicker.IsOpen() || m.themePicker.IsOpen() || m.palette.IsOpen() || m.runtimeDialog.open || m.runtimePage.open {
			return m, nil
		}
		if attachment, ok, err := attachmentFromPasteContent(msg.Content); err != nil {
			m.setStatus("Paste failed: " + err.Error())
			return m, nil
		} else if ok {
			m.pendingImages = append(m.pendingImages, attachment)
			m.invalidateFooter()
			m.resize()
			m.setStatus("Attached image: " + attachment.Name)
			return m, nil
		}
		if strings.TrimSpace(msg.Content) == "" {
			return m, readClipboardImageCmd()
		}
	case tea.KeyPressMsg:
		if m.runtimeDialog.open {
			return m, m.handleRuntimeDialogKey(msg)
		}
		if m.inspectorOpen {
			if view, action, handled := m.inspector.HandleRuntimeViewActionKey(msg); handled {
				if action.ID != "" {
					return m, m.runRuntimeViewAction(view, action)
				}
				return m, nil
			}
		}
		if m.sessionPicker.IsOpen() {
			cmd, _, handled := m.sessionPicker.Update(msg)
			if handled {
				return m, cmd
			}
		}
		if m.modelPicker.IsOpen() {
			cmd, _, handled := m.modelPicker.Update(msg)
			if handled {
				return m, cmd
			}
		}
		if m.themePicker.IsOpen() {
			cmd, _, handled := m.themePicker.Update(msg)
			if handled {
				return m, cmd
			}
		}
		if m.palette.IsOpen() {
			cmd, _, handled := m.palette.Update(msg)
			if handled {
				return m, cmd
			}
		}
		if m.runtimePage.open {
			if cmd := m.handleRuntimePageKey(msg); cmd != nil {
				return m, cmd
			}
			return m, nil
		}
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Palette):
			m.palette.Open()
			return m, nil
		case key.Matches(msg, m.keys.ModelPick):
			m.modelPicker.Open(m.controller.Config().Provider.Kind, m.controller.Config().Provider.Model)
			return m, nil
		case key.Matches(msg, m.keys.SessionPick):
			sessions, err := m.controller.Sessions()
			if err != nil {
				m.setStatus("Error: " + err.Error())
				return m, nil
			}
			m.sessionPicker.Open(m.controller.Session().SessionID, sessions)
			return m, nil
		case key.Matches(msg, m.keys.Copy):
			return m, copySelectionCmd()
		case key.Matches(msg, m.keys.Paste):
			return m, readClipboardCmd(false)
		case key.Matches(msg, m.keys.ClearInput):
			if m.clearComposer() {
				m.setStatus("Input cleared")
			}
			return m, nil
		case key.Matches(msg, m.keys.Stop):
			return m, m.handleStopTurn()
		case key.Matches(msg, m.keys.Send):
			value := strings.TrimSpace(m.input.Value())
			if value == "" && len(m.pendingImages) == 0 {
				return m, nil
			}
			attachments := append([]media.Attachment(nil), m.pendingImages...)
			m.setStatus("Sending...")
			m.submitInFlight = !strings.HasPrefix(value, "/")
			m.input.Reset()
			m.input.SetValue("")
			m.clearComposerSelection()
			m.pendingImages = nil
			m.input.Focus()
			m.invalidateFooter()
			m.resize()
			cmd := submitCmd(m.controller, value, attachments)
			if m.submitInFlight {
				cmd = tea.Batch(cmd, m.startAgentStatus(value))
			}
			return m, cmd
		case key.Matches(msg, m.keys.RemoveImage):
			if len(m.pendingImages) == 0 {
				m.setStatus("No pending image")
				return m, nil
			}
			removed := m.pendingImages[len(m.pendingImages)-1]
			m.pendingImages = m.pendingImages[:len(m.pendingImages)-1]
			m.invalidateFooter()
			m.resize()
			m.setStatus("Removed image: " + removed.Name)
			return m, nil
		case key.Matches(msg, m.keys.TogglePane):
			m.inspectorOpen = !m.inspectorOpen
			m.invalidateFooter()
			m.resize()
			return m, nil
		case key.Matches(msg, m.keys.NextTab):
			if m.inspectorOpen {
				m.inspector.NextTab()
				m.syncInspectorLogs(false)
			}
			return m, m.maybeRefreshActiveRuntimeView()
		case key.Matches(msg, m.keys.PrevTab):
			if m.inspectorOpen {
				m.inspector.PrevTab()
				m.syncInspectorLogs(false)
			}
			return m, m.maybeRefreshActiveRuntimeView()
		case key.Matches(msg, m.keys.ScrollUp):
			return m, m.transcript.UpdateViewportCmd(msg)
		case key.Matches(msg, m.keys.ScrollDown):
			return m, m.transcript.UpdateViewportCmd(msg)
		case key.Matches(msg, m.keys.Reload):
			m.setStatus("Reloading...")
			return m, reloadCmd(m.controller)
		}
		if cmd := m.handleRuntimeCommandShortcut(msg); cmd != nil {
			return m, cmd
		}
		if handled, cmd := m.handleComposerKey(msg); handled {
			return m, cmd
		}
	case appEventsMsg:
		events := []history.EventEnvelope(msg)
		m.transcript.ApplyBatch(events)
		m.inspector.ApplyBatch(events)
		m.inspector.SetSessionMeta(m.controller.Session())
		if appEventsTouchLogs(events) {
			m.logsDirty = true
			m.syncInspectorLogs(false)
		}
		syncRuntimeUI := false
		for _, ev := range events {
			switch ev.Kind {
			case "status.thinking":
				payload := decode[history.StatusPayload](ev.Payload)
				if strings.TrimSpace(payload.Text) != "" {
					m.status = payload.Text
				}
			case "message.assistant.delta":
				m.stopAgentStatus()
			case "message.assistant.final":
				m.stopAgentStatus()
				m.status = "Ready"
			case "reload.finished":
				m.status = "Reloaded"
				syncRuntimeUI = true
			case "reload.failed", "system.error":
				m.status = "Error"
			}
		}
		if syncRuntimeUI {
			m.syncRuntimeUI()
		}
		m.inspector.SetStatus(m.status)
		return m, waitForEvent(m.controller.Events())
	case uiBrokerActionMsg:
		return m, tea.Batch(m.handleRuntimeAction(msg.request.action, msg.request.response), waitForUIBroker(m.runtimeBroker.actions))
	case toggleInspectorMsg:
		m.inspectorOpen = !m.inspectorOpen
		m.resize()
		return m, nil
	case nextTabMsg:
		if m.inspectorOpen {
			m.inspector.NextTab()
			m.syncInspectorLogs(false)
		}
		return m, nil
	case openModelPickerMsg:
		m.modelPicker.Open(m.controller.Config().Provider.Kind, m.controller.Config().Provider.Model)
		return m, nil
	case openSessionPickerMsg:
		sessions, err := m.controller.Sessions()
		if err != nil {
			m.setStatus("Error: " + err.Error())
			return m, nil
		}
		m.sessionPicker.Open(m.controller.Session().SessionID, sessions)
		return m, nil
	case newSessionMsg:
		if err := m.controller.NewSession(); err != nil {
			m.setStatus("Error: " + err.Error())
			return m, nil
		}
		m.resetSessionViews()
		m.setStatus("New session: " + m.controller.Session().SessionID)
		return m, nil
	case copySelectionMsg:
		text := m.selectedComposerText()
		if strings.TrimSpace(text) == "" {
			text = m.transcript.SelectedText()
		}
		if strings.TrimSpace(text) == "" {
			m.setStatus("Nothing selected")
			return m, nil
		}
		if err := writeClipboardText(text); err != nil {
			m.setStatus("Copy failed: " + err.Error())
		} else {
			m.setStatus("Copied selection")
		}
		return m, nil
	case clearComposerMsg:
		if m.clearComposer() {
			m.setStatus("Input cleared")
		}
		return m, nil
	case stopTurnMsg:
		return m, m.handleStopTurn()
	case stopTurnDoneMsg:
		if !msg.stopped && !m.turnInFlight() {
			m.setStatus("No active turn")
		}
		return m, nil
	case modelspicker.Selected:
		if err := m.controller.SwitchModelForProvider(msg.ProviderID, msg.ModelID); err != nil {
			m.setStatus("Error: " + err.Error())
		} else {
			m.inspector.SetSessionMeta(m.controller.Session())
			m.invalidateHeader()
			m.setStatus("Model: " + msg.ModelID)
		}
		return m, nil
	case openThemePickerMsg:
		entries := m.availableThemes()
		m.themePicker.Open(entries, m.controller.Config().UI.Theme)
		return m, nil
	case resetThemeMsg:
		m.applyTheme(config.Default().UI.Theme)
		return m, nil
	case themepicker.Selected:
		m.applyTheme(msg.ThemeName)
		return m, nil
	case sessionpicker.Selected:
		if err := m.controller.OpenSession(msg.SessionID); err != nil {
			m.setStatus("Error: " + err.Error())
			return m, nil
		}
		m.resetSessionViews()
		m.invalidateHeader()
		m.setStatus("Session: " + msg.SessionID)
		return m, nil
	case submitDoneMsg:
		m.submitInFlight = false
		m.stopAgentStatus()
		m.invalidateFooter()
		if msg.err != nil {
			m.setStatus(msg.err.Error())
		} else {
			m.setStatus("Ready")
		}
		m.inspector.SetSessionMeta(m.controller.Session())
		return m, nil
	case reloadDoneMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error())
		}
		m.inspector.SetSessionMeta(m.controller.Session())
		return m, nil
	case clipboardPasteMsg:
		if msg.err != nil {
			m.setStatus("Paste failed: " + msg.err.Error())
			return m, nil
		}
		if msg.attached.ID == "" {
			if msg.text != "" {
				m.replaceComposerSelection("")
				m.input.InsertString(msg.text)
				m.invalidateFooter()
				m.resize()
			}
			return m, nil
		}
		m.pendingImages = append(m.pendingImages, msg.attached)
		m.invalidateFooter()
		m.resize()
		m.setStatus("Attached image: " + msg.attached.Name)
		return m, nil
	case runRuntimeCommandMsg:
		return m, m.handleRuntimeCommand(msg.CommandID)
	case runtimeToolActionDoneMsg:
		if msg.Err != nil {
			m.setStatus("Tool failed: " + msg.Err.Error())
		} else if strings.EqualFold(strings.TrimSpace(msg.Presentation), "status") || strings.TrimSpace(msg.Presentation) == "" {
			m.setStatus("Tool finished: " + msg.ToolName)
		}
		return m, nil
	case runtimeSessionHandoffMsg:
		if msg.err != nil {
			m.replyRuntimeAction(msg.response, luruntime.UIResult{ActionID: msg.action.ID}, msg.err)
			m.setStatus("Session handoff failed: " + msg.err.Error())
			return m, nil
		}
		m.replyRuntimeAction(msg.response, luruntime.UIResult{ActionID: msg.action.ID, Accepted: true}, nil)
		m.resetSessionViews()
		m.input.SetValue(msg.action.InitialInput)
		m.input.CursorEnd()
		m.input.Focus()
		m.invalidateFooter()
		m.setStatus("Session handoff: " + m.controller.Session().SessionID)
		return m, nil
	case runtimeTimelineNoteMsg:
		if msg.err != nil {
			m.replyRuntimeAction(msg.response, luruntime.UIResult{ActionID: msg.action.ID}, msg.err)
			m.setStatus("Timeline note failed: " + msg.err.Error())
			return m, nil
		}
		m.replyRuntimeAction(msg.response, luruntime.UIResult{ActionID: msg.action.ID, Accepted: true}, nil)
		m.setStatus("Timeline note added")
		return m, waitForEvent(m.controller.Events())
	case runtimeViewLoadedMsg:
		if msg.Err != nil {
			if m.runtimePage.open && m.runtimePage.view.ID == msg.ViewID {
				m.runtimePage.loading = false
				m.runtimePage.err = msg.Err.Error()
			}
			m.inspector.SetRuntimeViewContent(msg.ViewID, "Error: "+msg.Err.Error())
			m.setStatus("Runtime view error")
			return m, nil
		}
		m.inspector.SetRuntimeViewContent(msg.ViewID, msg.Rendered)
		if m.runtimePage.open && m.runtimePage.view.ID == msg.ViewID {
			m.runtimePage.loading = false
			m.runtimePage.content = msg.Rendered
			m.runtimePage.err = ""
		}
		return m, nil
	}

	var cmd tea.Cmd
	if pasteMsg, ok := msg.(tea.PasteMsg); ok && strings.TrimSpace(pasteMsg.Content) != "" {
		m.replaceComposerSelection("")
	}
	m.input, cmd = m.input.Update(msg)
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		m.handleComposerSelectionCollapse(keyMsg)
	}
	m.invalidateFooter()
	return m, cmd
}

func (m *Model) clearComposer() bool {
	if m.input.Value() == "" && len(m.pendingImages) == 0 {
		return false
	}
	m.input.Reset()
	m.input.SetValue("")
	m.pendingImages = nil
	m.clearComposerSelection()
	m.input.Focus()
	m.invalidateFooter()
	m.resize()
	return true
}

func (m *Model) handleComposerKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch {
	case platformSelectAllMsg(msg):
		if !m.selectAllComposer() {
			m.setStatus("Nothing to select")
			return true, nil
		}
		m.setStatus("Selected all input")
		return true, nil
	case msg.Code == tea.KeyLeft && msg.Mod == tea.ModShift:
		if m.extendComposerSelection(-1) {
			return true, nil
		}
	case msg.Code == tea.KeyRight && msg.Mod == tea.ModShift:
		if m.extendComposerSelection(1) {
			return true, nil
		}
	}

	if !m.hasComposerSelection() {
		return false, nil
	}

	switch {
	case isPrintableComposerKey(msg), isComposerDeleteKey(m.input, msg), key.Matches(msg, m.keys.Newline):
		m.replaceComposerSelection("")
		return false, nil
	case isComposerMoveLeftKey(m.input, msg):
		m.collapseComposerSelection(false)
		return true, nil
	case isComposerMoveRightKey(m.input, msg):
		m.collapseComposerSelection(true)
		return true, nil
	default:
		return false, nil
	}
}

func (m Model) turnInFlight() bool {
	return m.submitInFlight || m.controller.TurnActive()
}

func (m *Model) handleStopTurn() tea.Cmd {
	if !m.turnInFlight() {
		m.setStatus("No active turn")
		return nil
	}
	m.setStatus("Stopping...")
	m.invalidateFooter()
	return stopTurnCmd(m.controller)
}

func (m Model) View() tea.View {
	if m.width <= 0 || m.height <= 0 {
		v := tea.NewView("")
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}

	// Render the parts, measure footer, and give the body the remainder.
	// This guarantees the footer (input + hints + status) is ALWAYS visible.
	header := m.renderHeader()
	footer := m.renderFooter()
	headerH := lipgloss.Height(header)
	footerH := lipgloss.Height(footer)
	bodyH := max(1, m.height-headerH-footerH)

	body := m.renderBodyWithHeight(bodyH)

	// Clamp each section to its target height so nothing overflows. Each
	// section inherits the app background so unpainted gutter cells (padding
	// introduced by the clamp, whitespace between header/body/footer) pick up
	// the theme color instead of leaking the terminal's default background.
	sectionStyle := m.theme.App
	headerSection := sectionStyle.Width(m.width).Height(headerH).MaxHeight(headerH).Render(header)
	bodySection := sectionStyle.Width(m.width).Height(bodyH).MaxHeight(bodyH).Render(body)
	footerSection := sectionStyle.Width(m.width).Height(footerH).MaxHeight(footerH).Render(footer)

	content := lipgloss.JoinVertical(lipgloss.Left, headerSection, bodySection, footerSection)

	// Overlay modals (model picker wins over command palette).
	switch {
	case m.sessionPicker.IsOpen():
		content = lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.sessionPicker.View(),
			lipgloss.WithWhitespaceChars(" "),
		)
	case m.modelPicker.IsOpen():
		content = lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.modelPicker.View(),
			lipgloss.WithWhitespaceChars(" "),
		)
	case m.themePicker.IsOpen():
		content = lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.themePicker.View(),
			lipgloss.WithWhitespaceChars(" "),
		)
	case m.palette.IsOpen():
		content = lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.palette.View(),
			lipgloss.WithWhitespaceChars(" "),
		)
	case m.runtimeDialog.open:
		content = m.renderRuntimeDialog()
	case m.runtimePage.open:
		content = m.renderRuntimePage()
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	// BackgroundColor is painted by the terminal (OSC 11), so every cell of
	// the alt-screen — including cells we never style with lipgloss — picks
	// up the theme bg. Without this, any row or column a child renderer
	// forgets to paint (e.g. bubbles textarea's untouched rows, padding
	// gutters, viewport empties) leaks the user's terminal background.
	// Built-in themes leave this nil so the terminal's native bg stays.
	v.BackgroundColor = m.theme.Background
	return v
}

func (m *Model) resize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.invalidateChrome()
	transcriptHeight := m.bodyHeight()
	m.transcript.SetSize(m.transcriptWidth(), transcriptHeight)
	m.inspector.SetSize(m.inspectorWidth(), m.inspectorHeight())
	m.input.SetWidth(max(24, m.transcriptWidth()-4))
	m.palette.SetSize(m.width, m.height)
	m.modelPicker.SetSize(m.width, m.height)
	m.sessionPicker.SetSize(m.width, m.height)
	m.themePicker.SetSize(m.width, m.height)
}

func (m Model) transcriptWidth() int {
	iw := m.inspectorWidth()
	if iw == 0 {
		return max(24, m.width-4)
	}
	return max(24, m.width-iw-3)
}

func (m Model) inspectorWidth() int {
	switch {
	case m.width < 120:
		if m.inspectorOpen {
			return max(24, m.width-4)
		}
		return 0
	case m.inspectorOpen:
		return min(56, max(42, m.width/4))
	default:
		return 30
	}
}

func (m Model) inspectorHeight() int {
	if m.width < 120 && m.inspectorOpen {
		return max(8, m.height/3)
	}
	return max(1, m.bodyHeight())
}

func (m Model) bodyHeight() int {
	_, _, bodyH := m.layoutHeights()
	return bodyH
}

func (m Model) layoutHeights() (headerH, footerH, bodyH int) {
	chrome := m.chrome
	if chrome == nil || chrome.headerDirty {
		m.renderHeader()
		chrome = m.chrome
	}
	if chrome == nil || chrome.footerDirty {
		m.renderFooter()
		chrome = m.chrome
	}
	headerH = chrome.headerHeight
	footerH = chrome.footerHeight
	bodyH = max(1, m.height-headerH-footerH)
	return headerH, footerH, bodyH
}

func (m Model) wheelInBody(msg tea.MouseWheelMsg) bool {
	headerH, _, bodyH := m.layoutHeights()
	return msg.Y >= headerH && msg.Y < headerH+bodyH
}

func (m Model) transcriptMouseRow(x, y int) (int, bool) {
	headerH, _, bodyH := m.layoutHeights()
	if y < headerH || y >= headerH+bodyH {
		return 0, false
	}
	if x < 0 || x >= m.transcriptWidth() {
		return 0, false
	}
	return y - headerH, true
}

func (m Model) hasSidebar() bool {
	return m.width >= 120
}

func (m Model) renderHeader() string {
	chrome := m.chrome
	if chrome != nil && !chrome.headerDirty {
		return chrome.header
	}
	usable := max(0, m.width-2)
	brand := m.theme.HeaderBrand.Render("luc")
	meta := m.theme.HeaderMeta.Render(compactHeaderPath(m.controller.Workspace().Root, max(16, usable/2)))
	ruleWidth := max(4, usable-lipgloss.Width(brand)-lipgloss.Width(meta)-2)
	rule := m.theme.HeaderRule.Render(strings.Repeat("─", ruleWidth))
	header := lipgloss.JoinHorizontal(lipgloss.Center, brand, " ", rule, " ", meta)
	if chrome != nil {
		chrome.header = header
		chrome.headerHeight = lipgloss.Height(header)
		chrome.headerDirty = false
	}
	return header
}

func compactHeaderPath(root string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}

	path := filepath.ToSlash(filepath.Clean(strings.TrimSpace(root)))
	if path == "." || path == "" {
		return root
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		home = filepath.ToSlash(filepath.Clean(home))
		switch {
		case path == home:
			path = "~"
		case strings.HasPrefix(path, home+"/"):
			path = "~/" + strings.TrimPrefix(path, home+"/")
		}
	}

	if lipgloss.Width(path) <= maxWidth {
		return path
	}

	prefix := ""
	remainder := path
	switch {
	case strings.HasPrefix(path, "~/"):
		prefix = "~/"
		remainder = strings.TrimPrefix(path, "~/")
	case strings.HasPrefix(path, "/"):
		prefix = "/"
		remainder = strings.TrimPrefix(path, "/")
	}

	parts := strings.Split(strings.Trim(remainder, "/"), "/")
	if len(parts) == 0 {
		return truncateLeft(path, maxWidth)
	}

	best := parts[len(parts)-1]
	for i := len(parts) - 2; i >= 0; i-- {
		candidate := "…/" + parts[i] + "/" + best
		if lipgloss.Width(prefix+candidate) > maxWidth {
			break
		}
		best = parts[i] + "/" + best
	}

	if len(parts) > 1 && lipgloss.Width(prefix+"…/"+best) <= maxWidth {
		return prefix + "…/" + best
	}
	if lipgloss.Width(prefix+best) <= maxWidth {
		return prefix + best
	}
	return truncateLeft(prefix+best, maxWidth)
}

func truncateLeft(value string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= maxWidth {
		return value
	}
	if maxWidth == 1 {
		return "…"
	}
	runes := []rune(value)
	keep := maxWidth - 1
	if keep > len(runes) {
		keep = len(runes)
	}
	return "…" + string(runes[len(runes)-keep:])
}

func (m Model) renderBody() string {
	return m.renderBodyWithHeight(m.bodyHeight())
}

func (m Model) renderBodyWithHeight(bodyH int) string {
	chrome := m.chrome
	if chrome != nil {
		key := m.bodyRenderKey(bodyH)
		if !chrome.bodyDirty && chrome.bodyKey == key {
			return chrome.body
		}
		chrome.bodyKey = key
	}

	transcriptView := m.theme.Body.Width(m.transcriptWidth()).Height(bodyH).MaxHeight(bodyH).Render(m.transcript.View())

	var body string
	switch {
	case m.width < 120 && m.inspectorOpen:
		detail := m.inspector.DetailView()
		body = lipgloss.JoinVertical(
			lipgloss.Left,
			transcriptView,
			lipgloss.NewStyle().Height(m.inspectorHeight()).Render(detail),
		)
	case m.hasSidebar():
		sidebar := m.inspector.SummaryView()
		if m.inspectorOpen {
			sidebar = m.inspector.DetailView()
		}
		sep := m.theme.Subtle.Render(strings.Repeat("│\n", max(1, bodyH)))
		body = lipgloss.JoinHorizontal(lipgloss.Top, transcriptView, sep, sidebar)
	default:
		body = transcriptView
	}

	if chrome != nil {
		chrome.body = body
		chrome.bodyDirty = false
	}
	return body
}

func (m Model) bodyRenderKey(bodyH int) string {
	key := fmt.Sprintf(
		"%d:%d:%d:%d:%t:%t:%s",
		m.width,
		bodyH,
		m.transcriptWidth(),
		m.inspectorWidth(),
		m.inspectorOpen,
		m.hasSidebar(),
		m.transcript.RenderKey(),
	)
	switch {
	case m.width < 120 && m.inspectorOpen:
		return key + ":detail:" + m.inspector.DetailRenderKey()
	case m.hasSidebar():
		if m.inspectorOpen {
			return key + ":detail:" + m.inspector.DetailRenderKey()
		}
		return key + ":summary:" + m.inspector.SummaryRenderKey()
	default:
		return key
	}
}

func (m Model) renderFooter() string {
	chrome := m.chrome
	if chrome != nil && !chrome.footerDirty {
		return chrome.footer
	}
	frame := m.theme.InputFrame.Width(max(24, m.transcriptWidth())).Render(m.input.View())
	hints := m.renderFooterHints()
	pending := m.renderFooterPendingImages()

	var footer string
	if pending == "" {
		footer = lipgloss.JoinVertical(lipgloss.Left, frame, hints)
	} else {
		footer = lipgloss.JoinVertical(lipgloss.Left, frame, pending, hints)
	}
	if chrome != nil {
		chrome.footer = footer
		chrome.footerHeight = lipgloss.Height(footer)
		chrome.footerDirty = false
	}
	return footer
}

func (m Model) renderFooterHints() string {
	chrome := m.chrome
	turnActive := m.turnInFlight()
	key := fmt.Sprintf("%d:%t:%t:%t", m.width, m.inspectorOpen, len(m.pendingImages) > 0, turnActive)
	if chrome != nil && chrome.footerHintsKey == key {
		return chrome.footerHints
	}

	bindings := []string{
		"enter send",
		"shift+enter newline",
		platformHint("cmd+a select all", "ctrl+a select all"),
		"shift+←/→ select",
		"esc clear",
		platformHint("ctrl/cmd+v paste", "ctrl+v paste"),
	}
	if turnActive {
		bindings = append(bindings, "ctrl+. stop")
	}
	bindings = append(bindings,
		"ctrl+p commands",
		"ctrl+m model",
		"ctrl+l sessions",
		platformHint("ctrl+y/cmd+c copy", "ctrl+y/ctrl+c copy"),
		"ctrl+o details",
	)
	if len(m.pendingImages) > 0 {
		bindings = append(bindings, "ctrl+d drop image")
	}
	if m.inspectorOpen {
		bindings = append(bindings, "ctrl+] tab")
	}
	bindings = append(bindings, "ctrl+r reload", "ctrl+c quit")

	sep := "  •  "
	hintStr := strings.Join(bindings, sep)
	if lipgloss.Width(hintStr) > m.width {
		compact := []string{"enter send", "esc clear", "ctrl+p cmds"}
		if turnActive {
			compact = append(compact, "ctrl+. stop")
		}
		compact = append(compact, "ctrl+c quit")
		hintStr = strings.Join(compact, sep)
	}
	hints := m.theme.Footer.Render(hintStr)
	if chrome != nil {
		chrome.footerHintsKey = key
		chrome.footerHints = hints
	}
	return hints
}

func (m Model) renderFooterPendingImages() string {
	if len(m.pendingImages) == 0 {
		return ""
	}
	chrome := m.chrome
	key := pendingImagesCacheKey(m.pendingImages, m.transcriptWidth())
	if chrome != nil && chrome.footerPendingKey == key {
		return chrome.footerPending
	}
	pending := m.renderPendingImages()
	if chrome != nil {
		chrome.footerPendingKey = key
		chrome.footerPending = pending
	}
	return pending
}

func waitForEvent(ch <-chan history.EventEnvelope) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		batch := []history.EventEnvelope{ev}
	drain:
		for len(batch) < 256 {
			select {
			case ev, ok := <-ch:
				if !ok {
					break drain
				}
				batch = append(batch, ev)
			default:
				break drain
			}
		}
		return appEventsMsg(compactAppEvents(batch))
	}
}

func compactAppEvents(events []history.EventEnvelope) []history.EventEnvelope {
	if len(events) < 2 {
		return events
	}

	out := make([]history.EventEnvelope, 0, len(events))
	for _, ev := range events {
		if ev.Kind != "message.assistant.delta" {
			out = append(out, ev)
			continue
		}
		payload := decode[history.MessageDeltaPayload](ev.Payload)
		if len(out) == 0 || out[len(out)-1].Kind != "message.assistant.delta" {
			out = append(out, ev)
			continue
		}
		prev := decode[history.MessageDeltaPayload](out[len(out)-1].Payload)
		if prev.ID != payload.ID {
			out = append(out, ev)
			continue
		}
		prev.Delta += payload.Delta
		out[len(out)-1].Seq = ev.Seq
		out[len(out)-1].At = ev.At
		out[len(out)-1].Payload = prev
	}
	return out
}

func appEventsTouchLogs(events []history.EventEnvelope) bool {
	for _, ev := range events {
		switch ev.Kind {
		case "tool.finished", "reload.failed", "system.error", "hook.failed":
			return true
		}
	}
	return false
}

func submitCmd(controller *kernel.Controller, value string, attachments []media.Attachment) tea.Cmd {
	return func() tea.Msg {
		return submitDoneMsg{err: controller.SubmitMessage(context.Background(), value, attachments)}
	}
}

func stopTurnCmd(controller *kernel.Controller) tea.Cmd {
	return func() tea.Msg {
		return stopTurnDoneMsg{stopped: controller.CancelTurn()}
	}
}

func reloadCmd(controller *kernel.Controller) tea.Cmd {
	return func() tea.Msg {
		return reloadDoneMsg{err: controller.Reload(context.Background())}
	}
}

func copySelectionCmd() tea.Cmd {
	return func() tea.Msg {
		return copySelectionMsg{}
	}
}

func (m *Model) syncInspectorLogs(force bool) {
	if !force && (!m.logsDirty || !m.inspector.IsLogsActive()) {
		return
	}
	m.inspector.SetLogs(m.controller.LogEntries())
	m.logsDirty = false
}

func (m *Model) resetSessionViews() {
	th, variant, _ := theme.Load(m.controller.Config().UI.Theme, m.controller.Workspace().Root)
	m.theme = th
	m.transcript = transcript.New(m.theme, variant)
	m.inspector = inspector.New(m.controller.Workspace(), m.controller.Session(), m.theme, variant)
	m.palette = commands.New(m.registry, m.theme)
	m.modelPicker = modelspicker.New(m.controller.Registry(), m.theme)
	m.sessionPicker = sessionpicker.New(m.theme)
	m.pendingImages = nil
	m.clearComposerSelection()
	applyInputTheme(&m.input, m.theme, variant)
	m.runtimeDialog = runtimeDialogState{}
	m.runtimePage = runtimePageState{}
	events := m.controller.SessionEvents()
	m.transcript.ApplyBatch(events)
	m.inspector.ApplyBatch(events)
	m.logsDirty = true
	m.syncInspectorLogs(true)
	m.inspector.SetStatus(m.status)
	m.syncRuntimeUI()
	m.invalidateChrome()
	m.resize()
}

func (m *Model) setStatus(status string) {
	m.status = status
	m.inspector.SetStatus(status)
}

// availableThemes enumerates the entries shown by the theme picker: the two
// compiled-in variants (light/dark) plus every YAML/JSON file found under the
// workspace and user theme directories.
func (m Model) availableThemes() []themepicker.Entry {
	entries := []themepicker.Entry{
		{Name: theme.VariantLight, Display: "light", BuiltIn: true},
		{Name: theme.VariantDark, Display: "dark", BuiltIn: true},
	}
	names, err := extensions.ListThemes(m.controller.Workspace().Root)
	if err == nil {
		for _, name := range names {
			if name == theme.VariantLight || name == theme.VariantDark {
				continue
			}
			entries = append(entries, themepicker.Entry{Name: name, Display: name})
		}
	}
	return entries
}

// applyTheme swaps the active theme and rebuilds every view that caches
// styles. The transcript is re-populated from the controller's full session
// log so no session content is lost. The active scroll position is
// intentionally not preserved — re-theming is uncommon and avoiding a partial
// redraw is simpler than selectively recomputing styles.
func (m *Model) applyTheme(name string) {
	m.controller.SetTheme(name)
	th, variant, err := theme.Load(name, m.controller.Workspace().Root)
	if err != nil {
		m.setStatus("Theme error: " + err.Error())
		return
	}
	m.theme = th
	m.transcript = transcript.New(m.theme, variant)
	m.inspector = inspector.New(m.controller.Workspace(), m.controller.Session(), m.theme, variant)
	m.palette = commands.New(m.registry, m.theme)
	m.modelPicker = modelspicker.New(m.controller.Registry(), m.theme)
	m.sessionPicker = sessionpicker.New(m.theme)
	m.themePicker = themepicker.New(m.theme)
	applyInputTheme(&m.input, m.theme, variant)
	events := m.controller.SessionEvents()
	m.transcript.ApplyBatch(events)
	m.inspector.ApplyBatch(events)
	m.logsDirty = true
	m.syncInspectorLogs(true)
	m.inspector.SetStatus(m.status)
	m.invalidateChrome()
	m.resize()

	if name == "" {
		m.setStatus("Theme: default")
	} else {
		m.setStatus("Theme: " + name)
	}
}

func applyInputTheme(input *textarea.Model, th theme.Theme, variant string) {
	// The bubbles textarea has eight distinct style slots per focus state
	// (Base, Text, LineNumber, CursorLineNumber, CursorLine, EndOfBuffer,
	// Placeholder, Prompt). Leaving any of them at bubbles' default means
	// some cell in the input area will render with bubbles' assumed
	// background instead of our theme's — visible as a light band inside
	// the rounded input frame on dark themes. Set every slot explicitly.
	styles := textarea.DefaultStyles(variant == theme.VariantDark)
	base := th.InputText
	state := textarea.StyleState{
		Base:             base,
		Text:             base,
		LineNumber:       base,
		CursorLineNumber: base,
		CursorLine:       base,
		EndOfBuffer:      base,
		Placeholder:      th.InputPlaceholder,
		Prompt:           th.InputPrompt,
	}
	styles.Focused = state
	styles.Blurred = state
	input.SetStyles(styles)
}

func clamp(value, lo, hi int) int {
	if value < lo {
		return lo
	}
	if value > hi {
		return hi
	}
	return value
}

func decode[T any](payload any) T {
	return history.DecodePayload[T](payload)
}
