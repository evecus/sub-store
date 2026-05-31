package proxy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ProcessProxies applies all operators defined in the subscription/collection config.
func ProcessProxies(proxies []Proxy, ops []interface{}) ([]Proxy, []string) {
	logs := []string{}
	result := proxies

	for _, opRaw := range ops {
		op, ok := opRaw.(map[string]interface{})
		if !ok {
			continue
		}
		typ, _ := op["type"].(string)
		args := op["args"]

		before := len(result)
		var err error
		result, err = applyOperator(result, typ, args)
		if err != nil {
			logs = append(logs, fmt.Sprintf("[operator:%s] error: %v", typ, err))
			continue
		}
		after := len(result)
		if before != after {
			logs = append(logs, fmt.Sprintf("[operator:%s] %d -> %d nodes", typ, before, after))
		} else {
			logs = append(logs, fmt.Sprintf("[operator:%s] applied to %d nodes", typ, after))
		}
	}
	return result, logs
}

func applyOperator(proxies []Proxy, typ string, args interface{}) ([]Proxy, error) {
	// Normalize: handle both UPPER_SNAKE and CamelCase and mixed
	norm := strings.ToUpper(strings.ReplaceAll(typ, "_", ""))
	switch norm {
	// ---- Filters ----
	case "KEYWORDFILTER":
		return keywordFilter(proxies, args, false)
	case "KEYWORDDELETEOPERATOR":
		return keywordFilter(proxies, args, true)
	case "REGEXFILTER":
		return regexFilter(proxies, args, false)
	case "REGEXDELETEOPERATOR":
		return regexFilter(proxies, args, true)
	case "TYPEFILTER":
		return typeFilter(proxies, args, false)
	case "TYPEDELETEOPERATOR":
		return typeFilter(proxies, args, true)
	case "REGIONFILTER":
		return regionFilter(proxies, args)
	case "PORTFILTER":
		return portFilter(proxies, args, false)
	case "PORTDELETEOPERATOR":
		return portFilter(proxies, args, true)
	case "SCRIPTFILTER":
		return proxies, nil

	// ---- Operators ----
	case "KEYWORDRENAMEOPERATOR":
		return keywordRename(proxies, args)
	case "REGEXRENAMEOPERATOR":
		return regexRename(proxies, args)
	case "FLAGOPERATOR":
		return flagOperator(proxies, args)
	case "REMOVEFLAGOPERATOR":
		return removeFlagOperator(proxies)
	case "SORTOPERATOR":
		return sortOperator(proxies, args)
	case "DEDUPLICATEOPERATOR":
		return deduplicateOperator(proxies)
	case "HANDLEDUPLICATEOPERATOR":
		return handleDuplicateOperator(proxies, args)
	case "SCRIPTOPERATOR":
		return proxies, nil
	case "SETPROPERTYOPERATOR":
		return setPropertyOperator(proxies, args)
	case "RESOLVEHOSTNAMEOPERATOR":
		return proxies, nil
	case "SNELLVERSIONOPERATOR":
		return snellVersionOperator(proxies, args)
	case "QUOTAOPERATOR":
		return quotaOperator(proxies, args)
	default:
		return proxies, nil
	}
}

// ---- Filter implementations ----

func keywordFilter(proxies []Proxy, args interface{}, invert bool) ([]Proxy, error) {
	keywords := toStringSlice(args)
	if len(keywords) == 0 {
		return proxies, nil
	}
	var result []Proxy
	for _, p := range proxies {
		matched := false
		for _, kw := range keywords {
			if strings.Contains(p.Name, kw) {
				matched = true
				break
			}
		}
		if invert {
			if !matched {
				result = append(result, p)
			}
		} else {
			if matched {
				result = append(result, p)
			}
		}
	}
	if result == nil {
		result = []Proxy{}
	}
	return result, nil
}

func regexFilter(proxies []Proxy, args interface{}, invert bool) ([]Proxy, error) {
	patterns := toStringSlice(args)
	if len(patterns) == 0 {
		return proxies, nil
	}
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		compiled = append(compiled, re)
	}
	var result []Proxy
	for _, p := range proxies {
		matched := false
		for _, re := range compiled {
			if re.MatchString(p.Name) {
				matched = true
				break
			}
		}
		if invert {
			if !matched {
				result = append(result, p)
			}
		} else {
			if matched {
				result = append(result, p)
			}
		}
	}
	if result == nil {
		result = []Proxy{}
	}
	return result, nil
}

