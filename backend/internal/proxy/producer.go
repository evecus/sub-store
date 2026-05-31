package proxy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Produce converts proxies to the requested target format.
func Produce(proxies []Proxy, target string, opts map[string]interface{}) (string, error) {
	out, _, err := ProduceWithContentType(proxies, target, opts)
	return out, err
}

// ProduceWithContentType returns content + MIME type.
func ProduceWithContentType(proxies []Proxy, target string, opts map[string]interface{}) (string, string, error) {
	switch strings.ToUpper(target) {
	case "CLASH", "CLASHMETA", "META":
		out, err := produceClashMeta(proxies, opts)
		return out, "application/yaml; charset=utf-8", err
	case "SURGE", "SURGEMAC", "SURGE4":
		out, err := produceSurge(proxies, opts)
		return out, "text/plain; charset=utf-8", err
	case "QX", "QUANTUMULTX":
		out, err := produceQX(proxies, opts)
		return out, "text/plain; charset=utf-8", err
	case "LOON":
		out, err := produceLoon(proxies, opts)
		return out, "text/plain; charset=utf-8", err
	case "SHADOWROCKET":
		out, err := produceShadowrocket(proxies, opts)
		return out, "text/plain; charset=utf-8", err
	case "SINGBOX", "SING-BOX":
		out, err := produceSingBox(proxies, opts)
		return out, "application/json; charset=utf-8", err
	case "STASH":
		out, err := produceClashMeta(proxies, opts) // Stash uses Clash format
		return out, "application/yaml; charset=utf-8", err
	case "V2RAY", "V2RAYN":
		out, err := produceV2Ray(proxies, opts)
		return out, "text/plain; charset=utf-8", err
	case "JSON":
		out, err := produceJSON(proxies, opts)
		return out, "application/json; charset=utf-8", err
	default:
		out, err := produceClashMeta(proxies, opts)
		return out, "application/yaml; charset=utf-8", err
	}
}

// ---- ClashMeta YAML ----

