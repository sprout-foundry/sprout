package modelcontract

import "testing"

func approx(a, b float64) bool {
	d := a - b
	return d < 1e-6 && d > -1e-6
}

func find(models []CanonicalModel, id string) (CanonicalModel, bool) {
	for _, m := range models {
		if m.ID == id {
			return m, true
		}
	}
	return CanonicalModel{}, false
}

func TestParseDeepInfra(t *testing.T) {
	body := []byte(`[
	  {"model_name":"Qwen/Qwen3-Max","reported_type":"text-generation","max_tokens":256000,
	   "tags":["tools","reasoning","json","multimodal"],
	   "pricing":{"type":"tokens","cents_per_input_token":0.00012,"cents_per_output_token":0.0006}},
	  {"model_name":"meta/no-tools-8k","reported_type":"text-generation","max_tokens":8000,
	   "tags":["json"],"pricing":{"type":"tokens","cents_per_input_token":0.00001,"cents_per_output_token":0.00002}},
	  {"model_name":"cached/model","reported_type":"text-generation","max_tokens":128000,
	   "tags":["tools"],"pricing":{"type":"tokens","cents_per_input_token":0.0001,"cents_per_output_token":0.0005,"cents_per_cached_input_token":0.00001}},
	  {"model_name":"old/model","reported_type":"text-generation","deprecated":1693526400,"max_tokens":128000},
	  {"model_name":"black-forest/FLUX","reported_type":"text-to-image","max_tokens":null}
	]`)

	models, err := parseDeepInfra(body)
	if err != nil {
		t.Fatal(err)
	}
	// Image + deprecated models are filtered out.
	if len(models) != 3 {
		t.Fatalf("expected 3 models, got %d: %+v", len(models), models)
	}

	qwen, ok := find(models, "Qwen/Qwen3-Max")
	if !ok {
		t.Fatal("Qwen3-Max missing")
	}
	if qwen.ContextWindow != 256000 {
		t.Errorf("context: got %d", qwen.ContextWindow)
	}
	if !IsTrue(qwen.Capabilities.Tools) || !IsTrue(qwen.Capabilities.Vision) || !IsTrue(qwen.Capabilities.Reasoning) {
		t.Errorf("capabilities not set: %+v", qwen.Capabilities)
	}
	if qwen.Pricing == nil || !approx(qwen.Pricing.InputPerMTok, 1.2) || !approx(qwen.Pricing.OutputPerMTok, 6.0) {
		t.Errorf("pricing: %+v", qwen.Pricing)
	}

	// A model without the "tools" tag is known-false (not unknown).
	noTools, _ := find(models, "meta/no-tools-8k")
	if !IsKnownFalse(noTools.Capabilities.Tools) {
		t.Errorf("expected tools known-false, got %v", noTools.Capabilities.Tools)
	}

	// Model with cached-input pricing → verify cents_per_cached_input_token is
	// extracted and converted to per-million correctly.
	cached, _ := find(models, "cached/model")
	if cached.Pricing == nil {
		t.Fatal("cached/model has no pricing")
	}
	if !approx(cached.Pricing.CachedPerMTok, 0.1) {
		t.Errorf("cached price: got %v, want 0.1 (0.00001 cents/token * 1e4)", cached.Pricing.CachedPerMTok)
	}
}

func TestParseOpenRouter(t *testing.T) {
	body := []byte(`{"data":[
	  {"id":"openai/gpt-x","name":"GPT-X","context_length":200000,
	   "architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},
	   "pricing":{"prompt":"0.000002","completion":"0.000008"},
	   "top_provider":{"max_completion_tokens":16384},
	   "supported_parameters":["tools","tool_choice","structured_outputs","reasoning"]},
	  {"id":"x/free-small","context_length":16000,
	   "pricing":{"prompt":"0","completion":"0"},
	   "supported_parameters":["response_format"]}
	]}`)

	models, err := parseOpenRouter(body)
	if err != nil {
		t.Fatal(err)
	}
	gptx, ok := find(models, "openai/gpt-x")
	if !ok {
		t.Fatal("gpt-x missing")
	}
	if gptx.ContextWindow != 200000 || gptx.MaxOutputTokens != 16384 {
		t.Errorf("limits: ctx=%d maxout=%d", gptx.ContextWindow, gptx.MaxOutputTokens)
	}
	if !IsTrue(gptx.Capabilities.Tools) || !IsTrue(gptx.Capabilities.Vision) ||
		!IsTrue(gptx.Capabilities.StructuredOutput) || !IsTrue(gptx.Capabilities.Reasoning) {
		t.Errorf("caps: %+v", gptx.Capabilities)
	}
	if gptx.Pricing == nil || !approx(gptx.Pricing.InputPerMTok, 2.0) || !approx(gptx.Pricing.OutputPerMTok, 8.0) {
		t.Errorf("pricing: %+v", gptx.Pricing)
	}

	free, _ := find(models, "x/free-small")
	if free.Pricing == nil || free.Pricing.InputPerMTok != 0 {
		t.Errorf("free pricing should be present-and-zero: %+v", free.Pricing)
	}
	if IsTrue(free.Capabilities.Tools) {
		t.Errorf("free-small should not have tools")
	}
}

