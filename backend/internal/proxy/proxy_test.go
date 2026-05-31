package proxy

import (
	"encoding/base64"
	"strings"
	"testing"
)

// ---- URI parsing tests ----

func TestParseSSURI(t *testing.T) {
	// ss://base64(method:password)@host:port#name
	userinfo := base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:password"))
	uri := "ss://" + userinfo + "@192.168.1.1:8388#TestSS"
	p, err := parseURI(uri)
	if err != nil {
		t.Fatalf("parseSSURI: %v", err)
	}
	if p.Type != "ss" {
		t.Errorf("type: want ss, got %s", p.Type)
	}
	if p.Server != "192.168.1.1" {
		t.Errorf("server: want 192.168.1.1, got %s", p.Server)
	}
	if p.Port != 8388 {
		t.Errorf("port: want 8388, got %d", p.Port)
	}
	if p.Cipher != "aes-256-gcm" {
		t.Errorf("cipher: want aes-256-gcm, got %s", p.Cipher)
	}
}

func TestParseVmessURI(t *testing.T) {
	vmessJSON := `{"v":"2","ps":"TestVMess","add":"1.2.3.4","port":"443","id":"test-uuid","aid":"0","net":"ws","tls":"tls","path":"/ws","scy":"auto"}`
	encoded := base64.StdEncoding.EncodeToString([]byte(vmessJSON))
	uri := "vmess://" + encoded
	p, err := parseURI(uri)
	if err != nil {
		t.Fatalf("parseVmessURI: %v", err)
	}
	if p.Type != "vmess" {
		t.Errorf("type: want vmess, got %s", p.Type)
	}
	if p.Name != "TestVMess" {
		t.Errorf("name: want TestVMess, got %q", p.Name)
	}
	if p.Server != "1.2.3.4" {
		t.Errorf("server: want 1.2.3.4, got %s", p.Server)
	}
	if p.Port != 443 {
		t.Errorf("port: want 443, got %d", p.Port)
	}
	if !p.TLS {
		t.Error("tls: want true")
	}
	if p.WSPath != "/ws" {
		t.Errorf("ws-path: want /ws, got %s", p.WSPath)
	}
}

func TestParseTrojanURI(t *testing.T) {
	uri := "trojan://secret-password@example.com:443?sni=example.com#MyTrojan"
	p, err := parseURI(uri)
	if err != nil {
		t.Fatalf("parseTrojanURI: %v", err)
	}
	if p.Type != "trojan" {
		t.Errorf("type: want trojan, got %s", p.Type)
	}
	if p.Password != "secret-password" {
		t.Errorf("password: want secret-password, got %s", p.Password)
	}
	if p.SNI != "example.com" {
		t.Errorf("sni: want example.com, got %s", p.SNI)
	}
	if p.Name != "MyTrojan" {
		t.Errorf("name: want MyTrojan, got %q", p.Name)
	}
}

func TestParseVlessURI(t *testing.T) {
	uri := "vless://test-uuid@1.2.3.4:443?type=ws&sni=test.com&security=tls#VlessNode"
	p, err := parseURI(uri)
	if err != nil {
		t.Fatalf("parseVlessURI: %v", err)
	}
	if p.Type != "vless" {
		t.Errorf("type: want vless, got %s", p.Type)
	}
	if p.UUID != "test-uuid" {
		t.Errorf("uuid: want test-uuid, got %s", p.UUID)
	}
	if !p.TLS {
		t.Error("tls: want true")
	}
}

func TestParseHysteria2URI(t *testing.T) {
	uri := "hysteria2://mypassword@server.example.com:443?sni=example.com&insecure=1#HY2Node"
	p, err := parseURI(uri)
	if err != nil {
		t.Fatalf("parseHysteria2URI: %v", err)
	}
	if p.Type != "hysteria2" {
		t.Errorf("type: want hysteria2, got %s", p.Type)
	}
	if p.Password != "mypassword" {
		t.Errorf("password: want mypassword, got %s", p.Password)
	}
	if !p.Insecure {
		t.Error("insecure: want true")
	}
}

// ---- Clash YAML parsing ----

