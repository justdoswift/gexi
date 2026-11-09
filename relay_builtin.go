package main

// The relay this build reaches, compiled in. There is no relay settings page
// on a Mac that carries one: the code is the whole setup. To move the relay,
// change these constants and build again.

const (
	builtinRelayAddress = "49.232.40.224:47825"
	// builtinRelayHost is the Mac that claims the tunnel. Every copy of the
	// app carries the same token, so without this the other Macs would
	// claim it too and knock each other off every second.
	builtinRelayHost        = "gepengdemacbook-pro"
	builtinRelayTunnel      = "mac"
	builtinRelayURL         = "https://gexi.shunji.online:12345"
	builtinRelayFingerprint = "e410a3f0f19c98a4882953641b6f8aba4296867b49fcb19d94478d0468880298"
	// builtinRelayToken is the tunnel's secret. It ships inside the app, so
	// rotate it on the relay if a build ever leaks.
	builtinRelayToken = "5d4282e76aaaffa255a127282d1e61c77b040b408ac3b391da346cd129cb3455"
)

// builtinRelayEnabled reports whether this build carries a relay.
func builtinRelayEnabled() bool {
	return builtinRelayAddress != "" && builtinRelayToken != "" &&
		builtinRelayToken != "REPLACE_WITH_TOKEN" && deviceName() == builtinRelayHost
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
