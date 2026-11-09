// Command gen-scenarios generates internal/scenarios/scenarios_gen.go from the acceptance
// documents, through criteria-review's neutral JSON emit.
//
// The journeys never carry clause text of their own. They implement the fields this generator
// writes, so the acceptance documents stay the single source of what each journey proves
// (docs/decisions.md D-003).
//
// Usage:
//
//	go run ./tools/gen-scenarios          write the generated file
//	go run ./tools/gen-scenarios -check   fail if the generated file is out of date
//
// criteria-review is resolved from node_modules/.bin, then PATH, and the generator fails when
// neither has it. It never downloads one: a fetch inside a gate turns a missing tool into a
// network dependency, or worse, into a pass.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// emit is the part of criteria-review's JSON emit this generator reads.
type emit struct {
	StandardVersion string     `json:"standardVersion"`
	Scenarios       []scenario `json:"scenarios"`
}

type scenario struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	TitleDisplay string   `json:"titleDisplay"`
	Feature      string   `json:"feature"`
	Status       string   `json:"status"`
	Persona      string   `json:"persona"`
	Intent       string   `json:"intent"`
	Steps        []string `json:"steps"`
	StepsDisplay []string `json:"stepsDisplay"`
	Source       string   `json:"source"`
}

const defaultOut = "internal/scenarios/scenarios_gen.go"

func main() {
	check := flag.Bool("check", false, "fail if the generated file differs from what the documents produce")
	root := flag.String("root", ".", "repository root holding criteria.json and acceptance/")
	out := flag.String("out", defaultOut, "generated file, relative to the root")
	flag.Parse()

	data, err := readEmit(*root)
	if err != nil {
		fail(err)
	}
	src, err := render(data)
	if err != nil {
		fail(err)
	}

	target := filepath.Join(*root, *out)
	if *check {
		current, err := os.ReadFile(target)
		if err != nil {
			fail(fmt.Errorf("%s is missing; run go run ./tools/gen-scenarios", target))
		}
		if !bytes.Equal(current, src) {
			fail(fmt.Errorf("%s is out of date with acceptance/; run go run ./tools/gen-scenarios", target))
		}
		fmt.Printf("gen-scenarios: %s is current (%d scenarios)\n", target, len(data.Scenarios))
		return
	}
	if err := os.WriteFile(target, src, 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("gen-scenarios: wrote %s (%d scenarios, standard %s)\n", target, len(data.Scenarios), data.StandardVersion)
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "gen-scenarios: %v\n", err)
	os.Exit(1)
}

