---
id: prerequisites
title: 运行准备
order: 2
estimate: 2 分钟
keywords:
  - 运行准备
  - 邮箱
  - 代理
  - clearance
  - Turnstile
---

## 运行前要准备什么

先准备一个独立运行根目录。这里保存配置、日志、运行状态、PID/锁文件、每次任务的输出和本地凭证池。两台机器不要共用同一个本地根目录。

## 三个外部条件

- **邮箱**：能创建地址并读取验证码即可。供应商不同，流程不变。
- **代理与 clearance**：注册请求和 clearance 必须走同一出口。换代理时，二者要一起换，否则 cookie 可能失效。
- **浏览器打码**：browser provider 通过 `GROK_PYTHON` 调用 Python 与 Playwright；lite provider 调用轻量服务。浏览器模式通常决定整体速度。

<!-- story -->

## 阅读故事

注册好比进一座需要邀请函的楼：邮箱是信箱，代理是出行路线，clearance 是门卫见过你后发的通行贴纸。你不能从 A 路线拿贴纸，再从 B 路线进门。
