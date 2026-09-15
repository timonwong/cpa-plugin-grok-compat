package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"sync"
)

const schemaCacheSize = 16

type toolSchema struct {
	Type       string                `json:"type"`
	Properties map[string]toolSchema `json:"properties"`
	Items      *toolSchema           `json:"items"`
}

type toolCatalog map[string]toolSchema

type schemaCache struct {
	mu      sync.Mutex
	entries map[[sha256.Size]byte]toolCatalog
	order   [][sha256.Size]byte
}

func (cache *schemaCache) Lookup(raw []byte) toolCatalog {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	key := sha256.Sum256(raw)
	cache.mu.Lock()
	if catalog, ok := cache.entries[key]; ok {
		cache.mu.Unlock()
		return catalog
	}
	cache.mu.Unlock()

	catalog := parseToolCatalog(raw)
	cache.mu.Lock()
	if cache.entries == nil {
		cache.entries = make(map[[sha256.Size]byte]toolCatalog, schemaCacheSize)
	}
	if _, exists := cache.entries[key]; !exists {
		cache.entries[key] = catalog
		cache.order = append(cache.order, key)
		if len(cache.order) > schemaCacheSize {
			delete(cache.entries, cache.order[0])
			cache.order = cache.order[1:]
		}
	}
	cache.mu.Unlock()
	return catalog
}

func parseToolCatalog(raw []byte) toolCatalog {
	var document any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&document); err != nil {
		return nil
	}
	catalog := make(toolCatalog)
	collectToolSchemas(document, catalog)
	return catalog
}

func collectToolSchemas(value any, catalog toolCatalog) {
	switch node := value.(type) {
	case []any:
		for _, child := range node {
			collectToolSchemas(child, catalog)
		}
	case map[string]any:
		if name, ok := node["name"].(string); ok {
			if parameters, ok := node["parameters"].(map[string]any); ok {
				rawSchema, err := json.Marshal(parameters)
				if err == nil {
					var schema toolSchema
					if json.Unmarshal(rawSchema, &schema) == nil {
						catalog[name] = schema
					}
				}
			}
		}
		for _, child := range node {
			collectToolSchemas(child, catalog)
		}
	}
}
