# 发布 Squirrel

Squirrel 通过同一个 `v*` Tag 发布 GitHub Host 二进制和 npm 启动器。

## 用户入口

```bash
npx @talex-touch/squirrel
npx @talex-touch/squirrel doctor
npx @talex-touch/squirrel web
```

安装后可直接使用：

```bash
npm install --global @talex-touch/squirrel
squirrel
squirrel doctor
```

## 发布前置

1. GitHub 仓库为 `TalexDreamSoul/touch-squirrel`。
2. npm Scope `@talex-touch` 已授权发布 `squirrel` 包。
3. GitHub Actions Secret `NPM_TOKEN` 包含 npm Automation Token。
4. `npm/squirrel/package.json` 的主版本策略与 Host 保持一致。

## 发布

```bash
make release-check
git tag v0.2.0
git push origin v0.2.0
```

`Release Squirrel` Workflow 会：

1. 运行 Go 与 npm 测试；
2. 构建 macOS arm64/amd64、Linux arm64/amd64、Windows amd64；
3. 生成 `checksums.txt` 并创建 GitHub Release；
4. 将 `@talex-touch/squirrel` 的版本对齐 Tag 并发布到 npm。

npm 启动器不会携带 Go 二进制。它按自己的版本下载同版本 Release Asset，校验 SHA-256 后缓存；因此 npm 版本与 GitHub Tag 必须一一对应。
