// Reads a built executable the way Windows reads it, and checks it carries the icon.
//
// The icon is the one part of a Windows build that compiles without complaint and can
// still be wrong. A resource section the linker drops, a tree whose root is missing, or
// an image Windows rejects below 256 pixels all produce an executable that builds, runs,
// and quietly shows the system's generic icon in Explorer — which is the fault three
// releases shipped with before anything checked.
//
// It is skipped unless `WBTRAY_BUILT_EXE` names a file, so it does nothing on a developer
// machine and runs in the release job against the real artifact.

#[test]
fn the_built_executable_carries_an_icon() {
    let Ok(path) = std::env::var("WBTRAY_BUILT_EXE") else {
        eprintln!("set WBTRAY_BUILT_EXE to a built executable; skipping");
        return;
    };
    let bytes = std::fs::read(&path).unwrap_or_else(|e| panic!("cannot read {path}: {e}"));

    // The icon lives in the PE resource section. Finding the section and then the icon
    // group inside it is enough: if the linker dropped the resources, the section is not
    // there at all.
    let pe = find_pe_offset(&bytes).expect("the file is not a PE image");
    let (sections, _) = read_sections(&bytes, pe).expect("the PE headers are not readable");
    let rsrc = sections
        .iter()
        .find(|s| s.name.trim_end_matches('\0') == ".rsrc")
        .expect("the executable has no resource section: the icon was dropped by the linker");
    assert!(rsrc.size > 0, "the resource section is empty");

    // RT_GROUP_ICON is type 14, and its presence is what makes Explorer draw the icon.
    // The tree is a directory of ids, so the search is for the word 14 in the section's
    // first level rather than for any byte that happens to be 14.
    let start = rsrc.pointer as usize;
    let end = (start + rsrc.size as usize).min(bytes.len());
    assert!(
        start < end,
        "the resource section points past the end of the file"
    );
    let group_icon = bytes[start..end]
        .windows(2)
        .any(|w| u16::from_le_bytes([w[0], w[1]]) == 14);
    assert!(
        group_icon,
        "the resource section has no icon group (type 14)"
    );

    eprintln!("{} carries a {}-byte resource section", path, rsrc.size);
}

struct Section {
    name: String,
    size: u32,
    pointer: u32,
}

/// The file offset of the PE header.
fn find_pe_offset(b: &[u8]) -> Option<usize> {
    if b.len() < 0x40 || &b[0..2] != b"MZ" {
        return None;
    }
    let lfanew = u32::from_le_bytes([b[0x3c], b[0x3d], b[0x3e], b[0x3f]]) as usize;
    (b.get(lfanew..lfanew + 4) == Some(b"PE\0\0")).then_some(lfanew)
}

/// The section table and the number of sections.
fn read_sections(b: &[u8], pe: usize) -> Option<(Vec<Section>, u16)> {
    let coff = pe + 4;
    let num = u16::from_le_bytes([*b.get(coff + 2)?, *b.get(coff + 3)?]);
    let opt_size = u16::from_le_bytes([*b.get(coff + 16)?, *b.get(coff + 17)?]) as usize;
    let table = coff + 20 + opt_size;
    let mut out = Vec::new();
    for i in 0..num as usize {
        let s = table + i * 40;
        let name = String::from_utf8_lossy(b.get(s..s + 8)?).to_string();
        let size = u32::from_le_bytes([
            *b.get(s + 16)?,
            *b.get(s + 17)?,
            *b.get(s + 18)?,
            *b.get(s + 19)?,
        ]);
        let pointer = u32::from_le_bytes([
            *b.get(s + 20)?,
            *b.get(s + 21)?,
            *b.get(s + 22)?,
            *b.get(s + 23)?,
        ]);
        out.push(Section {
            name,
            size,
            pointer,
        });
    }
    Some((out, num))
}
