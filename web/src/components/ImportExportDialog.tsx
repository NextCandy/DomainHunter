import { useRef, useState } from "react";
import { api, UnauthorizedError } from "../lib/api";
import type { DomainImportResult, ImportMode } from "../lib/api";
import { Spinner, cx, useToast } from "./ui";

type Format = "csv" | "json";
type Panel = "import" | "export";

const MODES: Array<{ value: ImportMode; label: string; hint: string }> = [
  { value: "skip", label: "跳过已有域名", hint: "已有记录与重复行都不覆盖" },
  { value: "overwrite", label: "覆盖已有域名", hint: "更新文件夹、标签、备注和开关" },
  { value: "deduplicate", label: "去重导入", hint: "保留每个域名的第一条记录" },
];

export function ImportExportDialog({
  onClose,
  onDone,
  onUnauthorized,
}: {
  onClose: () => void;
  onDone: () => void;
  onUnauthorized: () => void;
}) {
  const toast = useToast();
  const inputRef = useRef<HTMLInputElement>(null);
  const [panel, setPanel] = useState<Panel>("import");
  const [file, setFile] = useState<File | null>(null);
  const [format, setFormat] = useState<Format>("csv");
  const [mode, setMode] = useState<ImportMode>("skip");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<DomainImportResult | null>(null);

  async function importFile() {
    if (!file) return;
    setBusy(true);
    setResult(null);
    try {
      const content = await file.text();
      const response = await api.domains.importDomains(content, format, mode);
      setResult(response.result);
      toast("域名导入完成", "success");
      onDone();
    } catch (error) {
      if (error instanceof UnauthorizedError) {
        onUnauthorized();
        return;
      }
      toast(error instanceof Error ? error.message : "导入失败", "error");
    } finally {
      setBusy(false);
    }
  }

  async function exportDomains(exportFormat: Format) {
    setBusy(true);
    try {
      const download = await api.domains.exportDomains(exportFormat);
      const url = URL.createObjectURL(download.blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = download.filename;
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
      toast(`已导出 ${exportFormat.toUpperCase()} 文件`, "success");
    } catch (error) {
      if (error instanceof UnauthorizedError) {
        onUnauthorized();
        return;
      }
      toast(error instanceof Error ? error.message : "导出失败", "error");
    } finally {
      setBusy(false);
    }
  }

  function chooseFile(next: File | null) {
    setFile(next);
    setResult(null);
    if (!next) return;
    setFormat(next.name.toLowerCase().endsWith(".json") || next.type.includes("json") ? "json" : "csv");
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-overlay/40" onClick={onClose} role="presentation" />
      <section
        className="card relative max-h-[calc(100vh-2rem)] w-full max-w-lg overflow-y-auto p-4"
        role="dialog"
        aria-modal="true"
        aria-labelledby="import-export-title"
      >
        <header className="flex items-start justify-between gap-3">
          <div>
            <h2 id="import-export-title" className="text-[15px] font-semibold">导入与导出</h2>
            <p className="mt-1 text-[12px] text-ink-muted">导出包含文件夹、标签、备注和监控开关。</p>
          </div>
          <button
            type="button"
            className="btn btn-ghost h-7 w-7 px-0"
            onClick={onClose}
            aria-label="关闭导入导出窗口"
            title="关闭窗口"
          >
            <span aria-hidden="true">×</span>
          </button>
        </header>

        <div className="mt-4 flex gap-1 border-b border-line" role="tablist" aria-label="导入导出操作">
          <button
            type="button"
            role="tab"
            aria-selected={panel === "import"}
            className={cx(
              "-mb-px border-b-2 px-3 py-2 text-[13px] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent",
              panel === "import" ? "border-accent font-medium text-ink" : "border-transparent text-ink-muted",
            )}
            onClick={() => setPanel("import")}
          >
            导入
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={panel === "export"}
            className={cx(
              "-mb-px border-b-2 px-3 py-2 text-[13px] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent",
              panel === "export" ? "border-accent font-medium text-ink" : "border-transparent text-ink-muted",
            )}
            onClick={() => setPanel("export")}
          >
            导出
          </button>
        </div>

        {panel === "import" ? (
          <div className="mt-4 space-y-4">
            <div>
              <span className="label">文件</span>
              <input
                ref={inputRef}
                type="file"
                className="sr-only"
                accept=".csv,.json,text/csv,application/json"
                aria-label="上传 CSV 或 JSON 文件"
                onChange={(event) => chooseFile(event.target.files?.[0] ?? null)}
              />
              <button
                type="button"
                className="btn w-full justify-start"
                onClick={() => inputRef.current?.click()}
                aria-label="选择 CSV 或 JSON 文件"
                title="选择 CSV 或 JSON 文件"
              >
                {file ? file.name : "选择 CSV 或 JSON 文件"}
              </button>
              {file && (
                <p className="mt-1 text-[11px] text-ink-faint">
                  {format.toUpperCase()} · {(file.size / 1024).toFixed(1)} KB
                </p>
              )}
            </div>

            <fieldset>
              <legend className="label">导入格式</legend>
              <div className="grid grid-cols-2 gap-2">
                {(["csv", "json"] as Format[]).map((value) => (
                  <label key={value} className="flex items-center gap-2 rounded-md border border-line px-3 py-2 text-[13px]">
                    <input type="radio" name="import-format" checked={format === value} onChange={() => setFormat(value)} />
                    {value.toUpperCase()}
                  </label>
                ))}
              </div>
            </fieldset>

            <fieldset>
              <legend className="label">冲突处理</legend>
              <div className="space-y-2">
                {MODES.map((item) => (
                  <label key={item.value} className="flex items-start gap-2 rounded-md border border-line px-3 py-2 text-[13px]">
                    <input
                      className="mt-0.5"
                      type="radio"
                      name="import-mode"
                      checked={mode === item.value}
                      onChange={() => setMode(item.value)}
                    />
                    <span>
                      <span className="block font-medium">{item.label}</span>
                      <span className="block text-[11px] text-ink-faint">{item.hint}</span>
                    </span>
                  </label>
                ))}
              </div>
            </fieldset>

            {result && <ImportResult result={result} />}
            <div className="flex justify-end gap-2">
              <button type="button" className="btn" onClick={onClose} disabled={busy}>取消</button>
              <button
                type="button"
                className="btn btn-primary"
                onClick={() => void importFile()}
                disabled={busy || !file}
                aria-label="开始导入域名"
                title="开始导入域名"
              >
                {busy && <Spinner />} 开始导入
              </button>
            </div>
          </div>
        ) : (
          <div className="mt-4 space-y-3">
            <p className="text-[13px] text-ink-muted">导出全部监控域名。CSV 适合表格编辑，JSON 保留数组字段。</p>
            <div className="grid gap-2 sm:grid-cols-2">
              <button
                type="button"
                className="btn h-10 justify-start"
                onClick={() => void exportDomains("csv")}
                disabled={busy}
                aria-label="导出 CSV"
                title="导出 CSV 文件"
              >
                <span className="font-medium">导出 CSV</span>
                <span className="text-[11px] text-ink-faint">电子表格格式</span>
              </button>
              <button
                type="button"
                className="btn h-10 justify-start"
                onClick={() => void exportDomains("json")}
                disabled={busy}
                aria-label="导出 JSON"
                title="导出 JSON 文件"
              >
                <span className="font-medium">导出 JSON</span>
                <span className="text-[11px] text-ink-faint">结构化格式</span>
              </button>
            </div>
            <div className="flex justify-end">
              <button type="button" className="btn" onClick={onClose} disabled={busy}>关闭</button>
            </div>
          </div>
        )}
      </section>
    </div>
  );
}

function ImportResult({ result }: { result: DomainImportResult }) {
  return (
    <div className="rounded-card border border-accent/30 bg-accent-soft/45 px-3 py-2 text-[12px] text-ink" role="status">
      导入 {result.imported} 个 · 覆盖 {result.overwritten} 个 · 跳过 {result.skipped} 个 · 重复 {result.duplicates} 个
      {result.invalid && result.invalid.length > 0 && <span> · 无效 {result.invalid.length} 个</span>}
    </div>
  );
}
