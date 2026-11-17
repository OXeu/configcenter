<p align="center">
  <img src="assets/icon-128.webp" width="88" height="88" alt="ConfigCenter logo" />
</p>
<h3 align="center">ConfigCenter</h3>
<p align="center">A self-hosted configuration service with project isolation, scoped API keys and an agent-ready MCP endpoint.</p>

<p align="center">
  <a href="README.md">English</a> · <a href="README_zh.md">简体中文</a>
</p>

<p align="center">
  <a href="docs/index.md">Documentation</a> · <a href="#quick-start">Deployment</a> · <a href="docs/guide.md#configuration">Environment variables</a> · <a href="docs/api.md">HTTP API</a>
</p>

<p align="center">
  <a href="https://github.com/OXeu/configcenter/actions/workflows/ci.yml"><img src="https://github.com/OXeu/configcenter/actions/workflows/ci.yml/badge.svg" alt="CI status" /></a>
  <a href="go.mod"><img src="https://img.shields.io/badge/Go-1.26.10-00ADD8?logo=go&amp;logoColor=white" alt="Go 1.26.10" /></a>
  <a href="package.json"><img src="https://img.shields.io/badge/Vue-3-42b883?logo=vuedotjs&amp;logoColor=white" alt="Vue 3" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache_2.0-blue" alt="Apache License 2.0" /></a>
</p>

## Preview

![ConfigCenter preview](docs/assets/preview.png)

Project management in the actual web UI, rendered with synthetic example data.

## Features

- Projects, environments, configuration items and reviewed change proposals.
- OAuth sign-in and API keys scoped by project and operation.
- REST, a Go SDK and MCP for application and agent integration.
- English/Chinese UI, light/dark themes, SQLite or MySQL, and an embedded frontend.

## Quick Start

Install Git, Make, Node.js 24.15+ with npm, and Go 1.26.10. Bootstrap also needs Git access to the pinned [center-common](https://github.com/OXeu/center-common) repository.

```sh
git clone https://github.com/OXeu/configcenter.git
cd configcenter
make bootstrap
make build
CC_DB=file:.tmp/configcenter.db ./.tmp/bin/configcenter
```

Open http://127.0.0.1:8080. This starts a local SQLite instance; sign-in remains disabled until an OAuth provider is configured.

### Enable sign-in

Create an OAuth application at your identity provider with callback `http://127.0.0.1:8080/oauth/callback`. Set `CC_OAUTH_ISSUER`, `CC_OAUTH_CLIENT_ID` and `CC_OAUTH_CLIENT_SECRET` in an ignored `.env.local`, then restart the binary. Set `CC_BOOTSTRAP_ADMIN_EMAIL` to the intended administrator’s verified email before the first database initialization. Use `CC_PUBLIC_URL` for the deployed public address.

### Production and development

The [deployment and configuration guide](docs/guide.md) retains the full environment table, MySQL and Tencent SCF instructions, API/SDK overview and development workflow. Run `make check` with golangci-lint installed before shipping. Generated assets and binaries stay in `.tmp/`; the production binary serves the UI without Node.js.

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=OXeu/configcenter&type=Date)](https://www.star-history.com/#OXeu/configcenter&Date)

## LICENSE

Licensed under the [Apache License 2.0](LICENSE).
