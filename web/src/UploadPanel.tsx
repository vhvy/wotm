import { useEffect, useRef, useState } from "react";
import { Check, FileUp, LoaderCircle, Upload, X } from "lucide-react";
import { api, message } from "./types";

type Item = {
  id: number;
  file: File;
  state: "queued" | "uploading" | "done" | "error" | "cancelled";
  progress: number;
  error?: string;
};
export type FileBatch = { files: File[]; error?: string };
export function droppedFiles(data: DataTransfer): FileBatch {
  if (
    Array.from(data.items).some(
      (item) => item.webkitGetAsEntry?.()?.isDirectory,
    )
  ) {
    return {
      files: [],
      error: "暂不支持上传文件夹，请选择文件或先打包为压缩文件。",
    };
  }
  return { files: Array.from(data.files) };
}
const maxSize = 256 * 1024 * 1024;
function size(bytes: number) {
  return bytes < 1024 * 1024
    ? `${(bytes / 1024).toFixed(1)} KiB`
    : `${(bytes / 1024 / 1024).toFixed(1)} MiB`;
}

export function UploadPanel({
  open,
  transferID,
  batch,
  onConsumed,
  onClose,
}: {
  open: boolean;
  transferID: string;
  batch: FileBatch | null;
  onConsumed: () => void;
  onClose: () => void;
}) {
  const [directory, setDirectory] = useState("");
  const [items, setItems] = useState<Item[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const picker = useRef<HTMLInputElement>(null);
  const request = useRef<XMLHttpRequest | null>(null);
  const stopped = useRef(false);
  const mounted = useRef(true);
  const counter = useRef(0);
  const callbacks = useRef(onConsumed);
  callbacks.current = onConsumed;
  useEffect(
    () => () => {
      mounted.current = false;
      stopped.current = true;
      request.current?.abort();
    },
    [],
  );
  useEffect(() => {
    if (!open || !transferID || directory) return;
    const controller = new AbortController();
    setLoading(true);
    setError("");
    api<{ directory: string }>(
      `/transfers/${transferID}`,
      "GET",
      undefined,
      undefined,
      controller.signal,
    )
      .then((info) => setDirectory(info.directory))
      .catch((err) => {
        if (!controller.signal.aborted) setError(message(err));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [open, transferID]);
  function addFiles(files: File[]) {
    setItems((previous) => [
      ...previous,
      ...files.map((file) => ({
        id: ++counter.current,
        file,
        state: file.size > maxSize ? ("error" as const) : ("queued" as const),
        progress: 0,
        error: file.size > maxSize ? "超过 256 MiB 限制" : undefined,
      })),
    ]);
  }
  useEffect(() => {
    if (batch) {
      addFiles(batch.files);
      if (batch.error) setError(batch.error);
      callbacks.current();
    }
  }, [batch]);
  function update(id: number, patch: Partial<Item>) {
    if (mounted.current)
      setItems((previous) =>
        previous.map((item) => (item.id === id ? { ...item, ...patch } : item)),
      );
  }
  function send(item: Item, target: string) {
    return new Promise<void>((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      request.current = xhr;
      xhr.open(
        "PUT",
        `/api/transfers/${transferID}/file?${new URLSearchParams({ directory: target, name: item.file.name })}`,
      );
      xhr.setRequestHeader("Content-Type", "application/octet-stream");
      xhr.timeout = 15 * 60 * 1000;
      xhr.upload.onprogress = (event) => {
        if (event.lengthComputable)
          update(item.id, {
            progress: Math.min(
              99,
              Math.round((event.loaded / event.total) * 100),
            ),
          });
      };
      xhr.onload = () => {
        if (xhr.status === 201) {
          update(item.id, { state: "done", progress: 100 });
          resolve();
        } else {
          let text = "上传失败，请检查服务器连接";
          try {
            text = JSON.parse(xhr.responseText).error || text;
          } catch {
            /* proxy may return HTML */
          }
          reject(new Error(text));
        }
      };
      xhr.onerror = () => reject(new Error("连接已中断，请检查服务和网络"));
      xhr.ontimeout = () => reject(new Error("上传超时"));
      xhr.onabort = () => reject(new DOMException("已取消", "AbortError"));
      xhr.send(item.file);
    });
  }
  async function upload() {
    if (!directory.startsWith("/")) {
      setError("请填写远程目录的绝对路径，例如 /home/ubuntu");
      return;
    }
    setError("");
    setBusy(true);
    stopped.current = false;
    const pending = items.filter(
      (item) =>
        item.state === "queued" ||
        item.state === "cancelled" ||
        (item.state === "error" && item.file.size <= maxSize),
    );
    for (const item of pending) {
      if (stopped.current || !mounted.current) break;
      update(item.id, { state: "uploading", progress: 0, error: undefined });
      try {
        await send(item, directory);
      } catch (err) {
        update(item.id, {
          state:
            err instanceof DOMException && err.name === "AbortError"
              ? "cancelled"
              : "error",
          error: message(err),
        });
      } finally {
        request.current = null;
      }
    }
    if (mounted.current) setBusy(false);
  }
  const queued = items.some(
    (item) => item.state !== "done" && item.file.size <= maxSize,
  );
  return (
    <aside className="upload-panel" hidden={!open} aria-label="文件上传">
      <div className="upload-heading">
        <div>
          <FileUp size={18} />
          <h3>上传文件</h3>
        </div>
        <button
          className="icon-button"
          aria-label="收起上传面板"
          onClick={onClose}
        >
          <X size={18} />
        </button>
      </div>
      <p className="muted tiny">复用当前 SSH 连接，通过 SFTP 传输。</p>
      <label>
        远程目标目录
        <input
          value={directory}
          disabled={busy || loading}
          onChange={(event) => setDirectory(event.target.value)}
          placeholder={loading ? "正在读取默认目录…" : "/home/username"}
        />
      </label>
      <p className="upload-note">
        默认使用远程主目录，不随终端中的 cd 命令变化。同名文件不会覆盖。
      </p>
      <input
        ref={picker}
        type="file"
        multiple
        hidden
        onChange={(event) => {
          addFiles(Array.from(event.target.files || []));
          event.target.value = "";
        }}
      />
      <button
        className="file-dropzone"
        disabled={!transferID || busy}
        onClick={() => picker.current?.click()}
        onDragOver={(event) => {
          event.preventDefault();
          event.stopPropagation();
        }}
        onDrop={(event) => {
          event.preventDefault();
          event.stopPropagation();
          if (!busy) {
            const batch = droppedFiles(event.dataTransfer);
            if (batch.error) setError(batch.error);
            else addFiles(batch.files);
          }
        }}
      >
        <Upload size={25} />
        <strong>拖拽文件到这里</strong>
        <span>或点击选择文件 · 单文件最多 256 MiB</span>
      </button>
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      <div className="upload-items">
        {items.map((item) => (
          <div className={`upload-item ${item.state}`} key={item.id}>
            <div>
              <span title={item.file.name}>{item.file.name}</span>
              {item.state === "done" ? (
                <Check size={15} />
              ) : item.state === "uploading" ? (
                <LoaderCircle className="spin" size={14} />
              ) : (
                !busy && (
                  <button
                    className="icon-button"
                    aria-label={`移除 ${item.file.name}`}
                    onClick={() =>
                      setItems((previous) =>
                        previous.filter((value) => value.id !== item.id),
                      )
                    }
                  >
                    <X size={13} />
                  </button>
                )
              )}
            </div>
            <small>
              {size(item.file.size)} ·{" "}
              {item.state === "done"
                ? "上传完成"
                : item.state === "uploading"
                  ? `${item.progress}%`
                  : item.state === "queued"
                    ? "等待上传"
                    : item.error || "已取消"}
            </small>
            {item.state === "uploading" && (
              <progress
                max={100}
                value={item.progress}
                aria-label={`${item.file.name} 上传进度`}
              />
            )}
          </div>
        ))}
      </div>
      <div className="upload-actions">
        {busy ? (
          <button
            className="button"
            onClick={() => {
              stopped.current = true;
              request.current?.abort();
            }}
          >
            取消上传
          </button>
        ) : (
          <>
            <button
              className="button"
              disabled={!items.length}
              onClick={() => setItems([])}
            >
              清空列表
            </button>
            <button
              className="button primary"
              disabled={!transferID || !queued || loading || !directory}
              onClick={upload}
            >
              <Upload size={15} />
              开始上传
            </button>
          </>
        )}
      </div>
      {!transferID && <p className="muted tiny">终端连接成功后可上传文件。</p>}
    </aside>
  );
}
