package proxy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ParseContent auto-detects the subscription format and parses proxies.
func ParseContent(content, hint string) ([]Proxy, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return []Proxy{}, nil
	}
	// Try base64 decode if it looks encoded
	if !strings.ContainsAny(content, "\n :{}[]") && len(content) > 20 {
		if decoded := tryBase64Decode(content); decoded != "" {
			content = decoded
		}
	}
	switch {
	case strings.HasPrefix(content, "proxies:") || strings.Contains(content, "\nproxies:"):
		return parseClashYAML(content)
	case isURIList(content):
		return parseURIList(content)
	case looksLikeSurge(content):
		return parseSurgeConfig(content)
	default:
		proxies, err := parseClashYAML(content)
		if err == nil && len(proxies) > 0 {
			return proxies, nil
		}
		return parseURIList(content)
	}
}

func tryBase64Decode(s string) string {
	padded := s
	for len(padded)%4 != 0 {
		padded += "="
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding} {
		decoded, err := enc.DecodeString(padded)
		if err == nil {
			result := string(decoded)
			if strings.Contains(result, "://") || strings.Contains(result, "proxies:") {
				return result
			}
		}
	}
	return ""
}

func isURIList(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "://") {
			return true
		}
	}
	return false
}

func looksLikeSurge(s string) bool {
	return strings.Contains(s, "[Proxy]") ||
		strings.Contains(s, "= vmess,") ||
		strings.Contains(s, "= trojan,") ||
		strings.Contains(s, "= ss,")
}

// ---- Clash YAML ----

type clashConfig struct {
	Proxies []map[string]interface{}
}

func parseClashYAML(content string) ([]Proxy, error) {
	var cfg clashConfig
	if err := UnmarshalYAML([]byte(content), &cfg); err != nil {
		return nil, fmt.Errorf("yaml parse: %w", err)
	}
	proxies := make([]Proxy, 0, len(cfg.Proxies))
	for _, p := range cfg.Proxies {
		proxy, err := mapToProxy(p)
		if err != nil {
			continue
		}
		proxies = append(proxies, proxy)
	}
	return proxies, nil
}

