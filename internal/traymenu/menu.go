// Package traymenu is the menu model the system menu is built from.
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
	// CommandRow is an ordinary clickable row.
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
	// OwnerDraw asks the front end to paint the row itself.
	//
	// It exists for one reason: a row the system dims has its icon dimmed with
	// it, so a health colour drawn as a menu bitmap comes out grey exactly where
	// it carries the most meaning. A row drawn by hand keeps its colour, which is
	// why the status rows use this and the rest of the menu does not.
	OwnerDraw bool
	// Dot draws a small coloured pip before the label, which is how a status row
	// shows health without a second column of text.
	Dot raster.RGBA
	// Children are the rows of a submenu.
	Children []Item
	// Preview is the rendered icon for a style row.
	Preview *raster.Canvas
}

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

// Value builds a readout row, which the system draws dimmed and inert.
func Value(text, value string) Item {
	return Item{Kind: ValueRow, Text: text, Value: value, Disabled: true}
}

// Status builds a readout row that the front end paints itself.
//
// It is a value row in every respect but the drawing, so the colour it carries
// survives the system's habit of dimming anything that cannot be clicked.
func Status(text, value string, dot raster.RGBA, bold bool) Item {
	return Item{
		Kind:      ValueRow,
		Text:      text,
		Value:     value,
		Dot:       dot,
		Bold:      bold,
		Disabled:  true,
		OwnerDraw: true,
	}
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
}
