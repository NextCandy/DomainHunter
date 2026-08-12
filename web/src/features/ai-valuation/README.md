# AI 域名鉴定估价前端模块

本模块位于 `web/src/features/ai-valuation`，采用 React 18、TypeScript 与项目既有的 Tailwind / `ui.tsx` 组件，已挂载到域名详情与“AI 研究与自动化”页面。它已通过当前项目的 `npm run typecheck`、`npm run lint` 与 `npm run build`；Go 后端同步提供下述 `/api/v2/ai/*` 契约，浏览器只访问同源 API，API Key 仅由后端读取。

## 目录

| 路径 | 职责 |
| --- | --- |
| `components/DomainValuationPanel.tsx` | 域名详情的估价模块，按“域名 / 评分 / 价格评估 / 核心分析”逐行展示，完整覆盖空态、数据复核禁用、入队、轮询、结果、失败、取消、额度与免责声明。 |
| `components/DeepSeekProfileForm.tsx` | DeepSeek / OpenAI-compatible 档案配置，覆盖 Base URL、模型、Key 写入、连接测试与并发额度。 |
| `hooks/useDomainValuation.ts` | 当前任务加载、2.5 秒轮询、入队、取消、错误映射和 401 回调。 |
| `hooks/useAIProfiles.ts` | 档案加载、保存、删除、默认档案选择和连接测试。 |
| `lib/valuation-api.ts` | 同源 API 适配层，复用 `web/src/lib/api.ts` 的 CSRF 和认证行为。 |
| `lib/valuation-types.ts` | 估价报告、人民币价格区间、队列任务、档案、配额与结果状态类型。 |
| `valuation.css` | 精致化 token、估价面板与工作台辅助样式。 |

## 1. 后端 API 契约

前端调用均经由 DomainHunter 同源 API；**浏览器不能直接调用 AI 提供商，也不得接触 API Key**。后端应返回 JSON，非 2xx 返回 `{ "error": "可读错误信息" }`。

| 方法与路径 | 用途 | 最小响应 |
| --- | --- | --- |
| `GET /api/v2/ai/valuation-policy` | 读取前端展示用的数据边界 | `status_guard`、`research_only_disclaimer` |
| `GET /api/v2/ai/profiles` | 列出安全脱敏的 Provider 档案 | `{ profiles: AIProfile[] }` |
| `POST /api/v2/ai/profiles` | 新建档案 | `AIProfile` |
| `PUT /api/v2/ai/profiles/:id` | 修改档案 | `AIProfile` |
| `DELETE /api/v2/ai/profiles/:id` | 删除档案 | `{ status: "deleted" }` |
| `POST /api/v2/ai/profiles/test-connection` | 仅以合成输入测试后端到 Provider 的连接 | `ConnectionTestResult` |
| `GET /api/v2/domains/:domain/valuation` | 读取当前/最近任务；尚无任务时返回 404 | `ValuationJob`，完成结果含 `score`、`price_evaluation_cny`、`core_analysis` |
| `POST /api/v2/domains/:domain/valuation` | 创建或复用幂等估价任务 | `ValuationJob` |
| `GET /api/v2/ai/jobs/:id` | 轮询任务状态 | `ValuationJob` |
| `POST /api/v2/ai/jobs/:id/cancel` | 取消排队任务 | `ValuationJob` |

## 2. 在 `DomainDrawer` 中挂载估价面板

先将 `DomainInfo` 扩展为后端返回的 `review` 字段；不要在浏览器用日期计算复核状态。

```ts
// web/src/lib/api.ts
export interface ReviewState {
  required: boolean;
  reasons: Array<
    | "query_error"
    | "unknown_status"
    | "low_confidence"
    | "drop_status_future_expiry"
    | "provider_conflict"
    | "stale_evidence"
  >;
  severity?: "info" | "warning" | "critical";
  explanation?: string;
}

export interface DomainInfo {
  // 保留既有字段
  review?: ReviewState;
}
```

然后在 `web/src/components/DomainDrawer.tsx` 导入并替换概览页片段：

```tsx
import { DomainValuationPanel } from "../features/ai-valuation";

// 原本：{tab === "overview" && <OverviewTab info={info} />}
{tab === "overview" && (
  <div className="space-y-4">
    <DomainValuationPanel
      domain={info.name}
      status={info.status}
      confidence={info.confidence}
      reviewRequired={Boolean(info.review?.required)}
      onUnauthorized={onUnauthorized}
      onCompleted={() => {
        onChanged?.();
        void load();
      }}
      compact
    />
    <OverviewTab info={info} />
  </div>
)}
```

估价面板将自动禁用低可信、未知、错误、跳过或已标记复核的对象。不要移除该前端提醒；更不能把它当成后端授权校验的替代，后端仍必须拒绝不满足规则的入队请求。

## 3. 在“AI 与自动化”页面挂载 Provider 配置

在 AI 配置区导入：

```tsx
import { DeepSeekProfileForm } from "../features/ai-valuation";

<DeepSeekProfileForm
  profile={selectedProfile}
  onUnauthorized={onUnauthorized}
  onSaved={(saved) => {
    setSelectedProfile(saved);
    void reloadProfiles();
  }}
  onCancel={() => setEditing(false)}
/>
```

默认档案使用 DeepSeek 官方 OpenAI-compatible 接口：`https://api.deepseek.com` 和 `deepseek-v4-flash`；用户也可以在表单中切换其他已允许的 OpenAI-compatible 网关。用户只填写 **Base URL**；前端会阻止 `/chat/completions`、query、fragment、userinfo 与非 HTTPS 输入，但这些只是体验校验。后端必须再次执行 DNS/IP 私网拒绝、出站 allowlist、重定向逐跳检查与 URL 规范化。

## 4. UI 精致化接入

在入口文件（如 `web/src/main.tsx`）或在首次使用估价模块的页面导入一次：

```ts
import "./features/ai-valuation/valuation.css";
```

随后将应用范围内的页面头部、表格和复核标记按如下方式渐进替换：

```tsx
<header className="workspace-header px-6 py-5">
  <span className="workspace-kicker">DOMAIN OPERATIONS</span>
  <h1 className="mt-1 text-2xl font-semibold tracking-tight">域名资产</h1>
</header>

<table className="data-table-refined">…</table>
<span className="review-badge inline-flex items-center rounded px-1.5 py-0.5">数据需复核</span>
```

不要把整站一次性换成渐变、玻璃拟态、重阴影或多种状态色。优先改造页面标题、筛选条、表格行、详情抽屉和空状态五类高频结构；稳定的留白、边线和排版比新增装饰更能提升精致感。

## 5. 前端验收

| 场景 | 预期 |
| --- | --- |
| 未配置档案 | 面板显示空态与配置引导，不显示假装可用的估价按钮。 |
| 复核 / 低可信域名 | 估价按钮安全禁用，明确解释必须先重新检查。 |
| 新任务 | 任务进入 `queued` 或 `running`，每 2.5 秒轮询，不重复入队。 |
| 额度耗尽 | API `429` 显示额度提示；UI 不继续自动重试。 |
| 返回非法 JSON | 后端 Job 标记失败，面板显示安全失败态，不渲染分数。 |
| Key 已存在 | 表单只显示来源/是否已设置；完全不回显 Key。 |
| 自定义 Base URL | 浏览器仅做格式反馈，服务端继续执行完整 SSRF 校验。 |
| 完成结果 | 显示研究性指示区间、质量、流动性、风险、数据缺口、状态护栏和免责声明。 |

## 6. 验证命令

```bash
cd web
npm run typecheck
npm run lint
npm run build
```
