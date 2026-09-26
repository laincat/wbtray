package traymenu

import "testing"

func TestFindSearchesSubmenus(t *testing.T) {
	items := []Item{
		Command(1, "one"),
		Submenu("more", []Item{Command(2, "two"), Command(3, "three")}),
	}
	for id, want := range map[uint32]string{1: "one", 2: "two", 3: "three"} {
		got, ok := Find(items, id)
		if !ok {
			t.Fatalf("id %d not found", id)
		}
		if got.Text != want {
			t.Errorf("id %d = %q, want %q", id, got.Text, want)
		}
	}
	if _, ok := Find(items, 99); ok {
		t.Error("found an id that is not in the menu")
	}
}

func TestCountRowsIncludesChildren(t *testing.T) {
	items := []Item{
		Command(1, "one"),
		Submenu("more", []Item{Command(2, "two"), Separator(), Command(3, "three")}),
	}
	if got := CountRows(items); got != 5 {
		t.Fatalf("CountRows = %d, want 5", got)
	}
}

func TestExpandedRoundTrips(t *testing.T) {
	row := Submenu("more", nil)
	if row.IsExpanded() {
		t.Fatal("a new submenu row should be collapsed")
	}
	if !row.Expanded(true).IsExpanded() {
		t.Fatal("Expanded(true) did not stick")
	}
}
