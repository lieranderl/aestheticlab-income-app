import { test, expect } from "bun:test";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";

function harness() {
	const listeners = new Map();
	const cacheStore = new Map();

	class MockCache {
		constructor(name) {
			this.name = name;
			if (!cacheStore.has(name)) {
				cacheStore.set(name, new Map());
			}
			this.items = cacheStore.get(name);
		}
		async addAll(urls) {
			for (const u of urls) {
				this.items.set(typeof u === "string" ? u : u.url, new Response("ok"));
			}
		}
		async match(req) {
			const key = typeof req === "string" ? req : req.url;
			return this.items.get(key) || null;
		}
		async put(req, res) {
			const key = typeof req === "string" ? req : req.url;
			this.items.set(key, res);
		}
		async keys() {
			return Array.from(this.items.keys()).map((url) => ({ url }));
		}
		async delete(req) {
			return this.items.delete(typeof req === "string" ? req : req.url);
		}
	}

	const caches = {
		async open(name) {
			return new MockCache(name);
		},
		async keys() {
			return Array.from(cacheStore.keys());
		},
		async delete(name) {
			return cacheStore.delete(name);
		},
		async match(req, opts = {}) {
			const key = typeof req === "string" ? req : req.url;
			const bare = (u) => u.split("?")[0];
			for (const store of cacheStore.values()) {
				if (store.has(key)) return store.get(key);
				if (opts.ignoreSearch) {
					for (const [k, v] of store) {
						if (bare(k) === bare(key)) return v;
					}
				}
			}
			return null;
		},
	};

	let skippedWaiting = false;
	let claimedClients = false;

	const scope = {
		caches,
		CACHE_NAME: undefined,
		OFFLINE_URL: undefined,
		self: {
			addEventListener(evt, fn) {
				listeners.set(evt, fn);
			},
			skipWaiting: async () => {
				skippedWaiting = true;
			},
			clients: {
				claim: async () => {
					claimedClients = true;
				},
			},
		},
		fetch: async () => {
			throw new Error("network error");
		},
		URL,
		Request,
		Response,
	};

	runInNewContext(readFileSync(new URL("./sw.js", import.meta.url), "utf8"), scope);

	return {
		listeners,
		cacheStore,
		caches,
		scope,
		getSkippedWaiting: () => skippedWaiting,
		getClaimedClients: () => claimedClients,
	};
}

test("install precaches static assets and offline page", async () => {
	const h = harness();
	const installFn = h.listeners.get("install");
	expect(installFn).toBeDefined();

	let waitedPromise;
	const evt = {
		waitUntil(p) {
			waitedPromise = p;
		},
	};
	installFn(evt);
	await waitedPromise;

	expect(h.getSkippedWaiting()).toBe(true);
	expect(h.cacheStore.has("kassa-v5")).toBe(true);
	const staticCache = h.cacheStore.get("kassa-v5");
	expect(staticCache.has("/offline")).toBe(true);
	expect(staticCache.has("/static/app.css")).toBe(true);
	expect([...staticCache.keys()].some((k) => k.startsWith("/static/app.css?v="))).toBe(false);
	expect(staticCache.has("/static/datastar.js")).toBe(true);
	expect(staticCache.has("/static/draft.js?v=3")).toBe(true);
	expect(staticCache.has("/manifest.webmanifest")).toBe(true);
});

test("activate removes outdated caches and claims clients", async () => {
	const h = harness();
	h.cacheStore.set("old-cache-v0", new Map());
	h.cacheStore.set("kassa-v1", new Map());
	h.cacheStore.set("kassa-v4", new Map());
	h.cacheStore.set("kassa-v5", new Map());

	const activateFn = h.listeners.get("activate");
	expect(activateFn).toBeDefined();

	let waitedPromise;
	const evt = {
		waitUntil(p) {
			waitedPromise = p;
		},
	};
	activateFn(evt);
	await waitedPromise;

	expect(h.getClaimedClients()).toBe(true);
	expect(h.cacheStore.has("old-cache-v0")).toBe(false);
	expect(h.cacheStore.has("kassa-v1")).toBe(false);
	expect(h.cacheStore.has("kassa-v4")).toBe(false); // the release before the hashed stylesheet
	expect(h.cacheStore.has("kassa-v5")).toBe(true);
});

