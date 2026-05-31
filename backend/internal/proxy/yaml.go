// Minimal YAML encoder sufficient for Clash proxy output.
// Supports map[string]interface{}, []interface{}, string, bool, int, float64.
package proxy

import (
	"fmt"
	"sort"
	"strings"
)

// MarshalYAML encodes v to YAML bytes.
func MarshalYAML(v interface{}) ([]byte, error) {
	var sb strings.Builder
	if err := encodeYAML(&sb, v, 0, false); err != nil {
		return nil, err
	}
	return []byte(sb.String()), nil
}

func encodeYAML(sb *strings.Builder, v interface{}, indent int, inSeq bool) error {
	prefix := strings.Repeat("  ", indent)
	seqPrefix := strings.Repeat("  ", indent)
	if inSeq {
		seqPrefix = strings.Repeat("  ", indent-1) + "- "
		prefix = strings.Repeat("  ", indent)
	}

	switch val := v.(type) {
	case nil:
		if inSeq {
			sb.WriteString(seqPrefix + "null\n")
		} else {
			sb.WriteString("null\n")
		}
	case bool:
		s := "false"
		if val {
			s = "true"
		}
		if inSeq {
			sb.WriteString(seqPrefix + s + "\n")
		} else {
			sb.WriteString(s + "\n")
		}
	case int:
		if inSeq {
			sb.WriteString(fmt.Sprintf("%s%d\n", seqPrefix, val))
		} else {
			sb.WriteString(fmt.Sprintf("%d\n", val))
		}
	case float64:
		if val == float64(int(val)) {
			if inSeq {
				sb.WriteString(fmt.Sprintf("%s%d\n", seqPrefix, int(val)))
			} else {
				sb.WriteString(fmt.Sprintf("%d\n", int(val)))
			}
		} else {
			if inSeq {
				sb.WriteString(fmt.Sprintf("%s%g\n", seqPrefix, val))
			} else {
				sb.WriteString(fmt.Sprintf("%g\n", val))
			}
		}
	case string:
		quoted := yamlQuoteString(val)
		if inSeq {
			sb.WriteString(seqPrefix + quoted + "\n")
		} else {
			sb.WriteString(quoted + "\n")
		}
	case []interface{}:
		if len(val) == 0 {
			if inSeq {
				sb.WriteString(seqPrefix + "[]\n")
			} else {
				sb.WriteString("[]\n")
			}
			break
		}
		if inSeq {
			// First element on same line as "- "
			sb.WriteString(seqPrefix)
			if err := encodeYAMLSeqItem(sb, val[0], indent); err != nil {
				return err
			}
			for _, item := range val[1:] {
				sb.WriteString(strings.Repeat("  ", indent-1) + "- ")
				if err := encodeYAMLSeqItem(sb, item, indent); err != nil {
					return err
				}
			}
		} else {
			for _, item := range val {
				sb.WriteString(prefix + "- ")
				if err := encodeYAMLSeqItem(sb, item, indent+1); err != nil {
					return err
				}
			}
		}
	case []string:
		iface := make([]interface{}, len(val))
		for i, s := range val {
			iface[i] = s
		}
		return encodeYAML(sb, iface, indent, inSeq)
	case []map[string]interface{}:
		iface := make([]interface{}, len(val))
		for i, m := range val {
			iface[i] = m
		}
		return encodeYAML(sb, iface, indent, inSeq)
	case map[string]interface{}:
		if len(val) == 0 {
			if inSeq {
				sb.WriteString(seqPrefix + "{}\n")
			} else {
				sb.WriteString("{}\n")
			}
			break
		}
		// Sort keys; put "name" and "type" first for readability
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			order := map[string]int{"name": 0, "type": 1, "server": 2, "port": 3}
			oi, oj := order[keys[i]], order[keys[j]]
			if oi != oj {
				return oi < oj
			}
			return keys[i] < keys[j]
		})
		first := true
		for _, k := range keys {
			if val[k] == nil {
				continue
			}
			var linePrefix string
			if inSeq && first {
				linePrefix = seqPrefix
				first = false
			} else {
				linePrefix = prefix
			}
			sb.WriteString(linePrefix + yamlKey(k) + ": ")
			child := val[k]
			switch cv := child.(type) {
			case map[string]interface{}, []interface{}, []string, []map[string]interface{}:
				sb.WriteString("\n")
				if err := encodeYAML(sb, cv, indent+1, false); err != nil {
					return err
				}
			default:
				if err := encodeYAML(sb, cv, indent+1, false); err != nil {
					return err
				}
			}
		}
	default:
		s := fmt.Sprintf("%v", val)
		if inSeq {
			sb.WriteString(seqPrefix + yamlQuoteString(s) + "\n")
		} else {
			sb.WriteString(yamlQuoteString(s) + "\n")
		}
	}
	return nil
}

func encodeYAMLSeqItem(sb *strings.Builder, item interface{}, indent int) error {
	switch v := item.(type) {
	case map[string]interface{}:
		// Inline map after "- "
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			order := map[string]int{"name": 0, "type": 1, "server": 2, "port": 3}
			oi, oj := order[keys[i]], order[keys[j]]
			if oi != oj {
				return oi < oj
			}
			return keys[i] < keys[j]
		})
		prefix := strings.Repeat("  ", indent)
		first := true
		for _, k := range keys {
			if v[k] == nil {
				continue
			}
			var linePrefix string
			if first {
				linePrefix = "" // already written "- "
				first = false
			} else {
				linePrefix = prefix
			}
			sb.WriteString(linePrefix + yamlKey(k) + ": ")
			child := v[k]
			switch cv := child.(type) {
			case map[string]interface{}, []interface{}, []string:
				sb.WriteString("\n")
				if err := encodeYAML(sb, cv, indent+1, false); err != nil {
					return err
				}
			default:
				if err := encodeYAML(sb, cv, 0, false); err != nil {
					return err
				}
			}
		}
	default:
		return encodeYAML(sb, item, 0, false)
	}
	return nil
}

func yamlKey(k string) string {
	if strings.ContainsAny(k, ": #{}[]|>&!%@`'\"") || strings.Contains(k, " ") {
		return `"` + strings.ReplaceAll(k, `"`, `\"`) + `"`
	}
	return k
}

func yamlQuoteString(s string) string {
	if s == "" {
		return `""`
	}
	// Strings that need quoting
	needsQuote := false
	for _, r := range s {
		if r == ':' || r == '#' || r == '{' || r == '}' || r == '[' || r == ']' ||
			r == ',' || r == '&' || r == '*' || r == '?' || r == '|' || r == '-' ||
			r == '<' || r == '>' || r == '=' || r == '!' || r == '%' || r == '@' ||
			r == '`' || r == '\'' || r == '"' || r == '\n' || r == '\r' {
			needsQuote = true
			break
		}
	}
	if s == "true" || s == "false" || s == "null" || s == "yes" || s == "no" || s == "on" || s == "off" {
		needsQuote = true
	}
	// Check if looks like number
	if len(s) > 0 && (s[0] >= '0' && s[0] <= '9' || s[0] == '-' || s[0] == '+') {
		needsQuote = true
	}
	if !needsQuote {
		return s
	}
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	escaped = strings.ReplaceAll(escaped, "\n", `\n`)
	escaped = strings.ReplaceAll(escaped, "\r", `\r`)
	return `"` + escaped + `"`
}
