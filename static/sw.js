// Aesthetic Lab Kassa — Service Worker (system-design §7)
// Caches versioned static assets and serves an offline fallback page when offline.
// HTML and dynamic data always come from the network.

const CACHE_NAME = 'kassa-v2';
const OFFLINE_URL = '/offline';

const PRECACHE_ASSETS = [
	OFFLINE_URL,
	'/static/app.css',
	'/static/app.css?v=2',
	'/static/datastar.js',
	'/static/draft.js',
	'/static/icons/favicon.svg',
	'/static/icons/apple-touch-icon.png',
	'/static/fonts/onest-cyrillic-wght-normal.woff2',
	'/static/fonts/onest-latin-wght-normal.woff2',
	'/manifest.webmanifest',
];

self.addEventListener('install', (event) => {
	event.waitUntil(
		caches
			.open(CACHE_NAME)
			.then((cache) => cache.addAll(PRECACHE_ASSETS))
			.then(() => self.skipWaiting()),
	);
});

self.addEventListener('activate', (event) => {
	event.waitUntil(
		caches
			.keys()
			.then((keys) =>
				Promise.all(
					keys.map((key) => {
						if (key !== CACHE_NAME) {
							return caches.delete(key);
						}
					}),
				),
			)
			.then(() => self.clients.claim()),
	);
});

self.addEventListener('fetch', (event) => {
	// Only handle GET requests.
	if (event.request.method !== 'GET') {
		return;
	}

	const url = new URL(event.request.url);

	// Navigation requests: HTML and data always come from the network.
	// When offline or network fails, fall back to the cached offline page.
	if (event.request.mode === 'navigate') {
		event.respondWith(
			fetch(event.request).catch(() => caches.match(OFFLINE_URL)),
		);
		return;
	}

	// Static assets: cache-first with network fallback.
	if (url.pathname.startsWith('/static/') || url.pathname === '/manifest.webmanifest') {
		event.respondWith(
			caches.match(event.request).then((cached) => {
				if (cached) {
					return cached;
				}
				return fetch(event.request).then((response) => {
					if (response.ok) {
						const copy = response.clone();
						caches.open(CACHE_NAME).then((cache) => cache.put(event.request, copy));
					}
					return response;
				});
			}),
		);
		return;
	}
});
