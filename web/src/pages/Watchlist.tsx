import { DomainsPage } from "./Domains";

/**
 * 观察列表 = 被标记为收藏的域名。复用域名页的全部筛选与批量能力，
 * 只是默认加上 favorite=true。
 */
export function WatchlistPage({ onUnauthorized }: { onUnauthorized: () => void }) {
  return <DomainsPage onUnauthorized={onUnauthorized} favoriteOnly title="观察列表" />;
}
