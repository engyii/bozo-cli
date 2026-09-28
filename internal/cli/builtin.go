package cli

// The OAuth client compiled into release builds, so users only run
// `bozo auth login`. Injected by GoReleaser with -ldflags -X from CI secrets
// (.goreleaser.yaml); never committed. Local builds leave them empty and
// login then needs --client-id.
//
// The secret is not confidential: anyone with the binary can extract it,
// and obfuscation cannot help because bozo must send it in clear to Zoho
// (RFC 8252 §8.5 says the same of every native app). It identifies the
// app; it grants nothing without the user's consent in the browser.
var builtinClientID, builtinClientSecret string

// builtinClientDC is the data center the built-in client is registered in.
// Zoho accepts a device-code request only there (other DCs answer
// invalid_client even with multi-DC enabled); users elsewhere are then
// redirected by other_dc.
var builtinClientDC string