// criteriaReview finds the checker the way a gate must: the project's pinned copy first, then
// PATH, and an error rather than a download when neither exists.
func criteriaReview(root string) (string, error) {
	local := filepath.Join(root, "node_modules", ".bin", "criteria-review")
	if _, err := os.Stat(local); err == nil {
		return local, nil
	}
	if p, err := exec.LookPath("criteria-review"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("criteria-review not found in node_modules/.bin or on PATH (npm install -g criteria-review)")
}

func readEmit(root string) (emit, error) {
	bin, err := criteriaReview(root)
	if err != nil {
		return emit{}, err
	}
	tmp, err := os.CreateTemp("", "scenarios-*.json")
	if err != nil {
		return emit{}, err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	cmd := exec.Command(bin, "generate", root, "--format", "json", "--out", tmpPath)
	var stderr bytes.Buffer
	cmd.Stdout = &stderr
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return emit{}, fmt.Errorf("criteria-review generate failed: %v\n%s", err, stderr.String())
	}
	raw, err := os.ReadFile(tmpPath)
	if err != nil {
		return emit{}, err
	}
	var e emit
	if err := json.Unmarshal(raw, &e); err != nil {
		return emit{}, fmt.Errorf("reading the emit: %w", err)
	}
	if len(e.Scenarios) == 0 {
		// An empty emit is far more likely to be a broken path than a repository with no
		// criteria, and generating an empty package would silently unbind every journey.
		return emit{}, fmt.Errorf("criteria-review emitted no scenarios from %s", root)
	}
	return e, nil
}

// markerPattern matches a glossary marker such as {engine.lower} or {session.lowerPlural}.
var markerPattern = regexp.MustCompile(`\{([A-Za-z][A-Za-z0-9]*)(?:\.([A-Za-z]+))?\}`)

// fieldName turns a raw clause into a Go identifier.
//
// Markers become their term KEY, not the displayed word, so a glossary rename changes captions
// but leaves every journey compiling. Plural and possessive variants add an "s" so that
// "a {session.lower}" and "the {session.lowerPlural}" stay distinguishable.
func fieldName(raw string) string {
	text := markerPattern.ReplaceAllStringFunc(raw, func(m string) string {
		parts := markerPattern.FindStringSubmatch(m)
		key, variant := parts[1], strings.ToLower(parts[2])
		word := upperFirst(key)
		if strings.Contains(variant, "plural") || variant == "possessive" {
			word += "s"
		}
		return " " + word + " "
	})
	// An apostrophe joins its word ("folder's" becomes Folders), so it never splits a word.
	text = strings.NewReplacer("'", "", "’", "").Replace(text)
	var b strings.Builder
	for _, word := range strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		b.WriteString(upperFirst(word))
	}
	return b.String()
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// typeName turns CONN-DOWN-001 into ConnDown001.
func typeName(id string) string {
	var b strings.Builder
	for _, part := range strings.Split(id, "-") {
		lower := strings.ToLower(part)
		b.WriteString(upperFirst(lower))
	}
	return b.String()
}

// fieldNames gives every clause a unique field name; a repeated clause gets a numeric suffix
// in document order, so two identical "And ..." lines remain two clauses to implement.
func fieldNames(steps []string) []string {
	seen := map[string]int{}
	names := make([]string, len(steps))
	for i, s := range steps {
		n := fieldName(s)
		seen[n]++
		if seen[n] > 1 {
			n += strconv.Itoa(seen[n])
		}
		names[i] = n
	}
	return names
}

func render(e emit) ([]byte, error) {
	scenarios := append([]scenario(nil), e.Scenarios...)
	sort.Slice(scenarios, func(i, j int) bool { return scenarios[i].ID < scenarios[j].ID })

	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by tools/gen-scenarios from acceptance/ (criteria-review standard %s). DO NOT EDIT.\n\n", e.StandardVersion)
	b.WriteString("package scenarios\n\n")

	b.WriteString("// Fields lists, for each generated type, the clause fields in document order. tools/clausecheck\n")
	b.WriteString("// reads it to fail the gate when a journey leaves a clause out.\n")
	b.WriteString("var Fields = map[string][]string{\n")
	for _, s := range scenarios {
		fmt.Fprintf(&b, "\t%q: {", typeName(s.ID))
		for i, n := range fieldNames(s.Steps) {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%q", n)
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n\n")

	for _, s := range scenarios {
		if len(s.Steps) != len(s.StepsDisplay) {
			return nil, fmt.Errorf("%s: %d steps but %d display steps", s.ID, len(s.Steps), len(s.StepsDisplay))
		}
		tn := typeName(s.ID)
		names := fieldNames(s.Steps)
		fmt.Fprintf(&b, "// %s is %s: %s\n//\n// Intent: %s\n// Source: %s\n", tn, s.ID, oneLine(s.TitleDisplay), oneLine(s.Intent), s.Source)
		fmt.Fprintf(&b, "type %s struct {\n", tn)
		for i, n := range names {
			fmt.Fprintf(&b, "\t// %s\n\t%s func()\n", oneLine(s.StepsDisplay[i]), n)
		}
		b.WriteString("}\n\n")

		fmt.Fprintf(&b, "// Scenario describes %s with the clause functions this journey supplied.\n", s.ID)
		fmt.Fprintf(&b, "func (s %s) Scenario() Scenario {\n\treturn Scenario{\n", tn)
		fmt.Fprintf(&b, "\t\tID: %q,\n\t\tTitle: %q,\n\t\tFeature: %q,\n\t\tStatus: %q,\n\t\tPersona: %q,\n\t\tIntent: %q,\n\t\tSource: %q,\n",
			s.ID, s.TitleDisplay, s.Feature, s.Status, s.Persona, s.Intent, s.Source)
		b.WriteString("\t\tSteps: []Step{\n")
		for i, n := range names {
			fmt.Fprintf(&b, "\t\t\t{Text: %q, Display: %q, Fn: s.%s},\n", s.Steps[i], s.StepsDisplay[i], n)
		}
		b.WriteString("\t\t},\n\t}\n}\n\n")
	}
	return format.Source(b.Bytes())
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
