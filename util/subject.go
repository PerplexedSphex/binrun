package util

import "strings"

// SubjectMatches reports whether a subject matches a pattern that can include
// NATS wildcards * (one token) and > (greedy remainder).
func SubjectMatches(pattern, subj string) bool {
	if pattern == subj {
		return true
	}
	pTok := strings.Split(pattern, ".")
	sTok := strings.Split(subj, ".")
	for i, pt := range pTok {
		switch pt {
		case ">":
			return true // matches remainder
		case "*":
			if i >= len(sTok) {
				return false
			}
			continue
		}
		if i >= len(sTok) {
			return false
		}
		if pt != sTok[i] {
			return false
		}
	}
	return len(sTok) == len(pTok)
}

// SelectorFor converts a NATS subject into a CSS selector targeting the
// element whose id is derived from the subject.
func SelectorFor(subj string) string {
	replacer := strings.NewReplacer(
		".", "-",
		"*", "wild",
		">", "fullwild",
	)
	return "#sub-" + replacer.Replace(subj)
}
