// internal/ui/mode_linkpicker.go
//
// Key handler for ModeLinkPicker: the link-choice modal opened by
// the `o` keybinding when the selected message has multiple links.
// Enter dispatches the chosen URL as OpenLinkMsg (routed by
// reduceLinks); esc/q closes.
package ui

import (
	tea "charm.land/bubbletea/v2"
)

func handleLinkPickerMode(a *App, msg tea.KeyMsg) tea.Cmd {
	item, chosen := a.linkPicker.HandleKey(msg.String())
	if chosen {
		a.SetMode(ModeNormal)
		url := item.URL
		return func() tea.Msg { return OpenLinkMsg{URL: url} }
	}
	if !a.linkPicker.IsVisible() {
		// esc/q closed the picker.
		a.SetMode(ModeNormal)
	}
	return nil
}
