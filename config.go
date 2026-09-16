package main

import (
	"fmt"
	"path"
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

const (
	pluginName       = "cpa-plugin-grok-compat"
	pluginVersion    = "0.1.0"
	defaultModelGlob = "grok-*"
)

type pluginConfig struct {
	Models []string `yaml:"models"`
}

type modelMatcher struct {
	patterns []string
}

type runtimeState struct {
	mu      sync.RWMutex
	matcher modelMatcher
	cache   schemaCache
}

var state = newRuntimeState()

func newRuntimeState() *runtimeState {
	return &runtimeState{matcher: modelMatcher{patterns: []string{defaultModelGlob}}}
}

func applyConfig(raw []byte) error {
	config := pluginConfig{Models: []string{defaultModelGlob}}
	if len(raw) > 0 {
		if err := yaml.Unmarshal(raw, &config); err != nil {
			return err
		}
		if config.Models == nil {
			config.Models = []string{defaultModelGlob}
		}
	}

	matcher, err := newModelMatcher(config.Models)
	if err != nil {
		return err
	}
	state.mu.Lock()
	state.matcher = matcher
	state.mu.Unlock()
	return nil
}

func newModelMatcher(patterns []string) (modelMatcher, error) {
	compiled := make([]string, 0, len(patterns))
	for _, raw := range patterns {
		pattern := strings.ToLower(strings.TrimSpace(raw))
		if pattern == "" {
			return modelMatcher{}, fmt.Errorf("models must not contain empty patterns")
		}
		if _, err := path.Match(pattern, "probe"); err != nil {
			return modelMatcher{}, fmt.Errorf("invalid model glob %q: %w", raw, err)
		}
		compiled = append(compiled, pattern)
	}
	return modelMatcher{patterns: compiled}, nil
}

func (matcher modelMatcher) Matches(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	for _, pattern := range matcher.patterns {
		matched, _ := path.Match(pattern, model)
		if matched {
			return true
		}
	}
	return false
}

func currentMatcher() modelMatcher {
	state.mu.RLock()
	matcher := state.matcher
	state.mu.RUnlock()
	return matcher
}

type registration struct {
	SchemaVersion uint32                 `json:"schema_version"`
	Metadata      pluginapi.Metadata     `json:"metadata"`
	Capabilities  registrationCapability `json:"capabilities"`
}

type registrationCapability struct {
	ResponseBeforeTranslator bool `json:"response_before_translator"`
}

func pluginRegistration() registration {
	return registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             pluginName,
			Version:          pluginVersion,
			Author:           "timonwong",
			GitHubRepository: "https://github.com/timonwong/cpa-plugin-grok-compat",
			ConfigFields: []pluginapi.ConfigField{{
				Name:        "models",
				Type:        pluginapi.ConfigFieldTypeArray,
				Description: "Case-insensitive model globs to which Grok tool-argument compatibility applies.",
			}},
		},
		Capabilities: registrationCapability{ResponseBeforeTranslator: true},
	}
}
