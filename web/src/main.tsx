import { useEffect, useRef, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import { createRoot } from "react-dom/client";
import {
  ArrowDownToLine,
  ArrowRight,
  Check,
  ChevronRight,
  CircleHelp,
  Copy,
  Edit3,
  FileKey2,
  FolderOpen,
  KeyRound,
  LogOut,
  Network,
  Plus,
  Search,
  Server,
  Settings,
  ShieldCheck,
  TerminalSquare,
  Trash2,
  X,
} from "lucide-react";
import { api, APIError, message } from "./types";
import type { Host, Snapshot, SSHKey, SOCKSProxy } from "./types";
import { ProxyForm, ProxyList } from "./ProxiesPanel";
import { TerminalPane } from "./TerminalPane";
import { startConfigSync } from "./configSync";
import "./style.css";
import "./transfers.css";

type Page = "hosts" | "keys" | "proxies" | "settings";
type Tab = { id: string; host: Host; password: string; revision: string };
type ModalState =
  | { type: "host"; host?: Host; revision: string }
  | { type: "key"; key?: SSHKey; revision: string }
  | { type: "proxy"; proxy?: SOCKSProxy; revision: string }
  | { type: "connect"; host: Host; revision: string };

// getRandomValues also works on ordinary intranet HTTP origins.
const tabID = () =>
  Array.from(crypto.getRandomValues(new Uint8Array(12)), (n) =>
    n.toString(16).padStart(2, "0"),
  ).join("");

function Logo({ small = false }: { small?: boolean }) {
  return (
    <div className={`brand ${small ? "small" : ""}`}>
      <span className="brand-mark">
        <TerminalSquare size={21} strokeWidth={1.8} />
      </span>
      <span>
        WOTM<span className="brand-caption">TERMINAL WORKSPACE</span>
      </span>
    </div>
  );
}
function Modal({
  title,
  subtitle,
  children,
  onClose,
}: {
  title: string;
  subtitle: string;
  children: ReactNode;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    dialog.current?.showModal();
    return () => dialog.current?.close();
  }, []);
  return (
    <dialog
      ref={dialog}
      className="modal"
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="modal-heading">
        <div>
          <h2>{title}</h2>
          <p>{subtitle}</p>
        </div>
        <button
          type="button"
          className="icon-button"
          aria-label="关闭"
          onClick={onClose}
        >
          <X size={20} />
        </button>
      </div>
      {children}
    </dialog>
  );
}
function Login({ onLogin }: { onLogin: (s: Snapshot) => void }) {
  const [username, setUsername] = useState("admin"),
    [password, setPassword] = useState(""),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api("/login", "POST", { username, password });
      setPassword("");
      onLogin(await api("/config"));
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="login-layout">
      <div className="login-story">
        <Logo />
        <div className="login-copy">
          <span className="eyebrow">YOUR SERVERS. YOUR WORKSPACE.</span>
          <h1>
            与每一台主机，
            <br />
            保持连接。
          </h1>
          <p>
            集中管理 SSH 主机与密钥，
            <br />
            让终端回到简单、直接的样子。
          </p>
          <div className="story-terminal">
            <div>
              <i />
              <i />
              <i />
              <span>workspace — ssh</span>
            </div>
            <pre>
              <span>➜</span> ~ ssh home-server
              <br />
              <span>✓</span> Connection established.
              <br />
              <br />
              Welcome back.
              <br />
              <span>❯</span> <b className="cursor"> </b>
            </pre>
          </div>
        </div>
        <span className="login-foot">轻量部署 · 配置文件驱动 · 无数据库</span>
      </div>
      <main className="login-main">
        <form className="login-form" onSubmit={submit}>
          <span className="section-label">WORKSPACE ACCESS</span>
          <h2>登录工作台</h2>
          <p>你的主机，触手可及。</p>
          <label>
            用户名
            <input
              autoComplete="username"
              required
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoFocus
            />
          </label>
          <label>
            密码
            <input
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </label>
          {error && (
            <div className="form-error" role="alert">
              {error}
            </div>
          )}
          <button className="button primary large" disabled={busy}>
            {busy ? "正在登录…" : "进入工作台"}
            <ArrowRight size={17} />
          </button>
          <div className="login-note">
            <ShieldCheck size={16} />
            仅授权用户可以访问此工作台
          </div>
        </form>
      </main>
    </div>
  );
}

