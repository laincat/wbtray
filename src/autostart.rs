// Starting the tray with Windows.
//
// A Run entry under the current user rather than a scheduled task or a service: this is
// a tray program, it belongs to whoever is signed in, and an entry that only exists
// while they are signed in is what that means.

#[cfg(windows)]
mod imp {
    use windows::core::w;
    use windows::Win32::Foundation::{ERROR_FILE_NOT_FOUND, ERROR_SUCCESS};
    use windows::Win32::System::Registry::{
        RegCloseKey, RegDeleteValueW, RegOpenKeyExW, RegQueryValueExW, RegSetValueExW, HKEY,
        HKEY_CURRENT_USER, KEY_READ, KEY_WRITE, REG_SZ,
    };

    const RUN_KEY: windows::core::PCWSTR = w!("Software\\Microsoft\\Windows\\CurrentVersion\\Run");
    const VALUE: windows::core::PCWSTR = w!("wbtray");

    fn open(write: bool) -> Option<HKEY> {
        let access = if write {
            KEY_READ | KEY_WRITE
        } else {
            KEY_READ
        };
        let mut key = HKEY::default();
        let rc = unsafe { RegOpenKeyExW(HKEY_CURRENT_USER, RUN_KEY, None, access, &mut key) };
        (rc == ERROR_SUCCESS).then_some(key)
    }

    /// Whether the Run entry is present.
    pub fn enabled() -> bool {
        let Some(key) = open(false) else {
            return false;
        };
        let rc = unsafe { RegQueryValueExW(key, VALUE, None, None, None, None) };
        unsafe {
            let _ = RegCloseKey(key);
        }
        rc == ERROR_SUCCESS
    }

    /// Add or remove the Run entry. `on` is what the operator asked for.
    pub fn set(on: bool) -> Result<(), String> {
        let key = open(true).ok_or("cannot open the Run key")?;
        let result = unsafe {
            if on {
                let exe = std::env::current_exe()
                    .map_err(|e| format!("cannot find this executable: {e}"))?;
                let value: Vec<u16> = exe
                    .as_os_str()
                    .encode_wide()
                    .chain(std::iter::once(0))
                    .collect();
                let bytes =
                    std::slice::from_raw_parts(value.as_ptr() as *const u8, value.len() * 2);
                RegSetValueExW(key, VALUE, None, REG_SZ, Some(bytes))
                    .ok()
                    .map_err(|e| format!("cannot write the Run entry: {e}"))
            } else {
                let rc = RegDeleteValueW(key, VALUE);
                // Already gone is the state the caller wanted.
                if rc == ERROR_SUCCESS || rc == ERROR_FILE_NOT_FOUND {
                    Ok(())
                } else {
                    Err(format!("cannot remove the Run entry: {rc:?}"))
                }
            }
        };
        unsafe {
            let _ = RegCloseKey(key);
        }
        result
    }

    use std::os::windows::ffi::OsStrExt;
}

#[cfg(not(windows))]
mod imp {
    pub fn enabled() -> bool {
        false
    }
    pub fn set(_on: bool) -> Result<(), String> {
        Err("autostart is only implemented on Windows".into())
    }
}

pub use imp::{enabled, set};
