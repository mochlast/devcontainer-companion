package devcontainer

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

// keyOrder records the key ordering of JSON objects at each nesting level.
type keyOrder struct {
	keys     []string
	children map[string]*keyOrder
}

// extractKeyOrder parses JSON data and returns the key ordering tree.
// For each object in the JSON, it records the keys in their original order.
func extractKeyOrder(data []byte) *keyOrder {
	dec := json.NewDecoder(bytes.NewReader(data))
	order, _ := decodeValueOrder(dec)
	return order
}

func decodeValueOrder(dec *json.Decoder) (*keyOrder, error) {
	t, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := t.(json.Delim); ok {
		switch delim {
		case '{':
			return decodeObjectOrder(dec)
		case '[':
			return decodeArrayOrder(dec)
		}
	}
	return nil, nil
}

func decodeObjectOrder(dec *json.Decoder) (*keyOrder, error) {
	order := &keyOrder{
		children: make(map[string]*keyOrder),
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return order, err
		}
		key, ok := t.(string)
		if !ok {
			continue
		}
		order.keys = append(order.keys, key)

		child, err := decodeValueOrder(dec)
		if err != nil {
			return order, err
		}
		if child != nil {
			order.children[key] = child
		}
	}
	// consume closing '}'
	dec.Token() //nolint:errcheck
	return order, nil
}

func decodeArrayOrder(dec *json.Decoder) (*keyOrder, error) {
	for dec.More() {
		decodeValueOrder(dec) //nolint:errcheck
	}
	// consume closing ']'
	dec.Token() //nolint:errcheck
	return nil, nil
}

// marshalOrdered serializes value as indented JSON, preserving key ordering
// from order for existing keys and appending new keys alphabetically.
func marshalOrdered(value any, order *keyOrder) []byte {
	var buf bytes.Buffer
	writeValue(&buf, value, order, "  ", 0)
	buf.WriteByte('\n')
	return buf.Bytes()
}

func writeValue(buf *bytes.Buffer, value any, order *keyOrder, indent string, depth int) {
	switch v := value.(type) {
	case map[string]any:
		writeObject(buf, v, order, indent, depth)
	case []any:
		writeArray(buf, v, indent, depth)
	default:
		b, _ := json.Marshal(v)
		buf.Write(b)
	}
}

func writeObject(buf *bytes.Buffer, m map[string]any, order *keyOrder, indent string, depth int) {
	if len(m) == 0 {
		buf.WriteString("{}")
		return
	}

	keys := orderedKeysForMap(m, order)

	buf.WriteString("{\n")
	prefix := strings.Repeat(indent, depth+1)
	for i, k := range keys {
		buf.WriteString(prefix)
		keyBytes, _ := json.Marshal(k)
		buf.Write(keyBytes)
		buf.WriteString(": ")

		var childOrder *keyOrder
		if order != nil {
			childOrder = order.children[k]
		}
		writeValue(buf, m[k], childOrder, indent, depth+1)

		if i < len(keys)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString(strings.Repeat(indent, depth))
	buf.WriteByte('}')
}

func writeArray(buf *bytes.Buffer, arr []any, indent string, depth int) {
	if len(arr) == 0 {
		buf.WriteString("[]")
		return
	}

	// Check if all elements are primitives for compact single-line output.
	allPrimitive := true
	for _, v := range arr {
		switch v.(type) {
		case map[string]any, []any:
			allPrimitive = false
		}
	}

	if allPrimitive {
		buf.WriteByte('[')
		for i, v := range arr {
			b, _ := json.Marshal(v)
			buf.Write(b)
			if i < len(arr)-1 {
				buf.WriteString(", ")
			}
		}
		buf.WriteByte(']')
		return
	}

	buf.WriteString("[\n")
	prefix := strings.Repeat(indent, depth+1)
	for i, v := range arr {
		buf.WriteString(prefix)
		writeValue(buf, v, nil, indent, depth+1)
		if i < len(arr)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString(strings.Repeat(indent, depth))
	buf.WriteByte(']')
}

// orderedKeysForMap returns the keys of m in the order specified by order.
// Existing keys keep their original order. New keys are appended at the end
// in alphabetical order.
func orderedKeysForMap(m map[string]any, order *keyOrder) []string {
	if order == nil {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	}

	seen := make(map[string]bool, len(order.keys))
	keys := make([]string, 0, len(m))
	for _, k := range order.keys {
		if _, exists := m[k]; exists {
			keys = append(keys, k)
			seen[k] = true
		}
	}

	var newKeys []string
	for k := range m {
		if !seen[k] {
			newKeys = append(newKeys, k)
		}
	}
	sort.Strings(newKeys)
	keys = append(keys, newKeys...)

	return keys
}
