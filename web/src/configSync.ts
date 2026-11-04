type Options<T> = {
  load: (signal: AbortSignal) => Promise<T>;
  onData: (data: T) => void;
  onOffline: () => void;
  onOnline: () => void;
  onUnauthorized: () => void;
  isUnauthorized: (error: unknown) => boolean;
  schedule?: (
    callback: () => void,
    delay: number,
  ) => ReturnType<typeof setTimeout>;
  cancel?: (timer: ReturnType<typeof setTimeout>) => void;
};

// One request at a time. Failed refreshes never create transient notifications.
export function startConfigSync<T>(options: Options<T>) {
  const schedule = options.schedule ?? setTimeout;
  const cancel = options.cancel ?? clearTimeout;
  let stopped = false;
  let failures = 0;
  let controller: AbortController | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const queue = (delay: number) => {
    if (!stopped) timer = schedule(() => void retry(), delay);
  };
  async function retry() {
    if (stopped || controller) return;
    if (timer !== undefined) cancel(timer);
    controller = new AbortController();
    const timeout = setTimeout(() => controller?.abort(), 10_000);
    try {
      const data = await options.load(controller.signal);
      if (stopped) return;
      failures = 0;
      options.onData(data);
      options.onOnline();
    } catch (error) {
      if (stopped) return;
      if (options.isUnauthorized(error)) {
        stopped = true;
        options.onUnauthorized();
        return;
      }
      failures++;
      options.onOffline();
    } finally {
      clearTimeout(timeout);
      controller = undefined;
      queue(Math.min(5_000 * 2 ** Math.min(failures, 3), 30_000));
    }
  }
  queue(5_000);
  return {
    retry,
    stop() {
      stopped = true;
      if (timer !== undefined) cancel(timer);
      controller?.abort();
    },
  };
}
