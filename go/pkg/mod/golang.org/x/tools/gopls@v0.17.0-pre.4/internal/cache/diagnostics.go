// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"golang.org/x/tools/gopls/internal/file"
	"golang.org/x/tools/gopls/internal/protocol"
	"golang.org/x/tools/gopls/internal/util/bug"
)

// A InitializationError is an error that causes snapshot initialization to fail.
// It is either the error returned from go/packages.Load, or an error parsing a
// workspace go.work or go.mod file.
//
// Such an error generally indicates that the View is malformed, and will never
// be usable.
type InitializationError struct {
	// MainError is the primary error. Must be non-nil.
	MainError error

	// Diagnostics contains any supplemental (structured) diagnostics extracted
	// from the load error.
	Diagnostics map[protocol.DocumentURI][]*Diagnostic
}

func byURI(d *Diagnostic) protocol.DocumentURI { return d.URI } // For use in maps.Group.

// An Diagnostic corresponds to an LSP Diagnostic.
// https://microsoft.github.io/language-server-protocol/specification#diagnostic
//
// It is (effectively) gob-serializable; see {encode,decode}Diagnostics.
type Diagnostic struct {
	URI      protocol.DocumentURI // of diagnosed file (not diagnostic documentation)
	Range    protocol.Range
	Severity protocol.DiagnosticSeverity
	Code     string // analysis.Diagnostic.Category (or "default" if empty) or hidden go/types error code
	CodeHref string

	// Source is a human-readable description of the source of the error.
	// Diagnostics generated by an analysis.Analyzer set it to Analyzer.Name.
	Source DiagnosticSource

	Message string

	Tags    []protocol.DiagnosticTag
	Related []protocol.DiagnosticRelatedInformation

	// Fields below are used internally to generate lazy fixes. They aren't
	// part of the LSP spec and historically didn't leave the server.
	//
	// Update(2023-05): version 3.16 of the LSP spec included support for the
	// Diagnostic.data field, which holds arbitrary data preserved in the
	// diagnostic for codeAction requests. This field allows bundling additional
	// information for lazy fixes, and gopls can (and should) use this
	// information to avoid re-evaluating diagnostics in code-action handlers.
	//
	// In order to stage this transition incrementally, the 'BundledFixes' field
	// may store a 'bundled' (=json-serialized) form of the associated
	// SuggestedFixes. Not all diagnostics have their fixes bundled.
	BundledFixes   *json.RawMessage
	SuggestedFixes []SuggestedFix
}

func (d *Diagnostic) String() string {
	return fmt.Sprintf("%v: %s", d.Range, d.Message)
}

// Hash computes a hash to identify the diagnostic.
// The hash is for deduplicating within a file, so does not incorporate d.URI.
func (d *Diagnostic) Hash() file.Hash {
	h := sha256.New()
	for _, t := range d.Tags {
		fmt.Fprintf(h, "tag: %s\n", t)
	}
	for _, r := range d.Related {
		fmt.Fprintf(h, "related: %s %s %s\n", r.Location.URI, r.Message, r.Location.Range)
	}
	fmt.Fprintf(h, "code: %s\n", d.Code)
	fmt.Fprintf(h, "codeHref: %s\n", d.CodeHref)
	fmt.Fprintf(h, "message: %s\n", d.Message)
	fmt.Fprintf(h, "range: %s\n", d.Range)
	fmt.Fprintf(h, "severity: %s\n", d.Severity)
	fmt.Fprintf(h, "source: %s\n", d.Source)
	if d.BundledFixes != nil {
		fmt.Fprintf(h, "fixes: %s\n", *d.BundledFixes)
	}
	var hash [sha256.Size]byte
	h.Sum(hash[:0])
	return hash
}

type DiagnosticSource string

const (
	UnknownError             DiagnosticSource = "<Unknown source>"
	ListError                DiagnosticSource = "go list"
	ParseError               DiagnosticSource = "syntax"
	TypeError                DiagnosticSource = "compiler"
	ModTidyError             DiagnosticSource = "go mod tidy"
	OptimizationDetailsError DiagnosticSource = "optimizer details"
	UpgradeNotification      DiagnosticSource = "upgrade available"
	Vulncheck                DiagnosticSource = "vulncheck imports"
	Govulncheck              DiagnosticSource = "govulncheck"
	TemplateError            DiagnosticSource = "template"
	WorkFileError            DiagnosticSource = "go.work file"
	ConsistencyInfo          DiagnosticSource = "consistency"
)

