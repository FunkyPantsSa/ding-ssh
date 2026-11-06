#!/usr/bin/env python3
"""Compose the terminal + SFTP acceptance matrix, relationship chart and
blue/pink comparison from the generated previews."""
from __future__ import annotations

import json
import sys
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parent.parent
DOCS = ROOT / "docs"
THEMES = ["blue", "pink", "green", "yellow", "purple"]

CJK_FONT = "/System/Library/Fonts/Hiragino Sans GB.ttc"
MONO_FONT = "/System/Library/Fonts/Menlo.ttc"

CANVAS = (1640, 1640)
BG = "#090b0d"
PANEL = "#101418"
BORDER = "#242a31"
TITLE = "#e8edf3"
MUTED = "#8d96a3"
ACCENT = "#c9d6e6"

CELL_W, CELL_H = 760, 494
GAP_X, GAP_Y = 40, 44
LEFT, TOP = 40, 116
MONO_SMALL = None
MONO_MID = None


def font(path: str, size: int) -> ImageFont.FreeTypeFont:
    try:
        return ImageFont.truetype(path, size)
    except OSError:
        return ImageFont.load_default()


def rounded_panel(draw: ImageDraw.ImageDraw, box, radius=10, fill=PANEL, outline=BORDER):
    draw.rounded_rectangle(box, radius=radius, fill=fill, outline=outline, width=1)


def crop_caption(image: Image.Image, caption_height: int = 40) -> Image.Image:
    return image.crop((0, caption_height, image.width, image.height))


def compose_compare(by_id: dict) -> None:
    pair = [by_id["blue"], by_id["pink"]]
    width, height = 1600, 600
    canvas = Image.new("RGB", (width, height), BG)
    draw = ImageDraw.Draw(canvas)
    draw.text((40, 28), "同源终端 Ink：蓝色 vs 粉色", font=font(CJK_FONT, 24), fill=TITLE)
    draw.text(
        (40, 62),
        "相同的 Tone 步进与色度预算，仅色相跟随主题；导航、顶部工具栏、SFTP 与终端保持同一冷暖方向。",
        font=font(CJK_FONT, 14),
        fill=MUTED,
    )
    for index, item in enumerate(pair):
        image = Image.open(DOCS / f"theme-dark-terminal-{item['id']}.png").convert("RGB")
        image = crop_caption(image)
        image = image.resize((760, 475), Image.LANCZOS)
        x = 40 + index * 780
        y = 96
        canvas.paste(image, (x, y))
        draw.rectangle((x, y, x + 760 - 1, y + 475 - 1), outline=BORDER, width=1)
        draw.text(
            (x + 16, y + 488),
            f"{item['name']}主题 {item['seed'].upper()}  ·  终端 Ink {item['terminal'].upper()}  ·  SFTP {item['sftp'].upper()}",
            font=font(CJK_FONT, 14),
            fill=MUTED,
        )
    canvas.save(DOCS / "theme-dark-terminal-compare.png", optimize=True)


