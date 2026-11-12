# Magpie 企业版

本项目基于 [Magpie](https://github.com/yetone/magpie)，包含两个交付物。

- **服务端**通过 Docker 运行，集中管理企业模型 API、主备路由、员工 Key 和用量。
- **员工桌面端**通过 Remote Magpie 接入企业网关，为 Claude Code 和 Codex 写入本地配置。

使用 `docker build --target enterprise-server -t magpie-enterprise-server .` 构建服务端，使用 `make enterprise-desktop` 构建桌面端。详细步骤见[部署与验收说明](docs/subsystems/enterprise-editions.md)。

[英文](README.md) · [许可证](LICENSE) · [第三方声明](THIRD_PARTY_NOTICES)
