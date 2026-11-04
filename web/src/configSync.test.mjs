import test from "node:test";
import assert from "node:assert/strict";
import { startConfigSync } from "./configSync.ts";

function fixture(load) {
  const delays = [],
    states = [],
    data = [];
  let signedOut = false;
  const poller = startConfigSync({
    load,
    schedule: (_, delay) => {
      delays.push(delay);
      return 1;
    },
    cancel: () => {},
    onData: (value) => data.push(value),
    onOnline: () => states.push("online"),
    onOffline: () => states.push("offline"),
    onUnauthorized: () => {
      signedOut = true;
    },
    isUnauthorized: (error) => error === 401,
  });
  return { poller, delays, states, data, signedOut: () => signedOut };
}
test("outages back off without overlapping requests and recover to normal polling", async () => {
  let online = false;
  const f = fixture(async () => {
    if (!online) throw new Error("offline");
    return { revision: "new" };
  });
  for (let i = 0; i < 4; i++) await f.poller.retry();
  assert.deepEqual(f.delays, [5000, 10000, 20000, 30000, 30000]);
  assert.equal(f.data.length, 0);
  online = true;
  await f.poller.retry();
  assert.equal(f.delays.at(-1), 5000);
  assert.deepEqual(f.data, [{ revision: "new" }]);
  assert.equal(f.states.at(-1), "online");
  f.poller.stop();
});
test("manual retry cannot overlap an in-flight refresh and stop ignores its response", async () => {
  let finish,
    calls = 0;
  const f = fixture(() => {
    calls++;
    return new Promise((resolve) => {
      finish = resolve;
    });
  });
  const pending = f.poller.retry();
  await f.poller.retry();
  assert.equal(calls, 1);
  f.poller.stop();
  finish({});
  await pending;
  assert.deepEqual(f.states, []);
  assert.deepEqual(f.data, []);
});
test("expired login stops background polling", async () => {
  const f = fixture(async () => {
    throw 401;
  });
  await f.poller.retry();
  await f.poller.retry();
  assert.equal(f.signedOut(), true);
  assert.deepEqual(f.delays, [5000]);
});
