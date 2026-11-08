"""Generate the Chaos Claw brand SVGs (flattened geometry, one light source).

Usage: python3 generate.py [out-dir]   (defaults to this directory)
Run build-icons.sh afterwards to refresh the app icons.
"""
import math
import sys
from pathlib import Path

OUT = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).resolve().parent
OUT.mkdir(parents=True, exist_ok=True)
C = 128.0

# Claw in local units, wrist at the bottom, opening towards local "up".
CLAW_PARTS = [
    ("ellipse", (26, 44, 19, 16, -25)),
    ("path", "M36 52 C52 46 62 30 58 10 C52 22 46 30 34 34 Z"),
    ("path", "M10 38 C10 18 28 2 54 4 C42 10 32 18 28 34 Z"),
]
CLAW_WRIST = (22.0, 58.0)  # local point that sits on the arm end
CLAW_UP_DEG = 8.0  # local opening leans this far right of straight up

CROWN = "M114 137 L111 118 L120 125 L128 112 L136 125 L145 118 L142 137 Z"


def f(v):
    return f"{v:.2f}".rstrip("0").rstrip(".")


def spiral(theta0_deg, width_mul, r0=18.0, r1=80.0, sweep=118.0, w0=40.0, w1=20.0, n=40):
    """Tapered blade along an outward clockwise spiral. Returns (path, end point, end tangent angle)."""
    left, right, centre = [], [], []
    for i in range(n + 1):
        t = i / n
        th = math.radians(theta0_deg + sweep * t)
        r = r0 + (r1 - r0) * (t ** 0.85)
        centre.append((C + r * math.cos(th), C + r * math.sin(th)))
    for i, (x, y) in enumerate(centre):
        a = centre[max(i - 1, 0)]
        b = centre[min(i + 1, n)]
        tx, ty = b[0] - a[0], b[1] - a[1]
        ln = math.hypot(tx, ty)
        nx, ny = -ty / ln, tx / ln
        t = i / n
        w = (w0 + (w1 - w0) * t) * width_mul / 2
        left.append((x + nx * w, y + ny * w))
        right.append((x - nx * w, y - ny * w))
    pts = left + right[::-1]
    d = "M" + " L".join(f"{f(x)} {f(y)}" for x, y in pts) + " Z"
    a, b = centre[-2], centre[-1]
    tang = math.degrees(math.atan2(b[1] - a[1], b[0] - a[0]))
    return d, b, tang


def claw_transform(end, tang_deg, scale):
    # Local "up" is -y (angle -90deg). Rotate so local up (+lean) aligns with the tangent.
    rot = tang_deg - (-90.0 + CLAW_UP_DEG)
    return (f"translate({f(end[0])} {f(end[1])}) rotate({f(rot)}) "
            f"scale({f(scale)}) translate({f(-CLAW_WRIST[0])} {f(-CLAW_WRIST[1])})")


def claw_markup(transform, fill, extra=""):
    parts = []
    for kind, v in CLAW_PARTS:
        if kind == "ellipse":
            cx, cy, rx, ry, rot = v
            parts.append(f'<ellipse cx="{cx}" cy="{cy}" rx="{rx}" ry="{ry}" transform="rotate({rot} {cx} {cy})"/>')
        else:
            parts.append(f'<path d="{v}"/>')
    return f'<g transform="{transform}" fill="{fill}"{extra}>' + "".join(parts) + "</g>"


CLAW_SAMPLES = [(36, 52), (52, 46), (62, 30), (58, 10), (52, 22), (10, 38), (10, 18), (28, 2), (54, 4)]
for _i in range(24):
    _a = 2 * math.pi * _i / 24
    _x, _y = 19 * math.cos(_a), 16 * math.sin(_a)
    _r = math.radians(-25)
    CLAW_SAMPLES.append((26 + _x * math.cos(_r) - _y * math.sin(_r), 44 + _x * math.sin(_r) + _y * math.cos(_r)))


def figure(fill_arm, fill_claw, width_mul=1.0, claw_scale=0.8, theta0=-150.0, stroke=None):
    """Three blades + claws, flattened (no rotated <use>), so gradients light them all from one side."""
    out = []
    for k in range(3):
        d, end, tang = spiral(theta0 + 120 * k, width_mul)
        s = f' stroke="{stroke[0]}" stroke-width="{stroke[1]}" stroke-linejoin="round"' if stroke else ""
        out.append(f'<path d="{d}" fill="{fill_arm}"{s}/>')
        out.append(claw_markup(claw_transform(end, tang, claw_scale), fill_claw, s))
    return "".join(out)


def svg(body, defs="", extra_attrs=""):
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256" width="256" height="256"{extra_attrs}>'
            f"<title>Rust King Ledger</title><defs>{defs}</defs>{body}</svg>\n")


