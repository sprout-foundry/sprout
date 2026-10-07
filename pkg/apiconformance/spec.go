// Package apiconformance is the conformance suite for the versioned backend
// contract in docs/api/openapi.yaml. Given a base URL it probes the live
// implementation, checks status codes and response shapes, and reports per
// family. It is a self-contained, dependency-light client: it parses the
// committed OpenAPI document, sends read-only HTTP requests, and validates
// response bodies against the schemas the document carries.
//
// A Suite is configured with a base URL and a Spec (the parsed document) and
// carries a set of Probes — one per operation the suite wants to check. The
// default probe set (DefaultProbes) is a curated table of safe, read-only GET
// operations that return a usable response on a fresh, disposable workspace.
// Mutating operations are never probed by the safe suite: RunMutating only
// runs when an explicitly disposable workspace root has been set
// (WithDisposableWorkspace), which is the gate that keeps a conformance run
// from writing to a real workspace.
//
// Schema validation is done with github.com/santhosh-tekuri/jsonschema/v6,
// chosen over Huma's own schema tooling on purpose. The generated document is
// a standalone OpenAPI file that is compiled directly and validated against an
// arbitrary response body — exactly what that library does (it compiles a JSON
// Schema and resolves $ref against a registered document). Huma's Registry and
// Validate are coupled to its registered-operation types and cannot validate a
// standalone document's schemas in isolation, which would force the suite to
// pull in the operation registry instead of the file. The santhosh-tekuri
// library is MIT-licensed, maintained, and already a repo dependency (see
// validate.go for the compile/resolve helper).
package apiconformance