func mapToProxy(m map[string]interface{}) (Proxy, error) {
	p := Proxy{}
	p.Name, _ = m["name"].(string)
	p.Type, _ = m["type"].(string)
	p.Server, _ = m["server"].(string)
	p.Port = toInt(m["port"])
	p.Password, _ = m["password"].(string)
	p.UDP, _ = m["udp"].(bool)
	p.Cipher, _ = m["cipher"].(string)
	if p.Cipher == "" {
		p.Cipher, _ = m["method"].(string)
	}
	p.UUID, _ = m["uuid"].(string)
	p.AlterID = toInt(m["alterId"])
	p.Network, _ = m["network"].(string)
	p.TLS, _ = m["tls"].(bool)
	p.SNI, _ = m["sni"].(string)
	if p.SNI == "" {
		p.SNI, _ = m["servername"].(string)
	}
	p.SkipCertVerify, _ = m["skip-cert-verify"].(bool)
	p.Fingerprint, _ = m["fingerprint"].(string)
	p.Flow, _ = m["flow"].(string)
	p.Username, _ = m["username"].(string)
	p.Plugin, _ = m["plugin"].(string)
	p.GRPCServiceName, _ = m["grpc-service-name"].(string)
	p.Protocol, _ = m["protocol"].(string)
	p.ProtocolParam, _ = m["protocol-param"].(string)
	p.Obfs, _ = m["obfs"].(string)
	p.ObfsParam, _ = m["obfs-param"].(string)
	p.ObfsPass, _ = m["obfs-password"].(string)
	p.AuthStr, _ = m["auth_str"].(string)
	p.UpMbps = toInt(m["up"])
	p.DownMbps = toInt(m["down"])
	p.Insecure, _ = m["insecure"].(bool)
	p.PrivateKey, _ = m["private-key"].(string)
	p.PublicKey, _ = m["public-key"].(string)
	p.Token, _ = m["token"].(string)
	p.CongCtrl, _ = m["congestion-controller"].(string)
	p.Version = toInt(m["version"])
	p.MTU = toInt(m["mtu"])

	// WS opts
	if wsOpts, ok := m["ws-opts"].(map[string]interface{}); ok {
		p.WSPath, _ = wsOpts["path"].(string)
		if hdrs, ok := wsOpts["headers"].(map[string]interface{}); ok {
			p.WSHeaders = make(map[string]string)
			for k, v := range hdrs {
				if s, ok := v.(string); ok {
					p.WSHeaders[k] = s
				}
			}
		}
	}
	if p.WSPath == "" {
		p.WSPath, _ = m["ws-path"].(string)
	}

	// gRPC opts
	if grpcOpts, ok := m["grpc-opts"].(map[string]interface{}); ok {
		p.GRPCServiceName, _ = grpcOpts["grpc-service-name"].(string)
	}

	// Reality opts
	if ro, ok := m["reality-opts"].(map[string]interface{}); ok {
		p.RealityOpts = ro
	}

	// Plugin opts
	if po, ok := m["plugin-opts"].(map[string]interface{}); ok {
		p.PluginOptsMap = po
	}

	// ALPN
	if alpnRaw, ok := m["alpn"]; ok {
		switch v := alpnRaw.(type) {
		case []interface{}:
			for _, a := range v {
				if s, ok := a.(string); ok {
					p.ALPN = append(p.ALPN, s)
				}
			}
		}
	}

	// DNS
	if dnsRaw, ok := m["dns"]; ok {
		switch v := dnsRaw.(type) {
		case []interface{}:
			for _, a := range v {
				if s, ok := a.(string); ok {
					p.DNS = append(p.DNS, s)
				}
			}
		}
	}

	// Collect extra fields
	knownKeys := map[string]bool{
		"name": true, "type": true, "server": true, "port": true, "password": true,
		"udp": true, "cipher": true, "method": true, "uuid": true, "alterId": true,
		"network": true, "tls": true, "sni": true, "servername": true,
		"skip-cert-verify": true, "fingerprint": true, "flow": true,
		"ws-path": true, "ws-opts": true, "username": true, "plugin": true,
		"plugin-opts": true, "grpc-opts": true, "grpc-service-name": true,
		"protocol": true, "protocol-param": true, "obfs": true, "obfs-param": true,
		"auth_str": true, "up": true, "down": true, "insecure": true,
		"private-key": true, "public-key": true, "token": true,
		"congestion-controller": true, "version": true, "mtu": true,
		"alpn": true, "dns": true, "reality-opts": true, "preshared-key": true,
		"ip": true, "ipv6": true, "peers": true,
	}
	p.Extra = make(map[string]interface{})
	for k, v := range m {
		if !knownKeys[k] {
			p.Extra[k] = v
		}
	}
	if p.Name == "" || p.Type == "" || p.Server == "" {
		return p, fmt.Errorf("incomplete proxy: name=%q type=%q server=%q", p.Name, p.Type, p.Server)
	}
	return p, nil
}

func toInt(v interface{}) int {
	if v == nil {
		return 0
	}
	switch t := v.(type) {
	case int:
		return t
	case float64:
		return int(t)
	case float32:
		return int(t)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	}
	return 0
}

// ---- URI list parser ----

func parseURIList(content string) ([]Proxy, error) {
	var proxies []Proxy
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		p, err := parseURI(line)
		if err != nil {
			continue
		}
		proxies = append(proxies, p)
	}
	return proxies, nil
}

