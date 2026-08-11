import { useCallback, useEffect, useRef, useState } from "react";
import { UnauthorizedError } from "./api";

interface AsyncState<T> {
  data: T | null;
  error: string;
  loading: boolean;
  reload: () => void;
}

/**
 * 统一的数据加载 Hook：处理 loading / error / 竞态与卸载后 setState。
 * 401 会向上抛给 App，由它切换到登录页。
 */
export function useAsync<T>(
  loader: () => Promise<T>,
  deps: unknown[],
  onUnauthorized?: () => void,
): AsyncState<T> {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [tick, setTick] = useState(0);
  const requestId = useRef(0);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    const id = ++requestId.current;
    setLoading(true);
    loader()
      .then((result) => {
        if (!mounted.current || id !== requestId.current) return;
        setData(result);
        setError("");
      })
      .catch((err: unknown) => {
        if (!mounted.current || id !== requestId.current) return;
        if (err instanceof UnauthorizedError) {
          onUnauthorized?.();
          return;
        }
        setError(err instanceof Error ? err.message : "加载失败");
      })
      .finally(() => {
        if (!mounted.current || id !== requestId.current) return;
        setLoading(false);
      });
    // loader 由调用方用 deps 控制，避免每次渲染都重新请求
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick]);

  const reload = useCallback(() => setTick((value) => value + 1), []);
  return { data, error, loading, reload };
}
