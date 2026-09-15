package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateModelsDefaultsAndRejectsEmpty(t *testing.T) {
	got, err := newModelMatcher([]string{defaultModelGlob})
	if err != nil || !got.Matches("GROK-4.1") {
		t.Fatalf("newModelMatcher() = %#v, %v", got, err)
	}
	if _, err := newModelMatcher([]string{""}); err == nil {
		t.Fatal("newModelMatcher accepted an empty pattern")
	}
}

func TestConfigureRejectsInvalidUpdateWithoutReplacingValidState(t *testing.T) {
	previous := currentMatcher()
	if err := applyConfig([]byte("models:\n  - \"[\"\n")); err == nil {
		t.Fatal("applyConfig accepted invalid model glob")
	}
	if currentMatcher().Matches("grok-4.1") != previous.Matches("grok-4.1") {
		t.Fatal("invalid configure replaced the active matcher")
	}
}

func TestNormalizeResponseConvertsSchemaIntegerAndWaitNumber(t *testing.T) {
	tools := state.cache.Lookup([]byte(`{"tools":[{"type":"function","name":"wait","parameters":{"type":"object","properties":{"yield_time_ms":{"type":"number"},"max_tokens":{"type":"integer"},"priority":{"type":"number"}}}}]}`))
	body := []byte(`{"type":"response.output_item.done","item":{"type":"function_call","name":"wait","arguments":"{\"yield_time_ms\":120000.0,\"max_tokens\":5000.0,\"priority\":2.0}"}}`)
	updated, changed := normalizeBody(body, tools)
	if !changed {
		t.Fatal("normalizeBody reported no change")
	}
	var event struct {
		Item struct {
			Arguments string `json:"arguments"`
		} `json:"item"`
	}
	if err := json.Unmarshal(updated, &event); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"yield_time_ms":120000`, `"max_tokens":5000`, `"priority":2.0`} {
		if !strings.Contains(event.Item.Arguments, want) {
			t.Fatalf("normalized arguments missing %s: %s", want, event.Item.Arguments)
		}
	}
}

func TestNormalizeResponseLeavesFractionAndDeltaUntouched(t *testing.T) {
	tools := state.cache.Lookup([]byte(`{"tools":[{"name":"wait","parameters":{"type":"object","properties":{"yield_time_ms":{"type":"number"}}}}]}`))
	fraction := []byte(`{"type":"response.function_call_arguments.done","name":"wait","arguments":"{\"yield_time_ms\":1.5}"}`)
	updated, changed := normalizeBody(fraction, tools)
	if changed || string(updated) != string(fraction) {
		t.Fatalf("fraction changed: changed=%v body=%s", changed, updated)
	}
	delta := []byte(`{"type":"response.function_call_arguments.delta","name":"wait","delta":"{\"yield_time_ms\":120000.0}"}`)
	updated, changed = normalizeBody(delta, tools)
	if changed || string(updated) != string(delta) {
		t.Fatalf("delta changed: changed=%v body=%s", changed, updated)
	}
}

func TestNormalizeResponseHandlesSSEAndInvalidJSON(t *testing.T) {
	tools := state.cache.Lookup([]byte(`{"tools":[{"name":"wait","parameters":{"type":"object","properties":{"yield_time_ms":{"type":"number"}}}}]}`))
	body := []byte("event: response.function_call_arguments.done\ndata: {\"type\":\"response.function_call_arguments.done\",\"name\":\"wait\",\"arguments\":\"{\\\"yield_time_ms\\\":120000.0}\"}\n\n")
	updated, changed := normalizeBody(body, tools)
	if !changed || !strings.Contains(string(updated), `yield_time_ms\":120000`) {
		t.Fatalf("SSE was not normalized: changed=%v body=%s", changed, updated)
	}
	invalid := []byte(`{"type":"response.output_item.done","item":{"name":"wait","arguments":"{"}`)
	updated, changed = normalizeBody(invalid, tools)
	if changed || string(updated) != string(invalid) {
		t.Fatalf("invalid JSON changed: changed=%v body=%s", changed, updated)
	}
}

func TestNormalizeBodyDoesNotTreatDataStringAsSSE(t *testing.T) {
	tools := state.cache.Lookup([]byte(`{"tools":[{"name":"wait","parameters":{"type":"object","properties":{"yield_time_ms":{"type":"number"}}}}]}`))
	body := []byte(`{"type":"response.output_item.done","item":{"type":"function_call","name":"wait","arguments":"{\"note\":\"data: text\",\"yield_time_ms\":120000.0}"}}`)
	updated, changed := normalizeBody(body, tools)
	if !changed || !strings.Contains(string(updated), `yield_time_ms\":120000`) {
		t.Fatalf("JSON containing data: was not normalized: changed=%v body=%s", changed, updated)
	}
}

