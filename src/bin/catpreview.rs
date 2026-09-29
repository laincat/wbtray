// Renders the cat at the sizes Windows asks for, so the shape can be looked at
// rather than guessed at. A development tool, not part of the program.

use std::fs::File;
use std::io::BufWriter;

#[path = "../cat.rs"]
mod cat;

fn main() {
    let sizes = [16u32, 20, 24, 32, 48, 64, 128, 256];
    let scale = [24u32, 20, 16, 12, 8, 6, 3, 1];
    let pad = 16u32;
    let tones = [[0x1au8, 0x1a, 0x1a], [0xff, 0xff, 0xff]];

    let row_h: u32 = sizes
        .iter()
        .zip(scale.iter())
        .map(|(s, k)| s * k)
        .max()
        .unwrap();
    let width: u32 = sizes
        .iter()
        .zip(scale.iter())
        .map(|(s, k)| s * k + pad)
        .sum::<u32>()
        + pad;
    let height = (row_h + pad) * tones.len() as u32 + pad;

    let mut img = vec![0u8; (width * height * 4) as usize];

    for (row, ink) in tones.iter().enumerate() {
        let bg = if row == 0 {
            [0xf4u8, 0xf4, 0xf4]
        } else {
            [0x20, 0x20, 0x20]
        };
        let y0 = pad + (row as u32) * (row_h + pad);
        for y in 0..row_h {
            for x in 0..width {
                let i = (((y0 + y) * width + x) * 4) as usize;
                img[i] = bg[0];
                img[i + 1] = bg[1];
                img[i + 2] = bg[2];
                img[i + 3] = 255;
            }
        }
        let mut x0 = pad;
        for (size, k) in sizes.iter().zip(scale.iter()) {
            let src = cat::icon_rgba_toned(*size, *ink);
            let kk = *k;
            for sy in 0..(*size * kk) {
                for sx in 0..(*size * kk) {
                    let sxp = sx / kk;
                    let syp = sy / kk;
                    let si = ((syp * size + sxp) * 4) as usize;
                    let a = src[si + 3] as u32;
                    if a == 0 {
                        continue;
                    }
                    let bgpx = &mut img[(((y0 + sy) * width + (x0 + sx)) * 4) as usize..];
                    for c in 0..3 {
                        let fg = src[si + c] as u32;
                        let bgc = bgpx[c] as u32;
                        bgpx[c] = ((fg * a + bgc * (255 - a)) / 255) as u8;
                    }
                }
            }
            x0 += size * kk + pad;
        }
    }

    let out = std::env::args()
        .nth(1)
        .unwrap_or_else(|| "cat-preview.png".into());
    let f = File::create(&out).expect("cannot write the preview");
    let mut w = BufWriter::new(f);
    let mut enc = png::Encoder::new(&mut w, width, height);
    enc.set_color(png::ColorType::Rgba);
    enc.set_depth(png::BitDepth::Eight);
    let mut writer = enc.write_header().unwrap();
    writer.write_image_data(&img).unwrap();
    println!("wrote {out} ({width}x{height})");
}
