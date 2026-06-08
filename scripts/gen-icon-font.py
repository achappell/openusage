#!/usr/bin/env python3
"""Generate the OpenUsage provider-icon TrueType font.

This script builds ``internal/tmux/assets/openusage-icons.ttf`` from the
monochrome provider SVG icons. The font lets tmux status bars (and any other
consumer) render real provider icons via a single custom-font glyph tier.

Source of truth
---------------
``internal/tmux/assets/icons.json`` is the authoritative manifest. It defines
the font family/version, the em-units-per-em (upem), and one glyph entry per
provider mapping a Private Use Area codepoint to an SVG basename. This script
reads that manifest and never invents glyphs of its own; to add or change a
glyph, edit the JSON.

For each glyph entry the matching SVG is read from
``website/public/icons/<svg>.svg``. The SVGs are monochrome
(``fill="currentColor"``), use ``viewBox="0 0 24 24"`` and ``fill-rule="evenodd"``,
and contain one or more ``<path d="...">`` elements. All paths in a single SVG
are merged into one glyph.

The SVG coordinate system has its origin at the top-left with the y-axis
pointing down, while fonts place the origin at the baseline with the y-axis
pointing up.

Scaling is driven by each glyph's actual *ink* bounding box rather than the
nominal 24x24 viewBox. These icons carry internal padding, so scaling by the
viewBox left the rendered glyph well short of the cell. Instead each outline is
measured (its ink bbox in SVG coordinates) and scaled uniformly so the ink
height fills ~92% of the em, then centered horizontally and vertically inside
the em box, keeping aspect ratio.

Usage
-----
    /tmp/fontvenv/bin/python scripts/gen-icon-font.py

Requires the ``fonttools`` library (tested with 4.63).
"""

from __future__ import annotations

import json
import os
import sys
import xml.etree.ElementTree as ET

try:
    from fontTools.fontBuilder import FontBuilder
    from fontTools.pens.boundsPen import ControlBoundsPen
    from fontTools.pens.cu2quPen import Cu2QuPen
    from fontTools.pens.recordingPen import RecordingPen
    from fontTools.pens.transformPen import TransformPen
    from fontTools.pens.ttGlyphPen import TTGlyphPen
    from fontTools.svgLib.path import parse_path
except ImportError as exc:  # pragma: no cover - environment guard
    sys.stderr.write(
        "error: fonttools is not installed. Run this with the prepared venv:\n"
        "  /tmp/fontvenv/bin/python scripts/gen-icon-font.py\n"
        "  (recreate with: python3 -m venv /tmp/fontvenv && "
        "/tmp/fontvenv/bin/pip install fonttools)\n"
        f"  underlying error: {exc}\n"
    )
    raise SystemExit(1)

# Repo paths, resolved relative to this script so it works from any cwd.
SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(SCRIPT_DIR)
MANIFEST_PATH = os.path.join(REPO_ROOT, "internal", "tmux", "assets", "icons.json")
ICONS_DIR = os.path.join(REPO_ROOT, "website", "public", "icons")
OUTPUT_PATH = os.path.join(REPO_ROOT, "internal", "tmux", "assets", "openusage-icons.ttf")

# SVG viewBox is always 24x24 for these icons.
SVG_VIEWBOX = 24.0

# Fraction of the em that the ink height should occupy. Leaves a small top and
# bottom margin so glyphs do not visually touch the cell edges.
INK_FILL = 0.92

NOTDEF = ".notdef"

SVG_NS = "http://www.w3.org/2000/svg"


def _path_ds(svg_path: str) -> list[str]:
    """Return the ``d`` attribute of every ``<path>`` element in an SVG file.

    Handles documents with and without the SVG namespace declared.
    """
    tree = ET.parse(svg_path)
    root = tree.getroot()

    # Match <path> with or without a namespace prefix.
    ds: list[str] = []
    for elem in root.iter():
        tag = elem.tag
        if isinstance(tag, str):
            local = tag.rsplit("}", 1)[-1]  # strip any {namespace}
            if local == "path":
                d = elem.get("d")
                if d:
                    ds.append(d)
    return ds