LIGHT = 'gradientUnits="userSpaceOnUse" x1="40" y1="40" x2="216" y2="216"'
GRADS = (
    f'<linearGradient id="gold" {LIGHT}><stop offset="0" stop-color="#fbe7a6"/><stop offset="0.45" stop-color="#e0b552"/><stop offset="1" stop-color="#b07d27"/></linearGradient>'
    f'<linearGradient id="claw" {LIGHT}><stop offset="0" stop-color="#f6834c"/><stop offset="0.5" stop-color="#d9482c"/><stop offset="1" stop-color="#a5321a"/></linearGradient>'
    '<radialGradient id="core" gradientUnits="userSpaceOnUse" cx="118" cy="116" r="34"><stop offset="0" stop-color="#ea6a3c"/><stop offset="0.7" stop-color="#b8391f"/><stop offset="1" stop-color="#7d2412"/></radialGradient>'
    '<linearGradient id="rim" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#f1d78c"/><stop offset="0.5" stop-color="#c89b3c"/><stop offset="1" stop-color="#8a6420"/></linearGradient>'
    '<filter id="shadow" x="-20%" y="-20%" width="140%" height="140%"><feDropShadow dx="0" dy="3" stdDeviation="2.5" flood-color="#000" flood-opacity="0.55"/></filter>'
)


def ticks(r_in, r_out, count, width, colour):
    segs = []
    for i in range(count):
        a = 2 * math.pi * i / count
        segs.append(f"M{f(C + r_in * math.cos(a))} {f(C + r_in * math.sin(a))} L{f(C + r_out * math.cos(a))} {f(C + r_out * math.sin(a))}")
    return f'<path d="{" ".join(segs)}" stroke="{colour}" stroke-width="{width}" fill="none"/>'


def core_rich(r=26):
    return (f'<circle cx="128" cy="128" r="{r + 5}" fill="#0b0806"/>'
            f'<circle cx="128" cy="128" r="{r}" fill="url(#core)" stroke="url(#rim)" stroke-width="4"/>'
            f'<path d="{CROWN}" fill="#f1d78c" stroke="#6e4a14" stroke-width="1.5" stroke-linejoin="round"/>')


def scaled(body, s, dx=0.0, dy=0.0):
    return f'<g transform="translate({f(128 + dx)} {f(128 + dy)}) scale({f(s)}) translate(-128 -128)">{body}</g>'




def figure_points(width_mul=1.0, claw_scale=0.8, theta0=-150.0):
    pts = []
    for k in range(3):
        d, end, tang = spiral(theta0 + 120 * k, width_mul)
        for tok in d.replace("M", "").replace("Z", "").split(" L"):
            x, y = tok.split()
            pts.append((float(x), float(y)))
        rot = math.radians(tang - (-90.0 + CLAW_UP_DEG))
        for lx, ly in CLAW_SAMPLES:
            px, py = (lx - CLAW_WRIST[0]) * claw_scale, (ly - CLAW_WRIST[1]) * claw_scale
            pts.append((end[0] + px * math.cos(rot) - py * math.sin(rot), end[1] + px * math.sin(rot) + py * math.cos(rot)))
    return pts


def best_theta(width_mul, claw_scale):
    """Rotation (within one 120deg period) whose bbox centre is closest to the rotation centre."""
    best = None
    for t10 in range(-1800, -600, 5):
        th = t10 / 10
        pts = figure_points(width_mul, claw_scale, th)
        xs, ys = [p[0] for p in pts], [p[1] for p in pts]
        off = math.hypot((min(xs) + max(xs)) / 2 - C, (min(ys) + max(ys)) / 2 - C)
        if best is None or off < best[0]:
            best = (off, th)
    return best[1]


def place_round(body, radius, width_mul, claw_scale, theta0):
    """Keep the rotation centre at the middle; scale so the farthest point reaches `radius`."""
    r = max(math.hypot(x - C, y - C) for x, y in figure_points(width_mul, claw_scale, theta0))
    return scaled(body, radius / r)


def place_square(body, half, width_mul, claw_scale, theta0):
    """Centre the bbox (optical centre) and scale its larger half-extent to `half`."""
    pts = figure_points(width_mul, claw_scale, theta0)
    xs, ys = [p[0] for p in pts], [p[1] for p in pts]
    s = half / (max(max(xs) - min(xs), max(ys) - min(ys)) / 2)
    cx, cy = (min(xs) + max(xs)) / 2, (min(ys) + max(ys)) / 2
    return f'<g transform="translate(128 128) scale({f(s)}) translate({f(-cx)} {f(-cy)})">{body}</g>'


def variant(fill_arm, fill_claw, core, wm, cs, th, stroke=None):
    return figure(fill_arm, fill_claw, width_mul=wm, claw_scale=cs, theta0=th, stroke=stroke) + core


mark_face = ('<radialGradient id="face" cx="0.42" cy="0.36" r="0.7"><stop offset="0" stop-color="#3b2519"/>'
             '<stop offset="0.65" stop-color="#1c120d"/><stop offset="1" stop-color="#0e0907"/></radialGradient>')
