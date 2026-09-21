#!/usr/bin/env python3
"""从 assets/WrtDeck.png 生成站点图标与前端内嵌 logo。

母版是 1254x1254 的透明 PNG，留白偏大（左 13.0% / 右 12.9% / 上 17.1% / 下 14.3%），
且上下不对称。这里先按 alpha 裁出内容，再重排到统一留白的正方形画布，
让同一个源图能同时产出「浏览器标签页」和「界面内 logo」两种尺寸需求。

依赖 Pillow，运行：python3 scripts/make-icons.py
"""

import math
import os
import sys

try:
    from PIL import Image
except ImportError:  # pragma: no cover - 仅在缺少依赖时触发
    sys.exit("缺少 Pillow，请先执行：python3 -m pip install Pillow")

# 仓库根目录，基于本文件位置推导（scripts/ 的上一级）
root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
master_path = os.path.join(root, "assets", "WrtDeck.png")
public_dir = os.path.join(root, "web", "public")
src_assets_dir = os.path.join(root, "web", "src", "assets")

# 内容四周留白占画布的比例。原图约 13%~17%，收紧到 5.5% 可让 16px 标签页图标更清晰。
margin = 0.055
# 标签页图标的多尺寸集合，favicon.ico 一个文件覆盖这三档
ico_sizes = (16, 32, 48)
# 独立 PNG 尺寸。16 / 32 两档已打包在 ico 里，重复导出只会多两个多余文件；
# 48x48 单独留一份给 Windows 磁贴等只认 PNG 的场景。
png_sizes = (48,)
# iOS 主屏图标尺寸。iOS 不支持透明，需要白底，否则描边会与黑色背景糊在一起。
apple_size = 180
# 界面内 logo 尺寸。顶栏用 36px、空状态用 72px，256 已能覆盖 3x 屏幕；
# 再往上体积增长很快（原图带轻微渐变，512 约 150 KB，256 只需 49 KB）。
logo_size = 256
# 描边主色是深藏青 #112031，在深色界面上会与背景 #08090c 接近。
# 深色场景由界面自行处理（顶栏留出一圈浅色底），此处只负责生成白底的 iOS 图标。
white = (255, 255, 255, 255)


def trim_content(image):
    """按 alpha 裁掉四周全透明的像素，返回内容区图像"""
    bbox = image.getchannel("A").point(lambda v: 255 if v > 8 else 0).getbbox()
    if bbox is None:
        sys.exit("母版里没有找到不透明像素，请确认 assets/WrtDeck.png 是否正常")
    return image.crop(bbox)


def to_square(image, ratio):
    """把内容居中放进正方形画布，四周留出 ratio 比例的空白"""
    width, height = image.size
    # 向上取整，保证内容加两侧留白后不会超出画布
    side = math.ceil(max(width, height) / (1 - 2 * ratio))
    canvas = Image.new("RGBA", (side, side), (0, 0, 0, 0))
    canvas.paste(image, ((side - width) // 2, (side - height) // 2))
    return canvas


def flatten(image, size, background):
    """缩放并合成到纯色底上，用于不支持透明的场景"""
    layer = image.resize((size, size), Image.LANCZOS)
    plate = Image.new("RGBA", (size, size), background)
    plate.alpha_composite(layer)
    return plate.convert("RGB")


def write_png(image, directory, name):
    """写出 PNG 并回报相对路径与体积"""
    path = os.path.join(directory, name)
    image.save(path, format="PNG", optimize=True)
    return path


def main():
    if not os.path.exists(master_path):
        sys.exit(f"母版不存在：{master_path}")

    os.makedirs(public_dir, exist_ok=True)
    os.makedirs(src_assets_dir, exist_ok=True)

    master = Image.open(master_path).convert("RGBA")
    content = trim_content(master)
    square = to_square(content, margin)

    written = []
    # 1. 多尺寸 ICO：交给 Pillow 用 LANCZOS 逐级重采样
    ico_path = os.path.join(public_dir, "favicon.ico")
    square.save(ico_path, format="ICO", sizes=[(s, s) for s in ico_sizes])
    written.append(ico_path)

    # 2. 显式声明的 PNG 尺寸
    for size in png_sizes:
        written.append(write_png(square.resize((size, size), Image.LANCZOS), public_dir, f"favicon-{size}x{size}.png"))

    # 3. 界面内 logo：保留透明，由页面自行控制底色
    written.append(write_png(square.resize((logo_size, logo_size), Image.LANCZOS), src_assets_dir, "logo.png"))

    print(f"内容区 {content.size[0]}x{content.size[1]} -> 画布 {square.size[0]}x{square.size[1]}"
          f"（内容占比 {content.size[0] / square.size[0] * 100:.1f}%）")
    for path in written:
        print(f"  生成 {os.path.relpath(path, root)}  {os.path.getsize(path) / 1024:.1f} KB")


if __name__ == "__main__":
    main()