def compose_relationship_chart(by_id: dict) -> None:
    width, height = 1480, 780
    canvas = Image.new("RGB", (width, height), BG)
    draw = ImageDraw.Draw(canvas)
    draw.text((40, 30), "深色终端 Ink 与相邻表面的明度 / 色度关系", font=font(CJK_FONT, 26), fill=TITLE)
    draw.text(
        (40, 68),
        "Atmosphere 75 / Separation 65 / Solid · 终端使用同一主题色相与更低 tint 预算，而不是固定中性冷黑。",
        font=font(CJK_FONT, 14),
        fill=MUTED,
    )

    columns = [
        ("主题", 40, 150),
        ("终端 Ink", 190, 245),
        ("顶部工具栏", 435, 235),
        ("SFTP", 670, 235),
        ("主导航", 905, 235),
        ("关系", 1140, 300),
    ]
    header_y = 116
    for name, x, _ in columns:
        draw.text((x, header_y), name, font=font(CJK_FONT, 13), fill=MUTED)
    draw.line((40, header_y + 26, width - 40, header_y + 26), fill=BORDER, width=1)

    row_h = 108
    y = header_y + 40
    for theme_id in THEMES:
        item = by_id[theme_id]
        metrics = item["metrics"]
        if (THEMES.index(theme_id) % 2) == 0:
            draw.rounded_rectangle((32, y - 8, width - 32, y + row_h - 18), radius=8, fill="#0d1115")

        draw.text((40, y + 8), item["name"], font=font(CJK_FONT, 17), fill=TITLE)
        draw.text((40, y + 34), item["seed"].upper(), font=MONO_SMALL, fill=MUTED)

        for key, x in (("terminal", 190), ("toolbar", 435), ("sftp", 670), ("nav", 905)):
            m = metrics[key]
            draw.rounded_rectangle((x, y + 4, x + 44, y + 48), radius=8, fill=m["hex"], outline="#2c333c")
            draw.text((x + 56, y + 8), m["hex"].upper(), font=MONO_MID, fill=TITLE)
            draw.text(
                (x + 56, y + 29),
                f"T {m['tone']:.1f} · C {m['hctChroma']:.1f}",
                font=MONO_SMALL,
                fill=MUTED,
            )

        terminal_c = metrics["terminal"]["oklchChroma"]
        sftp_c = metrics["sftp"]["oklchChroma"]
        ratio = terminal_c / sftp_c if sftp_c else 0
        tone_step = metrics["sftp"]["tone"] - metrics["terminal"]["tone"]
        draw.text((1140, y + 8), f"{ratio:.2f}× SFTP", font=MONO_MID, fill=ACCENT)
        draw.text((1140, y + 29), f"ΔT {tone_step:.1f}", font=MONO_SMALL, fill=MUTED)
        draw.text(
            (1140, y + 49),
            f"hue {metrics['terminal']['oklchHue']:.0f}° vs {metrics['sftp']['oklchHue']:.0f}°",
            font=MONO_SMALL,
            fill=MUTED,
        )

        # Tone rail: the terminal sits left of the toolbar, SFTP and nav.
        rail_x, rail_w = 40, width - 80
        rail_y = y + 76
        draw.line((rail_x, rail_y, rail_x + rail_w, rail_y), fill="#252b32", width=2)
        for key, color in (
            ("terminal", metrics["terminal"]["hex"]),
            ("toolbar", metrics["toolbar"]["hex"]),
            ("sftp", metrics["sftp"]["hex"]),
            ("nav", metrics["nav"]["hex"]),
        ):
            t = metrics[key]["tone"]
            px = rail_x + int((t - 10) / 24 * rail_w)
            draw.ellipse((px - 4, rail_y - 4, px + 4, rail_y + 4), fill=color, outline="#06080a")
        draw.text((rail_x, rail_y + 10), "T10", font=MONO_SMALL, fill="#525b66")
        draw.text((rail_x + rail_w - 34, rail_y + 10), "T34", font=MONO_SMALL, fill="#525b66")

        y += row_h

    draw.text(
        (40, height - 38),
        "色度以终端 / SFTP 的 OKLCH chroma 比值衡量；HCT C 与 OKLCH C 单位不同，不在数值上直接比较。",
        font=font(CJK_FONT, 12),
        fill=MUTED,
    )
    canvas.save(DOCS / "theme-dark-tone-chroma.png", optimize=True)


