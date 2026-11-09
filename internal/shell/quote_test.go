package shell

import (
	"os/exec"
	"reflect"
	"testing"
)

// Learning obligation O7, "shown equals run": pasting what is shown into a real shell must
// give back exactly the arguments that ran. This fails if any shape below is quoted wrongly,
// which would make the history teach a command that does something else.
func TestJoinRoundTripsThroughARealShell(t *testing.T) {
	shapes := [][]string{
		{"git", "-C", "/srv/project-a", "status"},
		{"echo", "two words"},
		{"echo", "it's"},
		{"echo", `"double"`},
		{"echo", "$HOME", "$(whoami)", "`id`"},
		{"echo", "semi;colon", "pipe|", "amp&", "glob*?[x]"},
		{"echo", "new\nline", "tab\tchar"},
		{"echo", ""},
		{"echo", "ünïcode", "日本"},
		{"echo", "back\\slash", "~tilde", "#hash", "!bang"},
	}
	for _, argv := range shapes {
		shown := Join(argv)
		// The shell prints each argument it received NUL-separated, so the comparison is
		// exact, empty arguments and embedded newlines included.
		got, err := exec.Command("sh", "-c", "set -- "+shown+`; for a in "$@"; do printf '%s\0' "$a"; done`).Output()
		if err != nil {
			t.Fatalf("%q: %v", shown, err)
		}
		parts := splitNUL(got)
		if !reflect.DeepEqual(parts, argv) {
			t.Fatalf("shown %q came back as %q, want %q", shown, parts, argv)
		}
	}
}

func TestPlainWordsStayReadable(t *testing.T) {
	if got := Join([]string{"zellij", "attach", "project-a"}); got != "zellij attach project-a" {
		t.Fatalf("plain words were quoted: %s", got)
	}
}

func splitNUL(b []byte) []string {
	var parts []string
	start := 0
	for i, c := range b {
		if c == 0 {
			parts = append(parts, string(b[start:i]))
			start = i + 1
		}
	}
	return parts
}
