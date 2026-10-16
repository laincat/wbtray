// Per-user Windows Run entry. It must be a quoted command targeting this executable.

#[cfg(any(windows, test))]
fn command(exe: &[u16]) -> Result<Vec<u16>, String> {
    if exe.is_empty() || exe.iter().any(|&unit| unit == 0 || unit == b'"' as u16) {
        return Err("the executable path is empty or contains an invalid character".into());
    }
    // Quoting is mandatory even for paths without spaces, so later installation
    // locations cannot accidentally turn a path prefix into a different executable.
    Ok(std::iter::once(b'"' as u16)
        .chain(exe.iter().copied())
        .chain([b'"' as u16, 0])
        .collect())
}

#[cfg(any(windows, test))]
fn matches_command(bytes: &[u8], expected: &[u16]) -> bool {
    // Require the entire REG_SZ value, including its terminator. Prefix matching
    // would accept extra arguments, embedded NULs, or an unrelated executable.
    bytes.len() == expected.len() * 2
        && bytes
            .as_chunks::<2>()
            .0
            .iter()
            .zip(expected)
            .all(|(pair, &unit)| u16::from_le_bytes([pair[0], pair[1]]) == unit)
}

#[cfg(windows)]
mod imp {
    use std::os::windows::ffi::OsStrExt;
    use windows::core::w;
    use windows::Win32::Foundation::{ERROR_FILE_NOT_FOUND, ERROR_SUCCESS};
    use windows::Win32::System::Registry::{
        RegCloseKey, RegDeleteValueW, RegOpenKeyExW, RegQueryValueExW, RegSetValueExW, HKEY,
        HKEY_CURRENT_USER, KEY_READ, KEY_WRITE, REG_SZ, REG_VALUE_TYPE,
    };

    const RUN_KEY: windows::core::PCWSTR = w!("Software\\Microsoft\\Windows\\CurrentVersion\\Run");
    const VALUE: windows::core::PCWSTR = w!("wbtray");

    struct Key(HKEY);
    impl Drop for Key {
        fn drop(&mut self) {
            unsafe {
                let _ = RegCloseKey(self.0);
            }
        }
    }

    fn open(write: bool) -> Result<Option<Key>, String> {
        let access = if write {
            KEY_READ | KEY_WRITE
        } else {
            KEY_READ
        };
        let mut key = HKEY::default();
        let rc = unsafe { RegOpenKeyExW(HKEY_CURRENT_USER, RUN_KEY, None, access, &mut key) };
        if rc == ERROR_SUCCESS {
            Ok(Some(Key(key)))
        } else if rc == ERROR_FILE_NOT_FOUND {
            Ok(None)
        } else {
            Err(format!("cannot open the Run key: {rc:?}"))
        }
    }

    fn current_command() -> Result<Vec<u16>, String> {
        let exe =
            std::env::current_exe().map_err(|e| format!("cannot find this executable: {e}"))?;
        super::command(&exe.as_os_str().encode_wide().collect::<Vec<_>>())
    }

    /// True only for our exact quoted command, not merely a value with our name.
    /// A stale location, additional arguments, malformed data or wrong registry
    /// type is disabled. Re-enabling replaces it with the current executable.
    pub fn enabled() -> bool {
        let Ok(Some(key)) = open(false) else {
            return false;
        };
        let Ok(expected) = current_command() else {
            return false;
        };
        let mut kind = REG_VALUE_TYPE::default();
        // Read into a bounded buffer sized to the only accepted command. An
        // oversized value returns ERROR_MORE_DATA and is rejected, without an
        // unbounded allocation or a size-query/read race.
        let mut bytes = vec![0u8; expected.len() * 2];
        let mut size = bytes.len() as u32;
        let rc = unsafe {
            RegQueryValueExW(
                key.0,
                VALUE,
                None,
                Some(&mut kind),
                Some(bytes.as_mut_ptr()),
                Some(&mut size),
            )
        };
        rc == ERROR_SUCCESS
            && kind == REG_SZ
            && size as usize == bytes.len()
            && super::matches_command(&bytes, &expected)
    }

