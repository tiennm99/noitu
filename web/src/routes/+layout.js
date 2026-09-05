// A pure single-page app. Every screen depends on a live socket, so there is
// nothing meaningful to render on a server or to freeze at build time; the
// static adapter emits one index.html and the Go binary falls back to it.
export const ssr = false;
export const prerender = false;
