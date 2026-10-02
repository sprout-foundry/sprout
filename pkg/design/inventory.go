package design

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// inventoryAssetKinds are the per-asset `kind` values the inventory rows
// carry, one per canonical design asset class (SP-140-2 §2c).
const (
	KindManifest  = "manifest"
	KindToken     = "token"
	KindBrand     = "brand"
	KindIcon      = "icon"
	KindWireframe = "wireframe"
	KindComponent = "component"
	KindScreen    = "screen"
	KindFlow      = "flow"
	KindFeedback  = "feedback"
	// KindRuntime lists the SP-143 screen-kit fixed assets under
	// design/runtime/ (the runtime, device chrome, base documents). They are
	// tool-owned, not an authoring tier, but design_assets lists the tier so
	// a model sees what is actually in the tree (SP-143 §143.5).
	KindRuntime = "runtime"
	KindUnkn    = "unknown"
)

// AssetRow is one inventory entry `{path, kind, name, status?, summary?}`
// (SP-140-2 §2c). Path is the workspace-relative slash path; Name is the
// asset's stem (the screen/flow/icon name, `README` for the manifest, or the
// file stem otherwise). Status is populated from the manifest's status
// markers when one is declared for the asset; Summary is a short,
// best-effort description parsed from the manifest listing when present.
type AssetRow struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Status  string `json:"status,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// TokenGroupCount is the number of leaf tokens in one top-level token group
// across every design/tokens/*.tokens.json file (SP-140-2 §2c "token group
// counts"). Counting is per group name, summed across files, because tokens
// are split one file per tier and consumers glob.
type TokenGroupCount struct {
	Group  string `json:"group"`
	Tokens int    `json:"tokens"`
}

// FlowCounts is the node/edge count of one design/flows/*.mmd file
// (SP-140-2 §2c "flow node/edge counts").
type FlowCounts struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Nodes int    `json:"nodes"`
	Edges int    `json:"edges"`
}

// ManifestSummary is the high-level manifest view: whether design/README.md
// exists, the device frames it declares, and the status markers it documents
// (SP-140-2 §2c "manifest summary").
type ManifestSummary struct {
	Exists bool     `json:"exists"`
	Frames []Frame  `json:"frames"`
	Status []string `json:"status"`
}

// Inventory is the structured `design_assets` result for a workspace that has
// a design/ tree (SP-140-2 §2c). Assets holds one row per discovered asset,
// sorted by path. TokenGroups holds per-group leaf-token counts sorted by
// group name. Flows holds per-flow node/edge counts sorted by path. Findings
// holds the (uncached, always-fresh) validator results for the tree, and
// BySeverity tallies them.
type Inventory struct {
	Assets      []AssetRow        `json:"assets"`
	TokenGroups []TokenGroupCount `json:"tokenGroups"`
	Flows       []FlowCounts      `json:"flows"`
	Manifest    ManifestSummary   `json:"manifest"`
	Findings    []Finding         `json:"findings"`
	BySeverity  map[string]int    `json:"bySeverity"`
}

// Scan walks the design/ tree under root and builds its inventory
// (SP-140-2 §2c). root is the workspace root (the parent of design/); it must
// already contain a design/ directory — callers check FileExists first.
//
// The walk is bounded to the canonical subdirectories; unknown files inside
// design/ are reported with KindUnkn rather than skipped, so the model sees
// what is actually there. Findings come from ValidateTree and are advisory:
// an error-severity finding never makes Scan fail. Only a real I/O failure
// (an unreadable matching file) returns an error.
func Scan(root string) (*Inventory, error) {
	inv := &Inventory{
		Assets:      []AssetRow{},
		TokenGroups: []TokenGroupCount{},
		Flows:       []FlowCounts{},
		Findings:    []Finding{},
		BySeverity:  map[string]int{"error": 0, "warn": 0, "info": 0, "fix": 0},
	}

	manifestRows, manifestSummary, err := scanManifest(root)
	if err != nil {
		return nil, err
	}
	inv.Manifest = manifestSummary
	inv.Assets = append(inv.Assets, manifestRows...)

	rows, err := scanAssets(root)
	if err != nil {
		return nil, err
	}
	inv.Assets = append(inv.Assets, rows...)

	groups, err := scanTokenGroups(root)
	if err != nil {
		return nil, err
	}
	inv.TokenGroups = groups

	flows, err := scanFlows(root)
	if err != nil {
		return nil, err
	}
	inv.Flows = flows

	findings, err := ValidateTree(root)
	if err != nil {
		return nil, err
	}
	inv.Findings = findings
	for _, f := range findings {
		inv.BySeverity[f.Severity.String()]++
	}

	// Attach the manifest's status marker + summary to each asset row, when
	// the manifest declares one. This is a best-effort enrichment; a missing
	// or unparsable listing simply leaves the fields empty.
	enrichAssetRows(inv.Assets, root)

	sort.Slice(inv.Assets, func(i, j int) bool { return inv.Assets[i].Path < inv.Assets[j].Path })
	return inv, nil
}

// scanManifest builds the manifest asset row plus the manifest summary. A
// missing README yields no row and an Exists:false summary (not an error).
func scanManifest(root string) ([]AssetRow, ManifestSummary, error) {
	rel := path.Join(DirName, ManifestName)
	abs := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ManifestSummary{}, nil
		}
		return nil, ManifestSummary{}, err
	}
	if info.IsDir() {
		return nil, ManifestSummary{}, nil
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, ManifestSummary{}, err
	}

	summary := ManifestSummary{Exists: true, Frames: []Frame{}, Status: []string{}}
	if frames, ferr := ParseFrames(string(data)); ferr == nil {
		summary.Frames = frames
	}
	for _, marker := range []string{"draft", "review", "ready"} {
		if strings.Contains(strings.ToLower(string(data)), marker) {
			summary.Status = append(summary.Status, marker)
		}
	}

	row := AssetRow{Path: rel, Kind: KindManifest, Name: "README"}
	return []AssetRow{row}, summary, nil
}

// scanAssets walks the canonical design subdirectories and returns one row
// per asset file. Files that do not match a canonical asset pattern are kept
// with KindUnkn so the inventory stays truthful about what exists.
func scanAssets(root string) ([]AssetRow, error) {
	designRoot := filepath.Join(root, DirName)
	rows := []AssetRow{}

	for _, sub := range append(append([]string(nil), Subdirs...), RuntimeSubdir) {
		dir := filepath.Join(designRoot, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				name := e.Name()
				if name == ".gitkeep" || strings.HasPrefix(name, ".") {
					continue
				}
				rows = append(rows, AssetRow{
					Path: path.Join(DirName, sub, name),
					Kind: assetKind(sub, name),
					Name: assetName(name),
				})
				continue
			}
			// runtime/base/ and any other one-level grouping under a tier:
			// the row keeps the nested slash path so the tree stays truthful.
			nested := filepath.Join(dir, e.Name())
			files, err := os.ReadDir(nested)
			if err != nil {
				return nil, err
			}
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				name := f.Name()
				if name == ".gitkeep" || strings.HasPrefix(name, ".") {
					continue
				}
				rows = append(rows, AssetRow{
					Path: path.Join(DirName, sub, e.Name(), name),
					Kind: assetKind(sub, name),
					Name: assetName(name),
				})
			}
		}
	}
	return rows, nil
}

// assetKind classifies one design asset file by subdirectory and extension,
// matching the dispatch shape of ValidateFile.
func assetKind(sub, name string) string {
	switch sub {
	case "tokens":
		if strings.HasSuffix(name, ".tokens.json") {
			return KindToken
		}
	case "brand":
		if name == "brand.md" || strings.HasSuffix(name, ".svg") {
			return KindBrand
		}
	case "icons":
		if strings.HasSuffix(name, ".svg") {
			return KindIcon
		}
	case "wireframes":
		if strings.HasSuffix(name, ".svg") {
			return KindWireframe
		}
	case "components":
		if strings.HasSuffix(name, ".svg") {
			return KindComponent
		}
	case "screens":
		if strings.HasSuffix(name, ".html") {
			return KindScreen
		}
	case "flows":
		// The .json source and the derived .mmd export are both flow assets;
		// a .layout.json sidecar is canvas state, not a flow (SP-140-9 §9b).
		if strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, layoutSidecarSuffix) {
			return KindFlow
		}
		if strings.HasSuffix(name, ".mmd") {
			return KindFlow
		}
	case "feedback":
		if strings.HasSuffix(name, ".json") {
			return KindFeedback
		}
	case RuntimeSubdir:
		// SP-143: the fixed screen-kit assets (runtime js, chrome.css, and
		// the base/ documents). Every file in the tier is runtime — it is
		// tool-owned, so an unknown file there is still listed as runtime
		// rather than inventing a kind per extension.
		return KindRuntime
	}
	return KindUnkn
}

// assetName derives the inventory name of an asset: the stem with known
// design extensions stripped (login.svg -> login, color.tokens.json -> color).
func assetName(fileName string) string {
	for _, ext := range []string{".tokens.json", ".svg", ".mmd", ".html", ".json", ".md"} {
		if strings.HasSuffix(fileName, ext) {
			return strings.TrimSuffix(fileName, ext)
		}
	}
	return fileName
}

// scanTokenGroups counts leaf tokens per top-level group across every
// design/tokens/*.tokens.json file (SP-140-2 §2c). Files are read in glob
// order; the result is sorted by group name. A missing tokens directory
// yields an empty slice.
func scanTokenGroups(root string) ([]TokenGroupCount, error) {
	matches, err := filepath.Glob(filepath.Join(root, DirName, "tokens", "*.tokens.json"))
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			return nil, err
		}
		incTokenGroupCounts(data, counts)
	}

	groups := make([]string, 0, len(counts))
	for g := range counts {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	out := make([]TokenGroupCount, 0, len(groups))
	for _, g := range groups {
		out = append(out, TokenGroupCount{Group: g, Tokens: counts[g]})
	}
	return out, nil
}

// incTokenGroupCounts parses one token document and adds its leaf-token
// counts into counts, keyed by the group's top-level name (the first path
// segment). A file that does not parse contributes nothing — its failure is
// reported by the validator, not the inventory. Only structurally valid
// leaves count; a malformed subtree is skipped rather than miscounted.
//
// Tokens are split one file per tier and consumers glob, so a top-level group
// name (e.g. "color") is deliberately summed across files: the same group in
// color.tokens.json and a project tier is one logical group. A JSON object
// cannot carry duplicate top-level keys, so the only merge is the intended
// cross-file sum.
func incTokenGroupCounts(content []byte, counts map[string]int) {
	root, _ := parseTokensDocument(content)
	if root == nil {
		return
	}
	var walk func(n *tokenNode)
	walk = func(n *tokenNode) {
		if n.leaf && !n.nonObject {
			if group := topGroup(n.path); group != "" {
				counts[group]++
			}
			return
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(root)
}

// topGroup returns the first dot-path segment of a token path.
func topGroup(p string) string {
	if i := strings.IndexByte(p, '.'); i >= 0 {
		return p[:i]
	}
	return p
}

// scanFlows collects node/edge counts per flow source document
// (design/flows/*.json, the .layout.json sidecars excluded, SP-140-9 §9b),
// sorted by path. A legacy .mmd with no .json source still counts (it is the
// only flow truth in a pre-migration tree); a source's derived .mmd does not
// double-count. A missing flows directory yields an empty slice.
func scanFlows(root string) ([]FlowCounts, error) {
	jsonMatches, err := filepath.Glob(filepath.Join(root, DirName, "flows", "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(jsonMatches)
	out := make([]FlowCounts, 0, len(jsonMatches))
	hasSource := map[string]bool{}
	for _, match := range jsonMatches {
		name := path.Base(filepath.ToSlash(match))
		if strings.HasSuffix(name, layoutSidecarSuffix) {
			continue
		}
		data, err := os.ReadFile(match)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(root, match)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		flowName := strings.TrimSuffix(name, ".json")
		hasSource[flowName] = true
		if src, parseErr := ParseFlowSource(rel, data); parseErr == nil {
			byID := map[string]bool{}
			for _, s := range src.Steps {
				byID[s.ID] = true
			}
			edgeCount := 0
			for _, s := range src.Steps {
				if s.Next != "" && byID[s.Next] {
					edgeCount++
				}
			}
			out = append(out, FlowCounts{Path: rel, Name: flowName, Nodes: len(src.Steps), Edges: edgeCount})
		} else {
			out = append(out, FlowCounts{Path: rel, Name: flowName, Nodes: 0, Edges: 0})
		}
	}
	mmdMatches, err := filepath.Glob(filepath.Join(root, DirName, "flows", "*.mmd"))
	if err != nil {
		return nil, err
	}
	sort.Strings(mmdMatches)
	for _, match := range mmdMatches {
		flowName := strings.TrimSuffix(path.Base(filepath.ToSlash(match)), ".mmd")
		if hasSource[flowName] {
			continue
		}
		data, err := os.ReadFile(match)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(root, match)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		fc := ParseFlowchart(string(data))
		out = append(out, FlowCounts{
			Path:  rel,
			Name:  flowName,
			Nodes: len(fc.NodeOrder),
			Edges: len(fc.Edges),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
