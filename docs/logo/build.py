#!/usr/bin/env python3
"""Write the selected chartreuse List and run logo to assets/.

Copied and adapted from o-proj/docs/logo/build.py. Its family wordmark and
layout are retained; d comes from oden, s from o-slack. Marks use the 8-unit
grid, a 128 tile with rx 28, and the family's x=112 wordmark origin.

    python3 docs/logo/build.py
    python3 docs/logo/build.py --check
    python3 docs/logo/build.py --preview /tmp/o-cmds-logo/artifacts

The default writes icon.svg, logo-light.svg and logo-dark.svg. --check compares
their exact bytes and checks the inherited family wordmarks. --preview keeps
the original concept sheet reproducible, without changing production assets;
PNGs are added if rsvg-convert is on PATH. Python standard library only.
"""
import argparse
import colorsys
import html
import pathlib
import shutil
import subprocess
import sys

TOOL = "o-cmds"
ASSETS = pathlib.Path(__file__).resolve().parents[2] / "assets"
R = 'stroke-linecap="round" stroke-linejoin="round"'


def palette(hue):
    """Five HSL stops: tile, deep ink, primary, highlight, pale ink.

    Hue is the sole input; saturation/lightness are fixed across candidates.
    This is a concept recipe, not a perceptually uniform colour system.
    """
    stops = (("tile", .10, .42), ("deep", .28, .70), ("pri", .53, .64),
             ("light", .76, .67), ("pale", .94, .60))
    return {name: "#" + "".join(f"{round(c * 255):02x}" for c in
            colorsys.hls_to_rgb(hue / 360, light, sat))
            for name, light, sat in stops}


# Primary families found: amber (oden), blue (o-ai), red (o-inventory),
# teal (o-proj), violet (o-gh), pink (o-mcp), green (o-slack/ochazuke),
# purple (matomeru), mint/blue (monogatari); o-json and o-family multicolour.
# Chartreuse has no primary owner in that inventory. Cyan is a comparison.
PALETTES = {"chartreuse": palette(72), "cyan": palette(196)}


def ink(theme, p):
    return p["pri"] if theme == "dark" else p["deep"]


def prompt(theme, p):
    body = ink(theme, p)
    return (f'<rect x="24" y="32" width="80" height="64" rx="8" fill="none" stroke="{body}" stroke-width="8"/>'
            f'<path d="M40 48L56 64L40 80M72 80H88" fill="none" stroke="{body}" stroke-width="8" {R}/>')


def stack(theme, p):
    body = ink(theme, p)
    return (f'<path d="M48 24H104V80M32 32H96V96" fill="none" stroke="{body}" stroke-width="8" {R}/>'
            f'<rect x="24" y="48" width="64" height="56" rx="8" fill="{body}"/>'
            f'<path d="M40 64H72M40 80H56" stroke="{p["pale"]}" stroke-width="8" {R}/>')


def play(theme, p):
    body = ink(theme, p)
    return (f'<path d="M24 32H104M24 56H56M24 80H48M24 104H104" stroke="{body}" stroke-width="8" {R}/>'
            f'<path d="M72 48L104 72L72 96Z" fill="{body}"/>')


def notebook(theme, p):
    body = ink(theme, p)
    return (f'<rect x="32" y="24" width="72" height="80" rx="8" fill="{body}"/>'
            f'<path d="M24 40H40M24 64H40M24 88H40" stroke="{p["light"] if theme == "dark" else p["deep"]}" stroke-width="8" {R}/>'
            f'<path d="M56 48H88M56 64H80M56 80H88" stroke="{p["pale"]}" stroke-width="8" {R}/>')


def run_list(theme, p):
    body = ink(theme, p)
    return (f'<path d="M24 32H88M24 56H72" stroke="{body}" stroke-width="8" {R}/>'
            f'<rect x="24" y="72" width="80" height="32" rx="8" fill="{body}"/>'
            f'<path d="M40 88H80M72 80L80 88L72 96" fill="none" stroke="{p["pale"]}" stroke-width="8" {R}/>')


def tray(theme, p):
    body = ink(theme, p)
    return (f'<rect x="40" y="24" width="48" height="56" rx="8" fill="{body}"/>'
            f'<path d="M56 40H72M56 56H72" stroke="{p["pale"]}" stroke-width="8" {R}/>'
            f'<path d="M24 72V104H104V72" fill="none" stroke="{body}" stroke-width="8" {R}/>')