function HostForm({
  host,
  keys,
  proxies,
  onSubmit,
  onClose,
}: {
  host?: Host;
  keys: SSHKey[];
  proxies: SOCKSProxy[];
  onSubmit: (h: Host) => Promise<void>;
  onClose: () => void;
}) {
  const [value, setValue] = useState<Host>(
    host || {
      id: "",
      name: "",
      address: "",
      port: 22,
      username: "root",
      auth: { mode: "password_prompt" },
      group: "",
      remark: "",
    },
  );
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  function field<K extends keyof Host>(key: K, val: Host[K]) {
    setValue((current) => ({ ...current, [key]: val }));
  }
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await onSubmit(value);
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form onSubmit={submit}>
      <label>
        主机名称
        <input
          required
          maxLength={120}
          value={value.name}
          onChange={(e) => field("name", e.target.value)}
          placeholder="例如：生产服务器"
          autoFocus
        />
      </label>
      <div className="form-grid">
        <label>
          主机地址
          <input
            required
            value={value.address}
            onChange={(e) => field("address", e.target.value)}
            placeholder="IP 地址或域名"
          />
        </label>
        <label>
          SSH 端口
          <input
            type="number"
            min={1}
            max={65535}
            required
            value={value.port}
            onChange={(e) => field("port", Number(e.target.value))}
          />
        </label>
      </div>
      <div className="form-grid">
        <label>
          登录用户
          <input
            required
            value={value.username}
            onChange={(e) => field("username", e.target.value)}
          />
        </label>
        <label>
          分组
          <input
            value={value.group || ""}
            onChange={(e) => field("group", e.target.value)}
            placeholder="可选，例如：生产环境"
          />
        </label>
      </div>
      <label>
        认证方式
        <select
          value={value.auth.mode}
          onChange={(e) =>
            field(
              "auth",
              e.target.value === "key"
                ? { mode: "key", key_id: keys[0]?.id || "" }
                : { mode: "password_prompt" },
            )
          }
        >
          <option value="password_prompt">连接时输入密码</option>
          <option value="key" disabled={!keys.length}>
            使用 SSH 密钥{!keys.length ? "（请先添加密钥）" : ""}
          </option>
        </select>
      </label>
      {value.auth.mode === "key" && (
        <label>
          SSH 密钥
          <select
            required
            value={value.auth.key_id}
            onChange={(e) =>
              field("auth", { mode: "key", key_id: e.target.value })
            }
          >
            {keys.map((k) => (
              <option key={k.id} value={k.id}>
                {k.name}
              </option>
            ))}
          </select>
        </label>
      )}
      <label>
        连接代理
        <select
          value={value.proxy_id || ""}
          onChange={(event) => field("proxy_id", event.target.value)}
        >
          <option value="">直连（不使用代理）</option>
          {proxies.map((proxy) => (
            <option key={proxy.id} value={proxy.id}>
              {proxy.name} · SOCKS5
            </option>
          ))}
        </select>
      </label>
      <label>
        备注
        <textarea
          rows={2}
          value={value.remark || ""}
          onChange={(e) => field("remark", e.target.value)}
          placeholder="可选"
        />
      </label>
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      <div className="modal-actions">
        <button type="button" className="button" onClick={onClose}>
          取消
        </button>
        <button className="button primary" disabled={busy}>
          {busy ? "正在保存…" : "保存主机"}
        </button>
      </div>
    </form>
  );
}
function KeyForm({
  sshKey,
  onSubmit,
  onClose,
}: {
  sshKey?: SSHKey;
  onSubmit: (data: unknown) => Promise<void>;
  onClose: () => void;
}) {
  const [name, setName] = useState(sshKey?.name || ""),
    [mode, setMode] = useState("generate"),
    [privateKey, setPrivateKey] = useState(""),
    [filename, setFilename] = useState(""),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await onSubmit(
        sshKey
          ? { name }
          : {
              name,
              generate: mode === "generate",
              private_key: mode === "upload" ? privateKey : "",
            },
      );
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form onSubmit={submit}>
      <label>
        密钥名称
        <input
          required
          maxLength={120}
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="例如：个人主密钥"
          autoFocus
        />
      </label>
      {!sshKey && (
        <>
          <div className="segmented">
            <button
              type="button"
              className={mode === "generate" ? "selected" : ""}
              onClick={() => setMode("generate")}
            >
              自动生成
            </button>
            <button
              type="button"
              className={mode === "upload" ? "selected" : ""}
              onClick={() => setMode("upload")}
            >
              上传私钥
            </button>
          </div>
          {mode === "generate" ? (
            <div className="info-box">
              <KeyRound size={20} />
              <div>
                <strong>Ed25519</strong>
                <p>生成后可复制公钥，手动添加到服务器的 authorized_keys。</p>
              </div>
            </div>
          ) : (
            <>
              <label className="upload-label">
                <ArrowDownToLine size={18} />
                {filename || "选择未加密的私钥文件"}
                <input
                  type="file"
                  onChange={async (e) => {
                    const file = e.target.files?.[0];
                    if (!file) return;
                    if (file.size > 65536) {
                      setError("私钥不能超过 64 KiB");
                      return;
                    }
                    try {
                      setPrivateKey(await file.text());
                      setFilename(file.name);
                      setError("");
                    } catch {
                      setError("文件读取失败");
                    }
                  }}
                />
              </label>
              <label>
                或粘贴私钥
                <textarea
                  className="mono"
                  rows={6}
                  required
                  value={privateKey}
                  onChange={(e) => setPrivateKey(e.target.value)}
                  placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                  spellCheck={false}
                />
              </label>
              <p className="muted tiny">
                不支持带口令的私钥。私钥仅在后端保存。
              </p>
            </>
          )}
        </>
      )}
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      <div className="modal-actions">
        <button type="button" className="button" onClick={onClose}>
          取消
        </button>
        <button className="button primary" disabled={busy}>
          {busy
            ? "正在保存…"
            : sshKey
              ? "保存名称"
              : mode === "generate"
                ? "生成密钥"
                : "导入密钥"}
        </button>
      </div>
    </form>
  );
}
function PasswordForm({
  host,
  onConnect,
}: {
  host: Host;
  onConnect: (password: string) => void;
}) {
  const [password, setPassword] = useState("");
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        onConnect(password);
        setPassword("");
      }}
    >
      <div className="info-box">
        <Server size={20} />
        <div>
          <strong>{host.name}</strong>
          <p>
            {host.username}@{host.address}:{host.port}
          </p>
        </div>
      </div>
      <label>
        SSH 登录密码
        <input
          type="password"
          required
          autoFocus
          autoComplete="off"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </label>
      <p className="muted tiny">仅用于本次连接，不会保存到配置文件。</p>
      <div className="modal-actions">
        <button className="button primary">
          连接主机
          <ArrowRight size={16} />
        </button>
      </div>
    </form>
  );
}
function SettingsForm({
  snapshot,
  onSaved,
  onSignedOut,
}: {
  snapshot: Snapshot;
  onSaved: (s: Snapshot) => void;
  onSignedOut: () => void;
}) {
  const [username, setUsername] = useState(snapshot.username),
    [password, setPassword] = useState(""),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  const revision = useRef(snapshot.revision);
  return (
    <form
      className="settings-card"
      onSubmit={async (e) => {
        e.preventDefault();
        setBusy(true);
        setError("");
        try {
          const result = await api(
            "/credentials",
            "PUT",
            { username, password },
            revision.current,
          );
          onSaved(result);
          onSignedOut();
        } catch (err) {
          setError(message(err));
        } finally {
          setBusy(false);
        }
      }}
    >
      <h3>登录凭据</h3>
      <p className="muted">修改后需要重新登录，现有登录会话将失效。</p>
      <label>
        用户名
        <input
          required
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          autoComplete="username"
        />
      </label>
      <label>
        新密码
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder="留空保留当前密码"
          autoComplete="new-password"
        />
      </label>
      <p className="muted tiny">
        按当前配置约定，Web 密码与 SSH 私钥以明文保存在 YAML 文件中。
      </p>
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      <button className="button primary" disabled={busy}>
        {busy ? "正在保存…" : "保存并重新登录"}
      </button>
    </form>
  );
}

