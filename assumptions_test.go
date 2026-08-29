package clientollama_test

import (
	"testing"

	clienthttp "github.com/omcrgnt/client-http"
	"github.com/omcrgnt/ecfg"
	"github.com/omcrgnt/res/unique"
)

// TestMarkClientHTTPLoadEnvRejectsMalformedBaseURL is a canary pinning one
// half of an assumption Client.Start's doc comment relies on: when this
// Client is wired the normal way (through app.Bootstrap), ecfg.LoadEnv
// rejects a malformed BaseURL via protovalidate before Config.Build ever
// runs. The other half — that this Client is NOT wired the normal way in
// this package's own tests, so the error branch is directly reachable and
// separately covered by TestClient_Start_malformedBaseURL — needs no canary,
// it's exercised directly.
//
// The value below is deliberately not something a naive "no spaces/control
// chars" sanitizer would catch (an invalid port, not a stray character) —
// it targets protovalidate's RFC 3986 host/authority parsing specifically,
// so a future loosening of that rule is more likely to be caught here than
// by a cruder invalid string.
func TestMarkClientHTTPLoadEnvRejectsMalformedBaseURL(t *testing.T) {
	prefix := ecfg.Prefix()
	t.Cleanup(func() { ecfg.SetPrefix(prefix) })
	ecfg.SetPrefix("MARKER")

	t.Setenv("MARKER_OLLAMA_LABEL", "ollama")
	t.Setenv("MARKER_OLLAMA_BASE_URL", "http://example.com:notaport")

	reg := unique.New()
	spec := &clienthttp.Config{}
	if err := reg.AddWithCustomTag(spec, ecfg.TagKey(), "OLLAMA"); err != nil {
		t.Fatal(err)
	}

	if err := ecfg.LoadEnv(reg); err == nil {
		t.Fatal("expected ecfg.LoadEnv to reject a malformed BaseURL; " +
			"clientollama.Client.Start's url.Parse branch may now be reachable")
	}
}
