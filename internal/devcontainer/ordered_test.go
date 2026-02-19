package devcontainer

import (
	"encoding/json"
	"os"
	"testing"
)

func TestExtractKeyOrder(t *testing.T) {
	input := `{"name": "test", "image": "ubuntu", "features": {"a": {}, "b": {}}}`
	order := extractKeyOrder([]byte(input))

	if order == nil {
		t.Fatal("expected non-nil order")
	}
	wantKeys := []string{"name", "image", "features"}
	if len(order.keys) != len(wantKeys) {
		t.Fatalf("got %d keys, want %d", len(order.keys), len(wantKeys))
	}
	for i, k := range wantKeys {
		if order.keys[i] != k {
			t.Errorf("key[%d] = %q, want %q", i, order.keys[i], k)
		}
	}

	featOrder := order.children["features"]
	if featOrder == nil {
		t.Fatal("expected features child order")
	}
	if len(featOrder.keys) != 2 || featOrder.keys[0] != "a" || featOrder.keys[1] != "b" {
		t.Errorf("features keys = %v, want [a, b]", featOrder.keys)
	}
}

func TestMarshalOrderedPreservesExistingOrder(t *testing.T) {
	// Original has non-alphabetical order: name, image, features.
	original := `{"name": "test", "image": "ubuntu", "features": {}}`
	order := extractKeyOrder([]byte(original))

	config := map[string]any{
		"name":     "test",
		"image":    "ubuntu",
		"features": map[string]any{},
	}

	got := string(marshalOrdered(config, order))

	// Verify keys appear in original order, not alphabetical.
	nameIdx := indexOf(got, `"name"`)
	imageIdx := indexOf(got, `"image"`)
	featIdx := indexOf(got, `"features"`)

	if nameIdx > imageIdx || imageIdx > featIdx {
		t.Errorf("key order not preserved:\n%s", got)
	}
}

func TestMarshalOrderedAppendsNewKeysAlphabetically(t *testing.T) {
	original := `{"name": "test", "image": "ubuntu"}`
	order := extractKeyOrder([]byte(original))

	config := map[string]any{
		"name":     "test",
		"image":    "ubuntu",
		"features": map[string]any{},
		"capAdd":   []any{"SYS_PTRACE"},
	}

	got := string(marshalOrdered(config, order))

	nameIdx := indexOf(got, `"name"`)
	imageIdx := indexOf(got, `"image"`)
	capIdx := indexOf(got, `"capAdd"`)
	featIdx := indexOf(got, `"features"`)

	// Original keys first in original order.
	if nameIdx > imageIdx {
		t.Errorf("original order not preserved: name=%d image=%d", nameIdx, imageIdx)
	}
	// New keys after originals, in alphabetical order.
	if imageIdx > capIdx {
		t.Errorf("new keys should come after original keys: image=%d capAdd=%d", imageIdx, capIdx)
	}
	if capIdx > featIdx {
		t.Errorf("new keys should be alphabetical: capAdd=%d features=%d", capIdx, featIdx)
	}
}

func TestMarshalOrderedDeletedKeysSkipped(t *testing.T) {
	original := `{"name": "test", "image": "ubuntu", "features": {}}`
	order := extractKeyOrder([]byte(original))

	// Config without "features" — it was deleted.
	config := map[string]any{
		"name":  "test",
		"image": "ubuntu",
	}

	got := string(marshalOrdered(config, order))

	if indexOf(got, `"features"`) != -1 {
		t.Errorf("deleted key should not appear:\n%s", got)
	}
}

func TestMarshalOrderedNoOriginal(t *testing.T) {
	// No original file → all keys alphabetical.
	config := map[string]any{
		"name":     "test",
		"image":    "ubuntu",
		"features": map[string]any{},
	}

	got := string(marshalOrdered(config, nil))

	featIdx := indexOf(got, `"features"`)
	imageIdx := indexOf(got, `"image"`)
	nameIdx := indexOf(got, `"name"`)

	if featIdx > imageIdx || imageIdx > nameIdx {
		t.Errorf("without original, keys should be alphabetical:\n%s", got)
	}
}