func typeFilter(proxies []Proxy, args interface{}, invert bool) ([]Proxy, error) {
	types := toStringSlice(args)
	if len(types) == 0 {
		return proxies, nil
	}
	typeSet := make(map[string]bool)
	for _, t := range types {
		typeSet[strings.ToLower(t)] = true
	}
	var result []Proxy
	for _, p := range proxies {
		matched := typeSet[strings.ToLower(p.Type)]
		if invert {
			if !matched {
				result = append(result, p)
			}
		} else {
			if matched {
				result = append(result, p)
			}
		}
	}
	if result == nil {
		result = []Proxy{}
	}
	return result, nil
}

func regionFilter(proxies []Proxy, args interface{}) ([]Proxy, error) {
	// Region filter uses emoji/country code matching against proxy name
	regions := toStringSlice(args)
	if len(regions) == 0 {
		return proxies, nil
	}
	var result []Proxy
	for _, p := range proxies {
		for _, region := range regions {
			if strings.Contains(p.Name, region) ||
				strings.Contains(strings.ToLower(p.Name), strings.ToLower(region)) {
				result = append(result, p)
				break
			}
		}
	}
	if result == nil {
		result = []Proxy{}
	}
	return result, nil
}

func portFilter(proxies []Proxy, args interface{}, invert bool) ([]Proxy, error) {
	ports := toIntSlice(args)
	if len(ports) == 0 {
		return proxies, nil
	}
	portSet := make(map[int]bool)
	for _, pt := range ports {
		portSet[pt] = true
	}
	var result []Proxy
	for _, p := range proxies {
		matched := portSet[p.Port]
		if invert {
			if !matched {
				result = append(result, p)
			}
		} else {
			if matched {
				result = append(result, p)
			}
		}
	}
	if result == nil {
		result = []Proxy{}
	}
	return result, nil
}

// ---- Operator implementations ----

func keywordRename(proxies []Proxy, args interface{}) ([]Proxy, error) {
	// args: [{old: "keyword", new: "replacement"}, ...]
	rules := toMapSlice(args)
	for i := range proxies {
		for _, rule := range rules {
			old, _ := rule["old"].(string)
			newStr, _ := rule["new"].(string)
			if old == "" {
				continue
			}
			proxies[i].Name = strings.ReplaceAll(proxies[i].Name, old, newStr)
		}
	}
	return proxies, nil
}

func regexRename(proxies []Proxy, args interface{}) ([]Proxy, error) {
	// args: [{expr: "regex", new: "replacement"}, ...]
	rules := toMapSlice(args)
	compiled := make([]struct {
		re  *regexp.Regexp
		new string
	}, 0, len(rules))
	for _, rule := range rules {
		expr, _ := rule["expr"].(string)
		newStr, _ := rule["new"].(string)
		if expr == "" {
			continue
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			continue
		}
		compiled = append(compiled, struct {
			re  *regexp.Regexp
			new string
		}{re, newStr})
	}
	for i := range proxies {
		for _, rule := range compiled {
			proxies[i].Name = rule.re.ReplaceAllString(proxies[i].Name, rule.new)
		}
	}
	return proxies, nil
}

func flagOperator(proxies []Proxy, args interface{}) ([]Proxy, error) {
	// args: {mode: "front|back"} - adds country flag emoji to proxy names
	// Simplified: we just pass through since we don't have GeoIP
	return proxies, nil
}

func removeFlagOperator(proxies []Proxy) ([]Proxy, error) {
	// Remove flag emoji from proxy names
	for i := range proxies {
		proxies[i].Name = removeFlagEmoji(proxies[i].Name)
	}
	return proxies, nil
}

func sortOperator(proxies []Proxy, args interface{}) ([]Proxy, error) {
	// args: {base: "name|server"} or just sort by name
	base := "name"
	if m, ok := args.(map[string]interface{}); ok {
		if b, ok := m["base"].(string); ok {
			base = b
		}
	}
	sorted := make([]Proxy, len(proxies))
	copy(sorted, proxies)
	sort.Slice(sorted, func(i, j int) bool {
		switch base {
		case "server":
			return sorted[i].Server < sorted[j].Server
		case "port":
			return sorted[i].Port < sorted[j].Port
		default:
			return sorted[i].Name < sorted[j].Name
		}
	})
	return sorted, nil
}

func deduplicateOperator(proxies []Proxy) ([]Proxy, error) {
	seen := make(map[string]bool)
	var result []Proxy
	for _, p := range proxies {
		key := fmt.Sprintf("%s:%s:%d", p.Type, p.Server, p.Port)
		if !seen[key] {
			seen[key] = true
			result = append(result, p)
		}
	}
	if result == nil {
		result = []Proxy{}
	}
	return result, nil
}

