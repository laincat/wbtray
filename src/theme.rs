// Which taskbar Windows is drawing, which is what decides the tray icon's ink.
//
// The notification area is drawn by the system, so the mark on it has to be chosen for a
// background this program does not control: a near-black cat is invisible on a dark
// taskbar and a white one is invisible on a light one. There is no single colour that
// reads on both and no way to draw a plate behind the mark without the block of colour
// this design exists to avoid, so the tone is read from Windows and the mark follows it.
//
// The setting is the app theme, not the system theme. `SystemUsesLightTheme` is about the
// taskbar and the tray in the older sense of the word; `AppsUseLightTheme` is the one the
// shell's own chrome follows, and it is the one that matches what the notification area
// looks like on a machine where the two disagree.

#[cfg(windows)]
pub fn taskbar_is_light() -> bool {
    use windows::core::w;
    use windows::Win32::Foundation::ERROR_SUCCESS;
    use windows::Win32::System::Registry::{
        RegCloseKey, RegOpenKeyExW, RegQueryValueExW, HKEY, HKEY_CURRENT_USER, KEY_READ, REG_DWORD,
    };

    unsafe {
        let mut key = HKEY::default();
        if RegOpenKeyExW(
            HKEY_CURRENT_USER,
            w!("Software\\Microsoft\\Windows\\CurrentVersion\\Themes\\Personalize"),
            None,
            KEY_READ,
            &mut key,
        ) != ERROR_SUCCESS
        {
            return false;
        }
        let mut kind = Default::default();
        let mut value: u32 = 0;
        let mut len = std::mem::size_of::<u32>() as u32;
        let ok = RegQueryValueExW(
            key,
            w!("AppsUseLightTheme"),
            None,
            Some(&mut kind),
            Some(&mut value as *mut u32 as *mut u8),
            Some(&mut len),
        ) == ERROR_SUCCESS;
        let _ = RegCloseKey(key);
        ok && kind == REG_DWORD && value == 1
    }
}

#[cfg(not(windows))]
pub fn taskbar_is_light() -> bool {
    false
}
