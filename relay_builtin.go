package main

// The relay this build reaches, compiled in. There is no relay settings page
// on a Mac that carries one: the code is the whole setup. To move the relay,
// change these constants and build again.

const (
	builtinRelayAddress     = "49.232.40.224:47825"
	builtinRelayTunnel      = "mac"
	builtinRelayURL         = "https://gexi.shunji.online:12345"
	builtinRelayFingerprint = "e410a3f0f19c98a4882953641b6f8aba4296867b49fcb19d94478d0468880298"
	// builtinRelayToken is the tunnel's secret. It ships inside the app, so
	// rotate it on the relay if a build ever leaks.
	builtinRelayToken = "56014ee364daaab8972aa9101f678ad390336a0f1c9dbb03c837d9a007f003ca"
)

// builtinRelayEnabled reports whether this build carries a relay.
func builtinRelayEnabled() bool {
	return builtinRelayAddress != "" && builtinRelayToken != "" && builtinRelayToken != "REPLACE_WITH_TOKEN"
}

// applyBuiltinRelay makes the compiled-in relay the settings the link uses.
func applyBuiltinRelay(s *appSettings) bool {
	if !builtinRelayEnabled() {
		return false
	}
	s.RelayAddress = builtinRelayAddress
	s.RelayTunnel = builtinRelayTunnel
	s.RelayToken = builtinRelayToken
	s.RelayFingerprint = builtinRelayFingerprint
	s.RelayURL = builtinRelayURL
	s.RelayEnabled = true
	return true
}