func parseURI(uri string) (Proxy, error) {
	idx := strings.Index(uri, "://")
	if idx == -1 {
		return Proxy{}, fmt.Errorf("not a URI")
	}
	scheme := strings.ToLower(uri[:idx])
	switch scheme {
	case "ss":
		return parseSSURI(uri)
	case "ssr":
		return parseSSRURI(uri)
	case "vmess":
		return parseVmessURI(uri)
	case "vless":
		return parseVlessURI(uri)
	case "trojan":
		return parseTrojanURI(uri)
	case "hysteria":
		return parseHysteriaURI(uri)
	case "hysteria2", "hy2":
		return parseHysteria2URI(uri)
	case "tuic":
		return parseTuicURI(uri)
	case "http", "https":
		return parseHTTPURI(uri)
	case "socks", "socks5":
		return parseSOCKS5URI(uri)
	default:
		return Proxy{}, fmt.Errorf("unknown scheme: %s", scheme)
	}
}

func decodeBase64Safe(s string) string {
	s = strings.TrimSpace(s)
	padded := s
	for len(padded)%4 != 0 {
		padded += "="
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding} {
		if decoded, err := enc.DecodeString(padded); err == nil {
			return string(decoded)
		}
	}
	return s
}

func parseSSURI(uri string) (Proxy, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return Proxy{}, err
	}
	name, _ := url.QueryUnescape(u.Fragment)
	if name == "" {
		name = u.Host
	}
	var cipher, password string
	userinfo := u.User.Username()
	decoded := decodeBase64Safe(userinfo)
	if strings.Contains(decoded, ":") {
		parts := strings.SplitN(decoded, ":", 2)
		cipher, password = parts[0], parts[1]
	} else {
		cipher = userinfo
		password, _ = u.User.Password()
	}
	port, _ := strconv.Atoi(u.Port())
	return Proxy{Name: name, Type: "ss", Server: u.Hostname(), Port: port,
		Cipher: cipher, Password: password}, nil
}

func parseSSRURI(uri string) (Proxy, error) {
	raw := strings.TrimPrefix(uri, "ssr://")
	decoded := decodeBase64Safe(raw)
	parts := strings.Split(decoded, ":")
	if len(parts) < 6 {
		return Proxy{}, fmt.Errorf("invalid SSR URI")
	}
	host := parts[0]
	port, _ := strconv.Atoi(parts[1])
	protocol, cipher, obfs := parts[2], parts[3], parts[4]
	rest := strings.Join(parts[5:], ":")
	var passB64, queryStr string
	if qIdx := strings.Index(rest, "/?"); qIdx != -1 {
		passB64, queryStr = rest[:qIdx], rest[qIdx+2:]
	} else if qIdx := strings.Index(rest, "?"); qIdx != -1 {
		passB64, queryStr = rest[:qIdx], rest[qIdx+1:]
	} else {
		passB64 = rest
	}
	password := decodeBase64Safe(passB64)
	params, _ := url.ParseQuery(queryStr)
	name := decodeBase64Safe(params.Get("remarks"))
	if name == "" {
		name = fmt.Sprintf("%s:%d", host, port)
	}
	return Proxy{
		Name: name, Type: "ssr", Server: host, Port: port,
		Cipher: cipher, Password: password, Protocol: protocol,
		ProtocolParam: decodeBase64Safe(params.Get("protoparam")),
		Obfs: obfs, ObfsParam: decodeBase64Safe(params.Get("obfsparam")),
	}, nil
}

func parseVmessURI(uri string) (Proxy, error) {
	raw := strings.TrimPrefix(uri, "vmess://")
	decoded := decodeBase64Safe(raw)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(decoded), &m); err != nil {
		return Proxy{}, fmt.Errorf("vmess JSON: %w", err)
	}
	name, _ := m["ps"].(string)
	if name == "" {
		name, _ = m["add"].(string)
	}
	server, _ := m["add"].(string)
	port := toInt(m["port"])
	uuid, _ := m["id"].(string)
	network, _ := m["net"].(string)
	tlsStr, _ := m["tls"].(string)
	sni, _ := m["sni"].(string)
	wsPath, _ := m["path"].(string)
	host, _ := m["host"].(string)
	cipher, _ := m["scy"].(string)
	if cipher == "" {
		cipher = "auto"
	}
	p := Proxy{
		Name: name, Type: "vmess", Server: server, Port: port,
		UUID: uuid, AlterID: toInt(m["aid"]), Cipher: cipher,
		Network: network, TLS: tlsStr == "tls", SNI: sni, WSPath: wsPath,
	}
	if host != "" {
		p.WSHeaders = map[string]string{"Host": host}
	}
	return p, nil
}