tile_defs = ('<radialGradient id="iron" cx="0.45" cy="0.35" r="0.85"><stop offset="0" stop-color="#4a2f1f"/>'
             '<stop offset="0.7" stop-color="#2a1a12"/><stop offset="1" stop-color="#4a2f1f"/></radialGradient>')
TH = best_theta(1.0, 0.8)
TH_S = best_theta(1.25, 0.95)


# Master mark (128px and up): coin, milled rim, engraved face, one light source.
fig = figure("url(#gold)", "url(#claw)", theta0=TH)
mark = svg(
    '<circle cx="128" cy="128" r="124" fill="#0b0806"/>'
    '<circle cx="128" cy="128" r="119" fill="#2a1a12" stroke="url(#rim)" stroke-width="6"/>'
    + ticks(106.5, 113, 84, 1.6, "#a8782a")
    + '<circle cx="128" cy="128" r="105" fill="url(#face)" stroke="#5a3c10" stroke-width="1.5"/>'
    + '<g filter="url(#shadow)">' + place_round(fig, 100, 1.0, 0.8, TH) + "</g>"
    + core_rich(),
    GRADS + mark_face)
(OUT / "rkl-mark.svg").write_text(mark)

# App tile (64px and up). Core is drawn inside the same transform so it stays at the swirl's centre.
tile_bg = ('<rect width="256" height="256" rx="56" fill="url(#iron)"/>'
           '<rect x="4" y="4" width="248" height="248" rx="52" fill="none" stroke="#c89b3c" stroke-width="4"/>')
tile_art = place_square('<g filter="url(#shadow)">' + fig + "</g>" + core_rich(), 104, 1.0, 0.8, TH)
(OUT / "rkl-app-icon.svg").write_text(svg(tile_bg + tile_art, GRADS + tile_defs))
(OUT / "rkl-app-icon-macos.svg").write_text(svg(scaled(tile_bg + tile_art, 824 / 1024), GRADS + tile_defs))

# App tile, 32px master: flat, no crown/shadow/gradients/inner border.
(OUT / "rkl-app-icon-32.svg").write_text(svg(
    '<rect width="256" height="256" rx="56" fill="#3a2418"/>'
    + place_square(variant("#e9bd5a", "#e2502e", '<circle cx="128" cy="128" r="24" fill="#ce422b"/>', 1.2, 0.9, TH_S), 106, 1.2, 0.9, TH_S)))

# App tile, 16px master: figure fills the tile, no border.
(OUT / "rkl-app-icon-16.svg").write_text(svg(
    '<rect width="256" height="256" rx="48" fill="#3a2418"/>'
    + place_square(variant("#f0c45e", "#ea5530", '<circle cx="128" cy="128" r="26" fill="#ce422b"/>', 1.25, 0.95, TH_S), 112, 1.25, 0.95, TH_S)))

# Colour glyph, 32px: disc, 16-unit ring, 12 units clear to the figure.
(OUT / "rkl-glyph.svg").write_text(svg(
    '<circle cx="128" cy="128" r="126" fill="#1a110c"/>'
    '<circle cx="128" cy="128" r="116" fill="none" stroke="#e0b552" stroke-width="16"/>'
    + place_round(variant("#e0b552", "#e2502e", '<circle cx="128" cy="128" r="22" fill="#ce422b"/>', 1.2, 0.85, TH_S), 96, 1.2, 0.85, TH_S)))

# Colour glyph, 16px / tray: transparent, 1px dark outline for light and dark trays.
outline = variant("#1a110c", "#1a110c", '<circle cx="128" cy="128" r="22" fill="#1a110c" stroke="#1a110c" stroke-width="16"/>', 1.25, 0.95, TH_S, stroke=("#1a110c", 16))
fill16 = variant("#f0c45e", "#ea5530", '<circle cx="128" cy="128" r="22" fill="#ce422b"/>', 1.25, 0.95, TH_S)
(OUT / "rkl-glyph-16.svg").write_text(svg(place_square(outline + fill16, 116, 1.25, 0.95, TH_S)))

# Mono (currentColor): no ring, ~240/256, crown knocked out of the core (crown follows the figure transform).
knock = ('<mask id="knock" maskUnits="userSpaceOnUse" x="-64" y="-64" width="384" height="384">'
         '<rect x="-64" y="-64" width="384" height="384" fill="#fff"/>'
         f'<g transform="translate(128 128) scale(1.1) translate(-128 -128)"><path d="{CROWN}" fill="#000"/></g></mask>')
mono_fig = ('<g mask="url(#knock)">'
            + variant("currentColor", "currentColor", '<circle cx="128" cy="128" r="30" fill="currentColor"/>', 1.2, 0.9, TH_S)
            + "</g>")
(OUT / "rkl-glyph-mono.svg").write_text(svg(place_square(mono_fig, 120, 1.2, 0.9, TH_S), knock, ' color="#000000"'))