// A SuggestedFix represents a suggested fix (for a diagnostic)
// produced by analysis, in protocol form.
//
// The fixes are reported to the client as a set of code actions in
// response to a CodeAction query for a set of diagnostics. Multiple
// SuggestedFixes may be produced for the same logical fix, varying
// only in ActionKind. For example, a fix may be both a Refactor
// (which should appear on the refactoring menu) and a SourceFixAll (a
// clear fix that can be safely applied without explicit consent).
type SuggestedFix struct {
	Title      string
	Edits      map[protocol.DocumentURI][]protocol.TextEdit
	Command    *protocol.Command
	ActionKind protocol.CodeActionKind
}

// SuggestedFixFromCommand returns a suggested fix to run the given command.
func SuggestedFixFromCommand(cmd *protocol.Command, kind protocol.CodeActionKind) SuggestedFix {
	return SuggestedFix{
		Title:      cmd.Title,
		Command:    cmd,
		ActionKind: kind,
	}
}

// lazyFixesJSON is a JSON-serializable list of code actions (arising
// from "lazy" SuggestedFixes with no Edits) to be saved in the
// protocol.Diagnostic.Data field. Computation of the edits is thus
// deferred until the action's command is invoked.
type lazyFixesJSON struct {
	// TODO(rfindley): pack some sort of identifier here for later
	// lookup/validation?
	Actions []protocol.CodeAction
}

// bundleLazyFixes attempts to bundle sd.SuggestedFixes into the
// sd.BundledFixes field, so that it can be round-tripped through the client.
// It returns false if the fixes cannot be bundled.
func bundleLazyFixes(sd *Diagnostic) bool {
	if len(sd.SuggestedFixes) == 0 {
		return true
	}
	var actions []protocol.CodeAction
	for _, fix := range sd.SuggestedFixes {
		if fix.Edits != nil {
			// For now, we only support bundled code actions that execute commands.
			//
			// In order to cleanly support bundled edits, we'd have to guarantee that
			// the edits were generated on the current snapshot. But this naively
			// implies that every fix would have to include a snapshot ID, which
			// would require us to republish all diagnostics on each new snapshot.
			//
			// TODO(rfindley): in order to avoid this additional chatter, we'd need
			// to build some sort of registry or other mechanism on the snapshot to
			// check whether a diagnostic is still valid.
			return false
		}
		action := protocol.CodeAction{
			Title:   fix.Title,
			Kind:    fix.ActionKind,
			Command: fix.Command,
		}
		actions = append(actions, action)
	}
	fixes := lazyFixesJSON{
		Actions: actions,
	}
	data, err := json.Marshal(fixes)
	if err != nil {
		bug.Reportf("marshalling lazy fixes: %v", err)
		return false
	}
	msg := json.RawMessage(data)
	sd.BundledFixes = &msg
	return true
}

// BundledLazyFixes extracts any bundled codeActions from the
// diag.Data field.
func BundledLazyFixes(diag protocol.Diagnostic) ([]protocol.CodeAction, error) {
	var fix lazyFixesJSON
	if diag.Data != nil {
		err := protocol.UnmarshalJSON(*diag.Data, &fix)
		if err != nil {
			return nil, fmt.Errorf("unmarshalling fix from diagnostic data: %v", err)
		}
	}

	var actions []protocol.CodeAction
	for _, action := range fix.Actions {
		// See bundleLazyFixes: for now we only support bundling commands.
		if action.Edit != nil {
			return nil, fmt.Errorf("bundled fix %q includes workspace edits", action.Title)
		}
		// associate the action with the incoming diagnostic
		// (Note that this does not mutate the fix.Fixes slice).
		action.Diagnostics = []protocol.Diagnostic{diag}
		actions = append(actions, action)
	}

	return actions, nil
}