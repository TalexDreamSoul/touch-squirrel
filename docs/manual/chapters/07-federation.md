---
id: federation
title: 联邦派发
order: 7
estimate: 2 分钟
keywords:
  - 联邦
  - heartbeat
  - assignment
  - 主节点
  - 从节点
---

## 主节点只派需求，不派秘密

主节点维护健康凭证数量和目标数量，计算 `need = target − healthy`。它不会把账号、邮箱或 token 下发给从节点。

## 从节点怎么领取工作

从节点定期发送 heartbeat，报告自身容量、是否忙碌和当前目标。主节点只有在仍有缺口且从节点不忙时，才回复 assignment。

一次 assignment 会同时受到全局缺口、`CLUSTER_ASSIGN_MIN`、`CLUSTER_ASSIGN_MAX` 与从节点 capacity 限制，最终通常是 0 或 1–10。多主场景下，从节点选择最大的 assignment 执行。

完成后，从节点可上传凭证，并向主节点报告完成数、上传数和失败数。

<!-- story -->

## 阅读故事

主节点像库房管理员，只说“还缺 8 箱”；从节点各自去本地采购和验货，完成后只报“交了几箱、坏了几箱”。货源和工具仍留在各自手里。