# Data drives the entire gallery: six independent functions, one hue override.
CONCEPTS = [
    ("01-prompt", "Prompt card", "A saved shell prompt, ready to inspect and run.", prompt),
    ("02-stack", "Script stack", "The newest script sits on top of a physical stack; this depicts layering, not list order.", stack),
    ("03-play", "Play the script", "A run triangle interrupts the lines of a small script.", play),
    ("04-notebook", "Command notebook", "A pocket spiral notebook collects reusable commands.", notebook),
    ("05-run-list", "List and run", "The newest bottom row becomes a run arrow: the strongest match to o-cmds.", run_list),
    ("06-tray", "Command tray", "An agent drops a script into the owner's tray for later use.", tray),
]
FAVOURITE = "05-run-list"


# --- the family wordmark -------------------------------------------------
# Each glyph is (width, path builder at x). Shapes are copied from the
# ochazuke and matomeru logos (o, c, h, a, z, u, k, e, m, t, r); i, n, v,
# p, j, y and the hyphen are drawn with the same pieces.
STROKE, GAP, START, BASE_H, DESC_H = 11, 15, 112, 96, 112


def f(v):
    s = f"{v:.1f}"
    return s[:-2] if s.endswith(".0") else s


GLYPHS = {
    "o": (40, lambda x: f"M{f(x+20)} 44a20 20 0 1 0 0.01 0"),
    "c": (34.1, lambda x: f"M{f(x+34.1)} 49.9A20 20 0 1 0 {f(x+34.1)} 78.1"),
    "h": (36, lambda x: f"M{f(x)} 18V84M{f(x)} 62a18 18 0 0 1 36 0V84"),
    "a": (40, lambda x: f"M{f(x+20)} 44a20 20 0 1 0 0.01 0M{f(x+40)} 44V84"),
    "z": (34, lambda x: f"M{f(x)} 44h34L{f(x)} 84h34"),
    "u": (36, lambda x: f"M{f(x)} 44V66a18 18 0 0 0 36 0M{f(x+36)} 44V84"),
    "k": (32, lambda x: f"M{f(x)} 18V84M{f(x+31)} 44L{f(x)} 70M{f(x+11)} 61L{f(x+32)} 84"),
    "e": (40, lambda x: f"M{f(x)} 64H{f(x+40)}A20 20 0 1 0 {f(x+34.1)} 78.1"),
    "m": (52, lambda x: f"M{f(x)} 44V84M{f(x)} 57a13 13 0 0 1 26 0V84M{f(x+26)} 57a13 13 0 0 1 26 0V84"),
    "t": (23, lambda x: f"M{f(x+7)} 26V73a11 11 0 0 0 11 11h4M{f(x)} 44H{f(x+23)}"),
    "r": (24, lambda x: f"M{f(x)} 44V84M{f(x)} 64a20 20 0 0 1 20 -20h4"),
    "i": (0, lambda x: f"M{f(x)} 44V84M{f(x)} 24h0.01"),
    "n": (36, lambda x: f"M{f(x)} 44V84M{f(x)} 62a18 18 0 0 1 36 0V84"),
    "v": (34, lambda x: f"M{f(x)} 44L{f(x+17)} 84L{f(x+34)} 44"),
    "-": (16, lambda x: f"M{f(x)} 64h16"),  # as in o-ai's logo
    # Descenders reach y 106; the hook is t's foot turned down and left.
    "p": (40, lambda x: f"M{f(x)} 44V106M{f(x+20)} 44a20 20 0 1 0 0.01 0"),
    # j is spaced like i, by its stem: its hook tucks under the letter before.
    "j": (0, lambda x: f"M{f(x)} 44V95a11 11 0 0 1 -11 11h-4M{f(x)} 24h0.01"),
    "y": (36, lambda x: f"M{f(x)} 44V66a18 18 0 0 0 36 0M{f(x+36)} 44V95a11 11 0 0 1 -11 11h-4"),
    "d": (40, lambda x: f"M{f(x+20)} 44a20 20 0 1 0 0.01 0M{f(x+40)} 18V84"),
    "s": (30, lambda x: f"M{f(x+30)} 50A15 10 0 0 0 {f(x+15)} 44A15 10 0 0 0 {f(x+15)} 64A15 10 0 0 1 {f(x+15)} 84A15 10 0 0 1 {f(x)} 78"),
}
DESCENDERS = set("pjy")


def wordmark(name):
    x, parts = START, []
    for ch in name:
        w, d = GLYPHS[ch]
        parts.append(d(x))
        x += w + GAP
    right = x - GAP
    width = int(right + STROKE / 2 + 3.5)  # ochazuke 509.1 -> 518, matomeru 524 -> 533
    height = DESC_H if DESCENDERS & set(name) else BASE_H
    return "".join(parts), width, height


def head(w, h):
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {w} {h}" width="{w}" height="{h}" '
            f'role="img" aria-label="{TOOL}">\n<title>{TOOL}</title>\n')


