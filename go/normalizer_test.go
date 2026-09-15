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
	body := []byte(`{"type":"response.output_item.done","item":{"type":"function_call","name":"wait","arguments":"{\"yield_time_ms\":120000.0}"}}`)
	updated, changed := normalizeBody(body, tools)
	if changed || string(updated) != string(body) {
		t.Fatal("unknown schema was normalized")
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