    pub fn set(on: bool) -> Result<(), String> {
        let Some(key) = open(true)? else {
            // A missing key already satisfies a request to disable autostart.
            return if on {
                Err("the Windows Run key does not exist".into())
            } else {
                Ok(())
            };
        };
        if on {
            // Key is an RAII guard, including if current_exe/encoding fails.
            let value = current_command()?;
            let bytes: Vec<u8> = value.iter().flat_map(|unit| unit.to_le_bytes()).collect();
            unsafe { RegSetValueExW(key.0, VALUE, None, REG_SZ, Some(&bytes)) }
                .ok()
                .map_err(|e| format!("cannot write the Run entry: {e}"))
        } else {
            let rc = unsafe { RegDeleteValueW(key.0, VALUE) };
            if rc == ERROR_SUCCESS || rc == ERROR_FILE_NOT_FOUND {
                Ok(())
            } else {
                Err(format!("cannot remove the Run entry: {rc:?}"))
            }
        }
    }
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

#[cfg(test)]
mod tests {
    use super::*;

    fn wide(text: &str) -> Vec<u16> {
        text.encode_utf16().collect()
    }
    fn bytes(units: &[u16]) -> Vec<u8> {
        units.iter().flat_map(|unit| unit.to_le_bytes()).collect()
    }

    #[test]
    fn executable_paths_are_always_quoted_and_terminated() {
        for exe in [
            r"C:\Program Files\wbtray\wbtray.exe",
            r"C:\wbtray.exe",
            r"C:\工具 🦀\wbtray.exe",
        ] {
            let value = command(&wide(exe)).unwrap();
            assert_eq!(value, wide(&format!("\"{exe}\"\0")));
            assert!(matches_command(&bytes(&value), &value));
        }
    }

    #[test]
    fn invalid_paths_are_rejected() {
        for exe in ["", "\0", "C:\\bad\0.exe", "C:\\bad\".exe"] {
            assert!(command(&wide(exe)).is_err());
        }
    }

    #[test]
    fn non_unicode_windows_paths_are_preserved_without_lossy_conversion() {
        let path = [b'C' as u16, b':' as u16, b'\\' as u16, 0xd800];
        let value = command(&path).unwrap();
        assert_eq!(&value[1..value.len() - 2], &path);
        assert!(matches_command(&bytes(&value), &value));
    }

    #[test]
    fn stale_unquoted_or_extended_commands_do_not_count_as_enabled() {
        let expected = command(&wide(r"C:\Program Files\wbtray\wbtray.exe")).unwrap();
        for actual in [
            "\"C:\\old\\wbtray.exe\"\0",
            "C:\\Program Files\\wbtray\\wbtray.exe\0",
            "\"C:\\Program Files\\wbtray\\wbtray.exe\" --extra\0",
            "\"C:\\Program Files\\wbtray\\wbtray.exe\"\0hidden\0",
            "\"C:\\Program Files\\wbtray\\wbtray.exe\"",
            "\"C:\\Program Files\\wbtray\\wbtray.exe\"\0\0",
            "",
        ] {
            assert!(
                !matches_command(&bytes(&wide(actual)), &expected),
                "{actual:?}"
            );
        }
    }

    #[test]
    fn malformed_byte_lengths_are_rejected() {
        let expected = command(&wide(r"C:\wbtray.exe")).unwrap();
        let mut actual = bytes(&expected);
        actual.pop();
        assert!(!matches_command(&actual, &expected));
        actual.extend([0, 0]);
        assert!(!matches_command(&actual, &expected));
    }

    #[cfg(not(windows))]
    #[test]
    fn unsupported_platform_does_not_claim_autostart() {
        assert!(!enabled());
        assert!(set(true).is_err());
        assert!(set(false).is_err());
    }
}
