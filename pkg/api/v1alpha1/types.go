package v1alpha1

// TlsProfile is the TLS configuration for the proxy.
type TlsProfile struct {
	Ciphers       []string           `json:"ciphers,omitempty"`
	MinTLSVersion TLSProtocolVersion `json:"minTLSVersion,omitempty"`
}

// TLSProtocolVersion is a way to specify the protocol version used for TLS connections.
type TLSProtocolVersion string

const (
	// VersionTLS10 is version 1.0 of the TLS security protocol.
	VersionTLS10 TLSProtocolVersion = "VersionTLS10"
	// VersionTLS11 is version 1.1 of the TLS security protocol.
	VersionTLS11 TLSProtocolVersion = "VersionTLS11"
	// VersionTLS12 is version 1.2 of the TLS security protocol.
	VersionTLS12 TLSProtocolVersion = "VersionTLS12"
	// VersionTLS13 is version 1.3 of the TLS security protocol.
	VersionTLS13 TLSProtocolVersion = "VersionTLS13"
)
