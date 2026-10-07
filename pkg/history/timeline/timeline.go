// Package timeline builds a project timeline from the recorded history.
//
// A timeline is an ordered list of entries. Each entry is either a change
// set — a history revision (the changes sharing one RequestHash, i.e. a
// history.RevisionGroup) rendered with a short deterministic template
// summary — a history revision (the changes sharing one RequestHash, i.e. a
// history.RevisionGroup) rendered with a short deterministic template
// summary and the plan scope IDs it pertains to — or a deploy recorded by
// the deploy package.
//
// The package is a sub-interface of pkg/history rather than part of it
// because a timeline is defined by joining two domain models: pkg/history
// and pkg/deploy, tagged with pkg/plancontract scope IDs. Keeping it in a
// distinct package leaves pkg/history free of the deploy and plancontract
// dependencies (the history data layer is used by WASM and CLI paths that
// must not drag in deploy/plan machinery), and gives the timeline one
// obvious home as the checkpoints surface and the Changes UI grow.
package timeline

import (
	"fmt"
	"sort"
	"time"

	"github.com/sprout-foundry/sprout/pkg/deploy"
	"github.com/sprout-foundry/sprout/pkg/history"
)

// Kind identifies what a timeline entry records. The set is closed: an
// entry is a change set (a history revision) or a deploy.
type Kind string

const (
	// KindChangeSet is a history revision: the changes sharing one
	// RequestHash, with a template summary of its diff.
	KindChangeSet Kind = "change_set"
	// KindDeploy is a deploy recorded by a deploy target.
	KindDeploy Kind = "deploy"
)

// Order fixes the direction a timeline is sorted in.
type Order string

const (
	// OldestFirst orders entries by timestamp ascending (oldest at index 0).
	OldestFirst Order = "oldest_first"
	// NewestFirst orders entries by timestamp descending (newest at index 0).
	NewestFirst Order = "newest_first"
)

// ChangeSet is a timeline entry for a history revision. Summary is the
// deterministic template summary of the revision's diff; ScopeIDs are the
// plan scope IDs the caller associated with the revision (optional).
type ChangeSet struct {
	// RevisionID is the revision's RequestHash — the change set's identity.
	RevisionID string
	// Timestamp is the revision's earliest change time.
	Timestamp time.Time
	// Summary is the short deterministic template summary of the diff.
	Summary string
	// Files is the list of files the revision touched, sorted.
	Files []string
	// FilesTouched, Insertions and Deletions are the diff counts the
	// summary is built from.
	FilesTouched int
	Insertions   int
	Deletions    int
	// ScopeIDs are the plan scope IDs this change set pertains to, as
	// supplied by the caller (the timeline never reaches into a plan
	// store). Empty when the caller supplied none.
	ScopeIDs []string
}

// Deploy is a timeline entry for a deploy. It carries the deploy
// package's Deployment value rather than a copy of its fields, so the
// timeline never reimplements the deploy model.
type Deploy struct {
	// Deployment is the recorded deploy.
	Deployment deploy.Deployment
	// Summary is the short deterministic template summary of the deploy.
	Summary string
}

// Entry is one item on the timeline: exactly one of ChangeSet or Deploy is
// non-nil, as reported by Kind().
type Entry struct {
	// Timestamp is the entry's ordering key.
	Timestamp time.Time
	// ChangeSet is set when the entry is a change set (KindChangeSet).
	ChangeSet *ChangeSet
	// Deploy is set when the entry is a deploy (KindDeploy).
	Deploy *Deploy
}

// Kind reports the entry's kind. It returns "" for a malformed entry that
// carries neither or both payloads.
func (e Entry) Kind() Kind {
	switch {
	case e.ChangeSet != nil && e.Deploy == nil:
		return KindChangeSet
	case e.Deploy != nil && e.ChangeSet == nil:
		return KindDeploy
	default:
		return ""
	}
}

// Summary returns the entry's template summary, or "" for a malformed
// entry.
func (e Entry) Summary() string {
	switch e.Kind() {
	case KindChangeSet:
		return e.ChangeSet.Summary
	case KindDeploy:
		return e.Deploy.Summary
	default:
		return ""
	}
}

// ScopeIDs returns the change set's plan scope IDs, or nil for a deploy or
// malformed entry.
func (e Entry) ScopeIDs() []string {
	if e.ChangeSet == nil {
		return nil
	}
	return e.ChangeSet.ScopeIDs
}

