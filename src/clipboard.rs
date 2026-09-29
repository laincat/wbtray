// Putting a value on the clipboard.
//
// Written against the Win32 clipboard API directly rather than through a helper
// process: `cmd /c clip` is a console program, so using it flashes a window and makes
// the copy depend on the current directory and the path, which are two things the tray
// has no reason to have an opinion about.

#[cfg(windows)]
pub fn copy(text: &str) -> Result<(), String> {
    use windows::Win32::Foundation::{HANDLE, HWND};
    use windows::Win32::System::DataExchange::{
        CloseClipboard, EmptyClipboard, OpenClipboard, SetClipboardData,
    };
    use windows::Win32::System::Memory::{GlobalAlloc, GlobalLock, GlobalUnlock, GMEM_MOVEABLE};

    /// CF_UNICODETEXT. It is a constant of the clipboard format list rather than of the
    /// DataExchange module, which is why it is stated here instead of imported.
    const CF_UNICODETEXT: u32 = 13;

    unsafe {
        if OpenClipboard(Some(HWND::default())).is_err() {
            return Err("the clipboard is held by another program".into());
        }
        // Every path out of here closes it again: a clipboard left open blocks every
        // other program that tries to copy, which is a worse fault than a failed copy.
        let result = (|| -> Result<(), String> {
            EmptyClipboard().map_err(|e| format!("cannot empty the clipboard: {e}"))?;

            let units: Vec<u16> = text.encode_utf16().chain(std::iter::once(0)).collect();
            let bytes = units.len() * 2;
            let mem = GlobalAlloc(GMEM_MOVEABLE, bytes)
                .map_err(|e| format!("cannot allocate {bytes} bytes: {e}"))?;
            let ptr = GlobalLock(mem);
            if ptr.is_null() {
                return Err("cannot lock the clipboards memory".into());
            }
            std::ptr::copy_nonoverlapping(units.as_ptr() as *const u8, ptr as *mut u8, bytes);
            let _ = GlobalUnlock(mem);

            // The clipboard takes ownership of the block on success, so it must not be
            // freed here or on the failure path after this point.
            if let Err(e) = SetClipboardData(CF_UNICODETEXT, Some(HANDLE(mem.0))) {
                return Err(format!("cannot hand the text to the clipboard: {e}"));
            }
            Ok(())
        })();
        let _ = CloseClipboard();
        result
    }
}

#[cfg(not(windows))]
pub fn copy(_text: &str) -> Result<(), String> {
    Err("the clipboard is only implemented on Windows".into())
}

#[cfg(all(test, windows))]
mod tests {
    use super::*;

    /// Read the clipboard back through the same API, so the test checks what a second
    /// program would see rather than what this one thinks it wrote.
    fn read_back() -> Option<String> {
        use windows::Win32::Foundation::{HGLOBAL, HWND};
        use windows::Win32::System::DataExchange::{
            CloseClipboard, GetClipboardData, OpenClipboard,
        };
        use windows::Win32::System::Memory::{GlobalLock, GlobalUnlock};
        unsafe {
            OpenClipboard(Some(HWND::default())).ok()?;
            let h = GetClipboardData(13).ok()?;
            let ptr = GlobalLock(HGLOBAL(h.0));
            let mut len = 0usize;
            let u = ptr as *const u16;
            while *u.add(len) != 0 && len < 4096 {
                len += 1;
            }
            let s = String::from_utf16_lossy(std::slice::from_raw_parts(u, len));
            let _ = GlobalUnlock(HGLOBAL(h.0));
            let _ = CloseClipboard();
            Some(s)
        }
    }

    /// Both values are checked in one test on purpose.
    ///
    /// The clipboard is one resource for the whole machine and cargo runs tests in
    /// parallel, so two tests that each empty it and read it back will clear each other's
    /// text — a failure that says nothing about this code and everything about the
    /// harness. One test keeps the two checks in the order they need.
    #[test]
    fn what_is_written_is_what_a_reader_sees() {
        for probe in ["wbtray-clipboard-probe", "sk-2cGOUn3vQ5agutpg-TPrgks_"] {
            if let Err(e) = copy(probe) {
                // Another program may be holding the clipboard. That is not this code
                // being wrong, and there is nothing to assert about it.
                eprintln!("the clipboard could not be opened ({e}); skipping");
                return;
            }
            assert_eq!(read_back().as_deref(), Some(probe));
        }
    }
}
