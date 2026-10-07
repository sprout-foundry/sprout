package apiconformance

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// defaultTimeout bounds each probe request so a hung implementation cannot stall
// a conformance run.
const defaultTimeout = 10 * time.Second

// errMutatingRequiresDisposable is returned by RunMutating when no explicitly
// disposable workspace has been set. It is the gate that keeps a conformance run
// from writing to a real workspace: mutating checks are only ever run against a
// workspace the caller declared disposable.
var errMutatingRequiresDisposable = errors.New(
	"apiconformance: RunMutating requires an explicitly disposable workspace; set one with WithDisposableWorkspace")

// Option configures a Suite. Options are applied in order by New.
type Option func(*Suite)

// WithClient overrides the HTTP client used to send probes. A nil client is
// ignored (the default client with the suite's timeout is kept).
func WithClient(c *http.Client) Option {
	return func(s *Suite) {
		if c != nil {
			s.client = c
		}
	}
}

// WithProbes replaces the default probe set. An explicitly empty set is honored
// (the suite then probes nothing); only when this option is not used at all does
// New fall back to DefaultProbes.
func WithProbes(probes []Probe) Option {
	return func(s *Suite) {
		s.probes = probes
		s.probesSet = true
	}
}

// WithHTTPHeader adds a single named header to every probe request. Multiple
// calls accumulate.
func WithHTTPHeader(name, value string) Option {
	return func(s *Suite) {
		s.header.Add(name, value)
	}
}

// WithDisposableWorkspace marks a workspace root as disposable, enabling
// RunMutating. A conformance run only mutates state inside a workspace the
// caller has explicitly declared disposable.
func WithDisposableWorkspace(root string) Option {
	return func(s *Suite) {
		s.disposableRoot = root
	}
}

// Suite is a conformance run against a base URL. It carries the loaded Spec, the
// probe set to send, and the transport/shape-checking configuration. Build one
// with New and run it with RunSafe (read-only) or RunMutating (gated).
type Suite struct {
	base           string
	spec           *Spec
	client         *http.Client
	probes         []Probe
	probesSet      bool
	header         http.Header
	disposableRoot string
	validator      *Validator
	initErr        error
}

// New builds a Suite against baseURL from a loaded Spec, applying opts in order.
// New never fails on spec content: if the spec's schemas cannot be compiled into
// a validator, the error is surfaced by RunSafe/RunMutating rather than here, so
// the constructor stays a simple value factory. When opts does not set the probe
// set, it defaults to DefaultProbes.
func New(baseURL string, spec *Spec, opts ...Option) *Suite {
	s := &Suite{
		base:   strings.TrimRight(baseURL, "/"),
		spec:   spec,
		client: &http.Client{Timeout: defaultTimeout},
		header: http.Header{},
	}
	for _, o := range opts {
		o(s)
	}
	if !s.probesSet {
		s.probes = DefaultProbes()
	}
	if spec != nil {
		v, err := NewValidator(spec)
		if err != nil {
			s.initErr = err
			return s
		}
		s.validator = v
	}
	return s
}

// Probes returns the probe set the suite will send (the configured set, or the
// default set when none was configured).
func (s *Suite) Probes() []Probe {
	return s.probes
}

// WithProbes replaces the suite's probe set and returns the suite for chaining.
// It is the imperative form of the WithProbes option, for callers that build a
// suite first and adjust it later (a later consumer overrides/extends the set
// this way).
func (s *Suite) WithProbes(probes []Probe) *Suite {
	s.probes = probes
	s.probesSet = true
	return s
}

// RunSafe sends every probe in the suite's set to the base URL and returns a
// per-family report. A probe passes when the response is a 2xx and its body
// shape-checks against the operation's 200 schema (or, when the operation has no
// concrete 200 schema, when the body is valid JSON). It returns an error only if
// the suite could not be initialized (a spec whose schemas do not compile).
func (s *Suite) RunSafe(ctx context.Context) (*Report, error) {
	if s.initErr != nil {
		return nil, s.initErr
	}
	results := make([]Result, 0, len(s.probes))
	for _, p := range s.probes {
		results = append(results, s.probe(ctx, p))
	}
	return NewReport(results), nil
}

// RunMutating runs the mutating checks, but only when a disposable workspace was
// explicitly set (WithDisposableWorkspace); otherwise it returns
// errMutatingRequiresDisposable. The mutating battery is deliberately a
// documented no-op for now: the gate (not a full mutation set) is what this
// package delivers, and a later consumer supplies the mutating probes. When the
// gate is cleared with no mutating probes, the result is an empty, all-passed
// report.
func (s *Suite) RunMutating(ctx context.Context) (*Report, error) {
	if s.disposableRoot == "" {
		return nil, errMutatingRequiresDisposable
	}
	if s.initErr != nil {
		return nil, s.initErr
	}
	// No mutating probes are registered yet; run them when a later consumer adds a
	// mutating set. The gate above is the deliverable for this package.
	return NewReport(nil), nil
}

// probe sends one probe and classifies its outcome. It never returns an error:
// transport failures are recorded as a failed Result (status 0) so a run always
// produces a report.
func (s *Suite) probe(ctx context.Context, p Probe) Result {
	req, err := http.NewRequestWithContext(ctx, p.Method, s.requestURL(p), nil)
	if err != nil {
		return Result{Probe: p, Status: 0, Reason: "build request: " + err.Error()}
	}
	s.applyHeader(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return Result{Probe: p, Status: 0, Reason: "request: " + err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{Probe: p, Status: resp.StatusCode, Reason: "read response body: " + err.Error()}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{
			Probe:  p,
			Status: resp.StatusCode,
			Reason: "status " + strconv.Itoa(resp.StatusCode) + ", want 2xx",
		}
	}

	schema := s.validator.SchemaFor(p.OperationID)
	ok, reason := ValidateBody(schema, body)
	if !ok {
		return Result{Probe: p, Status: resp.StatusCode, Reason: "body shape: " + reason}
	}
	return Result{Probe: p, Status: resp.StatusCode, Passed: true}
}

// requestURL builds the full request URL for a probe: base + path + query.
func (s *Suite) requestURL(p Probe) string {
	u := s.base + p.Path
	if len(p.Query) > 0 {
		q := url.Values{}
		for k, v := range p.Query {
			q.Set(k, v)
		}
		u += "?" + q.Encode()
	}
	return u
}

// applyHeader copies the suite's configured headers onto the request.
func (s *Suite) applyHeader(req *http.Request) {
	for k, vs := range s.header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
}
