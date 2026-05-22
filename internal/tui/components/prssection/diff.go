package prssection

import (
	tea "charm.land/bubbletea/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
)

func (m Model) diff() tea.Cmd {
	currRowData := m.GetCurrRow()
	if currRowData == nil {
		return nil
	}

	msg := common.OpenDiffMsg{
		PRNumber: currRowData.GetNumber(),
		Repo:     currRowData.GetRepoNameWithOwner(),
		Title:    currRowData.GetTitle(),
	}
	return func() tea.Msg { return msg }
}
