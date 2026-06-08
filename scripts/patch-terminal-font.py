#!/usr/bin/env python3
"""Augment a copy of a terminal font with OpenUsage provider-icon glyphs.

The original font is never modified. A renamed copy is written to --out so it
can coexist with the original in Font Book / iTerm2.

Run with the fonttools venv, e.g.:
    /tmp/fontvenv/bin/python scripts/patch-terminal-font.py \
        --base ~/Library/Fonts/MyFont.otf \
        --out  /tmp/MyFont-OpenUsage.otf
"""

import argparse
import json
import os
import sys
import xml.etree.ElementTree as ET

from fontTools.ttLib import TTFont
from fontTools.pens.boundsPen import ControlBoundsPen
from fontTools.pens.recordingPen import RecordingPen
from fontTools.pens.transformPen import TransformPen
from fontTools.pens.ttGlyphPen import TTGlyphPen
from fontTools.pens.t2CharStringPen import T2CharStringPen
from fontTools.pens.cu2quPen import Cu2QuPen
from fontTools.svgLib.path import parse_path


def repo_root():
    # scripts/ lives directly under the repo root.
    return os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def extract_path_ds(svg_path):
    """Return all `d` attributes from <path> elements, namespace-agnostic."""
    tree = ET.parse(svg_path)
    root = tree.getroot()
    ds = []
    for el in root.iter():
        tag = el.tag
        # Strip XML namespace if present: '{http://...}path' -> 'path'.
        if "}" in tag:
            tag = tag.split("}", 1)[1]
        if tag == "path":
            d = el.get("d")
            if d:
                ds.append(d)
    return ds


def detect_format(font):
    if "glyf" in font:
        return "glyf"
    if "CFF " in font or "CFF2" in font:
        return "CFF"
    raise SystemExit("error: base font has neither 'glyf' nor 'CFF '/'CFF2' tables")


def choose_advance(font, upem):
    """Pick a monospaced advance width: prefer 'M', then 'space', then 0.6*upem."""
    hmtx = font["hmtx"]
    cmap = font.getBestCmap()
    for cp in (ord("M"), ord("m"), ord("0")):
        name = cmap.get(cp)
        if name and name in hmtx.metrics:
            adv = hmtx.metrics[name][0]
            if adv > 0:
                return adv
    if "space" in hmtx.metrics and hmtx.metrics["space"][0] > 0:
        return hmtx.metrics["space"][0]
    return round(0.6 * upem)


def choose_cap_height(font, upem):
    if "OS/2" in font:
        ch = getattr(font["OS/2"], "sCapHeight", 0) or 0
        if ch > 0:
            return ch
    return round(0.7 * upem)


# Fraction of the base font ascender that the ink height should occupy, so
# icons rise to (near) the top of the line.
INK_FILL = 0.92
# Cap horizontal growth: tall logos may exceed the advance, but never by more
# than this factor, to avoid heavy overlap into neighboring cells.
MAX_WIDTH_FACTOR = 1.15


def record_svg(ds):
    """Replay every SVG path into a RecordingPen, returning the recording."""
    rec = RecordingPen()
    for d in ds:
        parse_path(d, rec)
    return rec