def icon(mark, palette):
    TILE = palette["tile"]
    return (head(128, 128) + f'<rect width="128" height="128" rx="28" fill="{TILE}"/>\n'
            f'<g>\n{mark("dark", palette)}\n</g>\n</svg>\n')


def logo(theme, mark, palette):
    TEXT = {"light": palette["tile"], "dark": palette["pale"]}
    d, w, h = wordmark(TOOL)
    # The mark's centre (64, 64) lands at (44, 48), where ochazuke's and
    # matomeru's marks sit in their logos.
    return (head(w, h) + f'<g transform="translate(-20 -16)">\n{mark(theme, palette)}\n</g>\n'
            f'<path d="{d}" fill="none" stroke="{TEXT[theme]}" stroke-width="{STROKE}" {R}/>\n</svg>\n')


def check():
    """Check production bytes and inherited family wordmarks."""
    import re
    ok = True
    for name, svg in production().items():
        path = ASSETS / name
        same = path.exists() and path.read_bytes() == svg.encode("utf-8")
        ok &= same
        print(f"{name}: {'same' if same else 'MISSING or DIFFERENT'}")
    for fam in ("ochazuke", "matomeru"):
        f = pathlib.Path.home() / f"proj/{fam}/assets/logo-light.svg"
        if not f.exists():
            print(f"skip {fam}: {f} not found")
            continue
        t = f.read_text()
        want = re.findall(r'<path d="(M[^"]*)" fill="none" stroke="#[0-9a-f]+" stroke-width="11"', t)[-1]
        want_w = int(re.search(r'viewBox="0 0 (\d+) 96"', t).group(1))
        d, w, _ = wordmark(fam)
        split = lambda s: re.sub(r"([A-Za-z])", r" \1 ", s).split()
        a, b = split(d), split(want)
        # Numbers may differ by 0.1 from rounding (ochazuke's e ends at 503.3, x + 34.2).
        near = lambda u, v: u == v or (u[0] not in "MmAaHhVvLlZz" and abs(float(u) - float(v)) < 0.15)
        same = len(a) == len(b) and all(map(near, a, b)) and w == want_w
        ok &= same
        print(f"{fam}: {'same' if same else 'DIFFERENT'} wordmark ({w} wide)")
    return ok


def production():
    p = PALETTES["chartreuse"]
    return {"icon.svg": icon(run_list, p),
            **{f"logo-{theme}.svg": logo(theme, run_list, p)
               for theme in ("light", "dark")}}


def variants():
    for slug, title, idea, mark in CONCEPTS:
        yield slug, title, idea, mark, "chartreuse"
    slug, title, idea, mark = next(c for c in CONCEPTS if c[0] == FAVOURITE)
    yield slug + "-cyan", title + " / cyan", "Same favourite geometry in an alternative hue.", mark, "cyan"


