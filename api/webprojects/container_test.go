package webprojects

import (
	"strings"
	"testing"
)

func TestInjectContainerScriptUsesRealHTMLEndTag(t *testing.T) {
	raw := []byte(`<!doctype html><html><head><script>const template = "</head>";</script><!-- </head> --></head><body>content</body></html>`)
	script := []byte(`<script id="container"></script>`)

	result, err := injectContainerScript(raw, script)
	if err != nil {
		t.Fatal(err)
	}
	expected := `<!doctype html><html><head><script>const template = "</head>";</script><!-- </head> --><script id="container"></script></head><body>content</body></html>`
	if string(result) != expected {
		t.Fatalf("injected HTML changed source token boundaries:\n%s", result)
	}
	if strings.Count(string(result), string(script)) != 1 {
		t.Fatal("container script was not injected exactly once")
	}
}

func TestInjectContainerScriptFallsBackToRealBodyEnd(t *testing.T) {
	raw := []byte(`<html><body><script>const template = "</body>";</script>content</body></html>`)
	script := []byte(`<script id="container"></script>`)

	result, err := injectContainerScript(raw, script)
	if err != nil {
		t.Fatal(err)
	}
	expected := `<html><body><script>const template = "</body>";</script>content<script id="container"></script></body></html>`
	if string(result) != expected {
		t.Fatalf("body fallback changed source token boundaries:\n%s", result)
	}
}
