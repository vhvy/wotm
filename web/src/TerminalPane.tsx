import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { createPortal } from "react-dom";
import {
  AlertTriangle,
  Fingerprint,
  LoaderCircle,
  RotateCw,
  Upload,
  Eraser,
} from "lucide-react";
import "@xterm/xterm/css/xterm.css";
import { api, message } from "./types";
import type { Host, Snapshot } from "./types";
import { UploadPanel, droppedFiles } from "./UploadPanel";
import type { FileBatch } from "./UploadPanel";

type Candidate = { public_key: string; fingerprint: string; changed: boolean };
export function TerminalPane({
  host,
  password,
  revision,
  active,
  onSaved,
  onPasswordUsed,
  onReconnect,
  toolsTarget,
}: {
  host: Host;
  password: string;
  revision: string;
  active: boolean;
  onSaved: (s: Snapshot) => void;
  onPasswordUsed: () => void;
  onReconnect: () => void;
  toolsTarget: HTMLDivElement | null;
}) {
  const container = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const secret = useRef(password);
  const callbacks = useRef({ onSaved, onPasswordUsed });
  callbacks.current = { onSaved, onPasswordUsed };
  const [attempt, setAttempt] = useState(0);
  const [status, setStatus] = useState("connecting");
  const [error, setError] = useState("");
  const [candidate, setCandidate] = useState<Candidate | null>(null);
  const [confirmed, setConfirmed] = useState(false);
  const [saving, setSaving] = useState(false);
  const [transferID, setTransferID] = useState("");
  const [uploadOpen, setUploadOpen] = useState(false);
  const [fileBatch, setFileBatch] = useState<FileBatch | null>(null);
  const [dragging, setDragging] = useState(false);
  const dragDepth = useRef(0);
  useEffect(() => {
    const terminal = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      lineHeight: 1.25,
      fontFamily: '"Cascadia Code", "SFMono-Regular", Consolas, monospace',
      scrollback: 5000,
      theme: {
        background: "#ffffff",
        foreground: "#25354b",
        cursor: "#337ee8",
        cursorAccent: "#ffffff",
        selectionBackground: "#cce4ff",
        selectionInactiveBackground: "#e4edfa",
        black: "#344054",
        red: "#c53948",
        green: "#167952",
        yellow: "#9a6700",
        blue: "#2865cb",
        magenta: "#9251bb",
        cyan: "#087f98",
        white: "#e8edf5",
        brightBlack: "#68788f",
        brightRed: "#dc4153",
        brightGreen: "#18845b",
        brightYellow: "#ad7400",
        brightBlue: "#3677e4",
        brightMagenta: "#a557c4",
        brightCyan: "#05859e",
        brightWhite: "#f5f8fd",
      },
    });
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(container.current!);
    fit.fit();
    termRef.current = terminal;
    fitRef.current = fit;
    let frame = 0;
    let lastWidth = 0;
    let lastHeight = 0;
    const observer = new ResizeObserver(() => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const element = container.current;
        if (!element?.offsetWidth || !element.offsetHeight) return;
        const width = element.clientWidth,
          height = element.clientHeight;
        if (width === lastWidth && height === lastHeight) return;
        lastWidth = width;
        lastHeight = height;
        fit.fit();
      });
    });
    observer.observe(container.current!);
    return () => {
      observer.disconnect();
      cancelAnimationFrame(frame);
      terminal.dispose();
      termRef.current = null;
      fitRef.current = null;
      secret.current = "";
    };
  }, []);
  useEffect(() => {
    const terminal = termRef.current!;
    const ws = new WebSocket(
      `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/api/terminal/${encodeURIComponent(host.id)}`,
    );
    ws.binaryType = "arraybuffer";
    let disposed = false;
    let reported = false;
    let ready = false;
    setStatus("connecting");
    setError("");
    setCandidate(null);
    setConfirmed(false);
    setTransferID("");
    ws.onopen = () =>
      ws.send(
        JSON.stringify({
          type: "init",
          password: secret.current,
          cols: terminal.cols,
          rows: terminal.rows,
        }),
      );
    ws.onmessage = (event) => {
      if (disposed) return;
      if (event.data instanceof ArrayBuffer) {
        terminal.write(new Uint8Array(event.data));
        return;
      }
      const data = JSON.parse(event.data);
      if (data.type === "ready") {
        ready = true;
        setStatus("ready");
        setTransferID(data.transfer_id || "");
        secret.current = "";
        callbacks.current.onPasswordUsed();
        terminal.focus();
      }
      if (data.type === "host_key") {
        reported = true;
        setStatus("trust");
        setCandidate(data);
      }
      if (data.type === "error" || data.type === "exit") {
        reported = true;
        secret.current = "";
        callbacks.current.onPasswordUsed();
        setStatus("closed");
        setTransferID("");
        setError(data.message);
      }
    };
    ws.onclose = () => {
      if (!disposed && !reported) {
        secret.current = "";
        callbacks.current.onPasswordUsed();
        setStatus("closed");
        setTransferID("");
        setError(ready ? "连接已断开" : "连接失败，请检查登录状态和网络");
      }
    };
    ws.onerror = () => {
      /* onclose provides the actionable error */
    };
    const dataListener = terminal.onData((data) => {
      if (ws.readyState === WebSocket.OPEN && ready)
        ws.send(new TextEncoder().encode(data));
    });
    const resizeListener = terminal.onResize(({ cols, rows }) => {
      if (ws.readyState === WebSocket.OPEN && ready)
        ws.send(JSON.stringify({ type: "resize", cols, rows }));
    });
    return () => {
      disposed = true;
      dataListener.dispose();
      resizeListener.dispose();
      ws.close();
    };
  }, [host.id, attempt]);
  useEffect(() => {
    if (active) {
      const timer = setTimeout(() => {
        fitRef.current?.fit();
        termRef.current?.focus();
      }, 50);
      return () => clearTimeout(timer);
    }
  }, [active]);
  async function trust() {
    if (!candidate) return;
    setSaving(true);
    setError("");
    try {
      const updated = await api(
        `/hosts/${host.id}/trust`,
        "PUT",
        { public_key: candidate.public_key },
        revision,
      );
      callbacks.current.onSaved(updated);
      setAttempt((value) => value + 1);
    } catch (err) {
      setError(message(err));
    } finally {
      setSaving(false);
    }
  }
  return (
    <section
      className="terminal-pane"
      hidden={!active}
      onDragEnter={(event) => {
        if (event.dataTransfer.types.includes("Files")) {
          event.preventDefault();
          dragDepth.current++;
          setDragging(true);
        }
      }}
      onDragOver={(event) => {
        if (event.dataTransfer.types.includes("Files")) {
          event.preventDefault();
          event.dataTransfer.dropEffect = transferID ? "copy" : "none";
        }
      }}
      onDragLeave={(event) => {
        event.preventDefault();
        dragDepth.current = Math.max(0, dragDepth.current - 1);
        if (!dragDepth.current) setDragging(false);
      }}
      onDrop={(event) => {
        if (!event.dataTransfer.types.includes("Files")) return;
        event.preventDefault();
        dragDepth.current = 0;
        setDragging(false);
        setUploadOpen(true);
        if (transferID) setFileBatch(droppedFiles(event.dataTransfer));
      }}
    >
      {active &&
        toolsTarget &&
        createPortal(
          <>
            <button
              className={`terminal-tool ${uploadOpen ? "selected" : ""}`}
              disabled={status !== "ready"}
              onClick={() => setUploadOpen((value) => !value)}
              title="上传文件（支持拖拽）"
            >
              <Upload size={15} />
              <span>上传文件</span>
            </button>
            <button
              className="terminal-tool"
              onClick={() => termRef.current?.clear()}
              title="清空终端显示"
              aria-label="清空终端显示"
            >
              <Eraser size={15} />
            </button>
          </>,
          toolsTarget,
        )}
      <div className="terminal-info">
        <span>
          <i className={`status-dot ${status === "ready" ? "live" : ""}`} />
          {status === "ready"
            ? "已连接"
            : status === "connecting"
              ? "正在连接"
              : status === "trust"
                ? "等待确认"
                : "已断开"}
        </span>
        <code>
          {host.username}@{host.address}:{host.port}
        </code>
        <span>{host.proxy_id ? "SOCKS5 · " : ""}SSH / xterm-256color</span>
      </div>
      <div className="terminal-workarea">
        <div className="terminal-surface" ref={container} />
        <UploadPanel
          open={uploadOpen}
          transferID={transferID}
          batch={fileBatch}
          onConsumed={() => setFileBatch(null)}
          onClose={() => setUploadOpen(false)}
        />
      </div>
      {dragging && (
        <div className="terminal-drop-hint">
          <Upload size={32} />
          <strong>
            {transferID ? "释放文件，添加到上传列表" : "请先连接主机"}
          </strong>
          <span>可在上传面板确认目标目录后开始传输</span>
        </div>
      )}
      {status === "connecting" && (
        <div className="terminal-overlay">
          <LoaderCircle className="spin" size={25} />
          <p>正在建立 SSH 连接…</p>
        </div>
      )}
      {candidate && status === "trust" && (
        <div className="terminal-overlay">
          <div className="trust-card">
            <div className={`trust-icon ${candidate.changed ? "warning" : ""}`}>
              {candidate.changed ? <AlertTriangle /> : <Fingerprint />}
            </div>
            <h3>
              {candidate.changed ? "服务器指纹发生变化" : "首次连接此主机"}
            </h3>
            <p>
              {candidate.changed
                ? "连接已阻止。请通过可信渠道核实服务器身份，再替换已保存的公钥。"
                : "请核对服务器指纹。确认后，主机公钥会保存到配置文件。"}
            </p>
            <code className="fingerprint">{candidate.fingerprint}</code>
            <label className="check">
              <input
                type="checkbox"
                checked={confirmed}
                onChange={(e) => setConfirmed(e.target.checked)}
              />
              我已通过可信渠道核对该指纹
            </label>
            {error && (
              <p className="form-error" role="alert">
                {error}
              </p>
            )}
            <button
              className="button primary"
              disabled={!confirmed || saving}
              onClick={trust}
            >
              {saving
                ? "正在保存…"
                : candidate.changed
                  ? "替换公钥并连接"
                  : "信任并连接"}
            </button>
          </div>
        </div>
      )}
      {status === "closed" && (
        <div className="connection-ended">
          <span>{error}</span>
          <button className="button subtle" onClick={onReconnect}>
            <RotateCw size={14} />
            重新连接
          </button>
        </div>
      )}
    </section>
  );
}
