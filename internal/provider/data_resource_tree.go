package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// parseDataResourceTree validates every node, including nested children (whose
// OpenAPI items are only declared as objects), without discarding unknown keys.
func parseDataResourceTree(text string) (json.RawMessage, string, error) {
	dec := json.NewDecoder(strings.NewReader(text))
	node, err := parseTreeNode(dec, 1, map[string]bool{})
	if err != nil {
		return nil, "", err
	}
	var extra any
	if err = dec.Decode(&extra); err == nil {
		return nil, "", errors.New("struct contains multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return nil, "", errors.New("invalid trailing struct JSON")
	}
	b, err := json.Marshal(node)
	if err != nil {
		return nil, "", err
	}
	return b, string(b), nil
}
func parseTreeNode(dec *json.Decoder, depth int, ancestors map[string]bool) (map[string]any, error) {
	if depth > 5 {
		return nil, errors.New("TREE struct exceeds five levels")
	}
	token, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("invalid TREE node: %w", err)
	}
	if token != json.Delim('{') {
		return nil, errors.New("TREE node must be an object")
	}
	node := map[string]any{}
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, errors.New("invalid TREE key")
		}
		if _, exists := node[key]; exists {
			return nil, fmt.Errorf("duplicate TREE key %q", key)
		}
		switch key {
		case "code", "name", "value":
			v, err := dec.Token()
			if err != nil {
				return nil, err
			}
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("TREE %s must be a string", key)
			}
			max := 50
			if key == "value" {
				max = 1000
			}
			if len([]rune(s)) > max || (key != "value" && s == "") {
				return nil, fmt.Errorf("TREE %s must be nonempty and at most %d characters", key, max)
			}
			node[key] = s
		case "extendFieldValue":
			v, err := dec.Token()
			if err != nil {
				return nil, err
			}
			if v != json.Delim('{') {
				return nil, errors.New("TREE extendFieldValue must be an object")
			}
			if dec.More() {
				return nil, errors.New("nonempty TREE extendFieldValue is not managed")
			}
			if _, err = dec.Token(); err != nil {
				return nil, err
			}
			node[key] = map[string]any{}
		case "children":
			// Children are parsed after code/name so ancestor checks also apply when
			// input JSON puts children before the required identity properties.
			var children json.RawMessage
			if err := dec.Decode(&children); err != nil {
				return nil, err
			}
			node[key] = children
		default:
			return nil, fmt.Errorf("unsupported TREE node property %q", key)
		}
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	code, cok := node["code"].(string)
	_, nok := node["name"].(string)
	if !cok || !nok {
		return nil, errors.New("TREE node requires code and name")
	}
	if ancestors[code] {
		return nil, fmt.Errorf("TREE cycle/repeated ancestor code %q", code)
	}
	if raw, ok := node["children"].(json.RawMessage); ok {
		childDec := json.NewDecoder(strings.NewReader(string(raw)))
		tok, err := childDec.Token()
		if err != nil {
			return nil, err
		}
		if tok != json.Delim('[') {
			return nil, errors.New("TREE children must be an array")
		}
		ancestors[code] = true
		defer delete(ancestors, code)
		codes, names := map[string]bool{}, map[string]bool{}
		children := []any{}
		for childDec.More() {
			child, err := parseTreeNode(childDec, depth+1, ancestors)
			if err != nil {
				return nil, err
			}
			c, n := child["code"].(string), child["name"].(string)
			if codes[c] || names[n] {
				return nil, fmt.Errorf("TREE sibling code or name repeated: %q / %q", c, n)
			}
			codes[c] = true
			names[n] = true
			children = append(children, child)
		}
		if _, err := childDec.Token(); err != nil {
			return nil, err
		}
		var trailing any
		if err := childDec.Decode(&trailing); !errors.Is(err, io.EOF) {
			return nil, errors.New("invalid TREE children JSON")
		}
		node["children"] = children
	}
	return node, nil
}
