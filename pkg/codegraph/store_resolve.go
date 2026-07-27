//go:build !js

package codegraph

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// leafMatch tracks how many nodes share a leaf suffix or display name,
// and stores the first id. Edge resolution only succeeds when exactly
// one candidate exists (count == 1), matching resolveEdgeNode's
// "exactly one match" semantics.
type leafMatch struct {
	count int
	id    int64
}

// resolutionMaps provides O(1) edge endpoint resolution via in-memory
// maps, replacing the O(n) per-edge SQL queries in resolveEdgeNode.
// Built once from a single SELECT before the edge insertion loop, it
// converts O(E × N) table scans into O(E) map lookups.
type resolutionMaps struct {
	exactQN     map[string]int64      // qualified_name → node id
	suffixLeaf  map[string]*leafMatch // leaf suffix (after last ".") → uniqueness
	displayName map[string]*leafMatch // display_name → uniqueness
}

// buildResolutionMaps loads all nodes into memory in a single query and
// constructs three lookup maps for edge resolution. This replaces the
// per-edge SQL queries in resolveEdgeNode with O(1) map lookups.
func buildResolutionMaps(ctx context.Context, tx *sql.Tx) (*resolutionMaps, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, qualified_name, display_name FROM nodes`)
	if err != nil {
		return nil, fmt.Errorf("query nodes for resolution maps: %w", err)
	}
	defer rows.Close()

	m := &resolutionMaps{
		exactQN:     make(map[string]int64),
		suffixLeaf:  make(map[string]*leafMatch),
		displayName: make(map[string]*leafMatch),
	}

	for rows.Next() {
		var id int64
		var qn, dn string
		if err := rows.Scan(&id, &qn, &dn); err != nil {
			return nil, fmt.Errorf("scan node for resolution maps: %w", err)
		}

		m.exactQN[qn] = id

		// Suffix matching: the LIKE '%.leafName' pattern matches any
		// qualified_name ending with ".leafName". The leafName passed to
		// resolve() may itself contain dots (e.g. "(*Agent).method" when
		// the paren check prevents stripping), so we index every suffix
		// that starts right after ANY dot in the qualified_name — not
		// just the last component.
		for i := 0; i < len(qn); i++ {
			if qn[i] != '.' {
				continue
			}
			suffix := qn[i+1:]
			entry := m.suffixLeaf[suffix]
			if entry == nil {
				entry = &leafMatch{}
				m.suffixLeaf[suffix] = entry
			}
			entry.count++
			if entry.count == 1 {
				entry.id = id
			}
		}

		// Display name: exact match on the node's display_name field.
		entry := m.displayName[dn]
		if entry == nil {
			entry = &leafMatch{}
			m.displayName[dn] = entry
		}
		entry.count++
		if entry.count == 1 {
			entry.id = id
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nodes for resolution maps: %w", err)
	}

	return m, nil
}

// resolve replicates resolveEdgeNode's three-step resolution logic using
// in-memory maps instead of SQL queries:
//
//  1. Exact qualified_name match.
//  2. Suffix match on the leaf name (after last "."), only when exactly
//     one node has that suffix.
//  3. Display name match, only when exactly one node has that display name.
//
// The receiver-stripping logic (e.g. "ag.ProcessQuery" → "ProcessQuery")
// matches resolveEdgeNode exactly.
func (m *resolutionMaps) resolve(qualifiedName string) (int64, bool) {
	// Step 1: exact qualified_name match.
	if id, ok := m.exactQN[qualifiedName]; ok {
		return id, true
	}

	// Extract leaf name, stripping a leading receiver variable prefix
	// (e.g. "ag.ProcessQuery" → "ProcessQuery"). Parenthesized names
	// (e.g. "(*Agent).ProcessQuery") are left intact since the dot is
	// part of the receiver syntax, not a package separator.
	leafName := qualifiedName
	if dotIdx := strings.LastIndexByte(qualifiedName, '.'); dotIdx >= 0 && !strings.Contains(qualifiedName, "(") {
		leafName = qualifiedName[dotIdx+1:]
	}

	// Step 2: suffix match — only resolves when exactly one node has
	// this leaf suffix in its qualified_name.
	if entry := m.suffixLeaf[leafName]; entry != nil && entry.count == 1 {
		return entry.id, true
	}

	// Step 3: display name match — only resolves when exactly one node
	// has this display name.
	if entry := m.displayName[leafName]; entry != nil && entry.count == 1 {
		return entry.id, true
	}

	return 0, false
}
