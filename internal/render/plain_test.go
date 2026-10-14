package render

import (
	"strings"
	"testing"
)

func TestPlainLeavesAFenceUnhighlighted(t *testing.T) {
	src := []byte("# T\n\n```go\nfunc main() {}\n```\n")
	lit, _, _, err := toHTML(src, ThemeDark, false, false)
	if err != nil {
		t.Fatal(err)
	}
	plain, _, _, err := toHTML(src, ThemeDark, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lit), "color:#") {
		t.Fatalf("highlighting off by default now: %s", lit)
	}
	if strings.Contains(string(plain), "color:#") {
		t.Fatalf("plain still highlighted: %s", plain)
	}
	if !strings.Contains(string(plain), "func main() {}") {
		t.Fatalf("plain lost the code: %s", plain)
	}
}