func produceClashMeta(proxies []Proxy, opts map[string]interface{}) (string, error) {
	clashProxies := make([]interface{}, 0, len(proxies))
	for _, p := range proxies {
		m := proxyToClashMap(p)
		if m != nil {
			clashProxies = append(clashProxies, m)
		}
	}
	doc := map[string]interface{}{
		"proxies": clashProxies,
	}
	raw, err := MarshalYAML(doc)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func proxyToClashMap(p Proxy) map[string]interface{} {
	m := map[string]interface{}{
		"name":   p.Name,
		"type":   p.Type,
		"server": p.Server,
		"port":   p.Port,
	}
	if p.UDP {
		m["udp"] = true
	}
	switch p.Type {
	case "ss":
		m["cipher"] = p.Cipher
		m["password"] = p.Password
		if p.Plugin != "" {
			m["plugin"] = p.Plugin
		}
		if len(p.PluginOptsMap) > 0 {
			m["plugin-opts"] = p.PluginOptsMap
		}
	case "ssr":
		m["cipher"] = p.Cipher
		m["password"] = p.Password
		m["protocol"] = p.Protocol
		m["protocol-param"] = p.ProtocolParam
		m["obfs"] = p.Obfs
		m["obfs-param"] = p.ObfsParam
	case "vmess":
		m["uuid"] = p.UUID
		m["alterId"] = p.AlterID
		m["cipher"] = orDefault(p.Cipher, "auto")
		m["tls"] = p.TLS
		if p.Network != "" {
			m["network"] = p.Network
		}
		if p.SNI != "" {
			m["servername"] = p.SNI
		}
		if p.SkipCertVerify {
			m["skip-cert-verify"] = true
		}
		if p.WSPath != "" {
			wsOpts := map[string]interface{}{"path": p.WSPath}
			if len(p.WSHeaders) > 0 {
				hdrs := make(map[string]interface{})
				for k, v := range p.WSHeaders {
					hdrs[k] = v
				}
				wsOpts["headers"] = hdrs
			}
			m["ws-opts"] = wsOpts
		}
		if p.GRPCServiceName != "" {
			m["grpc-opts"] = map[string]interface{}{"grpc-service-name": p.GRPCServiceName}
		}
	case "vless":
		m["uuid"] = p.UUID
		m["tls"] = p.TLS
		if p.Flow != "" {
			m["flow"] = p.Flow
		}
		if p.Network != "" {
			m["network"] = p.Network
		}
		if p.SNI != "" {
			m["servername"] = p.SNI
		}
		if p.SkipCertVerify {
			m["skip-cert-verify"] = true
		}
		if p.Fingerprint != "" {
			m["fingerprint"] = p.Fingerprint
		}
		if p.WSPath != "" {
			m["ws-opts"] = map[string]interface{}{"path": p.WSPath}
		}
		if p.GRPCServiceName != "" {
			m["grpc-opts"] = map[string]interface{}{"grpc-service-name": p.GRPCServiceName}
		}
		if len(p.RealityOpts) > 0 {
			m["reality-opts"] = p.RealityOpts
		}
	case "trojan":
		m["password"] = p.Password
		if p.SNI != "" {
			m["sni"] = p.SNI
		}
		if p.SkipCertVerify {
			m["skip-cert-verify"] = true
		}
		if p.Network != "" {
			m["network"] = p.Network
		}
		if p.GRPCServiceName != "" {
			m["grpc-opts"] = map[string]interface{}{"grpc-service-name": p.GRPCServiceName}
		}
		if p.WSPath != "" {
			m["ws-opts"] = map[string]interface{}{"path": p.WSPath}
		}
	case "hysteria":
		if p.AuthStr != "" {
			m["auth_str"] = p.AuthStr
		}
		m["up"] = fmt.Sprintf("%d Mbps", p.UpMbps)
		m["down"] = fmt.Sprintf("%d Mbps", p.DownMbps)
		if p.Obfs != "" {
			m["obfs"] = p.Obfs
		}
		if p.SNI != "" {
			m["sni"] = p.SNI
		}
		if p.Insecure {
			m["skip-cert-verify"] = true
		}
	case "hysteria2":
		m["password"] = p.Password
		if p.Obfs != "" {
			m["obfs"] = p.Obfs
			if p.ObfsPass != "" {
				m["obfs-password"] = p.ObfsPass
			}
		}
		if p.SNI != "" {
			m["sni"] = p.SNI
		}
		if p.Insecure {
			m["skip-cert-verify"] = true
		}
	case "tuic":
		m["uuid"] = p.UUID
		m["password"] = p.Password
		if p.CongCtrl != "" {
			m["congestion-controller"] = p.CongCtrl
		}
		if p.SNI != "" {
			m["sni"] = p.SNI
		}
		if p.SkipCertVerify {
			m["skip-cert-verify"] = true
		}
	case "http":
		if p.Username != "" {
			m["username"] = p.Username
		}
		if p.Password != "" {
			m["password"] = p.Password
		}
		if p.TLS {
			m["tls"] = true
		}
	case "socks5":
		if p.Username != "" {
			m["username"] = p.Username
		}
		if p.Password != "" {
			m["password"] = p.Password
		}
		if p.TLS {
			m["tls"] = true
		}
	case "wireguard":
		m["private-key"] = p.PrivateKey
		m["public-key"] = p.PublicKey
		if p.IP != "" {
			m["ip"] = p.IP
		}
		if p.IPv6 != "" {
			m["ipv6"] = p.IPv6
		}
		if p.MTU > 0 {
			m["mtu"] = p.MTU
		}
		if len(p.DNS) > 0 {
			iface := make([]interface{}, len(p.DNS))
			for i, d := range p.DNS {
				iface[i] = d
			}
			m["dns"] = iface
		}
		if len(p.Peers) > 0 {
			iface := make([]interface{}, len(p.Peers))
			for i, peer := range p.Peers {
				iface[i] = peer
			}
			m["peers"] = iface
		}
	case "snell":
		m["psk"] = p.Password
		if p.Version > 0 {
			m["version"] = p.Version
		}
		if p.Obfs != "" {
			m["obfs-opts"] = map[string]interface{}{"mode": p.Obfs}
		}
	}
	for k, v := range p.Extra {
		if _, exists := m[k]; !exists {
			m[k] = v
		}
	}
	return m
}

// ---- Surge ----

func produceSurge(proxies []Proxy, opts map[string]interface{}) (string, error) {
	var sb strings.Builder
	sb.WriteString("[Proxy]\n")
	for _, p := range proxies {
		line := proxyToSurgeLine(p)
		if line != "" {
			sb.WriteString(line + "\n")
		}
	}
	return sb.String(), nil
}

func proxyToSurgeLine(p Proxy) string {
	switch p.Type {
	case "ss":
		line := fmt.Sprintf("%s = ss, %s, %d, encrypt-method=%s, password=%s",
			p.Name, p.Server, p.Port, p.Cipher, p.Password)
		if p.UDP {
			line += ", udp-relay=true"
		}
		return line
	case "vmess":
		line := fmt.Sprintf("%s = vmess, %s, %d, username=%s",
			p.Name, p.Server, p.Port, p.UUID)
		if p.TLS {
			line += ", tls=true"
		}
		if p.SNI != "" {
			line += fmt.Sprintf(", sni=%s", p.SNI)
		}
		if p.WSPath != "" {
			line += fmt.Sprintf(", ws=true, ws-path=%s", p.WSPath)
		}
		if p.SkipCertVerify {
			line += ", skip-cert-verify=true"
		}
		return line
	case "trojan":
		line := fmt.Sprintf("%s = trojan, %s, %d, password=%s",
			p.Name, p.Server, p.Port, p.Password)
		if p.SNI != "" {
			line += fmt.Sprintf(", sni=%s", p.SNI)
		}
		if p.SkipCertVerify {
			line += ", skip-cert-verify=true"
		}
		return line
	case "http":
		typ := "http"
		if p.TLS {
			typ = "https"
		}
		line := fmt.Sprintf("%s = %s, %s, %d", p.Name, typ, p.Server, p.Port)
		if p.Username != "" {
			line += fmt.Sprintf(", username=%s, password=%s", p.Username, p.Password)
		}
		return line
	case "socks5":
		typ := "socks5"
		if p.TLS {
			typ = "socks5-tls"
		}
		line := fmt.Sprintf("%s = %s, %s, %d", p.Name, typ, p.Server, p.Port)
		if p.Username != "" {
			line += fmt.Sprintf(", username=%s, password=%s", p.Username, p.Password)
		}
		return line
	case "snell":
		line := fmt.Sprintf("%s = snell, %s, %d, psk=%s", p.Name, p.Server, p.Port, p.Password)
		if p.Version > 0 {
			line += fmt.Sprintf(", version=%d", p.Version)
		}
		return line
	default:
		return fmt.Sprintf("# Unsupported type for Surge: %s (%s)", p.Name, p.Type)
	}
}

// ---- QuantumultX ----

func produceQX(proxies []Proxy, opts map[string]interface{}) (string, error) {
	var sb strings.Builder
	for _, p := range proxies {
		line := proxyToQXLine(p)
		if line != "" {
			sb.WriteString(line + "\n")
		}
	}
	return sb.String(), nil
}

func proxyToQXLine(p Proxy) string {
	tag := fmt.Sprintf(", tag=%s", p.Name)
	switch p.Type {
	case "ss":
		return fmt.Sprintf("shadowsocks=%s:%d, method=%s, password=%s%s",
			p.Server, p.Port, p.Cipher, p.Password, tag)
	case "ssr":
		return fmt.Sprintf("shadowsocksr=%s:%d, method=%s, password=%s, ssr-protocol=%s, ssr-protocol-param=%s, obfs=%s, obfs-host=%s%s",
			p.Server, p.Port, p.Cipher, p.Password, p.Protocol, p.ProtocolParam, p.Obfs, p.ObfsParam, tag)
	case "vmess":
		line := fmt.Sprintf("vmess=%s:%d, method=%s, password=%s%s",
			p.Server, p.Port, orDefault(p.Cipher, "auto"), p.UUID, tag)
		if p.TLS {
			line += ", tls-verification=true"
		}
		if p.WSPath != "" {
			line += fmt.Sprintf(", obfs=ws, obfs-uri=%s", p.WSPath)
		}
		if p.SNI != "" {
			line += fmt.Sprintf(", tls-host=%s", p.SNI)
		}
		return line
	case "trojan":
		line := fmt.Sprintf("trojan=%s:%d, password=%s%s",
			p.Server, p.Port, p.Password, tag)
		if p.SNI != "" {
			line += fmt.Sprintf(", tls-host=%s", p.SNI)
		}
		if p.SkipCertVerify {
			line += ", certificate=0"
		}
		return line
	case "http":
		scheme := "http"
		if p.TLS {
			scheme = "https"
		}
		line := fmt.Sprintf("%s=%s:%d%s", scheme, p.Server, p.Port, tag)
		if p.Username != "" {
			line += fmt.Sprintf(", username=%s, password=%s", p.Username, p.Password)
		}
		return line
	case "socks5":
		line := fmt.Sprintf("socks5=%s:%d%s", p.Server, p.Port, tag)
		if p.Username != "" {
			line += fmt.Sprintf(", username=%s, password=%s", p.Username, p.Password)
		}
		return line
	default:
		return fmt.Sprintf("# Unsupported type for QX: %s (%s)", p.Name, p.Type)
	}
}

// ---- Loon ----

func produceLoon(proxies []Proxy, opts map[string]interface{}) (string, error) {
	var sb strings.Builder
	sb.WriteString("[Proxy]\n")
	for _, p := range proxies {
		line := proxyToLoonLine(p)
		if line != "" {
			sb.WriteString(line + "\n")
		}
	}
	return sb.String(), nil
}

func proxyToLoonLine(p Proxy) string {
	switch p.Type {
	case "ss":
		line := fmt.Sprintf("%s = Shadowsocks, %s, %d, %s, \"%s\"",
			p.Name, p.Server, p.Port, p.Cipher, p.Password)
		if p.Plugin == "obfs" && len(p.PluginOptsMap) > 0 {
			mode, _ := p.PluginOptsMap["mode"].(string)
			host, _ := p.PluginOptsMap["host"].(string)
			line += fmt.Sprintf(", obfs=%s, obfs-host=%s", mode, host)
		}
		return line
	case "ssr":
		return fmt.Sprintf("%s = ShadowsocksR, %s, %d, %s, \"%s\", protocol=%s, protocol-param=%s, obfs=%s, obfs-param=%s",
			p.Name, p.Server, p.Port, p.Cipher, p.Password, p.Protocol, p.ProtocolParam, p.Obfs, p.ObfsParam)
	case "vmess":
		transport := "tcp"
		extra := ""
		if p.WSPath != "" {
			transport = "ws"
			extra = fmt.Sprintf(", path=%s", p.WSPath)
			if host, ok := p.WSHeaders["Host"]; ok {
				extra += fmt.Sprintf(", host=%s", host)
			}
		}
		line := fmt.Sprintf("%s = vmess, %s, %d, %s, \"%s\", over-tls=%v, transport=%s%s",
			p.Name, p.Server, p.Port, orDefault(p.Cipher, "auto"), p.UUID, p.TLS, transport, extra)
		if p.SNI != "" {
			line += fmt.Sprintf(", tls-name=%s", p.SNI)
		}
		return line
	case "vless":
		line := fmt.Sprintf("%s = vless, %s, %d, \"%s\", over-tls=%v",
			p.Name, p.Server, p.Port, p.UUID, p.TLS)
		if p.SNI != "" {
			line += fmt.Sprintf(", tls-name=%s", p.SNI)
		}
		if p.WSPath != "" {
			line += fmt.Sprintf(", transport=ws, path=%s", p.WSPath)
		}
		if p.GRPCServiceName != "" {
			line += fmt.Sprintf(", transport=grpc, grpc-service-name=%s", p.GRPCServiceName)
		}
		return line
	case "trojan":
		line := fmt.Sprintf("%s = trojan, %s, %d, \"%s\", tls-name=%s",
			p.Name, p.Server, p.Port, p.Password, orDefault(p.SNI, p.Server))
		if p.SkipCertVerify {
			line += ", skip-cert-verify=true"
		}
		return line
	case "http":
		line := fmt.Sprintf("%s = http, %s, %d", p.Name, p.Server, p.Port)
		if p.Username != "" {
			line += fmt.Sprintf(", username=%s, password=%s", p.Username, p.Password)
		}
		return line
	case "socks5":
		line := fmt.Sprintf("%s = socks5, %s, %d", p.Name, p.Server, p.Port)
		if p.Username != "" {
			line += fmt.Sprintf(", username=%s, password=%s", p.Username, p.Password)
		}
		return line
	default:
		return fmt.Sprintf("# Unsupported for Loon: %s (%s)", p.Name, p.Type)
	}
}

// ---- Shadowrocket (Base64 URI list) ----

func produceShadowrocket(proxies []Proxy, opts map[string]interface{}) (string, error) {
	var uris []string
	for _, p := range proxies {
		uri := proxyToURI(p)
		if uri != "" {
			uris = append(uris, uri)
		}
	}
	content := strings.Join(uris, "\n")
	return base64.StdEncoding.EncodeToString([]byte(content)), nil
}

// ---- SingBox JSON ----

func produceSingBox(proxies []Proxy, opts map[string]interface{}) (string, error) {
	outbounds := make([]map[string]interface{}, 0, len(proxies))
	for _, p := range proxies {
		ob := proxyToSingBox(p)
		if ob != nil {
			outbounds = append(outbounds, ob)
		}
	}
	raw, err := json.MarshalIndent(map[string]interface{}{"outbounds": outbounds}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func proxyToSingBox(p Proxy) map[string]interface{} {
	m := map[string]interface{}{
		"tag":         p.Name,
		"type":        singBoxType(p.Type),
		"server":      p.Server,
		"server_port": p.Port,
	}
	switch p.Type {
	case "ss":
		m["method"] = p.Cipher
		m["password"] = p.Password
	case "vmess":
		m["uuid"] = p.UUID
		m["security"] = orDefault(p.Cipher, "auto")
		m["alter_id"] = p.AlterID
		if p.TLS {
			m["tls"] = map[string]interface{}{
				"enabled": true, "server_name": p.SNI, "insecure": p.SkipCertVerify,
			}
		}
		if p.Network == "ws" {
			m["transport"] = map[string]interface{}{"type": "ws", "path": p.WSPath}
		} else if p.Network == "grpc" {
			m["transport"] = map[string]interface{}{"type": "grpc", "service_name": p.GRPCServiceName}
		}
	case "vless":
		m["uuid"] = p.UUID
		if p.Flow != "" {
			m["flow"] = p.Flow
		}
		if p.TLS {
			tlsOpts := map[string]interface{}{
				"enabled": true, "server_name": p.SNI, "insecure": p.SkipCertVerify,
			}
			if len(p.RealityOpts) > 0 {
				tlsOpts["reality"] = p.RealityOpts
			}
			m["tls"] = tlsOpts
		}
		if p.Network == "ws" {
			m["transport"] = map[string]interface{}{"type": "ws", "path": p.WSPath}
		} else if p.Network == "grpc" {
			m["transport"] = map[string]interface{}{"type": "grpc", "service_name": p.GRPCServiceName}
		}
	case "trojan":
		m["password"] = p.Password
		m["tls"] = map[string]interface{}{
			"enabled": true, "server_name": p.SNI, "insecure": p.SkipCertVerify,
		}
		if p.Network == "ws" {
			m["transport"] = map[string]interface{}{"type": "ws", "path": p.WSPath}
		} else if p.Network == "grpc" {
			m["transport"] = map[string]interface{}{"type": "grpc", "service_name": p.GRPCServiceName}
		}
	case "hysteria":
		m["up_mbps"] = p.UpMbps
		m["down_mbps"] = p.DownMbps
		if p.AuthStr != "" {
			m["auth_str"] = p.AuthStr
		}
		m["tls"] = map[string]interface{}{
			"enabled": true, "server_name": p.SNI, "insecure": p.Insecure,
		}
		if p.Obfs != "" {
			m["obfs"] = p.Obfs
		}
	case "hysteria2":
		m["password"] = p.Password
		m["tls"] = map[string]interface{}{
			"enabled": true, "server_name": p.SNI, "insecure": p.Insecure,
		}
		if p.Obfs != "" {
			m["obfs"] = map[string]interface{}{"type": p.Obfs, "password": p.ObfsPass}
		}
	case "tuic":
		m["uuid"] = p.UUID
		m["password"] = p.Password
		if p.CongCtrl != "" {
			m["congestion_control"] = p.CongCtrl
		}
		m["tls"] = map[string]interface{}{
			"enabled": true, "server_name": p.SNI, "insecure": p.SkipCertVerify,
		}
	case "socks5":
		if p.Username != "" {
			m["username"] = p.Username
			m["password"] = p.Password
		}
		if p.TLS {
			m["tls"] = map[string]interface{}{"enabled": true}
		}
	case "http":
		if p.Username != "" {
			m["username"] = p.Username
			m["password"] = p.Password
		}
		if p.TLS {
			m["tls"] = map[string]interface{}{"enabled": true}
		}
	case "wireguard":
		m["private_key"] = p.PrivateKey
		if len(p.Peers) > 0 {
			m["peers"] = p.Peers
		}
		if p.IP != "" {
			m["local_address"] = []string{p.IP}
		}
		if p.MTU > 0 {
			m["mtu"] = p.MTU
		}
	}
	return m
}

func singBoxType(t string) string {
	switch t {
	case "ss":
		return "shadowsocks"
	case "ssr":
		return "shadowsocksr"
	case "socks5":
		return "socks"
	case "wireguard":
		return "wireguard"
	default:
		return t
	}
}

// ---- V2Ray base64 URI list ----

func produceV2Ray(proxies []Proxy, opts map[string]interface{}) (string, error) {
	var uris []string
	for _, p := range proxies {
		var uri string
		if p.Type == "vmess" {
			uri = proxyToVmessURI(p)
		} else {
			uri = proxyToURI(p)
		}
		if uri != "" {
			uris = append(uris, uri)
		}
	}
	content := strings.Join(uris, "\n")
	return base64.StdEncoding.EncodeToString([]byte(content)), nil
}

func proxyToVmessURI(p Proxy) string {
	m := map[string]interface{}{
		"v":    "2",
		"ps":   p.Name,
		"add":  p.Server,
		"port": strconv.Itoa(p.Port),
		"id":   p.UUID,
		"aid":  strconv.Itoa(p.AlterID),
		"scy":  orDefault(p.Cipher, "auto"),
		"net":  orDefault(p.Network, "tcp"),
		"tls":  boolStr(p.TLS, "tls", ""),
		"sni":  p.SNI,
		"path": p.WSPath,
	}
	raw, _ := json.Marshal(m)
	return "vmess://" + base64.StdEncoding.EncodeToString(raw)
}

func proxyToURI(p Proxy) string {
	switch p.Type {
	case "ss":
		userinfo := base64.StdEncoding.EncodeToString([]byte(p.Cipher + ":" + p.Password))
		return fmt.Sprintf("ss://%s@%s:%d#%s", userinfo, p.Server, p.Port, url.QueryEscape(p.Name))
	case "ssr":
		main := fmt.Sprintf("%s:%d:%s:%s:%s:%s",
			p.Server, p.Port, p.Protocol, p.Cipher, p.Obfs,
			base64.RawURLEncoding.EncodeToString([]byte(p.Password)))
		params := fmt.Sprintf("obfsparam=%s&protoparam=%s&remarks=%s",
			base64.RawURLEncoding.EncodeToString([]byte(p.ObfsParam)),
			base64.RawURLEncoding.EncodeToString([]byte(p.ProtocolParam)),
			base64.RawURLEncoding.EncodeToString([]byte(p.Name)))
		return "ssr://" + base64.RawURLEncoding.EncodeToString([]byte(main+"/?"+params))
	case "trojan":
		q := url.Values{}
		if p.SNI != "" {
			q.Set("sni", p.SNI)
		}
		if p.Network != "" {
			q.Set("type", p.Network)
		}
		raw := ""
		if len(q) > 0 {
			raw = "?" + q.Encode()
		}
		return fmt.Sprintf("trojan://%s@%s:%d%s#%s",
			url.QueryEscape(p.Password), p.Server, p.Port, raw, url.QueryEscape(p.Name))
	case "vless":
		q := url.Values{}
		if p.Network != "" {
			q.Set("type", p.Network)
		}
		if p.SNI != "" {
			q.Set("sni", p.SNI)
		}
		if p.Flow != "" {
			q.Set("flow", p.Flow)
		}
		if p.TLS {
			q.Set("security", "tls")
		}
		if p.Fingerprint != "" {
			q.Set("fp", p.Fingerprint)
		}
		raw := ""
		if len(q) > 0 {
			raw = "?" + q.Encode()
		}
		return fmt.Sprintf("vless://%s@%s:%d%s#%s",
			p.UUID, p.Server, p.Port, raw, url.QueryEscape(p.Name))
	case "hysteria2":
		q := url.Values{}
		if p.Obfs != "" {
			q.Set("obfs", p.Obfs)
			q.Set("obfs-password", p.ObfsPass)
		}
		if p.SNI != "" {
			q.Set("sni", p.SNI)
		}
		if p.Insecure {
			q.Set("insecure", "1")
		}
		raw := ""
		if len(q) > 0 {
			raw = "?" + q.Encode()
		}
		return fmt.Sprintf("hysteria2://%s@%s:%d%s#%s",
			url.QueryEscape(p.Password), p.Server, p.Port, raw, url.QueryEscape(p.Name))
	default:
		return ""
	}
}

// ---- JSON (debug/internal) ----

func produceJSON(proxies []Proxy, opts map[string]interface{}) (string, error) {
	raw, err := json.MarshalIndent(proxies, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ---- helpers ----

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func boolStr(b bool, t, f string) string {
	if b {
		return t
	}
	return f
}
