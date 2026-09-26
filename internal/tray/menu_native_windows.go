//go:build windows

package tray

import (
	"unsafe"

	"wbtray/internal/raster"
	"wbtray/internal/traymenu"
	"wbtray/internal/winapi"
)

// The system menu.
//
// This is the escape hatch: the drawn menu is themed and can show previews, but
// it is still a window this program paints, and there are situations — a screen
// reader, a locked-down desktop, a machine where the flyout is simply unwanted —
// where the shell's own menu is the right answer. Both are driven from the same
// []traymenu.Item, so switching between them changes nothing but the drawing.

// showNativeMenu builds and tracks the system menu at a point in screen
// coordinates.
func (t *Icon) showNativeMenu(x, y int) {
	items := t.menuItems()
	if len(items) == 0 {
		return
	}
	bitmaps := &bitmapOwner{}
	menu := buildMenu(items, bitmaps)
	if menu == 0 {
		return
	}
	defer winapi.ProcDestroyMenu.Call(menu)
	defer bitmaps.release()

	var pt winapi.Point
	winapi.ProcGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	if x != 0 || y != 0 {
		pt = winapi.Point{X: int32(x), Y: int32(y)}
	}

	hwnd := t.hwndForMenu()
	// Without taking the foreground first, the menu fails to dismiss when the
	// user clicks elsewhere — a well-known shell quirk.
	winapi.ProcSetForegroundWindow.Call(hwnd)
	cmd, _, _ := winapi.ProcTrackPopupMenu.Call(menu,
		tpmRightBtn|tpmReturnCmd, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	if cmd != 0 && t.cb.Select != nil {
		t.cb.Select(traymenu.Event{ID: uint32(cmd), Native: true})
	}
}

func (t *Icon) hwndForMenu() uintptr {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.hwnd
}

// buildMenu turns the model into an HMENU.
func buildMenu(items []traymenu.Item, bitmaps *bitmapOwner) uintptr {
	hMenu, _, _ := winapi.ProcCreatePopupMenu.Call()
	if hMenu == 0 {
		return 0
	}
	for _, it := range items {
		switch it.Kind {
		case traymenu.SeparatorRow:
			winapi.ProcAppendMenuW.Call(hMenu, mfSeparator, 0, 0)
		case traymenu.SubmenuRow:
			sub := buildMenu(it.Children, bitmaps)
			if sub == 0 {
				continue
			}
			winapi.ProcAppendMenuW.Call(hMenu, mfPopup, sub,
				uintptr(unsafe.Pointer(winapi.UTF16Ptr(it.Text))))
		case traymenu.StyleRow:
			// A style row carries its own preview: a menu bitmap is the one way
			// the system menu can show a picture, and a picture is the whole
			// reason the style rows exist.
			hbm := bitmaps.add(it.Preview)
			flags := uintptr(mfString | mfBitmap)
			if it.Checked {
				flags |= mfChecked
			}
			if hbm != 0 {
				winapi.ProcAppendMenuW.Call(hMenu, flags, uintptr(it.ID), hbm)
			} else {
				winapi.ProcAppendMenuW.Call(hMenu, uintptr(mfString), uintptr(it.ID),
					uintptr(unsafe.Pointer(winapi.UTF16Ptr(it.Text))))
			}
		default:
			flags := uintptr(mfString)
			if it.Checked {
				flags |= mfChecked
			}
			if it.Disabled {
				flags |= mfGrayed
			}
			label := it.Text
			if it.Value != "" {
				label += "\t" + it.Value
			}
			winapi.ProcAppendMenuW.Call(hMenu, flags, uintptr(it.ID),
				uintptr(unsafe.Pointer(winapi.UTF16Ptr(label))))
		}
	}
	return hMenu
}

// bitmapOwner keeps the HBITMAPs a menu is using alive until the menu closes:
// AppendMenu does not copy them, so releasing them early would leave the menu
// pointing at a deleted object.
type bitmapOwner struct{ handles []uintptr }

func (b *bitmapOwner) add(c *raster.Canvas) uintptr {
	if c == nil {
		return 0
	}
	// A menu bitmap is drawn at its natural size, so it is rendered at the icon
	// metric: the same size the taskbar uses.
	size := winapi.IconMetric()
	scaled := c
	if c.W != size {
		scaled = c.Scale(size, size)
	}
	hbm, bits := winapi.NewDIBSection(scaled.W, scaled.H)
	if hbm == 0 {
		return 0
	}
	dst := unsafe.Slice((*byte)(bits), scaled.W*scaled.H*4)
	copy(dst, scaled.BGRA())
	b.handles = append(b.handles, hbm)
	return hbm
}

func (b *bitmapOwner) release() {
	for _, h := range b.handles {
		winapi.ProcDeleteObject.Call(h)
	}
	b.handles = nil
}
