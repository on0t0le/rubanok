#!/usr/bin/env python3
"""Generate rubanok app icon: shield shape, Ukrainian blue/yellow, white checkmark."""
import os
import math
from PIL import Image, ImageDraw

BLUE   = (0, 91, 187, 255)     # #005BBB
YELLOW = (255, 215, 0, 255)    # #FFD700
WHITE  = (255, 255, 255, 255)

def shield_polygon(size: int, margin_frac: float = 0.06) -> list:
    m = int(size * margin_frac)
    x0, y0 = m, m
    w = size - 2 * m
    h = size - 2 * m
    return [
        (x0,          y0),
        (x0 + w,      y0),
        (x0 + w,      y0 + int(h * 0.62)),
        (x0 + w // 2, y0 + h),
        (x0,          y0 + int(h * 0.62)),
    ]

def shrink_polygon(points: list, amount: int) -> list:
    cx = sum(p[0] for p in points) / len(points)
    cy = sum(p[1] for p in points) / len(points)
    result = []
    for x, y in points:
        dx, dy = x - cx, y - cy
        length = math.hypot(dx, dy)
        if length > 0:
            result.append((int(x - dx / length * amount),
                           int(y - dy / length * amount)))
        else:
            result.append((int(x), int(y)))
    return result

def draw_icon(size: int) -> Image.Image:
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)

    border = max(3, size // 16)
    outer = shield_polygon(size)
    inner = shrink_polygon(outer, border)

    draw.polygon(outer, fill=YELLOW)
    draw.polygon(inner, fill=BLUE)

    # Checkmark: two line segments meeting at the check point
    lw = max(4, size // 10)
    s = size
    p1 = (int(s * 0.25), int(s * 0.54))  # left tip
    p2 = (int(s * 0.43), int(s * 0.72))  # bottom corner
    p3 = (int(s * 0.75), int(s * 0.34))  # right tip
    draw.line([p1, p2], fill=WHITE, width=lw)
    draw.line([p2, p3], fill=WHITE, width=lw)

    return img

CONTENTS_JSON = """{
  "images" : [
    {
      "filename" : "icon_1024.png",
      "idiom" : "universal",
      "platform" : "ios",
      "size" : "1024x1024"
    }
  ],
  "info" : {
    "author" : "xcode",
    "version" : 1
  }
}
"""

def main():
    script_dir = os.path.dirname(os.path.abspath(__file__))
    repo_root  = os.path.dirname(script_dir)
    out_dir = os.path.join(repo_root, "rubanok", "rubanok",
                           "Assets.xcassets", "AppIcon.appiconset")
    os.makedirs(out_dir, exist_ok=True)

    img = draw_icon(1024)
    png_path = os.path.join(out_dir, "icon_1024.png")
    img.save(png_path, "PNG")
    print(f"Generated {png_path}")

    contents_path = os.path.join(out_dir, "Contents.json")
    with open(contents_path, "w") as f:
        f.write(CONTENTS_JSON)
    print(f"Updated  {contents_path}")

if __name__ == "__main__":
    main()
