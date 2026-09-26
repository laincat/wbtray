package i18n

import "testing"

// TestEveryKeyIsTranslated is the reason the table is a map: a key added to one
// language and forgotten in the other would surface as an English word in a
// Chinese menu, which only a human would notice.
func TestEveryKeyIsTranslated(t *testing.T) {
	for _, key := range Keys() {
		for _, lang := range []Lang{ZH, EN} {
			if Lookup(lang)[key] == "" {
				t.Errorf("%s is missing from %s", key, lang)
			}
		}
	}
}

func TestTSubstitutes(t *testing.T) {
	if got := T("zh", "status.accounts", 3, 5); got != "账号 3/5 可用" {
		t.Fatalf("got %q", got)
	}
}

func TestUnknownKeyFallsBackToTheKey(t *testing.T) {
	if got := T("zh", "no.such.key"); got != "no.such.key" {
		t.Fatalf("got %q", got)
	}
}

func TestNumGroupsThousands(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{42, "42"},
		{1234, "1,234"},
		{1234567, "1,234,567"},
		{-4200, "-4,200"},
	} {
		if got := Num(tc.in); got != tc.want {
			t.Errorf("Num(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCompact(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want string
	}{
		{999, "999"},
		{1500, "1.5k"},
		{2500000, "2.5M"},
	} {
		if got := Compact(tc.in); got != tc.want {
			t.Errorf("Compact(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
