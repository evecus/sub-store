package proxy

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// UnmarshalYAML parses YAML text into a map. Handles Clash proxy list format.
// This is a simplified parser focused on the proxies: [...] structure.
func UnmarshalYAML(data []byte, v interface{}) error {
	// Convert YAML to a JSON-compatible structure using line-by-line parsing
	result, err := parseYAMLDoc(string(data))
	if err != nil {
		return err
	}
	// Round-trip through JSON to populate v
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

// parseYAMLDoc parses a YAML document into map[string]interface{}.
func parseYAMLDoc(text string) (map[string]interface{}, error) {
	lines := strings.Split(text, "\n")
	result := make(map[string]interface{})
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimRight(line, " \t\r")
		if trimmed == "" || strings.HasPrefix(strings.TrimSpace(trimmed), "#") {
			i++
			continue
		}
		indent := countIndent(trimmed)
		if indent > 0 {
			i++
			continue // top-level only
		}
		colonIdx := strings.Index(trimmed, ":")
		if colonIdx == -1 {
			i++
			continue
		}
		key := strings.TrimSpace(trimmed[:colonIdx])
		val := strings.TrimSpace(trimmed[colonIdx+1:])

		if val == "" || val == "|" || val == ">" {
			// Block value - could be a list or nested map
			i++
			childIndent := -1
			var childLines []string
			for i < len(lines) {
				cl := lines[i]
				clTrimmed := strings.TrimRight(cl, " \t\r")
				if clTrimmed == "" {
					childLines = append(childLines, "")
					i++
					continue
				}
				ci := countIndent(clTrimmed)
				if childIndent == -1 && ci > 0 {
					childIndent = ci
				}
				if childIndent > 0 && ci < childIndent {
					break
				}
				childLines = append(childLines, cl)
				i++
			}
			if len(childLines) > 0 {
				// Check if it's a list (starts with "  -")
				firstNonEmpty := ""
				for _, cl := range childLines {
					if strings.TrimSpace(cl) != "" {
						firstNonEmpty = strings.TrimSpace(cl)
						break
					}
				}
				if strings.HasPrefix(firstNonEmpty, "- ") || firstNonEmpty == "-" {
					// Parse as list
					unindented := make([]string, len(childLines))
					for j, cl := range childLines {
						if len(cl) >= childIndent {
							unindented[j] = cl[childIndent:]
						} else {
							unindented[j] = cl
						}
					}
					list, err := parseYAMLList(unindented)
					if err == nil {
						result[key] = list
					}
				} else {
					// Parse as nested map
					unindented := make([]string, len(childLines))
					for j, cl := range childLines {
						if len(cl) >= childIndent {
							unindented[j] = cl[childIndent:]
						} else {
							unindented[j] = cl
						}
					}
					nested, err := parseYAMLDoc(strings.Join(unindented, "\n"))
					if err == nil {
						result[key] = nested
					}
				}
			}
		} else {
			result[key] = parseYAMLScalar(val)
			i++
		}
	}
	return result, nil
}

