package tui

import (
	"runtime"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func platformPasteKeys() []string {
	if runtime.GOOS == "darwin" {
		return []string{"ctrl+v", "super+v"}
	}
	return []string{"ctrl+v"}
}

func platformCopyKeys() []string {
	if runtime.GOOS == "darwin" {
		return []string{"ctrl+y", "super+c"}
	}
	return []string{"ctrl+y", "ctrl+c"}
}

func platformSelectAllMsg(msg tea.KeyPressMsg) bool {
	if runtime.GOOS == "darwin" {
		return msg.Code == 'a' && msg.Mod == tea.ModSuper
	}
	return msg.Code == 'a' && msg.Mod == tea.ModCtrl
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
