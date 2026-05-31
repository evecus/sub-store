// Package proxy handles parsing, processing and producing subscription proxy configs.
package proxy

// Proxy represents a single proxy node with all possible fields across all supported protocols.
type Proxy struct {
	// --- Common ---
	Name     string `json:"name"`
	Type     string `json:"type"` // ss, ssr, vmess, vless, trojan, hysteria, hysteria2, tuic, http, socks5, wireguard, snell
	Server   string `json:"server"`
	Port     int    `json:"port"`
	Password string `json:"password,omitempty"`
	UDP      bool   `json:"udp,omitempty"`

	// --- SS / SSR ---
	Cipher        string `json:"cipher,omitempty"`
	Plugin        string `json:"plugin,omitempty"`
	PluginOptsMap map[string]interface{} `json:"plugin-opts,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
	ProtocolParam string `json:"protocol-param,omitempty"`
	Obfs          string `json:"obfs,omitempty"`
	ObfsParam     string `json:"obfs-param,omitempty"`

	// --- VMess / VLESS ---
	UUID            string            `json:"uuid,omitempty"`
	AlterID         int               `json:"alterId,omitempty"`
	Network         string            `json:"network,omitempty"`
	TLS             bool              `json:"tls,omitempty"`
	SNI             string            `json:"sni,omitempty"`
	ALPN            []string          `json:"alpn,omitempty"`
	SkipCertVerify  bool              `json:"skip-cert-verify,omitempty"`
	Fingerprint     string            `json:"fingerprint,omitempty"`
	Flow            string            `json:"flow,omitempty"`
	WSPath          string            `json:"ws-path,omitempty"`
	WSHeaders       map[string]string `json:"ws-headers,omitempty"`
	GRPCServiceName string            `json:"grpc-service-name,omitempty"`
	H2Path          []string          `json:"h2-path,omitempty"`
	H2Host          []string          `json:"h2-host,omitempty"`
	RealityOpts     map[string]interface{} `json:"reality-opts,omitempty"`

	// --- Hysteria ---
	AuthStr  string `json:"auth_str,omitempty"`
	AuthB64  string `json:"auth-base64,omitempty"`
	UpMbps   int    `json:"up,omitempty"`
	DownMbps int    `json:"down,omitempty"`
	Insecure bool   `json:"insecure,omitempty"`
	ObfsPass string `json:"obfs-password,omitempty"` // hysteria2 obfs password
	Ports    string `json:"ports,omitempty"`

	// --- TUIC ---
	Token    string `json:"token,omitempty"`
	CongCtrl string `json:"congestion-controller,omitempty"`

	// --- HTTP / SOCKS5 ---
	Username string            `json:"username,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`

	// --- WireGuard ---
	PrivateKey   string                   `json:"private-key,omitempty"`
	PublicKey    string                   `json:"public-key,omitempty"`
	PresharedKey string                   `json:"preshared-key,omitempty"`
	IP           string                   `json:"ip,omitempty"`
	IPv6         string                   `json:"ipv6,omitempty"`
	DNS          []string                 `json:"dns,omitempty"`
	MTU          int                      `json:"mtu,omitempty"`
	Peers        []map[string]interface{} `json:"peers,omitempty"`

	// --- Snell ---
	Version int `json:"version,omitempty"`

	// --- Extra / pass-through fields ---
	Extra map[string]interface{} `json:"_extra,omitempty"`

	// --- Internal metadata set by processors ---
	Geo         map[string]interface{} `json:"_geo,omitempty"`
	IP4         string                 `json:"_IPv4,omitempty"`
	IP6         string                 `json:"_IPv6,omitempty"`
	Tag         string                 `json:"_tag,omitempty"`
	Unavailable bool                   `json:"_unavailable,omitempty"`
}
