# touch-squirrel 品牌资产

松鼠囤坚果 ↔ 账号池囤号与调度。Slogan：**囤得住 · 派得准**。

## 文件

| 文件 | 尺寸 | 用途 |
|---|---|---|
| `logo-primary.png` | 1254×1254 RGB | 主标志。圆角方形蓝底 + 3D Q 版松鼠，适合 app icon、启动页、社交头像 |
| `logo-mark-flat.png` | 1254×1254 RGBA（透明底） | 扁平 mark 全身像。三色纯扁平，可叠任意底色，适合 README 顶部、侧边栏 header |
| `logo-mark-head.png` | 693×693 RGBA（透明底） | 头像版。由扁平 mark 镜像派生，**小尺寸唯一可用的一版**，favicon 走这个 |
| `brand-board.png` | 1024×1536 RGB | IP 设定板。三视图 / 表情 / 动作 / 色卡 / 周边 / 应用示意，品牌参考用 |
| `icon-preview.png` | — | 三套图标 16→256 的实际尺寸对照，改图后请重新生成 |

## 图标序列

每套均为 16 / 32 / 48 / 64 / 128 / 256 / 512 / 1024 八个尺寸：

```
icons/primary/icon-{size}.png     app icon，蓝底不透明
icons/mark/mark-{size}.png        扁平全身，透明底
icons/mark-head/head-{size}.png   扁平头像，透明底  ← favicon 用这套
```

**选哪套**：≤48px 只能用 `mark-head`；`primary` 在 48px 以下会糊成一团棕色，`mark` 在 32px 以下四肢和尾巴会丢。64px 以上三套都可用。

## 色板

| 名称 | 色值 | 用途 |
|---|---|---|
| 品牌主色 · 科技蓝 | `#2563FF` | 主色、主按钮、图标底板 |
| 辅助色 | `#60A5FA` | 渐变浅端、次级强调 |
| 活力橙 | `#FF8A3D` | 吉祥物毛色、强调点缀 |
| 松果绿 | `#22C55E` | 健康 / 成功态 |
| 暖光黄 | `#FFC83D` | 警告 / 进行中 |
| 坚果棕 | `#8B5E3C` | 橡果、深色描线 |

## 已知限制

全部是**位图**，没有矢量源文件。

- 位图改不了主题色。面板深色模式若要换色，仍然需要 SVG。
- 16px 即使是 `mark-head` 也只保留「双耳 + 双眼 + 腮红」的轮廓印象，细节全丢；能认出是动物脸，认不出是松鼠。要在 16px 保住松鼠特征，只能手工做像素级 hinting 或改用 SVG。
- `logo-primary` 与 `logo-mark-flat` 来自两次独立生成，造型细节不一致（前者写实绒毛，后者几何概括）。当「主标志 + 简化 mark」用没问题，但不是严格的同一套矢量派生。
- `logo-mark-head` 是镜像对称派生的，因此比原图更对称 —— 原扁平 mark 的脸略有手绘不对称感，头像版没有。

## 来源

由 git-wiki MCP 的 `prompt_asset_generate_image` 生成，风格资产 `pa_1b63e49bfd216c39`（IP设定板）。完整提示词与派生步骤见 [`PROMPTS.md`](./PROMPTS.md)。