func parseVlessURI(uri string) (Proxy, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return Proxy{}, err
	}
	name, _ := url.QueryUnescape(u.Fragment)
	port, _ := strconv.Atoi(u.Port())
	q := u.Query()
	return Proxy{
		Name: name, Type: "vless", Server: u.Hostname(), Port: port,
		UUID: u.User.Username(), Network: q.Get("type"), Flow: q.Get("flow"),
		SNI: q.Get("sni"), Fingerprint: q.Get("fp"),
		GRPCServiceName: q.Get("serviceName"), WSPath: q.Get("path"),
		TLS: q.Get("security") == "tls" || q.Get("security") == "reality",
	}, nil
}

func parseTrojanURI(uri string) (Proxy, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return Proxy{}, err
	}
	name, _ := url.QueryUnescape(u.Fragment)
	if name == "" {
		name = u.Host
	}
	port, _ := strconv.Atoi(u.Port())
	q := u.Query()
	p := Proxy{
		Name: name, Type: "trojan", Server: u.Hostname(), Port: port,
		Password: u.User.Username(), SNI: q.Get("sni"),
		Network: q.Get("type"), GRPCServiceName: q.Get("serviceName"),
		WSPath: q.Get("path"), TLS: true,
	}
	if sni := q.Get("peer"); sni != "" && p.SNI == "" {
		p.SNI = sni
	}
	return p, nil
}

func parseHysteriaURI(uri string) (Proxy, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return Proxy{}, err
	}
	name, _ := url.QueryUnescape(u.Fragment)
	port, _ := strconv.Atoi(u.Port())
	q := u.Query()
	up, _ := strconv.Atoi(q.Get("upmbps"))
	down, _ := strconv.Atoi(q.Get("downmbps"))
	return Proxy{
		Name: name, Type: "hysteria", Server: u.Hostname(), Port: port,
		AuthStr: q.Get("auth"), Obfs: q.Get("obfs"), SNI: q.Get("peer"),
		Insecure: q.Get("insecure") == "1", UpMbps: up, DownMbps: down,
	}, nil
}

func parseHysteria2URI(uri string) (Proxy, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return Proxy{}, err
	}
	name, _ := url.QueryUnescape(u.Fragment)
	port, _ := strconv.Atoi(u.Port())
	q := u.Query()
	password := u.User.Username()
	if pw, ok := u.User.Password(); ok {
		password = pw
	}
	return Proxy{
		Name: name, Type: "hysteria2", Server: u.Hostname(), Port: port,
		Password: password, Obfs: q.Get("obfs"), ObfsPass: q.Get("obfs-password"),
		SNI: q.Get("sni"), Insecure: q.Get("insecure") == "1",
	}, nil
}

func parseTuicURI(uri string) (Proxy, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return Proxy{}, err
	}
	name, _ := url.QueryUnescape(u.Fragment)
	port, _ := strconv.Atoi(u.Port())
	q := u.Query()
	return Proxy{
		Name: name, Type: "tuic", Server: u.Hostname(), Port: port,
		UUID: u.User.Username(), Password: q.Get("password"),
		CongCtrl: q.Get("congestion_control"), SNI: q.Get("sni"),
	}, nil
}