def ink_transform(rec, ascent, advance):
    """Build an affine transform that scales the recorded SVG ink to fill the
    line and centers it.

    The ink bounding box (SVG coords, y-down) is measured, then scaled uniformly
    so the ink HEIGHT maps to ``INK_FILL * ascent``. The scale is clamped so the
    ink WIDTH does not exceed ``advance * MAX_WIDTH_FACTOR``. The glyph is
    centered horizontally on the advance and vertically on the cap band (centered
    between the baseline and the ascender).

    Returns ``(transform, scaled_w, scaled_h)``.
    """
    bounds = ControlBoundsPen(None)
    rec.replay(bounds)
    if bounds.bounds is None:
        raise ValueError("empty outline")
    xmin, ymin, xmax, ymax = bounds.bounds
    ink_w = xmax - xmin
    ink_h = ymax - ymin
    if ink_w <= 0 or ink_h <= 0:
        raise ValueError("degenerate ink bbox")

    target_h = INK_FILL * ascent
    scale = target_h / ink_h
    # Clamp so the width stays within the cell tolerance.
    max_w = advance * MAX_WIDTH_FACTOR
    if ink_w * scale > max_w:
        scale = max_w / ink_w

    scaled_w = ink_w * scale
    scaled_h = ink_h * scale
    # Center horizontally on the advance.
    x_pad = (advance - scaled_w) / 2.0
    # Center vertically on the cap band: place the ink so its center sits at
    # ascent/2, i.e. it spans from near the baseline up toward the ascender.
    y_pad = (ascent - scaled_h) / 2.0

    # Affine mapping svg(x, y) -> font(X, Y), Y flipped (svg y is down):
    #   X = scale*(x - xmin) + x_pad
    #   Y = scale*(ymax - y) + y_pad
    # In (a, b, c, d, e, f): X = a*x + c*y + e ; Y = b*x + d*y + f
    transform = (
        scale,
        0.0,
        0.0,
        -scale,
        x_pad - scale * xmin,
        y_pad + scale * ymax,
    )
    return transform, scaled_w, scaled_h


def add_unique_name(order_set, base_name):
    name = base_name
    i = 1
    while name in order_set:
        name = "%s_%d" % (base_name, i)
        i += 1
    return name


def build_glyf_glyph(rec, transform, advance):
    tt_pen = TTGlyphPen(None)
    # Cubic -> quadratic conversion, then transform into font units.
    cu2qu = Cu2QuPen(tt_pen, max_err=1.0, reverse_direction=True)
    pen = TransformPen(cu2qu, transform)
    rec.replay(pen)
    return tt_pen.glyph()


def build_cff_charstring(rec, transform, advance):
    t2_pen = T2CharStringPen(advance, None)
    pen = TransformPen(t2_pen, transform)
    rec.replay(pen)
    return t2_pen.getCharString()


def insert_glyf(font, name, glyph, advance):
    font["glyf"][name] = glyph
    font["hmtx"][name] = (advance, 0)


def insert_cff(font, name, charstring, advance):
    cff = font["CFF "].cff
    top_dict_name = cff.fontNames[0]
    top_dict = cff[top_dict_name]
    char_strings = top_dict.CharStrings
    # Bind the charstring to this font's private dict / global subrs so it can
    # be re-serialized (decompile depends on these).
    private = top_dict.Private
    charstring.private = private
    charstring.globalSubrs = char_strings.globalSubrs
    if char_strings.charStringsAreIndexed:
        # Loaded from an OTF: charStrings maps name -> index into the index
        # list. Append the new charstring and register the name.
        index = char_strings.charStringsIndex
        index.append(charstring)
        char_strings.charStrings[name] = len(index) - 1
    else:
        char_strings.charStrings[name] = charstring
    # Keep the charset (the ordered list the table serializes from) in sync.
    if hasattr(top_dict, "charset") and name not in top_dict.charset:
        top_dict.charset.append(name)
    font["hmtx"][name] = (advance, 0)


def register_glyph_order(font, name):
    order = font.getGlyphOrder()
    if name not in order:
        order = list(order)
        order.append(name)
        font.setGlyphOrder(order)


def add_to_cmaps(font, codepoint, name):
    cmap_table = font["cmap"]
    for sub in cmap_table.tables:
        if sub.isUnicode():
            sub.cmap[codepoint] = name