func TestParseClashYAML(t *testing.T) {
	yaml := `proxies:
  - name: TestSS
    type: ss
    server: 1.2.3.4
    port: 8388
    cipher: aes-256-gcm
    password: testpassword
    udp: true
  - name: TestVmess
    type: vmess
    server: 5.6.7.8
    port: 443
    uuid: test-uuid
    alterId: 0
    cipher: auto
    network: ws
    tls: true
    ws-opts:
      path: /ws
`
	proxies, err := ParseContent(yaml, "")
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	if len(proxies) != 2 {
		t.Fatalf("expected 2 proxies, got %d", len(proxies))
	}
	ss := proxies[0]
	if ss.Type != "ss" || ss.Cipher != "aes-256-gcm" || !ss.UDP {
		t.Errorf("ss proxy mismatch: %+v", ss)
	}
	vm := proxies[1]
	if vm.Type != "vmess" || vm.WSPath != "/ws" {
		t.Errorf("vmess proxy mismatch: %+v", vm)
	}
}

func TestParseBase64Content(t *testing.T) {
	uris := "trojan://pass@1.2.3.4:443#Node1\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(uris))
	proxies, err := ParseContent(encoded, "")
	if err != nil {
		t.Fatalf("base64 parse: %v", err)
	}
	if len(proxies) == 0 {
		t.Error("expected at least 1 proxy")
	}
}

func TestParseURIList(t *testing.T) {
	content := strings.Join([]string{
		"trojan://pass1@1.1.1.1:443#Node1",
		"trojan://pass2@2.2.2.2:443#Node2",
		"# comment line",
		"trojan://pass3@3.3.3.3:443#Node3",
	}, "\n")
	proxies, err := ParseContent(content, "")
	if err != nil {
		t.Fatalf("uri list parse: %v", err)
	}
	if len(proxies) != 3 {
		t.Errorf("expected 3 proxies, got %d", len(proxies))
	}
}

// ---- Producer tests ----

func TestProduceClashMeta(t *testing.T) {
	proxies := []Proxy{
		{Name: "TestSS", Type: "ss", Server: "1.2.3.4", Port: 8388,
			Cipher: "aes-256-gcm", Password: "pass123", UDP: true},
		{Name: "TestVMess", Type: "vmess", Server: "5.6.7.8", Port: 443,
			UUID: "test-uuid", AlterID: 0, Cipher: "auto",
			Network: "ws", TLS: true, SNI: "example.com", WSPath: "/path"},
	}
	output, contentType, err := ProduceWithContentType(proxies, "ClashMeta", nil)
	if err != nil {
		t.Fatalf("ProduceClashMeta: %v", err)
	}
	if !strings.Contains(contentType, "yaml") {
		t.Errorf("content-type: want yaml, got %s", contentType)
	}
	if !strings.Contains(output, "proxies:") {
		t.Error("missing 'proxies:' key")
	}
	if !strings.Contains(output, "TestSS") {
		t.Error("missing TestSS")
	}
	if !strings.Contains(output, "aes-256-gcm") {
		t.Error("missing cipher")
	}
}

func TestProduceSurge(t *testing.T) {
	proxies := []Proxy{
		{Name: "SurgeSS", Type: "ss", Server: "1.1.1.1", Port: 443,
			Cipher: "aes-256-gcm", Password: "pass"},
		{Name: "SurgeTrojan", Type: "trojan", Server: "2.2.2.2", Port: 443,
			Password: "trojanpass", SNI: "example.com", TLS: true},
	}
	output, _, err := ProduceWithContentType(proxies, "Surge", nil)
	if err != nil {
		t.Fatalf("ProduceSurge: %v", err)
	}
	if !strings.Contains(output, "[Proxy]") {
		t.Error("missing [Proxy]")
	}
	if !strings.Contains(output, "sni=example.com") {
		t.Error("missing sni")
	}
}

func TestProduceQX(t *testing.T) {
	proxies := []Proxy{
		{Name: "QXProxy", Type: "ss", Server: "1.1.1.1", Port: 8388,
			Cipher: "chacha20-ietf-poly1305", Password: "qxpass"},
	}
	output, _, err := ProduceWithContentType(proxies, "QX", nil)
	if err != nil {
		t.Fatalf("ProduceQX: %v", err)
	}
	if !strings.Contains(output, "tag=QXProxy") {
		t.Error("missing tag=QXProxy")
	}
}

func TestProduceLoon(t *testing.T) {
	proxies := []Proxy{
		{Name: "LoonSS", Type: "ss", Server: "1.1.1.1", Port: 443,
			Cipher: "aes-256-gcm", Password: "pass"},
	}
	output, _, err := ProduceWithContentType(proxies, "Loon", nil)
	if err != nil {
		t.Fatalf("ProduceLoon: %v", err)
	}
	if !strings.Contains(output, "[Proxy]") {
		t.Error("missing [Proxy]")
	}
	if !strings.Contains(output, "LoonSS") {
		t.Error("missing LoonSS")
	}
}