func parseHTTPURI(uri string) (Proxy, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return Proxy{}, err
	}
	name, _ := url.QueryUnescape(u.Fragment)
	if name == "" {
		name = u.Host
	}
	port, _ := strconv.Atoi(u.Port())
	if port == 0 {
		if u.Scheme == "https" {
			port = 443
		} else {
			port = 80
		}
	}
	password, _ := u.User.Password()
	return Proxy{
		Name: name, Type: "http", Server: u.Hostname(), Port: port,
		Username: u.User.Username(), Password: password, TLS: u.Scheme == "https",
	}, nil
}

func parseSOCKS5URI(uri string) (Proxy, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return Proxy{}, err
	}
	name, _ := url.QueryUnescape(u.Fragment)
	if name == "" {
		name = u.Host
	}
	port, _ := strconv.Atoi(u.Port())
	password, _ := u.User.Password()
	return Proxy{
		Name: name, Type: "socks5", Server: u.Hostname(), Port: port,
		Username: u.User.Username(), Password: password,
	}, nil
}

// ---- Surge config parser ----

func parseSurgeConfig(content string) ([]Proxy, error) {
	inProxy := false
	var proxies []Proxy
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "[Proxy]" {
			inProxy = true
			continue
		}
		if strings.HasPrefix(line, "[") {
			inProxy = false
			continue
		}
		if !inProxy || line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p, err := parseSurgeLine(line)
		if err != nil {
			continue
		}
		proxies = append(proxies, p)
	}
	return proxies, nil
}

func parseSurgeLine(line string) (Proxy, error) {
	eq := strings.Index(line, "=")
	if eq == -1 {
		return Proxy{}, fmt.Errorf("no '='")
	}
	name := strings.TrimSpace(line[:eq])
	rest := strings.TrimSpace(line[eq+1:])
	parts := splitCSV(rest)
	if len(parts) < 3 {
		return Proxy{}, fmt.Errorf("too few parts")
	}
	typ := strings.ToLower(strings.TrimSpace(parts[0]))
	server := strings.TrimSpace(parts[1])
	port, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
	params := make(map[string]string)
	for _, part := range parts[3:] {
		part = strings.TrimSpace(part)
		if kv := strings.SplitN(part, "=", 2); len(kv) == 2 {
			params[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	p := Proxy{Name: name, Server: server, Port: port}
	switch typ {
	case "ss", "custom":
		p.Type = "ss"
		p.Cipher = params["encrypt-method"]
		p.Password = params["password"]
	case "vmess":
		p.Type = "vmess"
		p.UUID = params["username"]
	case "trojan":
		p.Type = "trojan"
		p.Password = params["password"]
		p.TLS = true
	case "http":
		p.Type = "http"
		p.Username = params["username"]
		p.Password = params["password"]
	case "https":
		p.Type = "http"
		p.TLS = true
		p.Username = params["username"]
		p.Password = params["password"]
	case "socks5":
		p.Type = "socks5"
		p.Username = params["username"]
		p.Password = params["password"]
	case "socks5-tls":
		p.Type = "socks5"
		p.TLS = true
		p.Username = params["username"]
		p.Password = params["password"]
	case "snell":
		p.Type = "snell"
		p.Password = params["psk"]
		p.Version, _ = strconv.Atoi(params["version"])
		p.Obfs = params["obfs"]
	default:
		return Proxy{}, fmt.Errorf("unknown Surge type: %s", typ)
	}
	if sni, ok := params["sni"]; ok {
		p.SNI = sni
	}
	if sv, ok := params["skip-cert-verify"]; ok {
		p.SkipCertVerify = sv == "true" || sv == "1"
	}
	if udp, ok := params["udp-relay"]; ok {
		p.UDP = udp == "true" || udp == "1"
	}
	return p, nil
}

func splitCSV(s string) []string {
	var result []string
	cur := strings.Builder{}
	inQuote := false
	for _, c := range s {
		switch {
		case c == '"':
			inQuote = !inQuote
		case c == ',' && !inQuote:
			result = append(result, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(c)
		}
	}
	result = append(result, cur.String())
	return result
}
