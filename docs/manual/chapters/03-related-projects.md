---
id: related-projects
title: 关联项目
order: 3
estimate: 2 分钟
keywords:
  - touch-mail-router
  - Resin
  - 邮箱接收
  - 代理池
---

## touch-mail-router

[touch-mail-router](https://github.com/TalexDreamSoul/touch-mail-router) 可以作为邮箱接收层。它提供兼容 DuckMail 的账户、令牌和收件箱 API，也能通过 Cloudflare Email Worker、转发、DoneMail 或入站 API 接收验证码邮件。

对注册流水线来说，它只需要做到两件事：创建邮箱，读取验证码。其余邮件接入细节留在 mail router 内部。

## Resin

[Resin](https://github.com/Resinat/Resin) 可以作为代理池和稳定出口。把 Resin 的 HTTP forward proxy 同时配置给 `REGISTER_PROXY` 与 `CLEARANCE_PROXY`，可以让注册与 clearance 保持同一出口。

当前流水线尚未消费每账号 Resin sticky 配置。需要固定出口时，先使用统一入口；账号级 sticky 必须等真正注入请求链路后再启用。

<!-- story -->

## 阅读故事

mail router 像收发室，把所有验证码信件送到同一个可查询的信箱；Resin 像调度中心，为每次出门选择稳定路线。一个解决“信到了没有”，一个解决“从哪里出去”。