function Workspace({
  initial,
  onSignedOut,
}: {
  initial: Snapshot;
  onSignedOut: () => void;
}) {
  const [snapshot, setSnapshot] = useState(initial),
    [page, setPage] = useState<Page>("hosts"),
    [search, setSearch] = useState(""),
    [group, setGroup] = useState("全部主机"),
    [modal, setModal] = useState<ModalState | null>(null),
    [toast, setToast] = useState(""),
    [tabs, setTabs] = useState<Tab[]>([]),
    [active, setActive] = useState(""),
    [copied, setCopied] = useState("");
  const [offline, setOffline] = useState(false);
  const retrySync = useRef<() => void>(() => {});
  const [terminalTools, setTerminalTools] = useState<HTMLDivElement | null>(
    null,
  );
  const toastTimer = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined,
  );
  const snapshotRef = useRef(snapshot);
  snapshotRef.current = snapshot;
  function notify(text: string) {
    setToast(text);
    clearTimeout(toastTimer.current);
    toastTimer.current = setTimeout(() => setToast(""), 5000);
  }
  function handleError(err: unknown) {
    if (err instanceof APIError && err.status === 401) onSignedOut();
    else notify(message(err));
  }
  useEffect(() => {
    const sync = startConfigSync({
      load: (signal) => api("/config", "GET", undefined, undefined, signal),
      onData: (s) =>
        setSnapshot((previous) =>
          previous.revision === s.revision &&
          previous.load_error === s.load_error &&
          previous.version === s.version
            ? previous
            : s,
        ),
      onOffline: () => setOffline(true),
      onOnline: () => setOffline(false),
      onUnauthorized: onSignedOut,
      isUnauthorized: (err) => err instanceof APIError && err.status === 401,
    });
    retrySync.current = () => void sync.retry();
    const resume = () => {
      if (!document.hidden) void sync.retry();
    };
    window.addEventListener("online", resume);
    document.addEventListener("visibilitychange", resume);
    return () => {
      sync.stop();
      window.removeEventListener("online", resume);
      document.removeEventListener("visibilitychange", resume);
      clearTimeout(toastTimer.current);
    };
  }, [onSignedOut]);
  const groups = [
    ...new Set(snapshot.hosts.map((h) => h.group).filter(Boolean)),
  ] as string[];
  const filteredHosts = snapshot.hosts.filter(
    (h) =>
      (group === "全部主机" || h.group === group) &&
      `${h.name} ${h.address} ${h.username} ${h.group || ""} ${h.remark || ""}`
        .toLowerCase()
        .includes(search.toLowerCase()),
  );
  const filteredKeys = snapshot.keys.filter((k) =>
    `${k.name} ${k.fingerprint}`.toLowerCase().includes(search.toLowerCase()),
  );
  function navigate(next: Page) {
    setPage(next);
    setActive("");
    setSearch("");
  }
  function openConnection(
    host: Host,
    password = "",
    revision = snapshot.revision,
  ) {
    const tab = { id: tabID(), host, password, revision };
    setTabs((prev) => [...prev, tab]);
    setActive(tab.id);
    setModal(null);
  }
  function connect(host: Host) {
    if (host.auth.mode === "password_prompt")
      setModal({ type: "connect", host, revision: snapshot.revision });
    else openConnection(host);
  }
  function closeTab(id: string) {
    setTabs((prev) => prev.filter((t) => t.id !== id));
    if (active === id) setActive("");
  }
  async function remove(
    type: "hosts" | "keys" | "proxies",
    id: string,
    name: string,
  ) {
    if (
      !confirm(
        `确定删除${type === "hosts" ? "主机" : type === "keys" ? "密钥" : "代理"}「${name}」？`,
      )
    )
      return;
    try {
      setSnapshot(
        await api(`/${type}/${id}`, "DELETE", undefined, snapshot.revision),
      );
      notify("已删除");
    } catch (err) {
      handleError(err);
    }
  }
  async function copyKey(key: SSHKey) {
    try {
      if (navigator.clipboard?.writeText)
        await navigator.clipboard.writeText(key.public_key);
      else {
        const input = document.createElement("textarea");
        input.value = key.public_key;
        document.body.append(input);
        input.select();
        const ok = document.execCommand("copy");
        input.remove();
        if (!ok)
          throw new Error("复制失败，请通过 HTTPS 或 localhost 访问后重试");
      }
      setCopied(key.id);
      setTimeout(() => setCopied(""), 2000);
      notify("公钥已复制");
    } catch (err) {
      handleError(err);
    }
  }
  const title =
    page === "hosts"
      ? "主机"
      : page === "keys"
        ? "SSH 密钥"
        : page === "proxies"
          ? "SOCKS5 代理"
          : "设置";
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <Logo small />
        <div className="workspace-label">
          <span className="workspace-avatar">W</span>
          <div>
            个人工作台<span>Personal workspace</span>
          </div>
          <span className="workspace-online" />
        </div>
        <span className="nav-label">工作空间</span>
        <nav>
          <button
            className={!active && page === "hosts" ? "active" : ""}
            onClick={() => navigate("hosts")}
          >
            <Server size={18} />
            主机<span className="nav-count">{snapshot.hosts.length}</span>
          </button>
          <button
            className={!active && page === "keys" ? "active" : ""}
            onClick={() => navigate("keys")}
          >
            <KeyRound size={18} />
            SSH 密钥<span className="nav-count">{snapshot.keys.length}</span>
          </button>
          <button
            className={!active && page === "proxies" ? "active" : ""}
            onClick={() => navigate("proxies")}
          >
            <Network size={18} />
            SOCKS5 代理
            <span className="nav-count">{snapshot.proxies.length}</span>
          </button>
        </nav>
        <div className="sidebar-bottom">
          <button
            className={!active && page === "settings" ? "active" : ""}
            onClick={() => navigate("settings")}
          >
            <Settings size={18} />
            设置
          </button>
          <div className="sidebar-note">
            <span className="status-dot live" />
            配置文件驱动<span>无需数据库，简单可控。</span>
          </div>
          <div className="user">
            <span className="user-avatar">
              {snapshot.username.slice(0, 1).toUpperCase()}
            </span>
            <div>
              {snapshot.username}
              <span>管理员</span>
            </div>
            <button
              className="icon-button"
              title="退出登录"
              aria-label="退出登录"
              onClick={async () => {
                try {
                  await api("/logout", "POST", {});
                  onSignedOut();
                } catch (err) {
                  handleError(err);
                }
              }}
            >
              <LogOut size={16} />
            </button>
          </div>
        </div>
      </aside>
      <main className="main">
        <header className="topbar">
          <div>
            工作空间
            <ChevronRight size={14} />
            <span>{active ? "终端" : title}</span>
          </div>
          <div className="topbar-meta">
            <button
              className={`connection-state ${offline ? "offline" : ""}`}
              onClick={() => retrySync.current()}
              title={
                offline ? "服务暂时不可用，点击重试" : "服务已连接，点击刷新"
              }
            >
              <span className={`status-dot ${offline ? "" : "live"}`} />
              {offline ? "服务不可用 · 重试" : "服务已连接"}
            </button>
            <code>{snapshot.version}</code>
          </div>
        </header>
        <div className="tabbar-row">
          <div className="tabbar">
            <button
              className={!active ? "selected" : ""}
              onClick={() => setActive("")}
            >
              <FolderOpen size={15} />
              资源管理
            </button>
            {tabs.map((tab) => (
              <div
                key={tab.id}
                className={`terminal-tab ${active === tab.id ? "selected" : ""}`}
              >
                <button onClick={() => setActive(tab.id)}>
                  <TerminalSquare size={14} />
                  {tab.host.name}
                </button>
                <button
                  className="tab-close"
                  aria-label={`关闭 ${tab.host.name}`}
                  onClick={() => closeTab(tab.id)}
                >
                  <X size={13} />
                </button>
              </div>
            ))}
          </div>
          <div className="terminal-tools" ref={setTerminalTools} />
        </div>
        {snapshot.load_error && (
          <div className="config-warning" role="alert">
            配置加载失败：{snapshot.load_error}
            。当前继续使用最近一次有效配置；请修正 YAML 文件。
          </div>
        )}
        <div className="content" hidden={!!active}>
          <div className="page-heading">
            <div>
              <span className="section-label">
                {page === "hosts"
                  ? "YOUR INFRASTRUCTURE"
                  : page === "keys"
                    ? "ACCESS CREDENTIALS"
                    : page === "proxies"
                      ? "NETWORK CONNECTIONS"
                      : "WORKSPACE PREFERENCES"}
              </span>
              <h1>
                {title}
                <span className="heading-count">
                  {page === "hosts"
                    ? snapshot.hosts.length
                    : page === "keys"
                      ? snapshot.keys.length
                      : page === "proxies"
                        ? snapshot.proxies.length
                        : ""}
                </span>
              </h1>
              <p>
                {page === "hosts"
                  ? "每一台主机，都在这里。选择一台，开始连接。"
                  : page === "keys"
                    ? "统一管理访问凭据，轻松连接你的 Linux 主机。"
                    : page === "proxies"
                      ? "为指定主机选择代理，连接不同网络中的服务器。"
                      : "管理工作台的登录凭据与配置。"}
              </p>
            </div>
            {page !== "settings" && (
              <button
                className="button primary"
                onClick={() =>
                  setModal(
                    page === "hosts"
                      ? { type: "host", revision: snapshot.revision }
                      : page === "proxies"
                        ? { type: "proxy", revision: snapshot.revision }
                        : { type: "key", revision: snapshot.revision },
                  )
                }
              >
                <Plus size={17} />
                {page === "hosts"
                  ? "添加主机"
                  : page === "proxies"
                    ? "添加代理"
                    : "添加密钥"}
              </button>
            )}
          </div>
          {page === "hosts" && (
            <>
              <div className="summary-row">
                <div>
                  <span className="summary-icon">
                    <Server size={20} />
                  </span>
                  <div>
                    <span>全部主机</span>
                    <strong>
                      {snapshot.hosts.length.toString().padStart(2, "0")}
                    </strong>
                  </div>
                  <span className="summary-detail">Linux / SSH</span>
                </div>
                <div>
                  <span className="summary-icon mint">
                    <KeyRound size={20} />
                  </span>
                  <div>
                    <span>密钥认证</span>
                    <strong>
                      {snapshot.hosts
                        .filter((h) => h.auth.mode === "key")
                        .length.toString()
                        .padStart(2, "0")}
                    </strong>
                  </div>
                  <span className="summary-detail">一键建立连接</span>
                </div>
                <div>
                  <span className="summary-icon sand">
                    <FolderOpen size={20} />
                  </span>
                  <div>
                    <span>主机分组</span>
                    <strong>{groups.length.toString().padStart(2, "0")}</strong>
                  </div>
                  <span className="summary-detail">井然有序</span>
                </div>
              </div>
              <div className="toolbar">
                <div className="filter-tabs">
                  {["全部主机", ...groups].map((g) => (
                    <button
                      key={g}
                      onClick={() => setGroup(g)}
                      className={group === g ? "selected" : ""}
                    >
                      {g}
                    </button>
                  ))}
                </div>
                <div className="search">
                  <Search size={16} />
                  <input
                    aria-label="搜索主机"
                    placeholder="搜索主机、地址…"
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                  />
                </div>
              </div>
              {filteredHosts.length ? (
                <div className="host-grid">
                  {filteredHosts.map((host) => (
                    <article className="host-card" key={host.id}>
                      <div className="card-top">
                        <span className="host-icon">
                          <Server size={23} strokeWidth={1.6} />
                        </span>
                        <span className="badge">LINUX</span>
                        <div className="card-actions">
                          <button
                            className="icon-button"
                            title="编辑主机"
                            aria-label={`编辑 ${host.name}`}
                            onClick={() =>
                              setModal({
                                type: "host",
                                host,
                                revision: snapshot.revision,
                              })
                            }
                          >
                            <Edit3 size={15} />
                          </button>
                          <button
                            className="icon-button danger"
                            title="删除主机"
                            aria-label={`删除 ${host.name}`}
                            onClick={() => remove("hosts", host.id, host.name)}
                          >
                            <Trash2 size={15} />
                          </button>
                        </div>
                      </div>
                      <h3>{host.name}</h3>
                      <code className="host-address">
                        {host.address}
                        <span>:{host.port}</span>
                      </code>
                      <div className="host-meta">
                        <span>{host.group || "未分组"}</span>
                        <span>{host.username}</span>
                        {host.proxy_id && (
                          <span title="通过 SOCKS5 代理连接">
                            {snapshot.proxies.find(
                              (proxy) => proxy.id === host.proxy_id,
                            )?.name || "SOCKS5"}
                          </span>
                        )}
                      </div>
                      <p className="host-remark">
                        {host.remark || "准备就绪，随时连接。"}
                      </p>
                      <div className="card-bottom">
                        <span>
                          {host.auth.mode === "key" ? (
                            <KeyRound size={13} />
                          ) : (
                            <ShieldCheck size={13} />
                          )}
                          {host.auth.mode === "key"
                            ? snapshot.keys.find(
                                (k) => k.id === host.auth.key_id,
                              )?.name
                            : "连接时输入密码"}
                        </span>
                        <button onClick={() => connect(host)}>
                          连接
                          <ArrowRight size={15} />
                        </button>
                      </div>
                    </article>
                  ))}
                </div>
              ) : (
                <Empty
                  icon={<Server size={28} />}
                  title={
                    snapshot.hosts.length
                      ? "没有匹配的主机"
                      : "从第一台主机开始"
                  }
                  text={
                    snapshot.hosts.length
                      ? "试试其他关键词或分组。"
                      : "添加你的 Linux 主机，在浏览器中随时打开终端。"
                  }
                  action={
                    !snapshot.hosts.length
                      ? () =>
                          setModal({
                            type: "host",
                            revision: snapshot.revision,
                          })
                      : undefined
                  }
                  actionText="添加主机"
                />
              )}
              <div className="hint">
                <CircleHelp size={15} />
                <span>
                  首次连接时需要确认服务器指纹。主机密码仅用于当前连接。
                </span>
              </div>
            </>
          )}
          {page === "keys" && (
            <>
              <div className="toolbar key-toolbar">
                <span className="muted">
                  {snapshot.keys.length} 把密钥 ·{" "}
                  {snapshot.hosts.filter((h) => h.auth.mode === "key").length}{" "}
                  台主机关联
                </span>
                <div className="search">
                  <Search size={16} />
                  <input
                    aria-label="搜索密钥"
                    placeholder="搜索密钥或指纹…"
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                  />
                </div>
              </div>
              {filteredKeys.length ? (
                <div className="keys-list">
                  {filteredKeys.map((key) => (
                    <article className="key-row" key={key.id}>
                      <span className="key-icon">
                        <FileKey2 size={24} />
                      </span>
                      <div className="key-details">
                        <h3>
                          {key.name}
                          <span className="badge">
                            {key.algorithm.replace("ssh-", "").toUpperCase()}
                          </span>
                        </h3>
                        <code>{key.fingerprint}</code>
                        <span>
                          {
                            snapshot.hosts.filter(
                              (h) => h.auth.key_id === key.id,
                            ).length
                          }{" "}
                          台主机关联
                        </span>
                      </div>
                      <div className="key-actions">
                        <button className="button" onClick={() => copyKey(key)}>
                          {copied === key.id ? (
                            <Check size={15} />
                          ) : (
                            <Copy size={15} />
                          )}
                          {copied === key.id ? "已复制" : "复制公钥"}
                        </button>
                        <button
                          className="icon-button"
                          aria-label={`重命名 ${key.name}`}
                          title="重命名密钥"
                          onClick={() =>
                            setModal({
                              type: "key",
                              key,
                              revision: snapshot.revision,
                            })
                          }
                        >
                          <Edit3 size={16} />
                        </button>
                        <button
                          className="icon-button danger"
                          aria-label={`删除 ${key.name}`}
                          title="删除密钥"
                          onClick={() => remove("keys", key.id, key.name)}
                        >
                          <Trash2 size={16} />
                        </button>
                      </div>
                    </article>
                  ))}
                </div>
              ) : (
                <Empty
                  icon={<KeyRound size={28} />}
                  title={
                    snapshot.keys.length
                      ? "没有匹配的密钥"
                      : "为主机准备一把密钥"
                  }
                  text="自动生成 Ed25519 密钥，或上传已有的未加密私钥。"
                  action={
                    !snapshot.keys.length
                      ? () =>
                          setModal({ type: "key", revision: snapshot.revision })
                      : undefined
                  }
                  actionText="添加密钥"
                />
              )}
              <div className="hint">
                <CircleHelp size={15} />
                <span>
                  复制公钥后，将其手动添加到目标服务器的
                  ~/.ssh/authorized_keys。
                </span>
              </div>
            </>
          )}
          {page === "settings" && (
            <div className="settings-layout">
              <SettingsForm
                key={snapshot.username}
                snapshot={snapshot}
                onSaved={setSnapshot}
                onSignedOut={onSignedOut}
              />
              <div className="settings-about">
                <FolderOpen size={24} />
                <h3>一份配置，所有连接</h3>
                <p>
                  主机、密钥、代理与登录凭据统一保存在 YAML 文件。支持通过 Web
                  或手动编辑维护。
                </p>
                <dl>
                  <dt>配置格式</dt>
                  <dd>YAML · v1</dd>
                  <dt>更新方式</dt>
                  <dd>自动重载</dd>
                  <dt>应用版本</dt>
                  <dd>{snapshot.version}</dd>
                </dl>
                <p className="tiny">
                  Web
                  保存会统一格式，移除原有注释。手动编辑有误时会保留最近一次有效配置。
                </p>
              </div>
            </div>
          )}
          {page === "proxies" && (
            <ProxyList
              proxies={snapshot.proxies}
              hosts={snapshot.hosts}
              search={search}
              onSearch={setSearch}
              onEdit={(proxy) =>
                setModal({ type: "proxy", proxy, revision: snapshot.revision })
              }
              onDelete={(proxy) => void remove("proxies", proxy.id, proxy.name)}
            />
          )}
          <footer className="page-footer">
            <span>
              WOTM <span> / </span> Your own terminal workspace.
            </span>
            <span>保持简单，保持连接。</span>
          </footer>
        </div>
        {tabs.map((tab) => (
          <TerminalPane
            key={tab.id}
            host={tab.host}
            password={tab.password}
            revision={tab.revision}
            active={active === tab.id}
            toolsTarget={terminalTools}
            onSaved={(s) => {
              setSnapshot(s);
              setTabs((prev) =>
                prev.map((t) =>
                  t.id === tab.id ? { ...t, revision: s.revision } : t,
                ),
              );
            }}
            onPasswordUsed={() =>
              setTabs((prev) =>
                prev.map((t) =>
                  t.id === tab.id && t.password ? { ...t, password: "" } : t,
                ),
              )
            }
            onReconnect={() => {
              closeTab(tab.id);
              const h = snapshotRef.current.hosts.find(
                (h) => h.id === tab.host.id,
              );
              if (h) connect(h);
              else notify("主机已被删除");
            }}
          />
        ))}
      </main>
      {toast && (
        <div className="toast" role="status">
          <span>{toast}</span>
          <button
            className="icon-button"
            aria-label="关闭通知"
            onClick={() => setToast("")}
          >
            <X size={15} />
          </button>
        </div>
      )}
      {modal && (
        <Modal
          title={
            modal.type === "host"
              ? modal.host
                ? "编辑主机"
                : "添加主机"
              : modal.type === "key"
                ? modal.key
                  ? "重命名密钥"
                  : "添加 SSH 密钥"
                : modal.type === "proxy"
                  ? modal.proxy
                    ? "编辑 SOCKS5 代理"
                    : "添加 SOCKS5 代理"
                  : "连接主机"
          }
          subtitle={
            modal.type === "host"
              ? "填写 Linux 主机的 SSH 连接信息。"
              : modal.type === "key"
                ? "管理用于主机认证的 SSH 密钥。"
                : modal.type === "proxy"
                  ? "填写代理地址和可选的认证信息。"
                  : "输入本次连接使用的 SSH 密码。"
          }
          onClose={() => setModal(null)}
        >
          {modal.type === "host" ? (
            <HostForm
              host={modal.host}
              keys={snapshot.keys}
              proxies={snapshot.proxies}
              onClose={() => setModal(null)}
              onSubmit={async (host) => {
                const result = await api(
                  `/hosts${modal.host ? `/${modal.host.id}` : ""}`,
                  modal.host ? "PUT" : "POST",
                  host,
                  modal.revision,
                );
                setSnapshot(result);
                setModal(null);
                notify("主机已保存");
              }}
            />
          ) : modal.type === "key" ? (
            <KeyForm
              sshKey={modal.key}
              onClose={() => setModal(null)}
              onSubmit={async (data) => {
                const result = await api(
                  `/keys${modal.key ? `/${modal.key.id}` : ""}`,
                  modal.key ? "PUT" : "POST",
                  data,
                  modal.revision,
                );
                setSnapshot(result);
                setModal(null);
                notify(modal.key ? "名称已保存" : "密钥已添加，可以复制公钥了");
              }}
            />
          ) : modal.type === "proxy" ? (
            <ProxyForm
              proxy={modal.proxy}
              onClose={() => setModal(null)}
              onSubmit={async (value) => {
                const result = await api(
                  `/proxies${modal.proxy ? `/${modal.proxy.id}` : ""}`,
                  modal.proxy ? "PUT" : "POST",
                  value,
                  modal.revision,
                );
                setSnapshot(result);
                setModal(null);
                notify("代理已保存");
              }}
            />
          ) : (
            <PasswordForm
              host={modal.host}
              onConnect={(password) =>
                openConnection(modal.host, password, modal.revision)
              }
            />
          )}
        </Modal>
      )}
    </div>
  );
}
function Empty({
  icon,
  title,
  text,
  action,
  actionText,
}: {
  icon: ReactNode;
  title: string;
  text: string;
  action?: () => void;
  actionText: string;
}) {
  return (
    <div className="empty">
      <span className="empty-icon">{icon}</span>
      <h3>{title}</h3>
      <p>{text}</p>
      {action && (
        <button className="button" onClick={action}>
          <Plus size={15} />
          {actionText}
        </button>
      )}
    </div>
  );
}
function App() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null),
    [loading, setLoading] = useState(true),
    [error, setError] = useState("");
  const signedOut = useRef(() => setSnapshot(null)).current;
  async function load() {
    setLoading(true);
    setError("");
    try {
      setSnapshot(await api("/config"));
    } catch (err) {
      if (!(err instanceof APIError && err.status === 401))
        setError(message(err));
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    void load();
  }, []);
  if (loading)
    return (
      <div className="boot">
        <Logo />
        <p>正在打开工作台…</p>
      </div>
    );
  if (error)
    return (
      <div className="boot">
        <Logo />
        <p className="form-error">{error}</p>
        <button className="button" onClick={load}>
          重试
        </button>
      </div>
    );
  return snapshot ? (
    <Workspace initial={snapshot} onSignedOut={signedOut} />
  ) : (
    <Login onLogin={setSnapshot} />
  );
}
createRoot(document.getElementById("root")!).render(<App />);