func TestEligibility(t *testing.T) {
	cases := []struct {
		name string
		m    CanonicalModel
		want []string
	}{
		{"big+tools", CanonicalModel{ContextWindow: 200000, Capabilities: Capabilities{Tools: Bool(true)}}, []string{RolePrimary, RoleSubagent}},
		{"warn-band+tools", CanonicalModel{ContextWindow: 64000, Capabilities: Capabilities{Tools: Bool(true)}}, []string{RoleSubagent}},
		{"below-floor+tools", CanonicalModel{ContextWindow: 32000, Capabilities: Capabilities{Tools: Bool(true)}}, []string{RoleLowContext}},
		{"big+no-tools", CanonicalModel{ContextWindow: 200000, Capabilities: Capabilities{Tools: Bool(false)}}, nil},
		{"big+unknown-tools", CanonicalModel{ContextWindow: 200000}, []string{RolePrimary, RoleSubagent}},
		{"small", CanonicalModel{ContextWindow: 8000, Capabilities: Capabilities{Tools: Bool(true)}}, nil},
	}
	for _, c := range cases {
		got := ClassifyEligibleRoles(c.m)
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v want %v", c.name, got, c.want)
				break
			}
		}
	}
}

func TestContextWarning(t *testing.T) {
	if w := ContextWarning(200000); w != "" {
		t.Errorf("adequate context should have no warning, got %q", w)
	}
	if w := ContextWarning(128000); w != "" {
		t.Errorf("exactly recommended context should have no warning, got %q", w)
	}
	if ContextWarning(96000) == "" {
		t.Error("64K–128K band should carry a strong warning")
	}
	if w := ContextWarning(32000); w == "" {
		t.Error("SP-125: 16K–64K band should now carry an LCM warning (not empty)")
	}
	if w := ContextWarning(8000); w != "" {
		t.Errorf("hard-blocked context (below 16K) should have no warning, got %q", w)
	}
}

func TestFillEligibleRoles_AttachesContextWarning(t *testing.T) {
	models := []CanonicalModel{
		{ID: "big", ContextWindow: 200000, Capabilities: Capabilities{Tools: Bool(true)}},
		{ID: "warn", ContextWindow: 80000, Capabilities: Capabilities{Tools: Bool(true)}},
		{ID: "low_context", ContextWindow: 32000, Capabilities: Capabilities{Tools: Bool(true)}},
		{ID: "blocked", ContextWindow: 8000, Capabilities: Capabilities{Tools: Bool(true)}},
	}
	FillEligibleRoles(models)
	if len(models[0].Warnings) != 0 {
		t.Errorf("big: expected no warnings, got %v", models[0].Warnings)
	}
	if len(models[1].Warnings) == 0 || len(models[1].EligibleRoles) == 0 {
		t.Errorf("warn: expected subagent role + a warning, got roles=%v warnings=%v", models[1].EligibleRoles, models[1].Warnings)
	}
	// SP-125: 32K is now RoleLowContext (not blocked) with an LCM warning.
	if !RoleHas(models[2].EligibleRoles, RoleLowContext) || len(models[2].Warnings) == 0 {
		t.Errorf("low_context: expected RoleLowContext + a warning, got roles=%v warnings=%v", models[2].EligibleRoles, models[2].Warnings)
	}
	// Below LowContextMinContext (16K) is still a hard block.
	if len(models[3].EligibleRoles) != 0 || len(models[3].Warnings) != 0 {
		t.Errorf("blocked: expected no roles and no warning, got roles=%v warnings=%v", models[3].EligibleRoles, models[3].Warnings)
	}
}

