// Package zoho holds what every Zoho app client shares: data centers and an
// authenticated JSON client.
package zoho

import (
	"fmt"
	"slices"
	"strings"
)

// DC is a Zoho data center. Every host of a DC is "<service>.<Domain>",
// e.g. accounts.zoho.eu and cliq.zoho.eu.
type DC struct {
	Code   string // location code as Zoho reports it ("us", "eu", ...)
	Domain string
}

// Source: https://www.zoho.com/accounts/protocol/oauth/multi-dc.html
var dcs = []DC{
	{"us", "zoho.com"},
	{"eu", "zoho.eu"},
	{"in", "zoho.in"},
	{"au", "zoho.com.au"},
	{"jp", "zoho.jp"},
	{"ca", "zohocloud.ca"},
	{"sa", "zoho.sa"},
	{"uk", "zoho.uk"},
}

// LookupDC resolves a location code. Hosts are only ever built from this
// table, never from a server response, so a tampered response cannot redirect
// tokens to another host.
func LookupDC(code string) (DC, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	i := slices.IndexFunc(dcs, func(d DC) bool { return d.Code == code })
	if i < 0 {
		return DC{}, fmt.Errorf("unknown Zoho data center %q (known: %s)", code, strings.Join(DCCodes(), ", "))
	}
	return dcs[i], nil
}

// DCCodes lists the known location codes.
func DCCodes() []string {
	codes := make([]string, len(dcs))
	for i, d := range dcs {
		codes[i] = d.Code
	}
	return codes
}

// URL returns the base URL of a service in this DC, e.g. URL("cliq").
func (d DC) URL(service string) string {
	return "https://" + service + "." + d.Domain
}

// AccountsURL is the OAuth server of this DC.
func (d DC) AccountsURL() string { return d.URL("accounts") }