test("navigation request returns network response when online", async () => {
	const h = harness();
	const fetchFn = h.listeners.get("fetch");

	const onlineResponse = new Response("<html>online</html>", { status: 200 });
	h.scope.fetch = async () => onlineResponse;

	let respondedPromise;
	const evt = {
		request: {
			method: "GET",
			mode: "navigate",
			url: "https://kassa.example.com/my",
		},
		respondWith(p) {
			respondedPromise = p;
		},
	};
	fetchFn(evt);
	const res = await respondedPromise;
	expect(res).toBe(onlineResponse);
});

test("navigation request falls back to /offline when offline", async () => {
	const h = harness();
	const offlineResponse = new Response("<html>offline</html>", { status: 200 });
	const cache = await h.caches.open("kassa-v5");
	await cache.put("/offline", offlineResponse);

	const fetchFn = h.listeners.get("fetch");
	h.scope.fetch = async () => {
		throw new Error("Failed to fetch (offline)");
	};

	let respondedPromise;
	const evt = {
		request: {
			method: "GET",
			mode: "navigate",
			url: "https://kassa.example.com/my",
		},
		respondWith(p) {
			respondedPromise = p;
		},
	};
	fetchFn(evt);
	const res = await respondedPromise;
	expect(res).toBe(offlineResponse);
});

test("static assets are served cache-first and cached on network hit", async () => {
	const h = harness();
	const cachedCSS = new Response("css-content", { status: 200 });
	const cache = await h.caches.open("kassa-v5");
	await cache.put("https://kassa.example.com/static/app.css", cachedCSS);

	const fetchFn = h.listeners.get("fetch");
	let respondedPromise;
	const evtCached = {
		request: {
			method: "GET",
			mode: "cors",
			url: "https://kassa.example.com/static/app.css",
		},
		respondWith(p) {
			respondedPromise = p;
		},
	};
	fetchFn(evtCached);
	const res = await respondedPromise;
	expect(res).toBe(cachedCSS);

	// Not in cache yet: fetch from network and cache
	const networkFont = new Response("font-content", { status: 200 });
	h.scope.fetch = async () => networkFont;
	const evtUncached = {
		request: {
			method: "GET",
			mode: "cors",
			url: "https://kassa.example.com/static/fonts/test.woff2",
		},
		respondWith(p) {
			respondedPromise = p;
		},
	};
	fetchFn(evtUncached);
	const res2 = await respondedPromise;
	expect(res2).toBe(networkFont);
	const stored = await cache.match("https://kassa.example.com/static/fonts/test.woff2");
	expect(stored).toBeDefined();
});

test("non-GET requests are not intercepted", () => {
	const h = harness();
	const fetchFn = h.listeners.get("fetch");
	let called = false;
	const evt = {
		request: {
			method: "POST",
			mode: "cors",
			url: "https://kassa.example.com/clients",
		},
		respondWith() {
			called = true;
		},
	};
	fetchFn(evt);
	expect(called).toBe(false);
});

test("offline, a not-yet-cached stylesheet version falls back to the cached copy", async () => {
	const h = harness();
	const precached = new Response("old-css", { status: 200 });
	const cache = await h.caches.open("kassa-v5");
	await cache.put("https://kassa.example.com/static/app.css", precached);
	h.scope.fetch = async () => {
		throw new Error("Failed to fetch (offline)");
	};
	let respondedPromise;
	h.listeners.get("fetch")({
		request: { method: "GET", mode: "no-cors", url: "https://kassa.example.com/static/app.css?v=newhash" },
		respondWith(p) {
			respondedPromise = p;
		},
	});
	expect(await respondedPromise).toBe(precached);
});

test("a new stylesheet version replaces the older versions in the cache", async () => {
	const h = harness();
	const cache = await h.caches.open("kassa-v5");
	await cache.put("https://kassa.example.com/static/app.css?v=old", new Response("old"));
	await cache.put("https://kassa.example.com/static/app.css", new Response("bare"));
	h.scope.fetch = async () => new Response("new", { status: 200 });
	let respondedPromise;
	h.listeners.get("fetch")({
		request: { method: "GET", mode: "no-cors", url: "https://kassa.example.com/static/app.css?v=new" },
		respondWith(p) {
			respondedPromise = p;
		},
	});
	await respondedPromise;
	await new Promise((r) => setTimeout(r, 10));
	const keys = Array.from(h.cacheStore.get("kassa-v5").keys());
	expect(keys).toContain("https://kassa.example.com/static/app.css?v=new");
	expect(keys).not.toContain("https://kassa.example.com/static/app.css?v=old");
	expect(keys).toContain("https://kassa.example.com/static/app.css"); // the offline fallback stays
});