func TestOpenRouterAdapter_CachePricing(t *testing.T) {
	body := []byte(`{"data":[
	  {"id":"anthropic/claude","name":"Claude","context_length":200000,
	   "pricing":{"prompt":"0.000003","completion":"0.000015","input_cache_read":"0.0000003"}},
	  {"id":"plain/uncached","context_length":128000,
	   "pricing":{"prompt":"0.000001","completion":"0.000002"}},
	  {"id":"free/cached","context_length":64000,
	   "pricing":{"prompt":"0","completion":"0","input_cache_read":"0"}}
	]}`)

	models, err := parseOpenRouter(body)
	if err != nil {
		t.Fatal(err)
	}

	// Model with a real cache-read rate → cached price set to per-million value.
	claude, ok := find(models, "anthropic/claude")
	if !ok {
		t.Fatal("anthropic/claude missing")
	}
	if claude.Pricing == nil || !approx(claude.Pricing.CachedPerMTok, 0.3) {
		t.Errorf("claude cached price: got %+v", claude.Pricing)
	}

	// Model without input_cache_read → CachedPerMTok is 0 (field absent).
	uncached, ok := find(models, "plain/uncached")
	if !ok {
		t.Fatal("plain/uncached missing")
	}
	if uncached.Pricing == nil || uncached.Pricing.CachedPerMTok != 0 {
		t.Errorf("uncached model should have 0 CachedPerMTok: %+v", uncached.Pricing)
	}

	// Model with input_cache_read="0" → free cache is valid; CachedPerMTok is 0.
	free, ok := find(models, "free/cached")
	if !ok {
		t.Fatal("free/cached missing")
	}
	if free.Pricing == nil || free.Pricing.CachedPerMTok != 0 {
		t.Errorf("free cached model should have 0 CachedPerMTok: %+v", free.Pricing)
	}
}

func TestParseOpenAIWithReference(t *testing.T) {
	ref := NewReferenceCatalog([]CanonicalModel{{
		ID: "openai/gpt-x", Provider: "openrouter", ContextWindow: 200000,
		Capabilities: Capabilities{Tools: Bool(true), Vision: Bool(true)},
		Pricing:      &Pricing{InputPerMTok: 2, OutputPerMTok: 8, Currency: "USD"},
	}})

	body := []byte(`{"data":[
	  {"id":"gpt-x"},
	  {"id":"o3-mini"},
	  {"id":"text-embedding-3-large"},
	  {"id":"whisper-1"},
	  {"id":"dall-e-3"}
	]}`)

	models := parseOpenAI(body, ref)
	// Only chat models survive the filter.
	if len(models) != 2 {
		t.Fatalf("expected 2 chat models, got %d: %+v", len(models), models)
	}

	gptx, ok := find(models, "gpt-x")
	if !ok {
		t.Fatal("gpt-x missing")
	}
	if gptx.Provider != "openai" {
		t.Errorf("expected provider openai, got %q", gptx.Provider)
	}
	if gptx.ContextWindow != 200000 || !IsTrue(gptx.Capabilities.Tools) || !IsTrue(gptx.Capabilities.Vision) {
		t.Errorf("not enriched from reference: %+v", gptx)
	}
	if gptx.Pricing == nil || !gptx.Pricing.Estimated {
		t.Errorf("borrowed pricing must be flagged estimated: %+v", gptx.Pricing)
	}

	// A chat model with no reference match keeps minimal info (streaming known,
	// the rest unknown) rather than guessed values.
	o3, _ := find(models, "o3-mini")
	if !IsTrue(o3.Capabilities.Streaming) {
		t.Errorf("o3-mini streaming should be known-true")
	}
	if o3.Capabilities.Tools != nil || o3.Pricing != nil {
		t.Errorf("o3-mini unmatched fields should stay unknown: caps=%+v pricing=%+v", o3.Capabilities, o3.Pricing)
	}
}

func TestReferenceEnrichment(t *testing.T) {
	ref := NewReferenceCatalog([]CanonicalModel{{
		ID: "openai/gpt-x", Provider: "openrouter", ContextWindow: 200000,
		Capabilities: Capabilities{Tools: Bool(true), Vision: Bool(true)},
		Pricing:      &Pricing{InputPerMTok: 2, OutputPerMTok: 8, Currency: "USD"},
	}})

	// OpenAI lists "gpt-x" with no metadata; enrich from the openai/ reference.
	openaiModel := CanonicalModel{ID: "gpt-x", Provider: "openai"}
	refModel, ok := ref.Lookup("openai", "gpt-x")
	if !ok {
		t.Fatal("reference lookup failed")
	}
	enriched := EnrichFromReference(openaiModel, refModel)

	if enriched.ContextWindow != 200000 || !IsTrue(enriched.Capabilities.Tools) {
		t.Errorf("not enriched: ctx=%d tools=%v", enriched.ContextWindow, enriched.Capabilities.Tools)
	}
	if enriched.Pricing == nil || !enriched.Pricing.Estimated || enriched.Pricing.Source != "openrouter-reference" {
		t.Errorf("borrowed pricing should be flagged estimated: %+v", enriched.Pricing)
	}
}