def _build_glyph(svg_path: str, upem: int) -> "object":
    """Parse all paths in *svg_path* into a single, ink-filling TrueType glyph.

    The outline is recorded once, measured for its *ink* bounding box in SVG
    coordinates, then scaled so the ink height fills ``INK_FILL`` of the em and
    centered inside the em box. The Y axis is flipped (SVG is y-down) and the
    result is converted to quadratics for the ``glyf`` table.
    """
    ds = _path_ds(svg_path)
    if not ds:
        raise ValueError(f"no <path> elements found in {svg_path}")

    # Record the raw SVG outline once so we can both measure and replay it.
    rec = RecordingPen()
    for d in ds:
        parse_path(d, rec)

    # Measure the ink bbox in SVG coords (control-point bounds are sufficient
    # and avoid pulling in a cython dependency for exact bezier extrema).
    bounds = ControlBoundsPen(None)
    rec.replay(bounds)
    if bounds.bounds is None:
        raise ValueError(f"empty outline in {svg_path}")
    xmin, ymin, xmax, ymax = bounds.bounds
    ink_w = xmax - xmin
    ink_h = ymax - ymin
    if ink_h <= 0 or ink_w <= 0:
        raise ValueError(f"degenerate ink bbox in {svg_path}")

    # Uniform scale so the ink HEIGHT maps to INK_FILL * upem, preserving aspect
    # ratio.
    scale = (INK_FILL * upem) / ink_h

    # Center the scaled ink inside the em box [0, upem] both axes.
    scaled_w = ink_w * scale
    scaled_h = ink_h * scale
    x_pad = (upem - scaled_w) / 2.0
    y_pad = (upem - scaled_h) / 2.0

    # Affine mapping svg(x, y) -> font(X, Y), with Y flipped (svg y is down):
    #   X = scale*(x - xmin) + x_pad
    #   Y = scale*(ymax - y) + y_pad
    # In (a, b, c, d, e, f) form (X = a*x + c*y + e ; Y = b*x + d*y + f):
    #   a = scale, c = 0, e = x_pad - scale*xmin
    #   b = 0, d = -scale, f = y_pad + scale*ymax
    transform = (
        scale,
        0.0,
        0.0,
        -scale,
        x_pad - scale * xmin,
        y_pad + scale * ymax,
    )

    pen = TTGlyphPen(None)
    # SVG paths use cubic beziers; TrueType glyf needs quadratics, so convert
    # via Cu2QuPen. Tolerance is in font units (~1 unit at upem=1000 is well
    # below pixel-perceptible at icon sizes).
    cu2qu = Cu2QuPen(pen, max_err=1.0, reverse_direction=True)
    tpen = TransformPen(cu2qu, transform)
    rec.replay(tpen)
    return pen.glyph()


def _notdef_glyph(upem: int) -> "object":
    """A simple hollow box glyph for ``.notdef``."""
    pen = TTGlyphPen(None)
    margin = int(upem * 0.1)
    inner = int(upem * 0.08)
    lo, hi = margin, upem - margin
    # Outer contour (clockwise).
    pen.moveTo((lo, lo))
    pen.lineTo((lo, hi))
    pen.lineTo((hi, hi))
    pen.lineTo((hi, lo))
    pen.closePath()
    # Inner contour (counter-clockwise) to hollow it out.
    ilo, ihi = lo + inner, hi - inner
    pen.moveTo((ilo, ilo))
    pen.lineTo((ihi, ilo))
    pen.lineTo((ihi, ihi))
    pen.lineTo((ilo, ihi))
    pen.closePath()
    return pen.glyph()


