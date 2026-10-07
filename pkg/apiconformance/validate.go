package apiconformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// specBaseURI is the stable base identifier the wrapper document is registered
// under. It uses a custom scheme so reference resolution never touches the
// filesystem (the resource is provided in memory via AddResource), which keeps
// the validator independent of the working directory.
const specBaseURI = "apiconformance:openapi"

// schemaResource is the top-level key under the wrapper document that holds the
// per-operation 200 schemas, alongside the shared components. It is an unknown
// keyword to the JSON-Schema compiler, so the compiler ignores it while still
// resolving #/components/... references that live in the same resource.
const schemaResource = "conformance"

// errModelKey is the wrapper-document key that aliases the shared ErrorModel
// component.
const errModelKey = "errorModel"

// errModelRef is the OpenAPI $ref to the shared error model.
const errModelRef = "#/components/schemas/ErrorModel"

// Validator compiles the response schemas of a Spec into JSON-Schema objects
// and validates decoded response bodies against them. It wraps the document's
// components.schemas in a single resource (so $ref resolves in-document) and
// adds a per-operation entry for every operation that carries a concrete 200
// schema.
//
// The choice of validator (santhosh-tekuri/jsonschema/v6 over Huma's own
// tooling) is recorded in the package doc; see spec.go's package comment.
type Validator struct {
	// c is the compiler, with the wrapper resource already added.
	c *jsonschema.Compiler

	// compiled maps an operationId (or errModelKey) to its compiled 200 schema.
	compiled map[string]*jsonschema.Schema
}

// NewValidator builds a Validator from a Spec. It registers a wrapper document
// (components + a per-operation schema map) as a single resource and pre-compiles
// each operation's 200 schema, the shared ErrorModel, and the inline schemas.
// Operations with no concrete 200 schema get no entry; their bodies are
// checked as "valid JSON" by the caller. A malformed document (one whose
// components do not form a valid JSON-Schema resource) is reported as an error.
func NewValidator(spec *Spec) (*Validator, error) {
	c := jsonschema.NewCompiler()

	wrapper := buildWrapperDoc(spec)
	if err := c.AddResource(specBaseURI, wrapper); err != nil {
		return nil, fmt.Errorf("register schema resource: %w", err)
	}

	v := &Validator{c: c, compiled: map[string]*jsonschema.Schema{}}

	// Compile the shared error model (used to check default error responses).
	if len(spec.ComponentsSchemas) > 0 {
		sch, err := c.Compile(specBaseURI + "#/" + schemaResource + "/" + errModelKey)
		if err != nil {
			return nil, fmt.Errorf("compile error model: %w", err)
		}
		v.compiled[errModelKey] = sch
	}

	// Compile each operation's 200 schema.
	for _, op := range spec.Operations {
		if !op.HasOKSchema || op.OperationID == "" {
			continue
		}
		sch, err := c.Compile(specBaseURI + "#/" + schemaResource + "/" + op.OperationID)
		if err != nil {
			return nil, fmt.Errorf("compile %s 200 schema: %w", op.OperationID, err)
		}
		v.compiled[op.OperationID] = sch
	}

	return v, nil
}

// buildWrapperDoc assembles the in-memory wrapper document: the document's
// components plus a per-operation schema map. The $schema pins the draft so the
// compiler does not guess; the wrapper's extra top-level keys are ignored by the
// compiler but keep the document a well-formed, self-contained resource.
func buildWrapperDoc(spec *Spec) map[string]any {
	schemas := spec.ComponentsSchemas
	if schemas == nil {
		schemas = map[string]any{}
	}
	conformance := map[string]any{}
	if len(schemas) > 0 {
		conformance[errModelKey] = map[string]any{"$ref": errModelRef}
	}
	for _, op := range spec.Operations {
		if !op.HasOKSchema || op.OperationID == "" {
			continue
		}
		if op.OKSchemaRef != "" {
			// A $ref-based 200 schema: alias it through the wrapper so the
			// reference resolves against the shared components.
			conformance[op.OperationID] = map[string]any{"$ref": op.OKSchemaRef}
		} else {
			// An inline 200 schema: embed it directly (it already resolves any
			// in-document $ref against the same resource).
			conformance[op.OperationID] = op.OKSchemaSchema
		}
	}

	doc := map[string]any{
		"$schema":      "https://json-schema.org/draft/2020-12/schema",
		"components":   map[string]any{"schemas": schemas},
		schemaResource: conformance,
	}
	return doc
}

// SchemaFor returns the compiled 200 schema for the operation with the given id
// (nil when the operation has no concrete 200 schema, in which case a body is
// shape-checked as "valid JSON").
func (v *Validator) SchemaFor(operationID string) *jsonschema.Schema {
	if v == nil {
		return nil
	}
	return v.compiled[operationID]
}

// ErrModel returns the compiled shared error model (nil when the document
// defines no components.schemas).
func (v *Validator) ErrModel() *jsonschema.Schema {
	if v == nil {
		return nil
	}
	return v.compiled[errModelKey]
}

// ValidateBody decodes the raw body as JSON and validates the value against
// schema. When schema is nil it only checks that the body is valid JSON (any
// value passes), which is the shape check for operations without a concrete 200
// schema. It returns (ok, reason); reason is empty on success and a short
// human-readable explanation of the first failure otherwise.
func ValidateBody(schema *jsonschema.Schema, body []byte) (bool, string) {
	var value any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return false, fmt.Sprintf("response body is not valid JSON: %v", err)
	}
	if schema == nil {
		return true, ""
	}
	if err := schema.Validate(value); err != nil {
		return false, firstValidationLine(err.Error())
	}
	return true, ""
}

// firstValidationLine reduces a multi-line jsonschema error to the root-cause
// line so a report reason stays a single line. The library prefixes the error
// with the schema location; the first "- at ..." line carries the actual failure
// (e.g. "at ”: got array, want object"), which is what the reader needs.
func firstValidationLine(err string) string {
	for _, line := range strings.Split(err, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "- at") {
			return strings.TrimPrefix(t, "- ")
		}
	}
	return strings.TrimSpace(err)
}
