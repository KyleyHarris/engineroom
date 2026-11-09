package main

import (
	"fmt"
	"regexp"
	"strings"
)

// The decisions doc is written as records, not as Markdown prose: each entry is a set of fields,
// a label at the start of a line and its value in a column to its right, continued on lines
// indented to that column.
//
//	Context:    What the problem was, over as many lines as it needs,
//	            each continued at the value's column.
//	Options:    (a) The first option [CHOSEN]
//	                continued deeper than its marker
//	            (b) Another - rejected: why
//	Decision:   - A point of the decision
//	              continued under its text
//	            Text back at the value's column is a paragraph after the list.
//	Status:     accepted
//
// Read as Markdown, a record runs together into one paragraph, so a doc declared with
// format = "record" is rewritten into Markdown before it is parsed: a field becomes a heading
// one level below the page's own with its value under it, options and points become list items,
// and a field whose value is one short line (Status) stays a single line. A line inside a record that
// fits none of this fails the build rather than being guessed at.

var recordField = regexp.MustCompile(`^([A-Z][A-Za-z]*):( +)(\S.*)$`)

// shortField is the longest value, written on one line, that is shown as "Label: value" rather
// than under a heading of its own.
const shortField = 60

// recordsToMarkdown rewrites every record in src as Markdown and leaves every other line as it
// is. level is the heading level a field gets.
func recordsToMarkdown(src []byte, source string, level int) ([]byte, error) {
	lines := strings.Split(string(src), "\n")
	var out []string
	inFence := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
		}
		m := recordField.FindStringSubmatch(line)
		if inFence || m == nil {
			out = append(out, line)
			continue
		}
		label, column := m[1], len(m[1])+1+len(m[2])
		value := []string{m[3]}
		for i+1 < len(lines) {
			next := lines[i+1]
			if strings.TrimSpace(next) == "" || recordField.MatchString(next) || strings.HasPrefix(next, "#") {
				break
			}
			indent := len(next) - len(strings.TrimLeft(next, " "))
			if indent < column {
				return nil, fmt.Errorf("%s:%d: a line inside the %s field starts left of its value's column (%d); indent it to the column, or end the record with a blank line",
					source, i+2, label, column+1)
			}
			value = append(value, next[column:])
			i++
		}
		out = append(out, fieldMarkdown(label, value, level)...)
	}
	return []byte(strings.Join(out, "\n")), nil
}

var optionMarker = regexp.MustCompile(`^\(([a-z])\) `)

// fieldMarkdown turns one field's value lines (already cut at the value's column) into Markdown
// blocks: a line starting "- " or "(a) " starts a list item, a deeper line continues the item
// above it, and a line at the column with no marker is paragraph text.
func fieldMarkdown(label string, value []string, level int) []string {
	type block struct {
		item bool
		text []string
	}
	var blocks []block
	for _, v := range value {
		deeper := strings.HasPrefix(v, " ")
		v = strings.TrimSpace(v)
		switch {
		case strings.HasPrefix(v, "- "):
			blocks = append(blocks, block{item: true, text: []string{strings.TrimPrefix(v, "- ")}})
		case optionMarker.MatchString(v):
			// The option's letter is kept, in bold, because the decision refers to options by it.
			blocks = append(blocks, block{item: true, text: []string{optionMarker.ReplaceAllString(v, "**($1)** ")}})
		case deeper && len(blocks) > 0 && blocks[len(blocks)-1].item,
			!deeper && len(blocks) > 0 && !blocks[len(blocks)-1].item:
			blocks[len(blocks)-1].text = append(blocks[len(blocks)-1].text, v)
		default:
			blocks = append(blocks, block{text: []string{v}})
		}
	}

	if len(value) == 1 && !blocks[0].item && len(value[0]) <= shortField {
		return []string{"**" + label + ":** " + escapeAngles(strings.Join(blocks[0].text, " ")), ""}
	}
	out := []string{strings.Repeat("#", level) + " " + label, ""}
	for i, b := range blocks {
		text := escapeAngles(strings.Join(b.text, " "))
		text = strings.ReplaceAll(text, "[CHOSEN]", "**(chosen)**")
		if b.item {
			out = append(out, "- "+text)
			// A list ends with a blank line before whatever follows it, and items stay together.
			if i+1 == len(blocks) || !blocks[i+1].item {
				out = append(out, "")
			}
			continue
		}
		out = append(out, text, "")
	}
	return out
}

// escapeAngles writes "<" outside code spans as an entity, so a placeholder in running text
// (<session>) reads as written instead of being taken for an HTML tag.
func escapeAngles(s string) string {
	parts := strings.Split(s, "`")
	for i := 0; i < len(parts); i += 2 {
		parts[i] = strings.ReplaceAll(parts[i], "<", "&lt;")
	}
	return strings.Join(parts, "`")
}
