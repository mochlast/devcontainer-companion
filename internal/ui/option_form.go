package ui

import "fmt"

// defaultToString converts an option default value to a string.
// Handles the case where JSON defaults are arrays (e.g. ["3", "3.12"])
// by using the first element.
func defaultToString(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case []any:
		if len(val) > 0 {
			return fmt.Sprintf("%v", val[0])
		}
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}
