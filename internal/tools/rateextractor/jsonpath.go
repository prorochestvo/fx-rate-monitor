package rateextractor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	reArraySegment = regexp.MustCompile(`^(\w+)\[(\d+)\]$`)
	rePlainSegment = regexp.MustCompile(`^\w+$`)
)

type pathSegment struct {
	Key      string
	HasIndex bool
	Index    int
}

func parseJSONPath(pattern string) ([]pathSegment, error) {
	if pattern == "" {
		return nil, fmt.Errorf("json_path: path pattern must not be empty")
	}

	rawSegments := strings.Split(pattern, ".")
	segments := make([]pathSegment, 0, len(rawSegments))

	for _, raw := range rawSegments {
		if m := reArraySegment.FindStringSubmatch(raw); m != nil {
			idx, err := strconv.Atoi(m[2])
			if err != nil {
				// Should not happen given \d+ regex, but guard anyway.
				return nil, fmt.Errorf("json_path: invalid array index in segment %q: %w", raw, err)
			}
			segments = append(segments, pathSegment{Key: m[1], HasIndex: true, Index: idx})
			continue
		}

		if rePlainSegment.MatchString(raw) {
			segments = append(segments, pathSegment{Key: raw})
			continue
		}

		return nil, fmt.Errorf("json_path: invalid path segment %q", raw)
	}

	return segments, nil
}

func (extractor *RateExtractor) extractJSONPath(pattern string, payload []byte) ([]byte, error) {
	segments, err := parseJSONPath(pattern)
	if err != nil {
		return nil, fmt.Errorf("json_path: parse path %q: %w", pattern, err)
	}

	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()

	var root interface{}
	if err = dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("json_path: unmarshal JSON: %w", err)
	}

	current := root
	for _, seg := range segments {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("json_path: expected object at %q, got %T", seg.Key, current)
		}

		val, exists := m[seg.Key]
		if !exists {
			return nil, fmt.Errorf("json_path: key %q not found", seg.Key)
		}

		if seg.HasIndex {
			arr, ok := val.([]interface{})
			if !ok {
				return nil, fmt.Errorf("json_path: key %q is not an array", seg.Key)
			}
			if seg.Index >= len(arr) {
				return nil, fmt.Errorf("json_path: index %d out of range for key %q (len %d)", seg.Index, seg.Key, len(arr))
			}
			current = arr[seg.Index]
		} else {
			current = val
		}
	}

	switch v := current.(type) {
	case json.Number:
		return []byte(v.String()), nil
	case float64:
		return []byte(strconv.FormatFloat(v, 'f', -1, 64)), nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("json_path: terminal value at %q has unsupported type %T", pattern, current)
	}
}
