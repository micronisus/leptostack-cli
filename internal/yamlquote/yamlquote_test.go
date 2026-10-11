package yamlquote

import (
	"strings"
	"testing"
)

func TestQuoteEscapesSpecialCharacters(t *testing.T) {
	cases := map[string]string{
		"a\nb":  `"a\nb"`,
		"a\rb":  `"a\rb"`,
		"a\tb":  `"a\tb"`,
		`a"b`:   `"a\"b"`,
		`a\b`:   `"a\\b"`,
		"plain": `"plain"`,
	}
	for in, want := range cases {
		got := Quote(in)
		if got != want {
			t.Errorf("Quote(%q) = %s want %s", in, got, want)
		}
	}
	if strings.Contains(Quote("a\nb"), "\n") {
		t.Error("quoted output must not contain a literal newline")
	}
}
