// The About box.
//
// The shell's own message box rather than a window of this program's: it is a modal
// statement of two versions and a licence, and a drawn window for that would be more
// machinery than the content deserves in either direction.

#[cfg(windows)]
pub fn about(title: &str, body: &str) {
    use windows::core::HSTRING;
    use windows::Win32::UI::WindowsAndMessaging::{MessageBoxW, MB_ICONINFORMATION, MB_OK};

    let t = HSTRING::from(title);
    let b = HSTRING::from(body);
    unsafe {
        MessageBoxW(None, &b, &t, MB_OK | MB_ICONINFORMATION);
    }
}

#[cfg(not(windows))]
pub fn about(title: &str, body: &str) {
    println!("{title}\n\n{body}");
}

/// A short failure notice, used when an action the operator asked for did not happen.
#[cfg(windows)]
pub fn warn(title: &str, body: &str) {
    use windows::core::HSTRING;
    use windows::Win32::UI::WindowsAndMessaging::{MessageBoxW, MB_ICONWARNING, MB_OK};

    let t = HSTRING::from(title);
    let b = HSTRING::from(body);
    unsafe {
        MessageBoxW(None, &b, &t, MB_OK | MB_ICONWARNING);
    }
}

#[cfg(not(windows))]
pub fn warn(title: &str, body: &str) {
    eprintln!("{title}: {body}");
}

/// Open a URL in the default browser.
#[cfg(windows)]
pub fn open_url(url: &str) -> Result<(), String> {
    use windows::core::HSTRING;
    use windows::Win32::UI::Shell::ShellExecuteW;
    use windows::Win32::UI::WindowsAndMessaging::SW_SHOWNORMAL;

    let op = HSTRING::from("open");
    let file = HSTRING::from(url);
    let ret = unsafe { ShellExecuteW(None, &op, &file, None, None, SW_SHOWNORMAL) };
    // ShellExecuteW returns a value above 32 on success; anything else is an error code.
    if ret.0 as isize > 32 {
        Ok(())
    } else {
        Err(format!(
            "the shell refused to open {url} (code {})",
            ret.0 as isize
        ))
    }
}

#[cfg(not(windows))]
pub fn open_url(url: &str) -> Result<(), String> {
    Err(format!("cannot open {url} on this platform"))
}
