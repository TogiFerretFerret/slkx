// internal/ui/mode_workspace_search.go
//
// Workspace-search mode key handler: the ctrl+f modal.
//
// Forwards normalized keys to the searchresults overlay and
// translates its actions: Submit dispatches the server-side
// search.messages query via the SearchService, Select closes the
// modal and navigates to the chosen message via the pendingLinkNav
// mechanism (FetchAround completes off-buffer targets).
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/searchresults"
)

func handleWorkspaceSearchMode(a *App, msg tea.KeyMsg) tea.Cmd {
	keyStr := normalizeFinderKey(msg)
	switch action := a.searchResults.HandleKey(keyStr); action {
	case searchresults.ActionClose:
		a.SetMode(ModeNormal)
		return nil
	case searchresults.ActionSubmit:
		query := a.searchResults.Query()
		search := a.searchSvc
		return func() tea.Msg { return search.SearchWorkspace(query) }
	case searchresults.ActionSelect:
		item, ok := a.searchResults.Selected()
		a.searchResults.Close()
		a.SetMode(ModeNormal)
		if !ok {
			return nil
		}
		a.pendingLinkNav = &pendingLinkNav{
			channelID: item.ChannelID,
			messageTS: item.TS,
			threadTS:  item.ThreadTS,
		}
		if item.ChannelID == a.activeChannelID {
			return a.completePendingLinkNav(a.activeChannelID, true)
		}
		// Resolve channel type from the local lookup when available;
		// fall back to "channel" for never-seen channels (the open
		// path tolerates this the same way permalink navigation does).
		name, chType, ok := a.channels.Lookup(ids.ChannelID(item.ChannelID))
		if !ok {
			name, chType = item.ChannelName, "channel"
		}
		return func() tea.Msg {
			return ChannelSelectedMsg{ID: item.ChannelID, Name: name, Type: chType}
		}
	}
	return nil
}
