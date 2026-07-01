# NekoCode Logo Assets

品牌标识源码与多格式导出。本目录位于 `gui/public/`，所有资源均为 GUI 专属。

## 设计概念

- **主体**：圆润黑猫脸 — 对应"猫娘"人设
- **眼睛**：青绿荧光发光瞳 — 代表"AI 智能"
- **顶部符号**：`</>` — 代表"编程助手"
- **底板**：暖紫→暖粉渐变、大圆角方形 — 软萌、现代
- **点缀**：腮红、W 形笑嘴、胡须 — 增加"猫感"与亲和力

## 文件清单

| 文件 | 用途 |
|---|---|
| `logo.svg` | 完整品牌版（含 `</>` 与 `NEKOCODE` 字），用于 About 页、README |
| `logo-icon.svg` | 纯图标版（仅猫脸），App 图标源 |
| `logo-icon-app.svg` | GUI 浏览器标签 favicon（引用自 `gui/index.html`） |
| `appicon.icns` | macOS 应用图标集（16/128/256/512/1024） |
| `appicon.ico` | Windows 应用图标集（16/32/64/128/256/512/1024） |
| `icon_*.png` | 各尺寸 PNG 中间产物（16/32/64/128/256/512/1024） |
| `build_icns.js` | 任意平台生成 ICNS 的 Node 脚本（无需 macOS `iconutil`） |

## 重新生成

```bash
cd gui/public/logo
# 1. 多尺寸 PNG
for s in 16 32 64 128 256 512 1024; do
  magick logo-icon.svg -background none -resize ${s}x${s} icon_${s}.png
done
# 2. Windows ICO
magick icon_16.png icon_32.png icon_64.png icon_128.png icon_256.png icon_512.png icon_1024.png appicon.ico
# 3. macOS ICNS
node build_icns.js
# 4. 同步到项目
cp appicon.icns appicon.ico ../../guiapp/
cp icon_256.png ../../build/flatpak/org.nekocode.NekoCode.png
```

## macOS `.icns` 手工构建说明

`iconutil` 是 macOS 专有工具，本项目用 `build_icns.js` 在任意平台生成标准 ICNS 容器。
ICNS 格式：`'icns'` magic (4B) + total_length (4B) + Σ[ OSType (4B) + block_len (4B) + data ]。

## 主题适配

当前设计同时适配亮/暗背景：
- 底板是饱和渐变，在亮/暗背景都有足够对比
- 猫脸用深色调，在浅色底板内稳定
- 眼睛发光在暗色背景上更醒目，在亮色背景上也有高光托底

如需"高对比版"（例如任务栏小图标），可在 `logo-icon.svg` 给猫脸描 4px 白色描边。
