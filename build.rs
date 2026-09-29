// Attaches the icon and the version information to the executable.
//
// A Windows program's icon and its version block are resources, and Rust has no way to
// emit one from inside the crate: the linker is what writes the resource section, so the
// objects have to exist before it runs. This is the step that makes them.
//
// Nothing here is optional decoration. Without the icon the executable shows the system's
// generic mark in Explorer and on the taskbar; without the version block it shows no
// publisher and no version at all, and a file with no version is a file nobody can tell
// apart from a virus scanner's point of view. Both are checked by a test against the built
// file rather than trusted, because both fail silently.

use std::io::Write;
use std::path::Path;

#[path = "src/cat.rs"]
mod cat;

fn main() {
    println!("cargo:rerun-if-changed=build.rs");
    println!("cargo:rerun-if-changed=src/cat.rs");
    println!("cargo:rerun-if-changed=assets/wbtray.ico");

    let target_os = std::env::var("CARGO_CFG_TARGET_OS").unwrap_or_default();
    if target_os != "windows" {
        return;
    }

    let out = std::env::var("OUT_DIR").expect("OUT_DIR is set by cargo");
    let version = std::env::var("CARGO_PKG_VERSION").unwrap_or_else(|_| "0.0.0".into());

    // The icon is generated here rather than read from assets/, and that is deliberate:
    // the file in the repository is for a person who wants to look at the mark, while the
    // executable gets one drawn from the same code the tray draws its icon with. There is
    // then no way for the two to be different pictures.
    let ico = Path::new(&out).join("wbtray.ico");
    write_ico(&ico, &version);

    let rc = Path::new(&out).join("wbtray.rc");
    write_rc(&rc, &version, &ico);

    // windres turns the .rc into an object the linker accepts. It comes with the mingw
    // toolchain this crate is built with on Windows, so asking for it by name is asking
    // for something that is already there.
    let obj = Path::new(&out).join("wbtray.res.o");
    let status = std::process::Command::new("windres")
        .arg("--input")
        .arg(&rc)
        .arg("--output")
        .arg(&obj)
        .arg("--output-format=coff")
        .status();
    match status {
        Ok(s) if s.success() => {}
        Ok(s) => panic!("windres exited with {s}: the icon and version would be missing"),
        Err(e) => panic!(
            "windres could not be run ({e}). It ships with the mingw-w64 toolchain; \
             install it, or install the MSVC build tools and use the msvc target."
        ),
    }
    println!("cargo:rustc-link-arg={}", obj.display());
}

/// Write the icon, as the sizes Windows asks for.
fn write_ico(path: &Path, _version: &str) {
    const SIZES: [u32; 6] = [256, 64, 48, 32, 24, 16];

    // An application icon is an identity rather than a reading, so it does not follow the
    // machine's theme the way the tray icon does. The mark goes on a transparent field.
    let images: Vec<(u32, Vec<u8>)> = SIZES
        .iter()
        .map(|&s| (s, cat::icon_rgba_toned(s, [0x14, 0x14, 0x14])))
        .map(|(s, rgba)| (s, encode_png(s, &rgba)))
        .collect();

    let mut file = Vec::new();
    file.extend_from_slice(&0u16.to_le_bytes());
    file.extend_from_slice(&1u16.to_le_bytes());
    file.extend_from_slice(&(images.len() as u16).to_le_bytes());

    let mut offset = 6 + images.len() * 16;
    for (size, png) in &images {
        // 256 is written as 0: the field is one byte and cannot hold it.
        let dim = if *size >= 256 { 0u8 } else { *size as u8 };
        file.extend_from_slice(&[dim, dim, 0, 0]);
        file.extend_from_slice(&1u16.to_le_bytes());
        file.extend_from_slice(&32u16.to_le_bytes());
        file.extend_from_slice(&(png.len() as u32).to_le_bytes());
        file.extend_from_slice(&(offset as u32).to_le_bytes());
        offset += png.len();
    }
    for (_, png) in &images {
        file.extend_from_slice(png);
    }
    std::fs::write(path, file).expect("cannot write the generated icon");
}

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

/// Write the resource script: the icon, and the version block Explorer shows.
fn write_rc(path: &Path, version: &str, ico: &Path) {
    // The version block is four 16-bit fields, and the tag has to be a version number
    // rather than whatever the crate happens to be called: 3.0.0 becomes 3,0,0,0.
    let mut parts = version
        .split(['.', '-'])
        .map(|p| p.parse::<u16>().unwrap_or(0))
        .collect::<Vec<_>>();
    while parts.len() < 4 {
        parts.push(0);
    }
    let quad = format!("{},{},{},{}", parts[0], parts[1], parts[2], parts[3]);
    let text = format!(
        "1 ICON \"{}\"\n\
         \n\
         1 VERSIONINFO\n\
         FILEVERSION {quad}\n\
         PRODUCTVERSION {quad}\n\
         FILEOS 0x40004\n\
         FILETYPE 0x1\n\
         BEGIN\n\
         \tBLOCK \"StringFileInfo\"\n\
         \tBEGIN\n\
         \t\tBLOCK \"040904B0\"\n\
         \t\tBEGIN\n\
         \t\t\tVALUE \"FileDescription\", \"wbtray — a tray for the workbuddy2api gateway\"\n\
         \t\t\tVALUE \"FileVersion\", \"{version}\"\n\
         \t\t\tVALUE \"ProductName\", \"wbtray\"\n\
         \t\t\tVALUE \"ProductVersion\", \"{version}\"\n\
         \t\t\tVALUE \"CompanyName\", \"laincat\"\n\
         \t\t\tVALUE \"LegalCopyright\", \"MIT licence\"\n\
         \t\t\tVALUE \"OriginalFilename\", \"wbtray.exe\"\n\
         \t\tEND\n\
         \tEND\n\
         \tBLOCK \"VarFileInfo\"\n\
         \tBEGIN\n\
         \t\tVALUE \"Translation\", 0x409, 1200\n\
         \tEND\n\
         END\n",
        // Forward slashes: the resource script has its own escape rules, and a Windows
        // path written the Windows way arrives at windres with its backslashes eaten —
        // "\t" becomes a tab and the path it then tries to open does not exist.
        ico.display().to_string().replace('\\', "/")
    );
    let mut f = std::fs::File::create(path).expect("cannot write the resource script");
    f.write_all(text.as_bytes())
        .expect("cannot write the resource script");
}
