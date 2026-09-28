package zoho

import "testing"

func TestLookupDC(t *testing.T) {
	eu, err := LookupDC(" EU ")
	if err != nil {
		t.Fatal(err)
	}
	if got := eu.URL("cliq"); got != "https://cliq.zoho.eu" {
		t.Errorf("cliq URL = %s", got)
	}
	ca, _ := LookupDC("ca")
	if got := ca.AccountsURL(); got != "https://accounts.zohocloud.ca" {
		t.Errorf("ca accounts = %s", got)
	}
	if _, err := LookupDC("evil.example"); err == nil {
		t.Error("unknown DC accepted")
	}
}
