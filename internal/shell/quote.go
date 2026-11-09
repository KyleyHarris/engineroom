// Package shell renders a command as text a person can read and paste into a POSIX shell, and
// get exactly the same arguments back.
//
// This is what makes "the command shown is the command that ran" true: the program builds an
// argument list, runs that list directly (never through a shell), and shows Join of the same
// list. Pasting what is shown reproduces the list, including spaces, quotes, $ and newlines.
package shell

import "strings"

// safe holds the characters that never need quoting.
const safe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./:=@%+,"

// Quote returns one argument as shell text. Plain words are left alone so commands stay
// readable; anything else is wrapped in single quotes. An embedded single quote is written as
// close-quote, backslash, quote, open-quote: the one form every POSIX shell agrees on.
func Quote(arg string) string {
	if arg == "" {
		return "''"
	}
	plain := true
	for _, r := range arg {
		if !strings.ContainsRune(safe, r) {
			plain = false
			break
		}
	}
	if plain {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

// Join renders a whole argument list.
func Join(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = Quote(a)
	}
	return strings.Join(parts, " ")
}
