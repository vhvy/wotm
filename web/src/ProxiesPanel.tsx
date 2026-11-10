import { useState } from "react";
import type { FormEvent } from "react";
import { Edit3, Network, Search, Trash2 } from "lucide-react";
import { message } from "./types";
import type { Host, SOCKSProxy } from "./types";

type ProxyInput = {
  name: string;
  address: string;
  port: number;
  username: string;
  password?: string;
};
export function ProxyForm({
  proxy,
  onSubmit,
  onClose,
}: {
  proxy?: SOCKSProxy;
  onSubmit: (value: ProxyInput) => Promise<void>;
  onClose: () => void;
}) {
  const [name, setName] = useState(proxy?.name || "");
  const [address, setAddress] = useState(proxy?.address || "");
  const [port, setPort] = useState(proxy?.port || 1080);
  const [auth, setAuth] = useState(!!proxy?.username);
  const [username, setUsername] = useState(proxy?.username || "");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await onSubmit({
        name,
        address,
        port,
        username: auth ? username : "",
        ...(!auth
          ? { password: "" }
          : password || !proxy?.has_password
            ? { password }
            : {}),
      });
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form onSubmit={submit}>
      <label>
        代理名称
        <input
          required
          maxLength={120}
          value={name}
          onChange={(event) => setName(event.target.value)}
          autoFocus
          placeholder="例如：办公网络"
        />
      </label>
      <div className="form-grid">
        <label>
          代理地址
          <input
            required
            value={address}
            onChange={(event) => setAddress(event.target.value)}
            placeholder="IP 地址或域名，不含 socks5://"
          />
        </label>
        <label>
          端口
          <input
            type="number"
            required
            min={1}
            max={65535}
            value={port}
            onChange={(event) => setPort(Number(event.target.value))}
          />
        </label>
      </div>
      <label className="check">
        <input
          type="checkbox"
          checked={auth}
          onChange={(event) => setAuth(event.target.checked)}
        />
        代理需要用户名和密码认证
      </label>
      {auth && (
        <>
          <label>
            代理用户名
            <input
              required
              maxLength={255}
              value={username}
              onChange={(event) => setUsername(event.target.value)}
              autoComplete="off"
            />
          </label>
          <label>
            代理密码
            <input
              type="password"
              required={!proxy?.has_password}
              maxLength={255}
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              autoComplete="new-password"
              placeholder={
                proxy?.has_password ? "留空保留已保存的密码" : "输入代理密码"
              }
            />
          </label>
        </>
      )}
      <p className="muted tiny">
        仅支持 SOCKS5。配置保存后，可在主机中选择此代理。
      </p>
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
          {busy ? "正在保存…" : "保存代理"}
        </button>
      </div>
    </form>
  );
}

export function ProxyList({
  proxies,
  hosts,
  search,
  onSearch,
  onEdit,
  onDelete,
}: {
  proxies: SOCKSProxy[];
  hosts: Host[];
  search: string;
  onSearch: (value: string) => void;
  onEdit: (proxy: SOCKSProxy) => void;
  onDelete: (proxy: SOCKSProxy) => void;
}) {
  const filtered = proxies.filter((proxy) =>
    `${proxy.name} ${proxy.address} ${proxy.username || ""}`
      .toLowerCase()
      .includes(search.toLowerCase()),
  );
  return (
    <>
      <div className="toolbar key-toolbar">
        <span className="muted">{proxies.length} 个 SOCKS5 代理</span>
        <div className="search">
          <Search size={16} />
          <input
            aria-label="搜索代理"
            placeholder="搜索代理名称、地址…"
            value={search}
            onChange={(event) => onSearch(event.target.value)}
          />
        </div>
      </div>
      {filtered.length ? (
        <div className="keys-list">
          {filtered.map((proxy) => (
            <article className="key-row" key={proxy.id}>
              <span className="key-icon">
                <Network size={24} />
              </span>
              <div className="key-details">
                <h3>
                  {proxy.name}
                  <span className="badge">SOCKS5</span>
                </h3>
                <code>
                  {proxy.address.includes(":")
                    ? `[${proxy.address}]`
                    : proxy.address}
                  :{proxy.port}
                </code>
                <span>
                  {hosts.filter((host) => host.proxy_id === proxy.id).length}{" "}
                  台主机关联 · {proxy.username ? "用户名认证" : "无需认证"}
                </span>
              </div>
              <div className="key-actions">
                <button
                  className="icon-button"
                  title="编辑代理"
                  aria-label={`编辑 ${proxy.name}`}
                  onClick={() => onEdit(proxy)}
                >
                  <Edit3 size={16} />
                </button>
                <button
                  className="icon-button danger"
                  title="删除代理"
                  aria-label={`删除 ${proxy.name}`}
                  onClick={() => onDelete(proxy)}
                >
                  <Trash2 size={16} />
                </button>
              </div>
            </article>
          ))}
        </div>
      ) : (
        <div className="empty">
          <span className="empty-icon">
            <Network size={28} />
          </span>
          <h3>{proxies.length ? "没有匹配的代理" : "添加你的第一个代理"}</h3>
          <p>添加 SOCKS5 代理后，可让指定主机通过代理连接。</p>
        </div>
      )}
      <div className="hint">
        <Network size={15} />
        <span>
          代理需能被 WOTM 后端访问。指定代理后，终端和文件上传共用该连接。
        </span>
      </div>
    </>
  );
}
