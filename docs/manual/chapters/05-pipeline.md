---
id: pipeline
title: 四段流水线
order: 5
estimate: 3 分钟
keywords:
  - Solve
  - Prepare
  - Confirm
  - OAuth
  - 队列
---

## S：Solve

获取 Turnstile token，并放入短生命周期的 T 队列。浏览器模式通常串行打码，目的是减少多浏览器争抢资源和出口不稳定。

## P：Prepare

创建邮箱、申请并轮询验证码，再把邮箱、密码和验证码放进 Q 队列。队列只保留少量就绪项，避免为了小目标过量创建邮箱。

## C：Confirm 与 OAuth

C 会原子地领取一对 T+Q，提交注册并提取 SSO。成功后立即写入账号与会话文件，再交给 OAuth 交换、探活和 CPA JSON 落盘。

- S、P、C 是同一 worker 内的 Go goroutine，不是四个系统进程
- T/Q 是有容量和过期时间的内存库存
- Python 或 Node 只在需要 bridge 时跨进程参与

<!-- story -->

## 阅读故事

这像装配线：先准备通行码，再准备身份资料，然后把两样东西一起交到窗口办理。办理成功后，另一条线去验钥匙、编号、入库。
