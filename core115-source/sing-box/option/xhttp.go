package option

// XHttpOutboundOptions 定义了自定义 xhttp 协议的出站配置反序列化结构
type XHttpOutboundOptions struct {
	DialerOptions `json:",inline"`
	ServerOptions `json:",inline"`
	Name          string             `json:"name,omitempty"`    // 用于精准匹配下发节点名
	Password      string             `json:"password"`          // 对应 YAML 里的 JWT Token
	NodeID        string             `json:"node-id,omitempty"` // 对应 YAML 里的 node-id
	Network       NetworkList        `json:"network,omitempty"`
	UDPOverTCP    *UDPOverTCPOptions `json:"udp_over_tcp,omitempty"` // 🚀 抄作业成功，100% 准确！
}
