// Writes the application icon: the same cat the tray draws, as a .ico Windows can attach
// to the executable.
//
// The sizes in the file are the ones Windows actually asks for. An .ico holding only one
// large image is scaled down by the shell and looks soft in the taskbar and in the
// Explorer's list, which are the two places this icon is seen most.
//
// It is a development tool. The file it writes is committed, because a release build
// should not have to run a renderer.
//
//
//     cargo run --release --bin iconbuild -- assets/wbtray.ico

use std::io::Write;

#[path = "../cat.rs"]
mod cat;

/// The sizes in the icon, largest first.
const SIZES: [u32; 6] = [256, 64, 48, 32, 24, 16];

fn main() {
    let out = std::env::args()
        .nth(1)
        .unwrap_or_else(|| "wbtray.ico".into());

    // A .ico stores each size as a whole PNG file, which is why a PNG encoder is a build
    // dependency of this tool. The mark is near-black on a transparent field: an
    // application icon is an identity rather than a reading, so it does not change with
    // the machine's theme the way the tray icon does.
    let images: Vec<(u32, Vec<u8>)> = SIZES
        .iter()
        .map(|&s| (s, cat::icon_rgba_toned(s, [0x14, 0x14, 0x14])))
        .map(|(s, rgba)| (s, encode_png(s, &rgba)))
        .collect();

    let mut file = Vec::new();
    // ICONDIR: reserved, type 1 for icons, then the number of images.
    file.extend_from_slice(&0u16.to_le_bytes());
    file.extend_from_slice(&1u16.to_le_bytes());
    file.extend_from_slice(&(images.len() as u16).to_le_bytes());

    // The directory comes first and each entry has to record where its image starts,
    // which depends on how long the directory is — so the images are encoded first and
    // their offsets worked out here.
    let mut offset = 6 + images.len() * 16;
    for (size, png) in &images {
        // 256 is written as 0: the field is one byte and cannot hold it.
        let dim = if *size >= 256 { 0u8 } else { *size as u8 };
        file.extend_from_slice(&[dim, dim, 0, 0]);
        file.extend_from_slice(&1u16.to_le_bytes()); // colour planes
        file.extend_from_slice(&32u16.to_le_bytes()); // bits per pixel
        file.extend_from_slice(&(png.len() as u32).to_le_bytes());
        file.extend_from_slice(&(offset as u32).to_le_bytes());
        offset += png.len();
    }
    for (_, png) in &images {
        file.extend_from_slice(png);
    }

    let mut w = std::fs::File::create(&out).expect("cannot write the icon");
    w.write_all(&file).expect("cannot write the icon");
    println!("wrote {out}: {} sizes, {} bytes", images.len(), file.len());
}

/// Encode one image as a PNG, which is the form a .ico entry takes.
fn encode_png(size: u32, rgba: &[u8]) -> Vec<u8> {
    let mut out = Vec::new();
    {
        let mut enc = png::Encoder::new(&mut out, size, size);
        enc.set_color(png::ColorType::Rgba);
        enc.set_depth(png::BitDepth::Eight);
        let mut writer = enc.write_header().expect("cannot start the PNG");
        writer.write_image_data(rgba).expect("cannot write the PNG");
    }
    out
}