def main() -> int:
    if not os.path.exists(MANIFEST_PATH):
        sys.stderr.write(f"error: manifest not found: {MANIFEST_PATH}\n")
        return 1

    with open(MANIFEST_PATH, "r", encoding="utf-8") as fh:
        manifest = json.load(fh)

    family = manifest.get("family", "OpenUsage Icons")
    version = str(manifest.get("version", "1.0"))
    upem = int(manifest.get("upem", 1000))
    entries = manifest.get("glyphs", [])
    if not entries:
        sys.stderr.write("error: manifest has no glyphs\n")
        return 1

    glyph_order = [NOTDEF]
    glyphs = {NOTDEF: _notdef_glyph(upem)}
    advance_widths = {NOTDEF: upem}
    cmap: dict[int, str] = {}
    multipath: list[str] = []
    missing: list[str] = []

    for entry in entries:
        provider = entry["provider"]
        svg_name = entry["svg"]
        codepoint = int(entry["codepoint"], 16)
        glyph_name = f"prov_{provider}"

        svg_path = os.path.join(ICONS_DIR, f"{svg_name}.svg")
        if not os.path.exists(svg_path):
            missing.append(f"{provider} -> {svg_path}")
            continue

        ds = _path_ds(svg_path)
        if len(ds) > 1:
            multipath.append(f"{provider} ({svg_name}.svg, {len(ds)} paths)")

        glyphs[glyph_name] = _build_glyph(svg_path, upem)
        advance_widths[glyph_name] = upem
        glyph_order.append(glyph_name)
        cmap[codepoint] = glyph_name

    if missing:
        sys.stderr.write("error: missing SVG sources:\n")
        for m in missing:
            sys.stderr.write(f"  - {m}\n")
        return 1

    # Build the TTF.
    fb = FontBuilder(upem, isTTF=True)
    fb.setupGlyphOrder(glyph_order)
    fb.setupCharacterMap(cmap)
    fb.setupGlyf(glyphs)

    metrics = {name: (advance_widths[name], 0) for name in glyph_order}
    # glyf table has computed bounding boxes; lsb of 0 is fine as a default.
    fb.setupHorizontalMetrics(metrics)
    fb.setupHorizontalHeader(ascent=upem, descent=0)

    name_strings = {
        "familyName": family,
        "styleName": "Regular",
        "uniqueFontIdentifier": f"OpenUsage;{family};{version}",
        "fullName": family,
        "psName": family.replace(" ", "") + "-Regular",
        "version": f"Version {version}",
    }
    fb.setupNameTable(name_strings)
    fb.setupOS2(sTypoAscender=upem, sTypoDescender=0, usWinAscent=upem, usWinDescent=0)
    fb.setupPost()

    # Deterministic output: pin the head timestamps instead of using wall-clock
    # time, so regenerating the font yields byte-identical bytes. Without this
    # every build produced a new sha256, which made the embedded-vs-installed
    # hash check report the font as perpetually "outdated" and let the macOS
    # and Linux release binaries embed differing fonts. Honor SOURCE_DATE_EPOCH
    # when set (reproducible-builds convention), else use a fixed epoch.
    epoch = int(os.environ.get("SOURCE_DATE_EPOCH", "0"))
    head = fb.font["head"]
    head.created = epoch
    head.modified = epoch

    os.makedirs(os.path.dirname(OUTPUT_PATH), exist_ok=True)
    fb.save(OUTPUT_PATH)

    size = os.path.getsize(OUTPUT_PATH)
    glyph_count = len(glyph_order) - 1  # exclude .notdef

    print("OpenUsage icon font generated.")
    print(f"  output:     {OUTPUT_PATH}")
    print(f"  family:     {family} (v{version}, upem={upem})")
    print(f"  glyphs:     {glyph_count} provider glyphs (+ .notdef)")
    print(f"  codepoints: {len(cmap)} mapped")
    print(f"  size:       {size} bytes ({size / 1024:.1f} KB)")

    # Report each glyph's actual glyf bbox height; it should be ~INK_FILL*upem.
    glyf = fb.font["glyf"]
    target = INK_FILL * upem
    print(f"  glyph ink heights (target ~{target:.0f}):")
    for name in glyph_order:
        if name == NOTDEF:
            continue
        g = glyf[name]
        g.recalcBounds(glyf)
        h = g.yMax - g.yMin if hasattr(g, "yMax") else 0
        print(f"    - {name:<22} height={h}")
    if multipath:
        print("  multi-path SVGs (merged into one glyph each):")
        for m in multipath:
            print(f"    - {m}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
