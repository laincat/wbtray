// Package i18n holds the tray's strings in the languages it speaks.
//
// Two languages is not enough to justify a loader or a bundle format, so the
// table is a Go map, and a key that exists in one language and not the other is
// a test failure rather than something a user finds in a menu.
package i18n

import (
	"fmt"
	"strings"
)

// Lang is a supported language tag.
type Lang string

// The languages the tray speaks.
const (
	ZH Lang = "zh"
	EN Lang = "en"
)

// Strings is one language's messages, keyed by a stable name.
type Strings map[string]string

// table maps a language to its messages.
var table = map[Lang]Strings{
	ZH: zh,
	EN: en,
}

// T returns the message for key in lang, with the arguments substituted.
//
// A key that is missing falls back to English and then to the key itself, so a
// gap shows up as a readable word rather than as a blank menu item.
func T(lang string, key string, args ...any) string {
	l := Parse(lang)
	s, ok := table[l][key]
	if !ok {
		s, ok = table[EN][key]
	}
	if !ok {
		return key
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// Parse maps a config value or a Windows locale onto a supported language.
func Parse(s string) Lang {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.HasPrefix(s, "zh"), strings.HasPrefix(s, "cn"), s == "chinese":
		return ZH
	default:
		return EN
	}
}

// Keys returns every message key, for the completeness test.
func Keys() []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range table {
		for k := range s {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	return out
}

// Lookup returns one language's table, for the completeness test.
func Lookup(l Lang) Strings { return table[l] }

// Num formats a count with thousands separators, which is what makes a credit
// balance readable at a glance.
func Num(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprintf("%d", v)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// Compact shortens a large number: 12345 becomes 12.3k.
func Compact(v float64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.1fG", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case v >= 1000:
		return fmt.Sprintf("%.1fk", v/1000)
	case v == float64(int64(v)):
		return fmt.Sprintf("%d", int64(v))
	default:
		return fmt.Sprintf("%.1f", v)
	}
}

// Round1 renders a float with one decimal.
func Round1(v float64) string { return fmt.Sprintf("%.1f", v) }

// Duration renders a span of seconds the way a status line wants it.
func Duration(sec int64) string {
	switch {
	case sec < 60:
		return fmt.Sprintf("%ds", sec)
	case sec < 3600:
		return fmt.Sprintf("%dm", sec/60)
	case sec < 86400:
		return fmt.Sprintf("%dh%02dm", sec/3600, (sec%3600)/60)
	default:
		return fmt.Sprintf("%dd%02dh", sec/86400, (sec%86400)/3600)
	}
}
