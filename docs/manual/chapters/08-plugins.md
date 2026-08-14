---
id: plugins
title: 多语言与插件
order: 8
estimate: 3 分钟
keywords:
  - 插件
  - Go
  - Python
  - Node
  - bridge
  - NDJSON
---

## Go 原生与 hybrid

当前 xAI 主线的协议、队列、OAuth 与 CPA 写入都在 Go 流水线中。它标记为 hybrid，是因为 Go 编排时会调用 Python 浏览器脚本完成 Turnstile mint。

## Bridge

bridge 插件由 Host 启动外部 Python 或 Node 进程。Host 传入 target、输出目录和插件配置；子进程通过 stdout 输出 NDJSON，回传 progress、log、captcha、artifact、error、done。

## JS runtime 的当前边界

manifest 可以声明 `go`、`js`、`hybrid` 或 bridge。当前通用 runner 已支持 xAI 的 Go 流水线和 bridge；纯 JS registrar 的通用执行器尚未落地，不能只因 manifest 写了 JS 就认为它已可运行。

<!-- story -->

## 阅读故事

Go 像总导演，Python 或 Node 像受邀的专业演员。演员可以完成自己的镜头，但场记、进度、产物和收工规则都必须回到导演手里。