def main() -> int:
    colors = json.loads((DOCS / "theme-dark-terminal-colors.json").read_text())
    by_id = {item["id"]: item for item in colors}

    canvas = Image.new("RGB", CANVAS, BG)
    draw = ImageDraw.Draw(canvas)

    title_font = font(CJK_FONT, 28)
    sub_font = font(CJK_FONT, 15)
    label_font = font(CJK_FONT, 14)
    mono_small = font(MONO_FONT, 11)
    mono_mid = font(MONO_FONT, 13)
    global MONO_SMALL, MONO_MID
    MONO_SMALL = mono_small
    MONO_MID = mono_mid

    draw.text((LEFT, 34), "深色终端 + SFTP 布局", font=title_font, fill=TITLE)
    draw.text(
        (LEFT, 74),
        "五主题完整布局 · Atmosphere 75 / Separation 65 / Solid · 终端 Ink 与导航、顶部工具栏、SFTP 同源",
        font=sub_font,
        fill=MUTED,
    )

    for index, theme_id in enumerate(THEMES):
        row, col = divmod(index, 2)
        x = LEFT + col * (CELL_W + GAP_X)
        y = TOP + row * (CELL_H + GAP_Y)
        image = Image.open(DOCS / f"theme-dark-terminal-{theme_id}.png").convert("RGB")
        image = image.resize((CELL_W, CELL_H), Image.LANCZOS)
        canvas.paste(image, (x, y))
        draw.rectangle((x, y, x + CELL_W - 1, y + CELL_H - 1), outline=BORDER, width=1)

    # The empty sixth cell explains the relationship the previews are meant to
    # verify, instead of leaving a blank hole in the matrix.
    legend_x = LEFT + CELL_W + GAP_X
    legend_y = TOP + 2 * (CELL_H + GAP_Y)
    rounded_panel(draw, (legend_x, legend_y, legend_x + CELL_W, legend_y + CELL_H))

    draw.text((legend_x + 26, legend_y + 24), "关系校准", font=font(CJK_FONT, 18), fill=TITLE)
    rows = [
        ("明度", "终端 Ink  <  顶部工具栏  <  SFTP  <  主导航"),
        ("色度", "终端始终低于导航与 SFTP；五主题约 0.56–0.72× SFTP"),
        ("色相", "与主题同源：蓝 / 粉 / 绿 / 黄 / 紫 各自延续冷暖"),
        ("连续", "Atmosphere 20–100 均保留同一色相，不再回退纯灰"),
        ("独立", "ANSI 16 / 256 / TrueColor 未改动；独立方案逐字节保留"),
    ]

    y = legend_y + 64
    for label, value in rows:
        draw.text((legend_x + 26, y), label, font=label_font, fill=ACCENT)
        draw.text((legend_x + 76, y), value, font=label_font, fill=MUTED)
        y += 34

    draw.text((legend_x + 26, y + 14), "主题色值", font=font(CJK_FONT, 16), fill=TITLE)
    y += 50
    header_y = y
    headers = ["主题", "终端 Ink", "顶部工具栏", "SFTP", "主导航"]
    xs = [0, 96, 226, 356, 470]
    for x, header in zip(xs, headers):
        draw.text((legend_x + 26 + x, header_y), header, font=label_font, fill=MUTED)
    y += 26
    draw.line((legend_x + 26, y, legend_x + CELL_W - 30, y), fill=BORDER, width=1)
    y += 8

    for theme_id in THEMES:
        item = by_id[theme_id]
        values = [item["name"], item["terminal"], item["toolbar"], item["sftp"], item["nav"]]
        for x, value in zip(xs, values):
            active_font = label_font if x == 0 else mono_small
            fill = ACCENT if x == 0 else MUTED
            draw.text((legend_x + 26 + x, y), value.upper() if value.startswith("#") else value, font=active_font, fill=fill)
        y += 24

    draw.text(
        (legend_x + 26, legend_y + CELL_H - 38),
        "预览由真实 material.ts Token + 应用 CSS 渲染。",
        font=mono_small,
        fill=MUTED,
    )

    out = DOCS / "theme-dark-terminal-sftp-matrix.png"
    canvas.save(out, optimize=True)
    compose_compare(by_id)
    compose_relationship_chart(by_id)
    print(out)
    print(DOCS / "theme-dark-terminal-compare.png")
    print(DOCS / "theme-dark-tone-chroma.png")
    return 0


if __name__ == "__main__":
    sys.exit(main())
