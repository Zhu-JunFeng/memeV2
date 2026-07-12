# XXYY 与 GMGN 已毕业项目交替接入

## 调度语义

- 服务启动后立即调用 XXYY，之后每 30 秒切换一次来源。
- 调用顺序为 `XXYY -> GMGN -> XXYY -> GMGN`，因此每个来源约每 60 秒执行一次。
- XXYY 请求 `COMPLETED` Feed，GMGN 请求 Trenches 的 `completed` 分类。
- 两个来源都只接收 KOL 数量大于等于 3 的项目，并按 CA 加入同一个候选池。
- 单次响应和候选池分别去重；一个来源失败不会触发其他备用来源，也不会改变下一次交替顺序。

## 接口

- XXYY：`POST /api/trade/open/api/feed/COMPLETED?chain=sol`，请求体使用 `kol: "3,"`。
- GMGN：`POST /v1/trenches?chain=sol`，只提交 `completed` 节点，并使用 `min_renowned_count: 3`。
- GMGN 单次最多读取 80 个已毕业项目，复用现有 GMGN OpenAPI Key。
