package common

// OpenDiffMsg is broadcast when something (a list section, a sidebar action,
// etc.) wants to open the in-app diff viewer for a PR. The top-level UI
// model listens for this and forwards it to the diffview component.
type OpenDiffMsg struct {
	PRNumber int
	Repo     string
	Title    string
}
