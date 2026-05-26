package skills

import (
	"fmt"
	"strings"
)

func requiredStringList(payload map[string]interface{}, key string) ([]string, error) {
	v, ok := payload[key]
	if !ok {
		return nil, fmt.Errorf("%s is required", key)
	}
	items, err := toStringList(v)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", key, err)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("%s must not be empty", key)
	}
	return items, nil
}

func optionalStringList(payload map[string]interface{}, key string) ([]string, error) {
	v, ok := payload[key]
	if !ok || v == nil {
		return nil, nil
	}
	items, err := toStringList(v)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", key, err)
	}
	return items, nil
}

func optionalStringMap(payload map[string]interface{}, key string) (map[string]string, error) {
	v, ok := payload[key]
	if !ok || v == nil {
		return map[string]string{}, nil
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("must be an object")
	}
	out := make(map[string]string, len(m))
	for k, raw := range m {
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("%s.%s must be a string", key, k)
		}
		out[strings.TrimSpace(k)] = s
	}
	return out, nil
}

func toStringList(v interface{}) ([]string, error) {
	rawList, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("must be an array")
	}
	out := make([]string, 0, len(rawList))
	for i, item := range rawList {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("item %d must be a string", i)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, fmt.Errorf("item %d must not be empty", i)
		}
		out = append(out, s)
	}
	return out, nil
}

func boolFromPayload(payload map[string]interface{}, key string, fallback bool) bool {
	v, ok := payload[key]
	if !ok {
		return fallback
	}
	b, ok := v.(bool)
	if !ok {
		return fallback
	}
	return b
}

func stringFromPayload(payload map[string]interface{}, key, fallback string) string {
	v, ok := payload[key]
	if !ok {
		return fallback
	}
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	return s
}