import (
	"fmt"
	"os"
	"sort"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Spec is the parsed in-memory form of the generated OpenAPI contract. It
// carries the metadata the suite needs (version and the family list), the flat
// list of operations, and the raw components section so a validator can
// resolve #/components/... references against the document.
type Spec struct {
	// Info holds the top-level document metadata (title and the contract
	// version).
	Info Info

	// Tags is the declared tag list (one entry per family). A family declared
	// but not yet used by any operation still appears here.
	Tags []string

	// Operations is every operation in the document, in file order.
	Operations []Operation

	// ComponentsSchemas is the raw components.schemas map. It is kept generic
	// (map[string]any) so a validator can hand it to a JSON-Schema compiler
	// and resolve $ref against the whole document.
	ComponentsSchemas map[string]any
}

// Info is the top-level document metadata.
type Info struct {
	// Title is the document title (info.title).
	Title string
	// Version is the contract version (info.version), the value the version
	// negotiation reports.
	Version string
}

// Operation is one HTTP operation (a single verb under a path) in the
// document. The fields the suite consumes are captured; the rest of the
// document stays in the generic tree.
type Operation struct {
	// OperationID is the operationId (e.g. "get-config").
	OperationID string

	// Method is the HTTP verb in lowercase (e.g. "get").
	Method string

	// Path is the path template as written in the document (e.g.
	// "/api/sessions/{id}/export").
	Path string

	// Summary is the human-readable summary.
	Summary string

	// Tags is the operation's family tags, in document order. The first entry
	// is the primary family a probe is grouped under.
	Tags []string

	// OKStatusCode is the documented 2xx status (default 200 when the document
	// lists a 200 response).
	OKStatusCode int

	// HasOKSchema is true when the documented 200 response carries a concrete
	// response schema (not just a description). Only a handful of operations
	// have one; the rest are shape-checked as "valid JSON".
	HasOKSchema bool

	// OKSchemaRef is the $ref (if any) of the 200 response schema, e.g.
	// "#/components/schemas/ErrorModel". Empty when the 200 schema is inline.
	OKSchemaRef string

	// OKSchemaSchema is the inline 200 response schema (the schema object
	// under responses."200".content.<mime>.schema), or nil when the response
	// has no concrete schema or the schema is a bare $ref.
	OKSchemaSchema map[string]any
}

// ContractVersion returns the contract version (info.version).
func (s *Spec) ContractVersion() string {
	return s.Info.Version
}

// Families returns the distinct family names: the union of the declared tag
// list and every tag an operation actually uses, sorted for stable output. A
// family may be declared with no operations yet, so the declared list is the
// source of truth; operation tags are added only if they are new.
func (s *Spec) Families() []string {
	seen := make(map[string]struct{}, len(s.Tags))
	for _, t := range s.Tags {
		seen[t] = struct{}{}
	}
	for _, op := range s.Operations {
		for _, t := range op.Tags {
			seen[t] = struct{}{}
		}
	}
	fams := make([]string, 0, len(seen))
	for f := range seen {
		fams = append(fams, f)
	}
	sort.Strings(fams)
	return fams
}

// LoadSpec parses the OpenAPI YAML at path into a Spec. The document is a
// generated OpenAPI 3.x file; only the fields the suite consumes are
// materialized as structs, the rest are kept in the generic tree.
func LoadSpec(path string) (*Spec, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is a caller-provided contract file
	if err != nil {
		return nil, fmt.Errorf("read openapi spec: %w", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse openapi spec %s: %w", path, err)
	}
	if doc == nil {
		return nil, fmt.Errorf("parse openapi spec %s: empty document", path)
	}
	return parseSpec(doc, path)
}

// parseSpec materializes the Spec from a parsed OpenAPI document. Split out of
// LoadSpec so the loader's file reading and the document parsing can be tested
// independently.
func parseSpec(doc map[string]any, path string) (*Spec, error) {
	spec := &Spec{ComponentsSchemas: map[string]any{}}

	infoM, _ := doc["info"].(map[string]any)
	if infoM == nil {
		return nil, fmt.Errorf("openapi spec %s: missing or malformed info section", path)
	}
	if v, ok := infoM["version"].(string); ok {
		spec.Info.Version = v
	}
	if t, ok := infoM["title"].(string); ok {
		spec.Info.Title = t
	}

	if tags, ok := doc["tags"].([]any); ok {
		for _, e := range tags {
			if m, ok := e.(map[string]any); ok {
				if name, ok := m["name"].(string); ok {
					spec.Tags = append(spec.Tags, name)
				}
			}
		}
	}

	if comps, ok := doc["components"].(map[string]any); ok {
		if schemas, ok := comps["schemas"].(map[string]any); ok {
			spec.ComponentsSchemas = schemas
		}
	}

	paths, _ := doc["paths"].(map[string]any)
	for _, p := range sortedKeys(paths) {
		item, _ := paths[p].(map[string]any)
		if item == nil {
			continue
		}
		for _, verb := range httpVerbs {
			block, ok := item[verb].(map[string]any)
			if !ok {
				continue
			}
			op := parseOperation(verb, p, block)
			spec.Operations = append(spec.Operations, op)
		}
	}

	return spec, nil
}

// parseOperation extracts the suite-consumed fields from a single verb block.
func parseOperation(verb, path string, block map[string]any) Operation {
	op := Operation{Method: verb, Path: path}
	if id, ok := block["operationId"].(string); ok {
		op.OperationID = id
	}
	if summary, ok := block["summary"].(string); ok {
		op.Summary = summary
	}
	if tags, ok := block["tags"].([]any); ok {
		for _, e := range tags {
			if name, ok := e.(string); ok {
				op.Tags = append(op.Tags, name)
			}
		}
	}

	responses, _ := block["responses"].(map[string]any)
	op.OKStatusCode, op.HasOKSchema, op.OKSchemaRef, op.OKSchemaSchema =
		inspectOKResponse(responses)
	return op
}

// inspectOKResponse inspects the responses map and returns, in order: the
// smallest documented 2xx status (default 200), whether the 200 response
// carries a concrete schema, that schema's $ref (if it is one), and the inline
// schema (if it is inline). A 200 response with only a description yields
// hasSchema=false and a nil inline schema.
func inspectOKResponse(responses map[string]any) (code int, hasSchema bool, ref string, inline map[string]any) {
	code = 200
	for k := range responses {
		if n, err := strconv.Atoi(k); err == nil && n >= 200 && n < 300 && (code == 200 || n < code) {
			code = n
		}
	}

	schema := okSchemaOf(responses, 200)
	if schema != nil {
		hasSchema = true
		if r, ok := schema["$ref"].(string); ok {
			ref = r
		} else {
			inline = schema
		}
	}
	return code, hasSchema, ref, inline
}

// okSchemaOf returns the 200 response's content.<mime>.schema map, or nil when
// the 200 response has no concrete schema (e.g. a description-only 200, or a
// response that documents only a problem+json default error).
func okSchemaOf(responses map[string]any, code int) map[string]any {
	resp, _ := responses[strconv.Itoa(code)].(map[string]any)
	if resp == nil {
		return nil
	}
	content, _ := resp["content"].(map[string]any)
	if content == nil {
		return nil
	}
	// The document uses a single media type per response (application/json for
	// the 200, application/problem+json for the default). Return the first
	// content entry's schema.
	for k := range content {
		body, _ := content[k].(map[string]any)
		if body == nil {
			continue
		}
		if schema, ok := body["schema"].(map[string]any); ok {
			return schema
		}
	}
	return nil
}

// sortedKeys returns the string keys of m in sorted order (deterministic
// operation order).
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// httpVerbs are the OpenAPI method keywords, in the order the suite records
// operations (read methods first). A verb not present under a path is skipped.
var httpVerbs = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}