// EntryInput is one entry passed to Build. Exactly one of Revision or
// Deployment must be set. ScopeIDs is optional and applies only to a
// Revision input.
type EntryInput struct {
	// Revision is a history revision to render as a change set.
	Revision *history.RevisionGroup
	// Deployment is a deploy to render as a deploy entry.
	Deployment *deploy.Deployment
	// ScopeIDs are the plan scope IDs the revision pertains to.
	ScopeIDs []string
}

// Build renders the timeline from the given entries, sorted per order.
//
// A change set's summary is a deterministic template over the revision's
// diff (see SummarizeRevision): the files it touched and its
// insertions/deletions. The plan scope IDs for a change set come from the
// matching EntryInput; the builder itself never reads a plan store.
//
// Timestamps are the ordering key. Ties are broken by a stable identity
// (revision ID for change sets, deployment ID for deploys), so the result
// is deterministic even when two entries share a timestamp; a change set
// sorts before a deploy at the same timestamp.
func Build(inputs []EntryInput, order Order) []Entry {
	if order != NewestFirst {
		order = OldestFirst
	}

	entries := make([]Entry, 0, len(inputs))
	for _, in := range inputs {
		if in.Revision != nil {
			entries = append(entries, Entry{
				Timestamp: in.Revision.Timestamp,
				ChangeSet: summarizeRevision(in.Revision, in.ScopeIDs),
			})
			continue
		}
		if in.Deployment != nil {
			d := *in.Deployment
			entries = append(entries, Entry{
				Timestamp: d.CreatedAt,
				Deploy:    &Deploy{Deployment: d, Summary: SummarizeDeploy(d)},
			})
		}
	}

	sortEntries(entries, order)
	return entries
}

// BuildTimeline reads the recorded history, renders each revision as a
// change set (with the given scope IDs, keyed by revision ID) and appends
// the given deploys, returning the timeline in the requested order.
//
// scopeIDsByRevision is optional: a nil map (or a missing revision) yields
// a change set with no scope IDs. The caller supplies the association; the
// timeline builder takes the active plan's mapping as input rather than
// reading a plan store, which keeps this package independent of the agent.
func BuildTimeline(deployments []deploy.Deployment, scopeIDsByRevision map[string][]string, order Order) ([]Entry, error) {
	groups, err := history.GetRevisionGroups()
	if err != nil {
		return nil, fmt.Errorf("reading revision groups: %w", err)
	}

	inputs := make([]EntryInput, 0, len(groups)+len(deployments))
	for i := range groups {
		g := groups[i]
		var scopeIDs []string
		if scopeIDsByRevision != nil {
			scopeIDs = scopeIDsByRevision[g.RevisionID]
		}
		inputs = append(inputs, EntryInput{Revision: &g, ScopeIDs: scopeIDs})
	}
	for i := range deployments {
		d := deployments[i]
		inputs = append(inputs, EntryInput{Deployment: &d})
	}

	return Build(inputs, order), nil
}

func sortEntries(entries []Entry, order Order) {
	less := func(i, j int) bool {
		a, b := entries[i], entries[j]
		if !a.Timestamp.Equal(b.Timestamp) {
			if order == NewestFirst {
				return a.Timestamp.After(b.Timestamp)
			}
			return a.Timestamp.Before(b.Timestamp)
		}
		ka, kb := a.Kind(), b.Kind()
		if ka != kb {
			// Change sets sort before deploys at an equal timestamp.
			return ka == KindChangeSet
		}
		return tieBreak(a) < tieBreak(b)
	}
	sort.SliceStable(entries, less)
}

func tieBreak(e Entry) string {
	switch e.Kind() {
	case KindChangeSet:
		return e.ChangeSet.RevisionID
	case KindDeploy:
		return e.Deploy.Deployment.ID
	default:
		return ""
	}
}

func summarizeRevision(group *history.RevisionGroup, scopeIDs []string) *ChangeSet {
	sum := SummarizeRevision(group)
	files := append([]string(nil), sum.Files...)
	ids := append([]string(nil), scopeIDs...)
	return &ChangeSet{
		RevisionID:   group.RevisionID,
		Timestamp:    group.Timestamp,
		Summary:      sum.Text,
		Files:        files,
		FilesTouched: sum.FilesTouched,
		Insertions:   sum.Insertions,
		Deletions:    sum.Deletions,
		ScopeIDs:     ids,
	}
}
