import { Link, useNavigate, useParams } from "react-router-dom";
import { DomainDrawer } from "../components/DomainDrawer";

/**
 * /domains/:name —— 域名详情的全屏形态。
 *
 * 抽屉是默认入口，但它分享不出去：关掉就没了，也没法把"这个域名为什么判成
 * 可注册"发给别人看。这个页面用的是同一个 DomainDrawer 组件（variant="page"），
 * 只是把抽屉外壳换成整页，内容与交互完全一致，不存在两份实现走偏的问题。
 */
export function DomainDetailPage({ onUnauthorized }: { onUnauthorized: () => void }) {
  const params = useParams();
  const navigate = useNavigate();
  const name = decodeURIComponent(params.name ?? "").trim();

  if (!name) {
    return (
      <p className="text-[13px] text-ink-muted">
        没有指定域名。<Link to="/domains" className="text-accent hover:underline">返回域名列表</Link>
      </p>
    );
  }

  return (
    <div className="space-y-3">
      <Link to="/domains" className="inline-flex text-[12px] text-ink-muted hover:text-accent">
        ← 返回域名列表
      </Link>
      <DomainDrawer
        variant="page"
        domain={name}
        onClose={() => navigate("/domains")}
        onUnauthorized={onUnauthorized}
      />
    </div>
  );
}
