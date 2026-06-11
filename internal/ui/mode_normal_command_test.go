package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/help"
)

func TestNormalMode_ColonEntersCommandMode(t *testing.T) {
	a := NewApp()
	_ = handleNormalMode(a, tea.KeyPressMsg{Code: ':', Text: ":"})
	if a.mode != ModeCommand {
		t.Fatalf("mode = %v, want ModeCommand", a.mode)
	}
}

func TestNormalMode_CtrlWNoLongerOpensWorkspaceFinder(t *testing.T) {
	a := NewApp()
	_ = handleNormalMode(a, tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	if a.mode == ModeWorkspaceFinder {
		t.Fatal("ctrl+w must not open the workspace finder (reclaimed as window prefix)")
	}
	if a.mode != ModeNormal {
		t.Fatalf("mode = %v, want ModeNormal (ctrl+w is a no-op in phase 1)", a.mode)
	}
}

func TestHelp_StillListsWorkspaceFinderViaWS(t *testing.T) {
	entries := help.FromKeyMap(DefaultKeyMap())
	found := false
	for _, e := range entries {
		if e.Key == "ctrl+w" {
			t.Fatalf("help entry %+v still advertises ctrl+w (reserved as window prefix)", e)
		}
		if e.Key == ":ws" && e.Desc == "switch workspace" {
			found = true
		}
	}
	if !found {
		t.Fatal("help entries missing {Key: \":ws\", Desc: \"switch workspace\"}")
	}
}