func TestNormalizeResponseDoesNotTouchUnknownSchema(t *testing.T) {
	tools := state.cache.Lookup([]byte(`{"tools":[{"name":"other","parameters":{"type":"object","properties":{"count":{"type":"integer"}}}}]}`))
	body := []byte(`{"type":"response.output_item.done","item":{"type":"function_call","name":"unknown","arguments":"{\"count\":120000.0}"}}`)
	updated, changed := normalizeBody(body, tools)
	if changed || string(updated) != string(body) {
		t.Fatal("unknown schema was normalized")
	}
}

func TestNormalizeResponseRepairsNestedCodexToolIntegerInExecSource(t *testing.T) {
	tools := state.cache.Lookup([]byte(`{"input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec","description":"nested tools include write_stdin(session_id: number)"}]}]}]}`))
	body := []byte(`{"type":"response.output_item.done","item":{"type":"function_call","name":"exec","arguments":"await tools.write_stdin({ session_id: 35879.0, yield_time_ms: 10000.0 });"}}`)
	updated, changed := normalizeBody(body, tools)
	if !changed {
		t.Fatal("nested Codex tool source was not normalized")
	}
	if strings.Contains(string(updated), `session_id: 35879.0`) || strings.Contains(string(updated), `yield_time_ms: 10000.0`) {
		t.Fatalf("nested integer arguments still contain .0: %s", updated)
	}
	if !strings.Contains(string(updated), `session_id: 35879`) || !strings.Contains(string(updated), `yield_time_ms: 10000`) {
		t.Fatalf("nested integer arguments missing normalized values: %s", updated)
	}
}

func TestNormalizeResponseRepairsNamespacedCodexExecSource(t *testing.T) {
	body := []byte(`{"type":"response.output_item.done","item":{"type":"function_call","name":"functions__exec","arguments":"await tools.write_stdin({ session_id: 35879.0 });"}}`)
	updated, changed := normalizeBody(body, nil)
	if !changed || strings.Contains(string(updated), `session_id: 35879.0`) || !strings.Contains(string(updated), `session_id: 35879`) {
		t.Fatalf("namespaced exec source was not normalized: changed=%v body=%s", changed, updated)
	}
}

func TestNormalizeResponseRepairsDirectCodexExecCommandArguments(t *testing.T) {
	body := []byte(`{"type":"response.output_item.done","item":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"pwd\",\"yield_time_ms\":2500.0,\"max_output_tokens\":1000.0}"}}`)
	updated, changed := normalizeBody(body, nil)
	if !changed {
		t.Fatal("direct exec_command arguments were not normalized")
	}
	if strings.Contains(string(updated), `yield_time_ms\":2500.0`) || strings.Contains(string(updated), `max_output_tokens\":1000.0`) {
		t.Fatalf("direct integer arguments still contain .0: %s", updated)
	}
	if !strings.Contains(string(updated), `yield_time_ms\":2500`) || !strings.Contains(string(updated), `max_output_tokens\":1000`) {
		t.Fatalf("direct integer arguments missing normalized values: %s", updated)
	}
}

func TestNormalizeResponseRepairsDirectWriteStdinObjectArguments(t *testing.T) {
	body := []byte(`{"type":"response.output_item.done","item":{"type":"function_call","name":"functions__write_stdin","arguments":{"session_id":35879.0,"yield_time_ms":2500.5,"note":"keep"}}}`)
	updated, changed := normalizeBody(body, nil)
	if !changed || !strings.Contains(string(updated), `"session_id":35879`) || strings.Contains(string(updated), `"session_id":35879.0`) {
		t.Fatalf("direct write_stdin arguments were not normalized: changed=%v body=%s", changed, updated)
	}
	if !strings.Contains(string(updated), `"yield_time_ms":2500.5`) || !strings.Contains(string(updated), `"note":"keep"`) {
		t.Fatalf("fraction or unrelated field changed: %s", updated)
	}
}

func TestNormalizeExecSourceLeavesStringsCommentsAndFractionsUntouched(t *testing.T) {
	source := `const text = "session_id: 7.0"; // yield_time_ms: 8.0
await tools.write_stdin({ session_id: 35879.5, yield_time_ms: 10000.0 });`
	updated, changed := normalizeSourceIntegers(source)
	if !changed {
		t.Fatal("integral source argument was not normalized")
	}
	if !strings.Contains(updated, `"session_id: 7.0"`) || !strings.Contains(updated, `// yield_time_ms: 8.0`) {
		t.Fatalf("string/comment content was changed: %s", updated)
	}
	if !strings.Contains(updated, `session_id: 35879.5`) || !strings.Contains(updated, `yield_time_ms: 10000`) {
		t.Fatalf("fraction or integral source value changed unexpectedly: %s", updated)
	}
}

func TestIntegralNumberHonorsSafeIntegerBoundary(t *testing.T) {
	if got, ok := integralNumber(json.Number("9007199254740991.0")); !ok || got != 9007199254740991 {
		t.Fatalf("safe integral number = %d, %v", got, ok)
	}
	if _, ok := integralNumber(json.Number("9007199254740992.0")); ok {
		t.Fatal("unsafe integral number was coerced")
	}
}