def preview(out):
    out.mkdir(parents=True, exist_ok=True)
    renderer = shutil.which("rsvg-convert")
    cards = []
    for slug, title, idea, mark, hue in variants():
        p = PALETTES[hue]
        tile = icon(mark, p)
        logos = {theme: logo(theme, mark, p) for theme in ("light", "dark")}
        svgs = {"icon": tile, **{f"logo-{t}": s for t, s in logos.items()}}
        for name, svg in svgs.items():
            (out / f"{slug}-{name}.svg").write_text(svg)
        samples = []
        for theme, bg in (("light", "#f8f9f3"), ("dark", "#10151b")):
            icons = "".join(f'<figure><div class="sample" style="width:{size}px;height:{size}px">{tile}</div>'
                            f'<figcaption>{size} px</figcaption></figure>' for size in (16, 32, 128))
            samples.append(f'<div class="theme {theme}"><div class="theme-label">{theme} background</div>'
                           f'<div class="sizes">{icons}</div><div class="logo">{logos[theme]}</div></div>')
            if renderer:
                for size in (16, 32, 128):
                    subprocess.run([renderer, "-w", str(size), "-h", str(size), "-b", bg,
                                    "-o", str(out / f"{slug}-icon-{theme}-{size}.png"),
                                    str(out / f"{slug}-icon.svg")], check=True)
                subprocess.run([renderer, "-w", "720", "-b", bg,
                                "-o", str(out / f"{slug}-logo-{theme}.png"),
                                str(out / f"{slug}-logo-{theme}.svg")], check=True)
        badge = ' <span class="badge">favourite</span>' if slug == FAVOURITE else ""
        cards.append(f'<article><h2>{html.escape(title)}{badge}</h2><p class="idea">{html.escape(idea)}</p>'
                     f'<div class="pair">{"".join(samples)}</div><p class="meta">{slug} · {hue} · {p["pri"]}</p></article>')
    swatches = "".join(f'<span class="swatch" style="background:{value};color:{PALETTES["chartreuse"]["pale"] if key in ("tile", "deep") else PALETTES["chartreuse"]["tile"]}">{key} {value}</span>'
                       for key, value in PALETTES["chartreuse"].items())
    document = """<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>o-cmds / logo concepts</title>
<style>
*{box-sizing:border-box}body{margin:0;background:#e9ece3;color:#20251b;font:16px/1.5 system-ui,sans-serif}
main{max-width:1440px;margin:auto;padding:48px 32px}header{max-width:920px;margin-bottom:32px}
h1{font-size:clamp(32px,5vw,56px);line-height:1.1;letter-spacing:-.04em;margin:12px 0 24px}
h2{font-size:23px;letter-spacing:-.02em;margin:0}.eyebrow,.meta,.theme-label,figcaption{font:12px/1.5 ui-monospace,monospace}
.eyebrow{text-transform:uppercase;letter-spacing:.16em}.intro{font-size:18px}.swatch{display:inline-block;padding:8px 12px;border-radius:6px;margin:0 6px 6px 0;font:12px ui-monospace,monospace}
.gallery{display:grid;gap:28px}article{border:1px solid #cbd1c1;border-radius:18px;padding:24px;background:#f2f4ed;break-inside:avoid}
.idea{margin:8px 0 20px}.pair{display:grid;grid-template-columns:1fr 1fr;gap:12px}.theme{padding:24px;border-radius:12px;min-width:0}
.light{background:#f8f9f3;color:#414938}.dark{background:#10151b;color:#cdd3d9}.theme-label{text-transform:uppercase;letter-spacing:.08em}
.sizes{display:flex;align-items:center;gap:32px;min-height:190px}figure{margin:0;text-align:center}.sample{margin:auto}.sample svg{display:block;width:100%;height:100%}
figcaption{margin-top:12px}.logo{margin-top:20px;max-width:480px}.logo svg{display:block;width:100%;height:auto}
.meta{margin-bottom:0;color:#606954}.badge{display:inline-block;font:11px ui-monospace,monospace;vertical-align:middle;text-transform:uppercase;background:#d9e592;padding:5px 8px;border-radius:20px;margin-left:8px}
footer{padding:28px 0;color:#606954;max-width:850px}@media(max-width:760px){main{padding:28px 16px}.pair{grid-template-columns:1fr}article{padding:16px}.sizes{gap:24px}}
@media print{body{background:white}main{padding:0}article{border-radius:0}.theme{-webkit-print-color-adjust:exact;print-color-adjust:exact}}
</style><main><header><div class="eyebrow">o-family / concepts first / 30 October 2026</div>
<h1>Small scripts.<br>Ready when you are.</h1>
<p class="intro">Six directions for o-cmds: agents leave scripts; the owner lists, views and runs them. The newest command sits at the bottom.</p>
<p>Chartreuse (72°) is unclaimed as a primary hue in the inspected family assets. Green, teal, blue, violet, pink, red and amber already have owners. Its yellow-green bias distinguishes it from the greens, though ochazuke uses lime as a detail. Cyan (196°) is an alternative on the favourite.</p>
<div>SWATCHES</div><p>Each tile is 128 × 128 with a 28-unit corner. Marks follow an 8-unit grid; samples below are actual 16, 32 and 128 CSS pixels. Logos retain the family's hand-drawn monoline wordmark, with the mark at x 0–88 and lettering from x 112.</p>
</header><div class="gallery">CARDS</div><footer>Recommendation: List and run. Its bottom-row emphasis reflects the tool's ordering and works as a small silhouette. Prompt card is the most familiar shell cue. No production logo has been selected; assets/ is untouched. All SVGs and styling are inline; this sheet needs no network or adjacent files.</footer></main></html>
""".replace("SWATCHES", swatches).replace("CARDS", "".join(cards))
    (out / "concepts.html").write_text(document)
    print(out / "concepts.html")
    print("PNG previews rendered" if renderer else "rsvg-convert unavailable; SVG and HTML only")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--check", action="store_true", help="compare production assets and inherited family wordmarks")
    mode.add_argument("--preview", type=pathlib.Path, metavar="DIR", help="render the original concept sheet")
    args = parser.parse_args()
    if args.check:
        sys.exit(0 if check() else 1)
    if args.preview is not None:
        preview(args.preview)
        return
    ASSETS.mkdir(exist_ok=True)
    for name, svg in production().items():
        path = ASSETS / name
        path.write_bytes(svg.encode("utf-8"))
        print(path)


if __name__ == "__main__":
    main()