def rename_font(font, suffix):
    name_table = font["name"]

    def ps_suffix():
        # PostScript names contain no spaces.
        return "-" + suffix.strip().replace(" ", "").lstrip("+")

    for rec in name_table.names:
        nid = rec.nameID
        cur = rec.toUnicode()
        if nid in (1, 4, 16):
            if not cur.endswith(suffix):
                name_table.setName(cur + suffix, nid, rec.platformID,
                                   rec.platEncID, rec.langID)
        elif nid == 6:
            new = cur + ps_suffix()
            name_table.setName(new, nid, rec.platformID, rec.platEncID, rec.langID)
        elif nid == 3:
            new = cur + suffix.strip().replace(" ", "")
            name_table.setName(new, nid, rec.platformID, rec.platEncID, rec.langID)


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--base", required=True, help="path to base font (.otf or .ttf)")
    ap.add_argument("--out", required=True, help="output font path")
    ap.add_argument("--name-suffix", default=" +OpenUsage",
                    help="suffix appended to family/full/typographic names")
    ap.add_argument("--manifest",
                    default=os.path.join(repo_root(), "internal", "tmux",
                                         "assets", "icons.json"))
    ap.add_argument("--svg-dir",
                    default=os.path.join(repo_root(), "website", "public", "icons"))
    args = ap.parse_args()

    font = TTFont(args.base)
    fmt = detect_format(font)
    upem = font["head"].unitsPerEm
    advance = choose_advance(font, upem)
    cap_height = choose_cap_height(font, upem)
    # Icons are scaled per-glyph by their ink bbox so they fill the line height.
    # Target ink height is INK_FILL of the base font ascender, so icons rise to
    # the top of the line ("full character height").
    ascent = font["hhea"].ascent

    with open(args.manifest) as fh:
        manifest = json.load(fh)

    order_set = set(font.getGlyphOrder())
    added = 0
    heights = []  # (name, glyph-height-in-font-units) for reporting

    for entry in manifest["glyphs"]:
        provider = entry["provider"]
        svg = entry["svg"]
        codepoint = int(entry["codepoint"], 16)
        svg_path = os.path.join(args.svg_dir, svg + ".svg")
        if not os.path.exists(svg_path):
            print("warn: missing svg %s, skipping %s" % (svg_path, provider),
                  file=sys.stderr)
            continue
        ds = extract_path_ds(svg_path)
        if not ds:
            print("warn: no <path d> in %s, skipping %s" % (svg_path, provider),
                  file=sys.stderr)
            continue

        name = add_unique_name(order_set, "ouicon_" + provider)
        order_set.add(name)

        rec = record_svg(ds)
        transform, scaled_w, scaled_h = ink_transform(rec, ascent, advance)

        if fmt == "glyf":
            glyph = build_glyf_glyph(rec, transform, advance)
            insert_glyf(font, name, glyph, advance)
        else:
            charstring = build_cff_charstring(rec, transform, advance)
            insert_cff(font, name, charstring, advance)

        register_glyph_order(font, name)
        add_to_cmaps(font, codepoint, name)
        heights.append((name, round(scaled_h)))
        added += 1

    # Keep maxp in sync with the new glyph count.
    font["maxp"].numGlyphs = len(font.getGlyphOrder())

    orig_glyph_count = len(order_set) - added
    rename_font(font, args.name_suffix)

    font.save(args.out)
    size = os.path.getsize(args.out)

    target_h = INK_FILL * ascent
    print("=== patch-terminal-font summary ===")
    print("base format:        %s" % fmt)
    print("upem:               %d" % upem)
    print("advance used:       %d" % advance)
    print("cap height:         %d" % cap_height)
    print("base ascender:      %d" % ascent)
    print("target ink height:  %.0f (%.0f%% of ascender)" % (target_h, INK_FILL * 100))
    print("original glyphs:    %d" % orig_glyph_count)
    print("glyphs added:       %d" % added)
    print("output:             %s (%d bytes)" % (args.out, size))
    print("augmented glyph ink heights (font units):")
    for name, h in heights:
        print("    - %-22s height=%d" % (name, h))


if __name__ == "__main__":
    main()
