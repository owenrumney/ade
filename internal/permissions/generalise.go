package permissions

import (
	"strings"
)

// subcommands is how many bare words a proposal keeps. Two would reach the
// argument in "rtk ls internal" and propose a rule matching one directory.
const subcommands = 1

// Generalise is the rule to propose for a call: the shape of the command.
// Prefilling the card with this is what stops the call being asked about again.
func Generalise(tool, target string) Rule {
	if tool != bashTool {
		return Rule(tool)
	}
	segment := firstReal(target)
	words := strings.Fields(segment)
	if len(words) == 0 {
		return Rule(tool)
	}
	kept := words[:1]
	rest := skipFlags(words[1:])
	for _, word := range rest {
		if len(kept) > subcommands || !bareWord(word) {
			break
		}
		kept = append(kept, word)
	}
	return Rule(tool + "(" + strings.Join(kept, " ") + ":*)")
}

// firstReal is the first segment that is not a cd: a rule for "cd" is no rule.
func firstReal(command string) string {
	parts := Segments(command)
	for _, segment := range parts {
		if words := strings.Fields(segment); len(words) > 0 && words[0] != "cd" {
			return segment
		}
	}
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// skipFlags drops the options between the command word and its subcommand, and
// the value of the last of them. "git -C .. show" is a rule about "git show".
func skipFlags(words []string) []string {
	for len(words) > 0 && strings.HasPrefix(words[0], "-") {
		words = words[1:]
		if len(words) > 0 && !strings.HasPrefix(words[0], "-") && !bareWord(words[0]) {
			words = words[1:]
		}
	}
	return words
}

// bareWord is a subcommand, not an argument: no flag, path, extension or sha.
func bareWord(word string) bool {
	if word == "" || strings.HasPrefix(word, "-") {
		return false
	}
	if strings.ContainsAny(word, "/.=$*\"'") || hexish.MatchString(word) {
		return false
	}
	return !strings.ContainsAny(word, "0123456789")
}
