package tui

import (
	"runtime"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func platformPasteKeys() []string {
	return []string{"ctrl+v", "super+v"}
}

func platformCopyKeys() []string {
	return []string{"ctrl+y", "super+c"}
}

func platformSelectAllMsg(msg tea.KeyPressMsg) bool {
	return msg.Code == 'a' && (msg.Mod == tea.ModCtrl || msg.Mod == tea.ModSuper)
}

func platformHint(darwin, other string) string {
	if runtime.GOOS == "darwin" {
		return darwin
	}
	return other
}

func platformPasteBinding() key.Binding {
	return key.NewBinding(
		key.WithKeys(platformPasteKeys()...),
		key.WithHelp(platformHint("ctrl/cmd+v", "ctrl+v"), "paste"),
	)
}

func platformCopyBinding() key.Binding {
	return key.NewBinding(
		key.WithKeys(platformCopyKeys()...),
		key.WithHelp(platformHint("ctrl+y/cmd+c", "ctrl+y/ctrl+c"), "copy"),
	)
}
