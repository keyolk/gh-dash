package tui

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cli/go-gh/v2/pkg/browser"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

func (m *Model) openBrowser() tea.Cmd {
	taskId := fmt.Sprintf("open_browser_%d", time.Now().Unix())
	task := context.Task{
		Id:           taskId,
		StartText:    "Opening in browser",
		FinishedText: "Opened in browser",
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := m.ctx.StartTask(task)
	openCmd := func() tea.Msg {
		b := browser.New("", os.Stdout, os.Stdin)
		currRow := m.getCurrRowData()
		if currRow == nil || reflect.ValueOf(currRow).IsNil() {
			return constants.TaskFinishedMsg{
				TaskId: taskId,
				Err:    errors.New("current selection doesn't have a URL"),
			}
		}
		err := b.Browse(currRow.GetUrl())
		return constants.TaskFinishedMsg{TaskId: taskId, Err: err}
	}
	return tea.Batch(startCmd, openCmd)
}

// openURL opens an arbitrary URL in the browser (used for individual check
// details URLs).
func (m *Model) openURL(url string) tea.Cmd {
	if url == "" {
		return m.notifyErr("This check has no details URL")
	}
	taskId := fmt.Sprintf("open_browser_%d", time.Now().Unix())
	task := context.Task{
		Id:           taskId,
		StartText:    "Opening check in browser",
		FinishedText: "Opened check in browser",
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := m.ctx.StartTask(task)
	openCmd := func() tea.Msg {
		b := browser.New("", os.Stdout, os.Stdin)
		err := b.Browse(url)
		return constants.TaskFinishedMsg{TaskId: taskId, Err: err}
	}
	return tea.Batch(startCmd, openCmd)
}
