// The cat: the program's face, and its tray icon.
//
// It is drawn from geometry rather than carried as a picture, because one icon has
// to be produced at every size Windows asks for — 16 in the notification area at
// 100% scaling, 20 at 125%, 32 in the alt-tab list — and a bitmap scaled to those
// sizes is a bitmap that is wrong at three of them.
//
// The shape is a silhouette: a broad head, two triangular ears, a narrower chin, and
// the eyes knocked out of it as rings. Knocking them out rather than drawing them on
// is what makes the icon work on any background — the hole shows whatever is behind
// the icon, so the face reads on a light taskbar and a dark one without a second
// drawing.

/// The supersampling factor. The icon is drawn this many times larger than its final
/// size and averaged down, which is what gives the curves their soft edge.
const SS: u32 = 8;

/// A normalised point, so the geometry below can be stated as fractions of the icon
/// and one set of numbers serves every size.
fn ellipse(cx: f64, cy: f64, rx: f64, ry: f64, x: f64, y: f64) -> bool {
    let dx = (x - cx) / rx;
    let dy = (y - cy) / ry;
    dx * dx + dy * dy <= 1.0
}

/// triangle reports whether a point is inside the triangle abc, by the sign of the
/// cross products. The winding does not matter: all three signs agreeing is the test.
fn triangle(a: (f64, f64), b: (f64, f64), c: (f64, f64), p: (f64, f64)) -> bool {
    let cross = |u: (f64, f64), v: (f64, f64), w: (f64, f64)| {
        (v.0 - u.0) * (w.1 - u.1) - (v.1 - u.1) * (w.0 - u.0)
    };
    let d1 = cross(a, b, p);
    let d2 = cross(b, c, p);
    let d3 = cross(c, a, p);
    let neg = d1 < 0.0 || d2 < 0.0 || d3 < 0.0;
    let pos = d1 > 0.0 || d2 > 0.0 || d3 > 0.0;
    !(neg && pos)
}

/// head reports whether a point belongs to the cat's silhouette.
///
/// The silhouette is the union of five shapes: a broad head, a narrower chin, the
/// bar that ties them together, and the two ears. Composing it rather than listing an
/// outline keeps the shape readable — the numbers below are the shape, and a change to
/// the width of the chin is a change to one number.
fn head(x: f64, y: f64) -> bool {
    // The head, as three rounds of decreasing size stacked with heavy overlap. Stating
    // it as one round for the skull and a smaller one for the chin produced a shape
    // with a visible seam — a ball welded under a ball — and the union of three is what
    // makes the taper from cheek to chin a curve. The widest is the skull, the middle
    // one carries the jaw, and the last is the chin the reference drawing ends on.
    if ellipse(0.5, 0.395, 0.500, 0.345, x, y) {
        return true;
    }
    if ellipse(0.5, 0.585, 0.375, 0.250, x, y) {
        return true;
    }
    if ellipse(0.5, 0.760, 0.272, 0.215, x, y) {
        return true;
    }
    // The ears, as triangles tall enough to be the silhouette's most recognisable
    // feature. Their bases are inside the skull, so only the points show.
    if triangle((0.030, 0.355), (0.215, -0.070), (0.480, 0.245), (x, y)) {
        return true;
    }
    if triangle((0.970, 0.355), (0.785, -0.070), (0.520, 0.245), (x, y)) {
        return true;
    }
    // The whiskers, as thin strokes leaving the cheeks. They are what separates a cat
    // from a bear at a glance, and they are the one part of the drawing that is allowed
    // to be fine: at sixteen pixels they blur into a suggestion of fur, which is the
    // right amount.
    whisker(x, y)
}

/// eye is the white ring around a pupil. The interior is left alone, so the silhouette
/// shows through and the pupil costs nothing to draw.
fn eye(cx: f64, cy: f64, r: f64, x: f64, y: f64) -> bool {
    let dx = x - cx;
    let dy = (y - cy) / 1.02;
    let d = (dx * dx + dy * dy).sqrt();
    // A thick ring: the reference's eyes are most of the face, and a thin outline at
    // this size is a smudge rather than an eye.
    d <= r && d >= r * 0.52
}

/// whisker is one of the strokes that leave the cheeks. There are three a side, and
/// they are the detail that says "cat" rather than "bear" on a shape this simple.
fn whisker(x: f64, y: f64) -> bool {
    // Three to a side, stated as (height at the cheek, height at the tip). The middle
    // one is horizontal and the outer two fan away from it, which is the arrangement
    // the reference drawing uses and the reason they read as whiskers rather than as
    // three unrelated lines.
    const STROKES: [(f64, f64); 3] = [(0.395, 0.315), (0.475, 0.475), (0.555, 0.655)];
    const ROOT: f64 = 0.60; // where they leave the face, as a distance from the centre
    const TIP: f64 = 1.0;
    const HALF: f64 = 0.009;

    // Mirror: measure as though the point were on the right, then flip for the left.
    let mirrored = x < 0.5;
    let mx = if mirrored { 1.0 - x } else { x };
    let off = mx - 0.5;
    if off < ROOT - 0.5 || off > TIP - 0.5 {
        return false;
    }
    let t = (off - (ROOT - 0.5)) / (TIP - ROOT);
    for (y_root, y_tip) in STROKES {
        let ly = y_root + (y_tip - y_root) * t;
        if (y - ly).abs() <= HALF {
            return true;
        }
    }
    false
}

/// Render the icon in a chosen ink colour.
///
/// The ink is a parameter because the notification area is drawn by the system and its
/// two tones need opposite marks: a near-black cat is invisible on a dark taskbar and a
/// white one is invisible on a light it. One geometry, two inks.
pub fn icon_rgba_toned(size: u32, ink: [u8; 3]) -> Vec<u8> {
    let n = size * SS;
    let mut out = vec![0u8; (size * size * 4) as usize];

    for py in 0..size {
        for px in 0..size {
            // Average the supersampled grid covering this pixel. Coverage is counted
            // for the silhouette, and separately for the two details that cut into it,
            // because a hole has to punch through the ink rather than blend with it.
            let mut body = 0u32;
            let mut hole = 0u32;
            for sy in 0..SS {
                for sx in 0..SS {
                    let x = (px * SS + sx) as f64 / n as f64;
                    let y = (py * SS + sy) as f64 / n as f64;
                    let on_body = head(x, y) || whisker(x, y);
                    if on_body {
                        body += 1;
                        if eye(0.315, 0.40, 0.165, x, y) || eye(0.685, 0.40, 0.165, x, y) {
                            hole += 1;
                        }
                    }
                }
            }
            let total = (SS * SS) as f64;
            let solid = (body as f64 - hole as f64) / total;
            if solid <= 0.0 {
                continue;
            }
            let alpha = (solid * 255.0 + 0.5) as u8;
            let i = ((py * size + px) * 4) as usize;
            out[i] = ink[0];
            out[i + 1] = ink[1];
            out[i + 2] = ink[2];
            out[i + 3] = alpha;
        }
    }
    out
}
