// The .ico Windows is asked to attach to the executable has to be readable by the same
// rules Windows uses, and the one that matters is the directory: an entry whose offset or
// length is wrong produces a file that opens in one program and shows a generic icon in
// Explorer.
//
// Nothing here decodes the images. It checks the container — count, dimensions, offsets,
// lengths, and that every image is the PNG it claims to be — which is the part a hand
// written encoder can get wrong.

#[test]
fn the_icon_file_is_well_formed() {
    let path = concat!(env!("CARGO_MANIFEST_DIR"), "/assets/wbtray.ico");
    let bytes =
        std::fs::read(path).expect("assets/wbtray.ico is missing; run `cargo run --bin iconbuild`");

    assert!(bytes.len() > 6, "the file is shorter than a header");
    let reserved = u16::from_le_bytes([bytes[0], bytes[1]]);
    let kind = u16::from_le_bytes([bytes[2], bytes[3]]);
    let count = u16::from_le_bytes([bytes[4], bytes[5]]) as usize;
    assert_eq!(reserved, 0, "the reserved field must be zero");
    assert_eq!(kind, 1, "type 1 is an icon");
    assert!(
        count >= 4,
        "an icon needs the sizes the shell asks for, found {count}"
    );

    let mut sizes = Vec::new();
    for i in 0..count {
        let e = 6 + i * 16;
        let width = bytes[e] as u32;
        let height = bytes[e + 1] as u32;
        let length =
            u32::from_le_bytes([bytes[e + 8], bytes[e + 9], bytes[e + 10], bytes[e + 11]]) as usize;
        let offset =
            u32::from_le_bytes([bytes[e + 12], bytes[e + 13], bytes[e + 14], bytes[e + 15]])
                as usize;

        assert_eq!(width, height, "entry {i} is not square");
        // 0 means 256: the field is one byte and cannot hold the value.
        sizes.push(if width == 0 { 256 } else { width });
        assert!(length > 0, "entry {i} declares no length");
        assert!(
            offset + 8 <= bytes.len(),
            "entry {i} starts past the end of the file"
        );
        let magic = &bytes[offset..offset + 8];
        assert_eq!(magic, b"\x89PNG\r\n\x1a\n", "entry {i} is not a PNG");
    }

    for want in [16u32, 32, 48, 256] {
        assert!(
            sizes.contains(&want),
            "the icon has no {want} pixel image: {sizes:?}"
        );
    }
}

#[test]
fn every_entry_declares_a_real_length() {
    let path = concat!(env!("CARGO_MANIFEST_DIR"), "/assets/wbtray.ico");
    let bytes = std::fs::read(path).expect("assets/wbtray.ico is missing");
    let count = u16::from_le_bytes([bytes[4], bytes[5]]) as usize;

    for i in 0..count {
        let e = 6 + i * 16;
        let length =
            u32::from_le_bytes([bytes[e + 8], bytes[e + 9], bytes[e + 10], bytes[e + 11]]) as usize;
        let offset =
            u32::from_le_bytes([bytes[e + 12], bytes[e + 13], bytes[e + 14], bytes[e + 15]])
                as usize;
        assert!(length > 0, "entry {i} has no image");
        assert!(
            offset + length <= bytes.len(),
            "entry {i} runs past the end: {offset}+{length} > {}",
            bytes.len()
        );
    }
}