// parseYAMLList parses a list of items (each starting with "- ").
func parseYAMLList(lines []string) ([]interface{}, error) {
	var result []interface{}
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			i++
			continue
		}
		if !strings.HasPrefix(trimmed, "- ") && trimmed != "-" {
			i++
			continue
		}
		// Inline scalar: "- value"
		itemContent := ""
		if strings.HasPrefix(trimmed, "- ") {
			itemContent = strings.TrimSpace(trimmed[2:])
		}

		if itemContent == "" || itemContent == "{}" {
			// Inline empty map or multi-line item
			i++
			// Collect child lines
			childIndent := -1
			var childLines []string
			for i < len(lines) {
				cl := lines[i]
				clTrimmed := strings.TrimRight(cl, " \t\r")
				if clTrimmed == "" {
					childLines = append(childLines, "")
					i++
					continue
				}
				ci := countIndent(clTrimmed)
				if childIndent == -1 && ci > 0 {
					childIndent = ci
				}
				if childIndent > 0 && ci < childIndent {
					break
				}
				if ci == 0 && (strings.HasPrefix(strings.TrimSpace(clTrimmed), "- ") || strings.TrimSpace(clTrimmed) == "-") {
					break
				}
				childLines = append(childLines, cl)
				i++
			}
			if len(childLines) == 0 {
				result = append(result, map[string]interface{}{})
				continue
			}
			// De-indent child lines
			unindented := make([]string, len(childLines))
			for j, cl := range childLines {
				if childIndent > 0 && len(cl) >= childIndent {
					unindented[j] = cl[childIndent:]
				} else {
					unindented[j] = cl
				}
			}
			nested, err := parseYAMLDoc(strings.Join(unindented, "\n"))
			if err != nil {
				result = append(result, map[string]interface{}{})
			} else {
				result = append(result, nested)
			}
			continue
		}

		// Check if itemContent looks like a map "{key: val, ...}"
		if strings.HasPrefix(itemContent, "{") && strings.HasSuffix(itemContent, "}") {
			m, err := parseInlineMap(itemContent)
			if err == nil {
				result = append(result, m)
				i++
				continue
			}
		}

		// Multi-line item with first key on same line
		colonIdx := strings.Index(itemContent, ":")
		if colonIdx != -1 {
			// First key is on the same line, rest are indented below
			firstKey := strings.TrimSpace(itemContent[:colonIdx])
			firstVal := strings.TrimSpace(itemContent[colonIdx+1:])
			i++

			// Collect more lines
			childIndent := -1
			var childLines []string
			for i < len(lines) {
				cl := lines[i]
				clTrimmed := strings.TrimRight(cl, " \t\r")
				if clTrimmed == "" {
					childLines = append(childLines, "")
					i++
					continue
				}
				ci := countIndent(clTrimmed)
				if childIndent == -1 && ci > 0 {
					childIndent = ci
				}
				if childIndent > 0 && ci < childIndent {
					break
				}
				ts := strings.TrimSpace(clTrimmed)
				if ci == 0 && (strings.HasPrefix(ts, "- ") || ts == "-") {
					break
				}
				childLines = append(childLines, cl)
				i++
			}

			unindented := make([]string, len(childLines))
			for j, cl := range childLines {
				if childIndent > 0 && len(cl) >= childIndent {
					unindented[j] = cl[childIndent:]
				} else {
					unindented[j] = cl
				}
			}
			childMap, err := parseYAMLDoc(strings.Join(unindented, "\n"))
			if err != nil {
				childMap = map[string]interface{}{}
			}
			item := map[string]interface{}{firstKey: parseYAMLScalar(firstVal)}
			for k, v := range childMap {
				item[k] = v
			}
			result = append(result, item)
			continue
		}

		// Plain scalar list item
		result = append(result, parseYAMLScalar(itemContent))
		i++
	}
	return result, nil
}

// parseInlineMap parses "{key: val, key2: val2}" style.
func parseInlineMap(s string) (map[string]interface{}, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return nil, fmt.Errorf("not an inline map")
	}
	s = s[1 : len(s)-1]
	result := make(map[string]interface{})
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		idx := strings.Index(part, ":")
		if idx == -1 {
			continue
		}
		k := strings.TrimSpace(part[:idx])
		v := strings.TrimSpace(part[idx+1:])
		result[k] = parseYAMLScalar(v)
	}
	return result, nil
}

func parseYAMLScalar(s string) interface{} {
	s = strings.TrimSpace(s)
	// Strip inline comment
	if idx := strings.Index(s, " #"); idx != -1 {
		s = strings.TrimSpace(s[:idx])
	}
	// Quoted string
	if (strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`)) ||
		(strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) {
		return s[1 : len(s)-1]
	}
	switch strings.ToLower(s) {
	case "true", "yes", "on":
		return true
	case "false", "no", "off":
		return false
	case "null", "~", "":
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return float64(n)
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

func countIndent(line string) int {
	n := 0
	for _, c := range line {
		if c == ' ' {
			n++
		} else if c == '\t' {
			n += 2
		} else {
			break
		}
	}
	return n
}
