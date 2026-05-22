package keys

import "charm.land/bubbles/v2/key"

// DiffKeyMap is the set of keys consumed by the in-app diff viewer.
type DiffKeyMap struct {
	Close      key.Binding
	ToggleMode key.Binding
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
}
