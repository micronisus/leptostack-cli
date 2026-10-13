package dns

import (
	"strings"
	"testing"
)

func TestRule(t *testing.T) {
	if got := Rule("example.test", "10.0.0.1"); got != "address=/example.test/10.0.0.1" {
		t.Errorf("Rule = %q", got)
	}
}

func TestGuidedInstructions(t *testing.T) {
	got := GuidedInstructions("alice-leptostack.test", "10.101.0.1")
	for _, want := range []string{
		"alice-leptostack.test",
		"10.101.0.1 alice-leptostack.test",
		"10.101.0.1 sso.alice-leptostack.test",
		"10.101.0.1 registry.alice-leptostack.test",
		"ipconfig /flushdns",
		"drivers\\etc\\hosts",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("guided instructions missing %q:\n%s", want, got)
		}
	}
	for _, p := range hostnamePrefixes {
		if !strings.Contains(got, p+".") {
			t.Errorf("guided instructions missing prefix %q", p)
		}
	}
}
