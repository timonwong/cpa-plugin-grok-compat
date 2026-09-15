package main

import (
	"bytes"
	"encoding/json"
	"math/big"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const maxSafeJSONInt = 9007199254740991

func normalizeResponse(raw []byte) ([]byte, error) {
	var request pluginapi.ResponseTransformRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	if !currentMatcher().Matches(request.Model) {
		return okEnvelope(pluginapi.PayloadResponse{Body: request.Body})
	}
	catalog := state.cache.Lookup(request.OriginalRequest)
	body, _ := normalizeBody(request.Body, catalog)
	return okEnvelope(pluginapi.PayloadResponse{Body: body})
}

func normalizeBody(body []byte, catalog toolCatalog) ([]byte, bool) {
	if looksLikeSSE(body) {
		return normalizeSSE(body, catalog)
	}
	return normalizeJSONEvent(body, catalog)
}

func normalizeJSONEvent(body []byte, catalog toolCatalog) ([]byte, bool) {
	if len(body) == 0 || !json.Valid(body) {
		return body, false
	}
	var document any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return body, false
	}
	if !normalizeEvent(document, catalog) {
		return body, false
	}
	updated, err := json.Marshal(document)
	if err != nil {
		return body, false
	}
	return updated, true
}

func looksLikeSSE(body []byte) bool {
	for _, line := range bytes.Split(body, []byte("\n")) {
		if bytes.HasPrefix(bytes.TrimSpace(line), []byte("data:")) {
			return true
		}
	}
	return false
}

func normalizeSSE(body []byte, catalog toolCatalog) ([]byte, bool) {
	lines := bytes.SplitAfter(body, []byte("\n"))
	changed := false
	for index, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if !bytes.HasPrefix(trimmed, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(trimmed[len("data:"):])
		if bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		updated, eventChanged := normalizeJSONEvent(payload, catalog)
		if !eventChanged {
			continue
		}
		prefix := line[:bytes.Index(line, []byte("data:"))+len("data:")]
		ending := lineEnding(line)
		lines[index] = append(append(append([]byte(nil), prefix...), ' '), append(updated, ending...)...)
		changed = true
	}
	if !changed {
		return body, false
	}
	return bytes.Join(lines, nil), true
}

func lineEnding(line []byte) []byte {
	if bytes.HasSuffix(line, []byte("\r\n")) {
		return []byte("\r\n")
	}
	if bytes.HasSuffix(line, []byte("\n")) {
		return []byte("\n")
	}
	return nil
}

func normalizeEvent(value any, catalog toolCatalog) bool {
	switch node := value.(type) {
	case []any:
		changed := false
		for _, child := range node {
			changed = normalizeEvent(child, catalog) || changed
		}
		return changed
	case map[string]any:
		if eventType, ok := node["type"].(string); ok && strings.Contains(strings.ToLower(eventType), "delta") {
			return false
		}
		changed := normalizeNamedArguments(node, catalog)
		if item, ok := node["item"].(map[string]any); ok {
			changed = normalizeEvent(item, catalog) || changed
		}
		if response, ok := node["response"].(map[string]any); ok {
			changed = normalizeEvent(response, catalog) || changed
		}
		if output, ok := node["output"].([]any); ok {
			changed = normalizeEvent(output, catalog) || changed
		}
		return changed
	default:
		return false
	}
}

func normalizeNamedArguments(node map[string]any, catalog toolCatalog) bool {
	name, hasName := node["name"].(string)
	if !hasName {
		return false
	}
	if _, exists := node["arguments"]; !exists {
		return false
	}
	schema, exists := catalog[name]
	if !exists {
		return false
	}
	return normalizeArguments(node, schema, name == "wait")
}

func normalizeArguments(node map[string]any, schema toolSchema, waitTool bool) bool {
	switch value := node["arguments"].(type) {
	case string:
		if !json.Valid([]byte(value)) {
			return false
		}
		var object any
		decoder := json.NewDecoder(strings.NewReader(value))
		decoder.UseNumber()
		if err := decoder.Decode(&object); err != nil || !normalizeObject(object, schema, waitTool) {
			return false
		}
		updated, err := json.Marshal(object)
		if err != nil {
			return false
		}
		node["arguments"] = string(updated)
		return true
	case map[string]any, []any:
		return normalizeObject(value, schema, waitTool)
	default:
		return false
	}
}

func normalizeObject(value any, schema toolSchema, waitTool bool) bool {
	switch node := value.(type) {
	case map[string]any:
		changed := false
		for key, child := range node {
			property, exists := schema.Properties[key]
			if !exists {
				continue
			}
			if shouldCoerce(property.Type, waitTool, key, child) {
				if coerced, ok := integralNumber(child); ok {
					node[key] = coerced
					changed = true
					continue
				}
			}
			changed = normalizeObject(child, property, waitTool) || changed
		}
		return changed
	case []any:
		if schema.Items == nil {
			return false
		}
		changed := false
		for _, child := range node {
			changed = normalizeObject(child, *schema.Items, waitTool) || changed
		}
		return changed
	default:
		return false
	}
}

func shouldCoerce(schemaType string, waitTool bool, key string, value any) bool {
	if _, ok := value.(json.Number); !ok {
		return false
	}
	return schemaType == "integer" || (waitTool && key == "yield_time_ms" && schemaType == "number")
}

func integralNumber(value any) (int64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	rational, ok := new(big.Rat).SetString(number.String())
	if !ok || !rational.IsInt() {
		return 0, false
	}
	limit := big.NewInt(maxSafeJSONInt)
	if rational.Num().Cmp(limit) > 0 || rational.Num().Cmp(new(big.Int).Neg(limit)) < 0 {
		return 0, false
	}
	return rational.Num().Int64(), true
}
