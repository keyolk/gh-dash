package keys

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	log "charm.land/log/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
)

type PRKeyMap struct {
	PrevSidebarTab       key.Binding
	NextSidebarTab       key.Binding
	Approve              key.Binding
	Assign               key.Binding
	Unassign             key.Binding
	Label                key.Binding
	Comment              key.Binding
	Diff                 key.Binding
	Checkout             key.Binding
	Close                key.Binding
	SummaryViewMore      key.Binding
	Ready                key.Binding
	Reopen               key.Binding
	Merge                key.Binding
	Update               key.Binding
	WatchChecks          key.Binding
	ApproveWorkflows     key.Binding
	ToggleSmartFiltering key.Binding
	ViewIssues           key.Binding
}

var PRKeys = PRKeyMap{
	PrevSidebarTab: key.NewBinding(
		key.WithKeys("["),
		key.WithHelp("[", "previous sidebar tab"),
	),
	NextSidebarTab: key.NewBinding(
		key.WithKeys("]"),
		key.WithHelp("]", "next sidebar tab"),
	),
	Approve: key.NewBinding(
		key.WithKeys("v"),
		key.WithHelp("v", "approve"),
	),
	Assign: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "assign"),
	),
	Unassign: key.NewBinding(
		key.WithKeys("A"),
		key.WithHelp("A", "unassign"),
	),
	Label: key.NewBinding(
		key.WithKeys("L"),
		key.WithHelp("L", "label"),
	),
	Comment: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "comment"),
	),
	Diff: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "diff"),
	),
	Checkout: key.NewBinding(
		key.WithKeys("C", "space"),
		key.WithHelp("C/Space", "checkout"),
	),
	Close: key.NewBinding(
		key.WithKeys("x"),
		key.WithHelp("x", "close"),
	),
	SummaryViewMore: key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "expand description"),
	),
	Reopen: key.NewBinding(
		key.WithKeys("X"),
		key.WithHelp("X", "reopen"),
	),
	Ready: key.NewBinding(
		key.WithKeys("W"),
		key.WithHelp("W", "ready for review"),
	),
	Merge: key.NewBinding(
		key.WithKeys("m"),
		key.WithHelp("m", "merge"),
	),
	Update: key.NewBinding(
		key.WithKeys("u"),
		key.WithHelp("u", "update pr from base branch"),
	),
	WatchChecks: key.NewBinding(
		key.WithKeys("w"),
		key.WithHelp("w", "watch checks"),
	),
	ApproveWorkflows: key.NewBinding(
		key.WithKeys("V"),
		key.WithHelp("V", "approve all workflows"),
	),
	ToggleSmartFiltering: key.NewBinding(
		key.WithKeys("t"),
		key.WithHelp("t", "toggle smart filtering"),
	),
	ViewIssues: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "switch to issues"),
	),
}

func PRFullHelp() []key.Binding {
	return []key.Binding{
		PRKeys.PrevSidebarTab,
		PRKeys.NextSidebarTab,
		PRKeys.Approve,
		PRKeys.Assign,
		PRKeys.Unassign,
		PRKeys.Label,
		PRKeys.Comment,
		PRKeys.Diff,
		PRKeys.Checkout,
		PRKeys.Close,
		PRKeys.Ready,
		PRKeys.Reopen,
		PRKeys.Merge,
		PRKeys.Update,
		PRKeys.WatchChecks,
		PRKeys.ApproveWorkflows,
		PRKeys.ToggleSmartFiltering,
		PRKeys.ViewIssues,
	}
}

func rebindPRKeys(keys []config.Keybinding) error {
	CustomPRBindings = []key.Binding{}
	touched := make(map[string]bool)

	for _, prKey := range keys {
		if prKey.Builtin == "" {
			// Handle custom commands
			if prKey.Command != "" {
				name := prKey.Name
				if prKey.Name == "" {
					name = config.TruncateCommand(prKey.Command)
				}

				customBinding := key.NewBinding(
					key.WithKeys(prKey.Key),
					key.WithHelp(prKey.Key, name),
				)

				CustomPRBindings = append(CustomPRBindings, customBinding)
			}
			continue
		}

		log.Debug("Rebinding PR key", "builtin", prKey.Builtin, "key", prKey.Key)

		var binding *key.Binding

		switch prKey.Builtin {
		case "prevSidebarTab":
			binding = &PRKeys.PrevSidebarTab
		case "nextSidebarTab":
			binding = &PRKeys.NextSidebarTab
		case "approve":
			binding = &PRKeys.Approve
		case "assign":
			binding = &PRKeys.Assign
		case "unassign":
			binding = &PRKeys.Unassign
		case "label":
			binding = &PRKeys.Label
		case "comment":
			binding = &PRKeys.Comment
		case "diff":
			binding = &PRKeys.Diff
		case "checkout":
			binding = &PRKeys.Checkout
		case "close":
			binding = &PRKeys.Close
		case "ready":
			binding = &PRKeys.Ready
		case "reopen":
			binding = &PRKeys.Reopen
		case "merge":
			binding = &PRKeys.Merge
		case "update":
			binding = &PRKeys.Update
		case "watchChecks":
			binding = &PRKeys.WatchChecks
		case "approveWorkflows":
			binding = &PRKeys.ApproveWorkflows
		case "viewIssues":
			binding = &PRKeys.ViewIssues
		case "summaryViewMore":
			binding = &PRKeys.SummaryViewMore
		default:
			return fmt.Errorf("unknown built-in pr key: '%s'", prKey.Builtin)
		}

		applyBuiltinKey(binding, prKey, touched)
	}

	return nil
}
