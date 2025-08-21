package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func fileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}

// orderEditsByImportGraph tries to topologically order edits by simple import/use relationships
// across common ecosystems (TS/JS, Python). If it cannot infer any edges, returns nil to signal fallback.
func orderEditsByImportGraph(ops []EditOperation) []EditOperation {
	if len(ops) <= 1 {
		return nil
	}

	// Preload contents and quick metadata
	type node struct {
		idx     int
		path    string
		dir     string
		base    string
		ext     string
		content string
	}
	nodes := make([]node, 0, len(ops))
	for i, op := range ops {
		b, _ := os.ReadFile(op.FilePath)
		dir := filepath.Dir(op.FilePath)
		base := strings.TrimSuffix(filepath.Base(op.FilePath), filepath.Ext(op.FilePath))
		ext := strings.ToLower(filepath.Ext(op.FilePath))
		nodes = append(nodes, node{idx: i, path: op.FilePath, dir: dir, base: base, ext: ext, content: string(b)})
	}

	// Build edges: importer -> imported (imported should come first)
	edges := make(map[int]map[int]bool)
	indeg := make(map[int]int)
	haveEdges := false

	// Regexes for TS/JS imports
	reFrom := regexp.MustCompile(`(?m)from\s+['\"]([^'\"]+)['\"]`)
	reReq := regexp.MustCompile(`(?m)require\(\s*['\"]([^'\"]+)['\"]\s*\)`)
	// Python imports
	rePyFrom := regexp.MustCompile(`(?m)^\s*from\s+([\w\.]+)\s+import\s+`)
	rePyImp := regexp.MustCompile(`(?m)^\s*import\s+([\w\.]+)`)
	// Java/Kotlin/Scala/C#/Swift
	reImpGeneric := regexp.MustCompile(`(?m)^\s*(import|using)\s+([A-Za-z0-9_\.]+)`)
	// PHP requires
	rePhpReq := regexp.MustCompile(`(?m)(require|include|require_once|include_once)\s*\(\s*['\"]([^'\"]+)['\"]\s*\)`)
	// Ruby requires
	reRbReqRel := regexp.MustCompile(`(?m)^\s*require_relative\s+['\"]([^'\"]+)['\"]`)
	// Rust modules
	reRsMod := regexp.MustCompile(`(?m)^\s*mod\s+([A-Za-z0-9_]+)\s*;`)

	resolveTS := func(n node, spec string) string {
		// only handle relative paths like ./util or ../lib/util
		if !strings.HasPrefix(spec, ".") {
			return ""
		}
		p := filepath.Clean(filepath.Join(n.dir, spec))
		// try common extensions
		candidates := []string{p + ".ts", p + ".tsx", p + ".js", p + ".jsx", filepath.Join(p, "index.ts"), filepath.Join(p, "index.tsx"), filepath.Join(p, "index.js"), filepath.Join(p, "index.jsx")}
		for _, c := range candidates {
			if fileExists(c) {
				return filepath.Clean(c)
			}
		}
		return ""
	}

	resolvePy := func(n node, mod string) string {
		// map module name to file in same dir when simple
		parts := strings.Split(mod, ".")
		name := parts[len(parts)-1]
		candidates := []string{filepath.Join(n.dir, name+".py"), filepath.Join(n.dir, name, "__init__.py")}
		for _, c := range candidates {
			if fileExists(c) {
				return filepath.Clean(c)
			}
		}
		return ""
	}

	resolvePHP := func(n node, spec string) string {
		if !strings.HasPrefix(spec, ".") {
			return ""
		}
		p := filepath.Clean(filepath.Join(n.dir, spec))
		if fileExists(p) {
			return filepath.Clean(p)
		}
		if fileExists(p + ".php") {
			return filepath.Clean(p + ".php")
		}
		if fileExists(filepath.Join(p, "index.php")) {
			return filepath.Clean(filepath.Join(p, "index.php"))
		}
		return ""
	}

	resolveRB := func(n node, spec string) string {
		if !strings.HasPrefix(spec, ".") && !strings.HasPrefix(spec, "/") {
			spec = "./" + spec
		}
		p := filepath.Clean(filepath.Join(n.dir, spec))
		if fileExists(p + ".rb") {
			return filepath.Clean(p + ".rb")
		}
		if fileExists(p) {
			return filepath.Clean(p)
		}
		return ""
	}

	for i, a := range nodes {
		// TS/JS
		if a.ext == ".ts" || a.ext == ".tsx" || a.ext == ".js" || a.ext == ".jsx" {
			specs := append(reFrom.FindAllStringSubmatch(a.content, -1), reReq.FindAllStringSubmatch(a.content, -1)...)
			for _, m := range specs {
				if len(m) < 2 {
					continue
				}
				if target := resolveTS(a, m[1]); target != "" {
					for j, b := range nodes {
						if filepath.Clean(b.path) == target {
							if edges[i] == nil {
								edges[i] = make(map[int]bool)
							}
							if !edges[i][j] {
								edges[i][j] = true
								indeg[j]++
								haveEdges = true
							}
						}
					}
				}
			}
		}
		// Python
		if a.ext == ".py" {
			for _, m := range rePyFrom.FindAllStringSubmatch(a.content, -1) {
				if len(m) >= 2 {
					if target := resolvePy(a, m[1]); target != "" {
						for j, b := range nodes {
							if filepath.Clean(b.path) == target {
								if edges[i] == nil {
									edges[i] = make(map[int]bool)
								}
								if !edges[i][j] {
									edges[i][j] = true
									indeg[j]++
									haveEdges = true
								}
								// PHP
								if a.ext == ".php" {
									for _, m := range rePhpReq.FindAllStringSubmatch(a.content, -1) {
										if len(m) >= 3 {
											if target := resolvePHP(a, m[2]); target != "" {
												for j, b := range nodes {
													if filepath.Clean(b.path) == target {
														if edges[i] == nil {
															edges[i] = make(map[int]bool)
														}
														if !edges[i][j] {
															edges[i][j] = true
															indeg[j]++
															haveEdges = true
														}
													}
												}
											}
										}
									}
								}
								// Ruby
								if a.ext == ".rb" {
									for _, m := range reRbReqRel.FindAllStringSubmatch(a.content, -1) {
										if len(m) >= 2 {
											if target := resolveRB(a, m[1]); target != "" {
												for j, b := range nodes {
													if filepath.Clean(b.path) == target {
														if edges[i] == nil {
															edges[i] = make(map[int]bool)
														}
														if !edges[i][j] {
															edges[i][j] = true
															indeg[j]++
															haveEdges = true
														}
													}
												}
											}
										}
									}
								}
								// Rust
								if a.ext == ".rs" {
									for _, m := range reRsMod.FindAllStringSubmatch(a.content, -1) {
										if len(m) >= 2 {
											name := m[1]
											candidates := []string{filepath.Join(a.dir, name+".rs"), filepath.Join(a.dir, name, "mod.rs")}
											for _, c := range candidates {
												if fileExists(c) {
													for j, b := range nodes {
														if filepath.Clean(b.path) == filepath.Clean(c) {
															if edges[i] == nil {
																edges[i] = make(map[int]bool)
															}
															if !edges[i][j] {
																edges[i][j] = true
																indeg[j]++
																haveEdges = true
															}
														}
													}
												}
											}
										}
									}
								}
								// Java/Kotlin/Scala/C#/Swift
								if a.ext == ".java" || a.ext == ".kt" || a.ext == ".scala" || a.ext == ".cs" || a.ext == ".swift" {
									for _, m := range reImpGeneric.FindAllStringSubmatch(a.content, -1) {
										if len(m) >= 3 {
											parts := strings.Split(m[2], ".")
											base := parts[len(parts)-1]
											for j, b := range nodes {
												if strings.EqualFold(b.base, base) {
													if edges[i] == nil {
														edges[i] = make(map[int]bool)
													}
													if !edges[i][j] {
														edges[i][j] = true
														indeg[j]++
														haveEdges = true
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
			for _, m := range rePyImp.FindAllStringSubmatch(a.content, -1) {
				if len(m) >= 2 {
					if target := resolvePy(a, m[1]); target != "" {
						for j, b := range nodes {
							if filepath.Clean(b.path) == target {
								if edges[i] == nil {
									edges[i] = make(map[int]bool)
								}
								if !edges[i][j] {
									edges[i][j] = true
									indeg[j]++
									haveEdges = true
								}
							}
						}
					}
				}
			}
		}
	}

	if !haveEdges {
		return nil
	}

	// Kahn's algorithm
	var queue []int
	for i := range nodes {
		if indeg[i] == 0 {
			queue = append(queue, i)
		}
	}
	var orderIdx []int
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		orderIdx = append(orderIdx, u)
		for v := range edges[u] {
			indeg[v]--
			if indeg[v] == 0 {
				queue = append(queue, v)
			}
		}
	}
	if len(orderIdx) != len(nodes) {
		// cycle or unresolved; decline to override
		return nil
	}
	// Map to ops order
	out := make([]EditOperation, 0, len(ops))
	for _, i := range orderIdx {
		out = append(out, ops[nodes[i].idx])
	}
	return out
}
