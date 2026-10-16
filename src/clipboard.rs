// Copy directly through Win32 without launching a console helper.

#[cfg(any(windows, test))]
fn unicode_text(text: &str) -> Result<Vec<u16>, String> {
    // CF_UNICODETEXT cannot represent an embedded NUL. Do not report a truncated
    // copy as success (particularly when copying credentials).
    if text.contains('\0') {
        return Err("clipboard text contains an embedded NUL".into());
    }
    Ok(text.encode_utf16().chain(std::iter::once(0)).collect())
}

#[cfg(windows)]
mod imp {
    use windows::core::w;
    use windows::Win32::Foundation::{GlobalFree, HANDLE, HGLOBAL, HWND};
    use windows::Win32::System::DataExchange::{
        CloseClipboard, EmptyClipboard, OpenClipboard, SetClipboardData,
    };
    use windows::Win32::System::Memory::{GlobalAlloc, GlobalLock, GlobalUnlock, GMEM_MOVEABLE};
    use windows::Win32::UI::WindowsAndMessaging::{
        CreateWindowExW, DestroyWindow, HWND_MESSAGE, WINDOW_EX_STYLE, WINDOW_STYLE,
    };

    pub(super) const CF_UNICODETEXT: u32 = 13;

    struct Owner(HWND);
    impl Owner {
        fn new() -> Result<Self, String> {
            // A built-in class avoids registration and a message-only window is
            // invisible. OpenClipboard(NULL) followed by EmptyClipboard leaves no
            // owner, which causes SetClipboardData to fail.
            unsafe {
                CreateWindowExW(
                    WINDOW_EX_STYLE::default(),
                    w!("STATIC"),
                    w!("wbtray clipboard"),
                    WINDOW_STYLE::default(),
                    0,
                    0,
                    0,
                    0,
                    Some(HWND_MESSAGE),
                    None,
                    None,
                    None,
                )
            }
            .map(Self)
            .map_err(|e| format!("cannot create the clipboard owner: {e}"))
        }
    }
    impl Drop for Owner {
        fn drop(&mut self) {
            unsafe {
                let _ = DestroyWindow(self.0);
            }
        }
    }

    pub(super) struct OpenClipboardGuard;
    impl Drop for OpenClipboardGuard {
        fn drop(&mut self) {
            unsafe {
                let _ = CloseClipboard();
            }
        }
    }

    struct Memory(Option<HGLOBAL>);
    impl Drop for Memory {
        fn drop(&mut self) {
            if let Some(handle) = self.0.take() {
                unsafe {
                    let _ = GlobalFree(Some(handle));
                }
            }
        }
    }

    pub fn copy(text: &str) -> Result<(), String> {
        let units = super::unicode_text(text)?;
        let bytes = units
            .len()
            .checked_mul(std::mem::size_of::<u16>())
            .ok_or("clipboard text is too large")?;
        unsafe {
            let mem = GlobalAlloc(GMEM_MOVEABLE, bytes)
                .map_err(|e| format!("cannot allocate {bytes} clipboard bytes: {e}"))?;
            // Until SetClipboardData succeeds, every failure must free this block.
            let mut memory = Memory(Some(mem));
            let ptr = GlobalLock(mem);
            if ptr.is_null() {
                return Err("cannot lock the clipboard memory".into());
            }
            std::ptr::copy_nonoverlapping(units.as_ptr(), ptr.cast::<u16>(), units.len());
            // A zero return is normal when the final lock is released.
            let _ = GlobalUnlock(mem);

            let owner = Owner::new()?;
            OpenClipboard(Some(owner.0)).map_err(|e| format!("cannot open the clipboard: {e}"))?;
            // Declared after owner so the clipboard closes before the window dies.
            let _clipboard = OpenClipboardGuard;
            EmptyClipboard().map_err(|e| format!("cannot empty the clipboard: {e}"))?;
            SetClipboardData(CF_UNICODETEXT, Some(HANDLE(mem.0)))
                .map_err(|e| format!("cannot hand the text to the clipboard: {e}"))?;
            memory.0 = None; // Only success transfers ownership to Windows.
            Ok(())
        }
    }
}

#[cfg(windows)]
pub use imp::copy;

#[cfg(not(windows))]
pub fn copy(_text: &str) -> Result<(), String> {
    Err("the clipboard is only implemented on Windows".into())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn encoding_preserves_unicode_and_line_breaks() {
        let text = "credential-密碼-🦀\r\nsecond line";
        let units = unicode_text(text).unwrap();
        assert_eq!(units.last(), Some(&0));
        assert_eq!(String::from_utf16(&units[..units.len() - 1]).unwrap(), text);
        assert_eq!(units.iter().filter(|&&unit| unit == 0).count(), 1);
    }

    #[test]
    fn empty_text_is_a_single_terminator() {
        assert_eq!(unicode_text("").unwrap(), vec![0]);
    }

    #[test]
    fn embedded_nul_is_rejected_in_every_position() {
        for text in ["\0", "\0prefix", "middle\0tail", "suffix\0"] {
            assert!(unicode_text(text).is_err());
        }
    }

    #[cfg(not(windows))]
    #[test]
    fn unsupported_platform_returns_an_error() {
        assert!(copy("probe").is_err());
    }

    // This changes the signed-in user's clipboard. Opt in on a Windows desktop:
    // cargo test clipboard::tests::interactive_round_trip -- --ignored
    // Errors are failures, never silently treated as a passing/skipped test.
    #[cfg(windows)]
    #[test]
    #[ignore = "requires an interactive Windows desktop and replaces the clipboard"]
    fn interactive_round_trip() {
        use windows::Win32::Foundation::HGLOBAL;
        use windows::Win32::System::DataExchange::{GetClipboardData, OpenClipboard};
        use windows::Win32::System::Memory::{GlobalLock, GlobalSize, GlobalUnlock};
        for probe in ["wbtray-probe", "密碼-🦀\r\nsecond line", ""] {
            copy(probe).expect("copy must succeed");
            unsafe {
                // NULL is valid for a reader, which does not empty the clipboard.
                OpenClipboard(None).expect("open for read");
                let _clipboard = imp::OpenClipboardGuard;
                let h = HGLOBAL(
                    GetClipboardData(imp::CF_UNICODETEXT)
                        .expect("text format")
                        .0,
                );
                let capacity = GlobalSize(h) / std::mem::size_of::<u16>();
                let ptr = GlobalLock(h).cast::<u16>();
                assert!(!ptr.is_null(), "lock clipboard data");
                let units = std::slice::from_raw_parts(ptr, capacity);
                // Copy the result before releasing the lock; bounds are checked
                // before any dereference, even for malformed clipboard contents.
                let result = units
                    .iter()
                    .position(|&unit| unit == 0)
                    .map(|len| String::from_utf16(&units[..len]));
                let _ = GlobalUnlock(h);
                assert_eq!(
                    result.expect("NUL terminator").expect("valid UTF-16"),
                    probe
                );
            }
        }
    }
}