func TestProduceSingBox(t *testing.T) {
	proxies := []Proxy{
		{Name: "SBTrojan", Type: "trojan", Server: "1.1.1.1", Port: 443,
			Password: "pass", SNI: "example.com", TLS: true},
	}
	output, contentType, err := ProduceWithContentType(proxies, "SingBox", nil)
	if err != nil {
		t.Fatalf("ProduceSingBox: %v", err)
	}
	if !strings.Contains(contentType, "json") {
		t.Errorf("content-type: want json, got %s", contentType)
	}
	if !strings.Contains(output, `"outbounds"`) {
		t.Error("missing outbounds")
	}
}

func TestProduceJSON(t *testing.T) {
	proxies := []Proxy{
		{Name: "JSONProxy", Type: "vmess", Server: "1.1.1.1", Port: 443, UUID: "id"},
	}
	output, _, err := ProduceWithContentType(proxies, "JSON", nil)
	if err != nil {
		t.Fatalf("ProduceJSON: %v", err)
	}
	if !strings.Contains(output, "JSONProxy") {
		t.Error("missing JSONProxy")
	}
}

// ---- Operator tests ----

func TestKeywordFilter(t *testing.T) {
	proxies := []Proxy{
		{Name: "Hong Kong 01", Type: "ss"},
		{Name: "US West 01", Type: "ss"},
		{Name: "Japan 01", Type: "trojan"},
	}
	ops := []interface{}{
		map[string]interface{}{
			"type": "KeywordFilter",
			"args": []interface{}{"Hong Kong", "Japan"},
		},
	}
	result, _ := ProcessProxies(proxies, ops)
	if len(result) != 2 {
		t.Errorf("keyword filter: want 2, got %d", len(result))
	}
}

func TestRegexFilter(t *testing.T) {
	proxies := []Proxy{
		{Name: "HK-01"}, {Name: "US-01"}, {Name: "HK-02"},
	}
	ops := []interface{}{
		map[string]interface{}{
			"type": "RegexFilter",
			"args": []interface{}{"^HK"},
		},
	}
	result, _ := ProcessProxies(proxies, ops)
	if len(result) != 2 {
		t.Errorf("regex filter: want 2, got %d", len(result))
	}
	for _, p := range result {
		if !strings.HasPrefix(p.Name, "HK") {
			t.Errorf("unexpected proxy: %s", p.Name)
		}
	}
}

func TestTypeFilter(t *testing.T) {
	proxies := []Proxy{
		{Name: "P1", Type: "ss"},
		{Name: "P2", Type: "vmess"},
		{Name: "P3", Type: "trojan"},
	}
	ops := []interface{}{
		map[string]interface{}{
			"type": "TypeFilter",
			"args": []interface{}{"ss", "trojan"},
		},
	}
	result, _ := ProcessProxies(proxies, ops)
	if len(result) != 2 {
		t.Errorf("type filter: want 2, got %d", len(result))
	}
}

func TestKeywordDeleteOperator(t *testing.T) {
	proxies := []Proxy{
		{Name: "Premium HK"}, {Name: "Free US"}, {Name: "Premium JP"},
	}
	ops := []interface{}{
		map[string]interface{}{
			"type": "KeywordDeleteOperator",
			"args": []interface{}{"Premium"},
		},
	}
	result, _ := ProcessProxies(proxies, ops)
	if len(result) != 1 || result[0].Name != "Free US" {
		t.Errorf("keyword delete: got %v", result)
	}
}

func TestDeduplicateOperator(t *testing.T) {
	proxies := []Proxy{
		{Name: "P1", Type: "ss", Server: "1.1.1.1", Port: 443},
		{Name: "P2", Type: "ss", Server: "1.1.1.1", Port: 443},
		{Name: "P3", Type: "trojan", Server: "2.2.2.2", Port: 443},
	}
	ops := []interface{}{
		map[string]interface{}{"type": "DeduplicateOperator"},
	}
	result, _ := ProcessProxies(proxies, ops)
	if len(result) != 2 {
		t.Errorf("dedup: want 2, got %d", len(result))
	}
}

func TestSortOperator(t *testing.T) {
	proxies := []Proxy{
		{Name: "Zebra"}, {Name: "Apple"}, {Name: "Mango"},
	}
	ops := []interface{}{
		map[string]interface{}{"type": "SortOperator"},
	}
	result, _ := ProcessProxies(proxies, ops)
	if result[0].Name != "Apple" || result[2].Name != "Zebra" {
		t.Errorf("sort: got %v %v %v", result[0].Name, result[1].Name, result[2].Name)
	}
}

