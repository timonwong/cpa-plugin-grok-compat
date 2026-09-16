package main

import (
	"bytes"
	"encoding/json"
	"math/big"
	"strconv"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const maxSafeJSONInt = 9007199254740991

// Responses Lite exposes Codex tools as a custom `exec` source string, so the
// nested tool schemas are present only in prose. These fields are integer
// runtime parameters even though that prose uses TypeScript's `number` type.
var codexIntegerSourceFields = map[string]struct{}{
	"session_id":        {},
	"yield_time_ms":     {},
	"max_output_tokens": {},
}

var codexIntegerArgumentFields = map[string]map[string]struct{}{
	"exec_command": {
		"yield_time_ms":     {},
		"max_output_tokens": {},
	},
	"write_stdin": {
		"session_id":        {},
		"yield_time_ms":     {},
		"max_output_tokens": {},
	},
	"wait": {
		"yield_time_ms":     {},
		"max_output_tokens": {},
	},
}

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
	schema, hasSchema := catalog[name]
	changed := hasSchema && normalizeArguments(node, schema, name == "wait")
	if fields, ok := codexIntegerArgumentFields[codexToolName(name)]; ok {
		changed = normalizeKnownIntegerArguments(node, fields) || changed
	}
	if isCodexExecName(name) {
		changed = normalizeExecSource(node) || changed
	}
	return changed
}

func codexToolName(name string) string {
	if separator := strings.LastIndex(name, "__"); separator >= 0 {
		return name[separator+2:]
	}
	return name
}

func normalizeKnownIntegerArguments(node map[string]any, fields map[string]struct{}) bool {
	switch value := node["arguments"].(type) {
	case string:
		decoder := json.NewDecoder(strings.NewReader(value))
		decoder.UseNumber()
		var object any
		if err := decoder.Decode(&object); err != nil {
			return false
		}
		if !normalizeKnownIntegerObject(object, fields) {
			return false
		}
		updated, err := json.Marshal(object)
		if err != nil {
			return false
		}
		node["arguments"] = string(updated)
		return true
	case map[string]any:
		return normalizeKnownIntegerObject(value, fields)
	default:
		return false
	}
}

func normalizeKnownIntegerObject(value any, fields map[string]struct{}) bool {
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	changed := false
	for key, child := range object {
		if _, ok := fields[key]; !ok {
			continue
		}
		if coerced, ok := integralNumber(child); ok {
			object[key] = coerced
			changed = true
		}
	}
	return changed
}

func isCodexExecName(name string) bool {
	return name == "exec" || strings.HasSuffix(name, "__exec")
}

func normalizeExecSource(node map[string]any) bool {
	arguments, ok := node["arguments"].(string)
	if !ok {
		return false
	}
	updated, changed := normalizeSourceIntegers(arguments)
	if changed {
		node["arguments"] = updated
	}
	return changed
}

func normalizeSourceIntegers(source string) (string, bool) {
	if !strings.Contains(source, ".") {
		return source, false
	}
	bytesSource := []byte(source)
	changed := false
	for index := 0; index < len(bytesSource); {
		switch bytesSource[index] {
		case '\'', '"', '`':
			index = skipSourceString(bytesSource, index)
			continue
		case '/':
			if index+1 < len(bytesSource) && bytesSource[index+1] == '/' {
				index = skipSourceLineComment(bytesSource, index+2)
				continue
			}
			if index+1 < len(bytesSource) && bytesSource[index+1] == '*' {
				index = skipSourceBlockComment(bytesSource, index+2)
				continue
			}
		}

		if !isSourceIdentifierStart(bytesSource[index]) {
			index++
			continue
		}
		start := index
		index++
		for index < len(bytesSource) && isSourceIdentifierPart(bytesSource[index]) {
			index++
		}
		if _, ok := codexIntegerSourceFields[string(bytesSource[start:index])]; !ok {
			continue
		}
		valueStart := skipSourceSpace(bytesSource, index)
		if valueStart >= len(bytesSource) || bytesSource[valueStart] != ':' {
			continue
		}
		valueStart = skipSourceSpace(bytesSource, valueStart+1)
		valueEnd := scanSourceNumber(bytesSource, valueStart)
		if valueEnd == valueStart {
			continue
		}
		value, ok := integralNumber(json.Number(string(bytesSource[valueStart:valueEnd])))
		if !ok {
			continue
		}
		replacement := []byte(strconv.FormatInt(value, 10))
		if bytes.Equal(replacement, bytesSource[valueStart:valueEnd]) {
			continue
		}
		bytesSource = append(bytesSource[:valueStart], append(replacement, bytesSource[valueEnd:]...)...)
		index = valueStart + len(replacement)
		changed = true
	}
	return string(bytesSource), changed
}

func skipSourceString(source []byte, index int) int {
	quote := source[index]
	index++
	for index < len(source) {
		if source[index] == '\\' {
			index += 2
			continue
		}
		if source[index] == quote {
			return index + 1
		}
		index++
	}
	return len(source)
}

func skipSourceLineComment(source []byte, index int) int {
	for index < len(source) && source[index] != '\n' {
		index++
	}
	return index
}

func skipSourceBlockComment(source []byte, index int) int {
	for index+1 < len(source) {
		if source[index] == '*' && source[index+1] == '/' {
			return index + 2
		}
		index++
	}
	return len(source)
}

func skipSourceSpace(source []byte, index int) int {
	for index < len(source) && (source[index] == ' ' || source[index] == '\t' || source[index] == '\r' || source[index] == '\n') {
		index++
	}
	return index
}

func scanSourceNumber(source []byte, index int) int {
	start := index
	if index < len(source) && (source[index] == '+' || source[index] == '-') {
		index++
	}
	for index < len(source) && source[index] >= '0' && source[index] <= '9' {
		index++
	}
	if index < len(source) && source[index] == '.' {
		index++
	}
	for index < len(source) && source[index] >= '0' && source[index] <= '9' {
		index++
	}
	if index < len(source) && (source[index] == 'e' || source[index] == 'E') {
		index++
		if index < len(source) && (source[index] == '+' || source[index] == '-') {
			index++
		}
		exponent := index
		for index < len(source) && source[index] >= '0' && source[index] <= '9' {
			index++
		}
		if index == exponent {
			return start
		}
	}
	if index == start || (index == start+1 && (source[start] == '+' || source[start] == '-')) {
		return start
	}
	return index
}

func isSourceIdentifierStart(value byte) bool {
	return value == '_' || value == '$' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func isSourceIdentifierPart(value byte) bool {
	return isSourceIdentifierStart(value) || value >= '0' && value <= '9'
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