func TestMarshalOrderedNestedPreservation(t *testing.T) {
	original := `{
  "customizations": {
    "vscode": {
      "settings": {},
      "extensions": []
    }
  },
  "name": "test"
}`
	order := extractKeyOrder([]byte(original))

	config := map[string]any{
		"name": "test",
		"customizations": map[string]any{
			"vscode": map[string]any{
				"settings":   map[string]any{"go.useLanguageServer": true},
				"extensions": []any{"golang.go"},
			},
		},
	}

	got := string(marshalOrdered(config, order))

	// Top-level: customizations before name.
	custIdx := indexOf(got, `"customizations"`)
	nameIdx := indexOf(got, `"name"`)
	if custIdx > nameIdx {
		t.Errorf("top-level order not preserved: customizations=%d name=%d", custIdx, nameIdx)
	}

	// Nested: settings before extensions.
	settIdx := indexOf(got, `"settings"`)
	extIdx := indexOf(got, `"extensions"`)
	if settIdx > extIdx {
		t.Errorf("nested order not preserved: settings=%d extensions=%d", settIdx, extIdx)
	}
}

func TestMarshalOrderedPrimitiveArrayCompact(t *testing.T) {
	config := map[string]any{
		"forwardPorts": []any{float64(3000), float64(8080)},
	}
	got := string(marshalOrdered(config, nil))

	// Primitive arrays should be on one line.
	expected := `[3000, 8080]`
	if indexOf(got, expected) == -1 {
		t.Errorf("primitive array not compact:\n%s\nwant substring: %s", got, expected)
	}
}

func TestMarshalOrderedRoundTrip(t *testing.T) {
	original := `{
  "name": "my-project",
  "image": "mcr.microsoft.com/devcontainers/base:ubuntu",
  "features": {
    "ghcr.io/devcontainers/features/node:1": {
      "version": "lts"
    }
  },
  "forwardPorts": [3000, 8080],
  "customizations": {
    "vscode": {
      "extensions": ["golang.go", "ms-python.python"]
    }
  }
}`
	order := extractKeyOrder([]byte(original))

	var config map[string]any
	json.Unmarshal([]byte(original), &config)

	got := string(marshalOrdered(config, order))

	// Re-parse to verify valid JSON.
	var reparsed map[string]any
	if err := json.Unmarshal([]byte(got), &reparsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, got)
	}

	// Verify key order is preserved.
	nameIdx := indexOf(got, `"name"`)
	imageIdx := indexOf(got, `"image"`)
	featIdx := indexOf(got, `"features"`)
	portsIdx := indexOf(got, `"forwardPorts"`)
	custIdx := indexOf(got, `"customizations"`)

	if nameIdx > imageIdx || imageIdx > featIdx || featIdx > portsIdx || portsIdx > custIdx {
		t.Errorf("round-trip order not preserved:\n%s", got)
	}
}

func TestWriteConfigPreservesOrder(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/devcontainer.json"

	// Write initial file with specific key order.
	initial := []byte(`{
  "name": "test",
  "image": "ubuntu",
  "features": {}
}
`)
	if err := writeFileForTest(path, initial); err != nil {
		t.Fatal(err)
	}

	// Modify and write back.
	config := map[string]any{
		"name":       "test",
		"image":      "ubuntu",
		"features":   map[string]any{},
		"remoteUser": "vscode",
	}
	if err := WriteConfig(path, config); err != nil {
		t.Fatal(err)
	}

	// Read back and check order.
	got := readFileForTest(t, path)
	nameIdx := indexOf(got, `"name"`)
	imageIdx := indexOf(got, `"image"`)
	featIdx := indexOf(got, `"features"`)
	remoteIdx := indexOf(got, `"remoteUser"`)

	if nameIdx > imageIdx || imageIdx > featIdx || featIdx > remoteIdx {
		t.Errorf("order not preserved after WriteConfig:\n%s", got)
	}
}

// --- helpers ---

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func writeFileForTest(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
