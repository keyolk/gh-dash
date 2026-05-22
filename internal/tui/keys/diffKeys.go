package keys

import "charm.land/bubbles/v2/key"

// DiffKeyMap is the set of keys consumed by the in-app diff viewer.
type DiffKeyMap struct {
	Close          key.Binding
	ToggleMode     key.Binding
	CursorUp       key.Binding
	CursorDown     key.Binding
	HalfPageDown   key.Binding
	HalfPageUp     key.Binding
	Top            key.Binding
	Bottom         key.Binding
	VisualLine     key.Binding
	VisualBlock    key.Binding
	ClearSelection key.Binding
	Comment        key.Binding
}

var DiffKeys = DiffKeyMap{
	Close: key.NewBinding(
		key.WithKeys("q", "esc"),
		key.WithHelp("q/esc", "close diff"),
	),
	ToggleMode: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "toggle inline/side-by-side"),
	),
	CursorUp: key.NewBinding(
		key.WithKeys("k", "up"),
		key.WithHelp("k/↑", "cursor up"),
	),
	CursorDown: key.NewBinding(
		key.WithKeys("j", "down"),
		key.WithHelp("j/↓", "cursor down"),
	),
	HalfPageDown: key.NewBinding(
		key.WithKeys("ctrl+d", "pgdown"),
		key.WithHelp("ctrl+d/pgdn", "half page down"),
	),
	HalfPageUp: key.NewBinding(
		key.WithKeys("ctrl+u", "pgup"),
		key.WithHelp("ctrl+u/pgup", "half page up"),
	),
	Top: key.NewBinding(
		key.WithKeys("g", "home"),
		key.WithHelp("g", "top"),
	),
	Bottom: key.NewBinding(
		key.WithKeys("G", "end"),
		key.WithHelp("G", "bottom"),
	),
	VisualLine: key.NewBinding(
		key.WithKeys("V"),
		key.WithHelp("V", "visual line"),
	),
	VisualBlock: key.NewBinding(
		key.WithKeys("ctrl+v"),
		key.WithHelp("ctrl+v", "visual block"),
	),
	ClearSelection: key.NewBinding(
		key.WithKeys("escape"),
		key.WithHelp("esc", "clear selection"),
	),
	Comment: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "comment on selection"),
	),
}