func handleDuplicateOperator(proxies []Proxy, args interface{}) ([]Proxy, error) {
	// args: {action: "rename|remove", template: "${name} ${index}"}
	m, _ := args.(map[string]interface{})
	action := "rename"
	template := "${name} ${index}"
	if m != nil {
		if a, ok := m["action"].(string); ok {
			action = a
		}
		if t, ok := m["template"].(string); ok {
			template = t
		}
	}

	// Count occurrences
	nameCount := make(map[string]int)
	for _, p := range proxies {
		nameCount[p.Name]++
	}

	if action == "remove" {
		var result []Proxy
		for _, p := range proxies {
			if nameCount[p.Name] == 1 {
				result = append(result, p)
			}
		}
		if result == nil {
			result = []Proxy{}
		}
		return result, nil
	}

	// Rename duplicates
	nameIndex := make(map[string]int)
	for i := range proxies {
		name := proxies[i].Name
		if nameCount[name] > 1 {
			nameIndex[name]++
			idx := nameIndex[name]
			newName := strings.ReplaceAll(template, "${name}", name)
			newName = strings.ReplaceAll(newName, "${index}", fmt.Sprintf("%d", idx))
			proxies[i].Name = newName
		}
	}
	return proxies, nil
}

func setPropertyOperator(proxies []Proxy, args interface{}) ([]Proxy, error) {
	// args: {key: "udp", value: true}
	m, ok := args.(map[string]interface{})
	if !ok {
		return proxies, nil
	}
	key, _ := m["key"].(string)
	value := m["value"]
	if key == "" {
		return proxies, nil
	}
	for i := range proxies {
		switch key {
		case "udp":
			if b, ok := value.(bool); ok {
				proxies[i].UDP = b
			}
		case "tls":
			if b, ok := value.(bool); ok {
				proxies[i].TLS = b
			}
		case "skip-cert-verify":
			if b, ok := value.(bool); ok {
				proxies[i].SkipCertVerify = b
			}
		case "sni":
			if s, ok := value.(string); ok {
				proxies[i].SNI = s
			}
		}
	}
	return proxies, nil
}

func snellVersionOperator(proxies []Proxy, args interface{}) ([]Proxy, error) {
	version := 0
	if m, ok := args.(map[string]interface{}); ok {
		if v, ok := m["version"].(float64); ok {
			version = int(v)
		}
	}
	for i := range proxies {
		if proxies[i].Type == "snell" {
			proxies[i].Version = version
		}
	}
	return proxies, nil
}

func quotaOperator(proxies []Proxy, args interface{}) ([]Proxy, error) {
	// Limit number of proxies
	quota := 0
	if m, ok := args.(map[string]interface{}); ok {
		if q, ok := m["quota"].(float64); ok {
			quota = int(q)
		}
	} else if q, ok := args.(float64); ok {
		quota = int(q)
	}
	if quota > 0 && len(proxies) > quota {
		return proxies[:quota], nil
	}
	return proxies, nil
}

// ---- Helpers ----

func toStringSlice(args interface{}) []string {
	if args == nil {
		return nil
	}
	switch v := args.(type) {
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return v
	case string:
		return []string{v}
	}
	return nil
}

func toIntSlice(args interface{}) []int {
	if args == nil {
		return nil
	}
	switch v := args.(type) {
	case []interface{}:
		result := make([]int, 0, len(v))
		for _, item := range v {
			if f, ok := item.(float64); ok {
				result = append(result, int(f))
			}
		}
		return result
	case []int:
		return v
	case float64:
		return []int{int(v)}
	}
	return nil
}

func toMapSlice(args interface{}) []map[string]interface{} {
	if args == nil {
		return nil
	}
	switch v := args.(type) {
	case []interface{}:
		result := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				result = append(result, m)
			}
		}
		return result
	case []map[string]interface{}:
		return v
	}
	return nil
}

func removeFlagEmoji(s string) string {
	// Remove leading/trailing flag emoji (Regional Indicator Symbols: U+1F1E0–U+1F1FF)
	result := []rune(s)
	start := 0
	end := len(result)
	// Strip leading flags (pairs of regional indicator symbols)
	for start+1 < end &&
		result[start] >= 0x1F1E0 && result[start] <= 0x1F1FF &&
		result[start+1] >= 0x1F1E0 && result[start+1] <= 0x1F1FF {
		start += 2
		// Skip space after flag
		if start < end && result[start] == ' ' {
			start++
		}
	}
	// Strip trailing flags
	for end-2 >= start &&
		result[end-2] >= 0x1F1E0 && result[end-2] <= 0x1F1FF &&
		result[end-1] >= 0x1F1E0 && result[end-1] <= 0x1F1FF {
		end -= 2
		if end > start && result[end-1] == ' ' {
			end--
		}
	}
	return strings.TrimSpace(string(result[start:end]))
}
