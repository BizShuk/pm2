package web

import (
	_ "embed"
	"net/http"
)

// indexHTML is the whole dashboard: one file, no build step, no npm, and
// no CDN. A CDN <script> would break the page on an offline host and
// would tell a third party every time someone opened their own process
// dashboard.
//
//go:embed ui/index.html
var indexHTML []byte

// contentSecurityPolicy keeps the page to its own origin. connect-src
// 'self' is the load-bearing one: it means nothing the page fetches can
// leave this server.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"connect-src 'self'; " +
	"base-uri 'none'; " +
	"form-action 'none'"

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(indexHTML)
}
