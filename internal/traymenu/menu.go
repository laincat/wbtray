// Package traymenu is the menu model the tray draws and the system menu is built
// from.
//
// It exists as its own package, free of any Windows import, because the model is
// where the application's decisions live — which rows exist, what they say, what
// is ticked — and those decisions are worth testing without opening a window.
package traymenu

import "wbtray/internal/raster"

// Kind says how a row is drawn and whether it can be clicked.
type Kind int

// The kinds of row a menu can hold.
const (
	// Command is an ordinary clickable row.
	CommandRow Kind = iota
	// CheckRow is a toggle; its state is drawn from Checked.
	CheckRow
	// RadioRow is one of a group; the chosen row carries a dot.
	RadioRow
	// ValueRow is a readout: it shows a label and a right-aligned figure and is
	// not clickable.
	ValueRow
	// SeparatorRow draws a rule.
	SeparatorRow
	// SubmenuRow opens a nested list.
	SubmenuRow
	// StyleRow is a row in the style gallery: it carries a rendered preview.
	StyleRow
)

// Item is one row of a menu.
type Item struct {
	Kind    Kind
	ID      uint32
	Text    string
	Value   string
	Checked bool
	Bold    bool
	// Disabled rows are drawn dimmed and do not respond.
	Disabled bool
	// Dot draws a small coloured pip before the label, which is how a status row
	// shows health without a second column of text.
	Dot raster.RGBA
	// Children are the rows of a submenu.
	Children []Item
	// Preview is the rendered icon for a style row.
	Preview *raster.Canvas
	// expanded reports whether a submenu's children are shown inline. The drawn
	// menu keeps them in place rather than opening a second window: one column
	// that scrolls is easier to use than a cascade.
	expanded bool
}

// Expanded marks a submenu row whose children are shown inline.
func (i Item) Expanded(on bool) Item {
	i.expanded = on
	return i
}

// IsExpanded reports whether the row's children are shown inline.
func (i Item) IsExpanded() bool { return i.expanded }

// Command builds a plain clickable row.
func Command(id uint32, text string) Item {
	return Item{Kind: CommandRow, ID: id, Text: text}
}

// Check builds a toggle row.
func Check(id uint32, text string, checked bool) Item {
	return Item{Kind: CheckRow, ID: id, Text: text, Checked: checked}
}

// Radio builds one row of a single-choice group.
func Radio(id uint32, text string, selected bool) Item {
	return Item{Kind: RadioRow, ID: id, Text: text, Checked: selected}
}

// Value builds a readout row.
func Value(text, value string) Item {
	return Item{Kind: ValueRow, Text: text, Value: value, Disabled: true}
}

// Submenu builds a row that opens a nested list.
func Submenu(text string, children []Item) Item {
	return Item{Kind: SubmenuRow, Text: text, Children: children}
}

// Separator builds a rule.
func Separator() Item { return Item{Kind: SeparatorRow} }

// Event is what a click reports back to the application.
type Event struct {
	// ID is the command that was chosen, or 0 for a dismissal.
	ID uint32
	// Native is true when the click came from the system menu, which cannot show
	// previews or keep a row's state in place.
	Native bool
}

// CountRows returns how many rows a menu and its children hold, which is what
// decides whether scrolling is needed.
func CountRows(items []Item) int {
	n := 0
	for _, it := range items {
		n++
		if it.Kind == SubmenuRow {
			n += CountRows(it.Children)
		}
	}
	return n
}

// Find returns the row with an id, searching submenus.
func Find(items []Item, id uint32) (Item, bool) {
	for _, it := range items {
		if it.Kind != SubmenuRow && it.ID == id {
			return it, true
		}
		if it.Kind == SubmenuRow {
			if found, ok := Find(it.Children, id); ok {
				return found, true
			}
		}
	}
	return Item{}, false
}