func TestRegexRenameOperator(t *testing.T) {
	proxies := []Proxy{
		{Name: "Hong Kong 01"},
		{Name: "US West 01"},
	}
	ops := []interface{}{
		map[string]interface{}{
			"type": "RegexRenameOperator",
			"args": []interface{}{
				map[string]interface{}{"expr": `\s+\d+$`, "new": ""},
			},
		},
	}
	result, _ := ProcessProxies(proxies, ops)
	if result[0].Name != "Hong Kong" {
		t.Errorf("regex rename: want 'Hong Kong', got %q", result[0].Name)
	}
	if result[1].Name != "US West" {
		t.Errorf("regex rename: want 'US West', got %q", result[1].Name)
	}
}

func TestHandleDuplicateOperator_Rename(t *testing.T) {
	proxies := []Proxy{
		{Name: "HK"}, {Name: "HK"}, {Name: "US"},
	}
	ops := []interface{}{
		map[string]interface{}{
			"type": "HandleDuplicateOperator",
			"args": map[string]interface{}{
				"action":   "rename",
				"template": "${name} ${index}",
			},
		},
	}
	result, _ := ProcessProxies(proxies, ops)
	if len(result) != 3 {
		t.Errorf("handle dup rename: want 3, got %d", len(result))
	}
	if result[0].Name == result[1].Name {
		t.Error("names should differ after rename")
	}
}

func TestQuotaOperator(t *testing.T) {
	proxies := make([]Proxy, 10)
	for i := range proxies {
		proxies[i] = Proxy{Name: "P", Type: "ss"}
	}
	ops := []interface{}{
		map[string]interface{}{
			"type": "QuotaOperator",
			"args": map[string]interface{}{"quota": float64(3)},
		},
	}
	result, _ := ProcessProxies(proxies, ops)
	if len(result) != 3 {
		t.Errorf("quota: want 3, got %d", len(result))
	}
}

func TestSetPropertyOperator(t *testing.T) {
	proxies := []Proxy{
		{Name: "P1", Type: "ss", UDP: false},
		{Name: "P2", Type: "trojan", UDP: false},
	}
	ops := []interface{}{
		map[string]interface{}{
			"type": "SetPropertyOperator",
			"args": map[string]interface{}{"key": "udp", "value": true},
		},
	}
	result, _ := ProcessProxies(proxies, ops)
	for _, p := range result {
		if !p.UDP {
			t.Errorf("expected udp=true on %s", p.Name)
		}
	}
}

// ---- YAML encoder tests ----

func TestMarshalYAML_Simple(t *testing.T) {
	m := map[string]interface{}{
		"name": "TestProxy",
		"type": "ss",
		"port": 8388,
		"tls":  false,
	}
	out, err := MarshalYAML(m)
	if err != nil {
		t.Fatalf("MarshalYAML: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "name: TestProxy") {
		t.Errorf("missing 'name: TestProxy' in:\n%s", s)
	}
	if !strings.Contains(s, "port: 8388") {
		t.Errorf("missing 'port: 8388' in:\n%s", s)
	}
}

func TestMarshalYAML_ProxiesList(t *testing.T) {
	doc := map[string]interface{}{
		"proxies": []interface{}{
			map[string]interface{}{
				"name": "SS01", "type": "ss", "server": "1.1.1.1", "port": 443,
			},
			map[string]interface{}{
				"name": "VM01", "type": "vmess", "server": "2.2.2.2", "port": 443,
			},
		},
	}
	out, err := MarshalYAML(doc)
	if err != nil {
		t.Fatalf("MarshalYAML list: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "proxies:") {
		t.Error("missing proxies:")
	}
	if !strings.Contains(s, "SS01") || !strings.Contains(s, "VM01") {
		t.Error("missing proxy names")
	}
}

func TestYAMLRoundTrip(t *testing.T) {
	yaml := `proxies:
  - name: RoundTripProxy
    type: trojan
    server: example.com
    port: 443
    password: test123
    tls: true
    sni: example.com
`
	proxies, err := ParseContent(yaml, "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(proxies) != 1 {
		t.Fatalf("expected 1 proxy, got %d", len(proxies))
	}
	p := proxies[0]
	if p.Name != "RoundTripProxy" {
		t.Errorf("name: %q", p.Name)
	}
	if p.Type != "trojan" {
		t.Errorf("type: %q", p.Type)
	}
	if p.Password != "test123" {
		t.Errorf("password: %q", p.Password)
	}
}
