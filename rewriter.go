package main

import (
	"bytes"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

func rewriteResponseHeaders(resp *http.Response, t *target, baseURL string) {
	// Some customized Emby backends return absolute upstream URLs in headers and
	// response bodies. Those paths are hard-coded by the backend and cannot be
	// fixed there, so the proxy must rewrite them back to proxy URLs here.
	if loc := resp.Header.Get("Location"); loc != "" {
		resp.Header.Set("Location", rewriteHeaderURL(loc, t, baseURL))
	}
	if cl := resp.Header.Get("Content-Location"); cl != "" {
		resp.Header.Set("Content-Location", rewriteHeaderURL(cl, t, baseURL))
	}
}

func rewriteHeaderURL(rawURL string, t *target, baseURL string) string {
	if strings.HasPrefix(rawURL, "https://") || strings.HasPrefix(rawURL, "http://") {
		return rewriteSingleURL(rawURL, baseURL)
	}
	if strings.HasPrefix(rawURL, "//") {
		return rawURL
	}
	if strings.HasPrefix(rawURL, "/") {
		return buildProxyURL(baseURL, t, rawURL)
	}
	return rawURL
}

func buildProxyURL(baseURL string, t *target, path string) string {
	var b strings.Builder
	b.Grow(len(baseURL) + 1 + len(t.Scheme) + 1 + len(t.Domain) + 1 + 5 + len(path))
	b.WriteString(baseURL)
	b.WriteByte('/')
	b.WriteString(t.Scheme)
	b.WriteByte('/')
	b.WriteString(t.Domain)
	b.WriteByte('/')
	b.WriteString(strconv.Itoa(t.Port))
	if path == "" || path[0] != '/' {
		b.WriteByte('/')
	}
	b.WriteString(path)
	return b.String()
}

var rewritableTypes = []string{
	"application/json",
	"text/html",
	"text/xml",
	"text/plain",
	"application/xml",
	"application/xhtml",
	"text/javascript",
	"application/javascript",
}

func shouldRewriteBody(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
	}
	ct := strings.ToLower(strings.TrimSpace(mediaType))
	for _, t := range rewritableTypes {
		if ct == t {
			return true
		}
	}
	return false
}

func shouldRewriteEmbyResponse(t *target, contentType string) bool {
	if !shouldRewriteBody(contentType) {
		return false
	}
	path := strings.ToLower(targetRequestPath(t))
	return path == "/" ||
		strings.HasPrefix(path, "/emby/") ||
		strings.HasPrefix(path, "/items") ||
		strings.HasPrefix(path, "/users") ||
		strings.HasPrefix(path, "/sessions") ||
		strings.HasPrefix(path, "/system") ||
		strings.HasPrefix(path, "/shows") ||
		strings.HasPrefix(path, "/movies") ||
		strings.HasPrefix(path, "/audio") ||
		strings.HasPrefix(path, "/artists") ||
		strings.HasPrefix(path, "/albums") ||
		strings.HasPrefix(path, "/playlists") ||
		strings.HasPrefix(path, "/web/")
}

var httpScheme = []byte("http://")
var httpsScheme = []byte("https://")

// rewriteBody scans for absolute URLs in Emby text responses and rewrites them
// back to proxy URLs. Emby may return stream URLs on different upstream hosts,
// so this intentionally rewrites absolute URLs without requiring them to match
// the current request target host.
// Uses bytes.Index for fast searching — no regex, no url.Parse per match.
func rewriteBody(body []byte, baseURL string) []byte {
	if !bytes.Contains(body, []byte("http")) {
		return body
	}

	var out []byte
	i := 0
	for i < len(body) {
		remaining := body[i:]
		httpPos := bytes.Index(remaining, httpScheme)
		httpsPos := bytes.Index(remaining, httpsScheme)

		pos := -1
		schemeLen := 0
		if httpPos >= 0 && (httpsPos < 0 || httpPos <= httpsPos) {
			if httpsPos >= 0 && httpsPos == httpPos {
				pos = httpsPos
				schemeLen = 8
			} else {
				pos = httpPos
				schemeLen = 7
			}
		} else if httpsPos >= 0 {
			pos = httpsPos
			schemeLen = 8
		}

		if pos < 0 {
			if out == nil {
				return body
			}
			out = append(out, remaining...)
			break
		}

		if out == nil {
			out = make([]byte, 0, len(body)+len(body)/8)
		}
		out = append(out, remaining[:pos]...)

		urlStart := i + pos
		urlEnd := urlStart + schemeLen
		for urlEnd < len(body) && !isURLTerminator(body[urlEnd]) {
			urlEnd++
		}

		raw := body[urlStart:urlEnd]
		out = append(out, rewriteURLFast(raw, schemeLen, baseURL)...)
		i = urlEnd
	}

	if out == nil {
		return body
	}
	return out
}

// rewriteURLFast rewrites a URL without url.Parse.
// Input: raw URL bytes like "https://example.com:8096/path?q=1"
// schemeLen: 7 for http://, 8 for https://
// Output: "baseURL/https/example.com/8096/path?q=1"
func rewriteURLFast(raw []byte, schemeLen int, baseURL string) []byte {
	afterScheme := raw[schemeLen:]
	pathIdx := len(afterScheme)
	if slashIdx := bytes.IndexByte(afterScheme, '/'); slashIdx >= 0 && slashIdx < pathIdx {
		pathIdx = slashIdx
	}
	if queryIdx := bytes.IndexByte(afterScheme, '?'); queryIdx >= 0 && queryIdx < pathIdx {
		pathIdx = queryIdx
	}
	if fragmentIdx := bytes.IndexByte(afterScheme, '#'); fragmentIdx >= 0 && fragmentIdx < pathIdx {
		pathIdx = fragmentIdx
	}
	var hostPort, pathAndQuery []byte
	if pathIdx < len(afterScheme) {
		hostPort = afterScheme[:pathIdx]
		pathAndQuery = afterScheme[pathIdx:]
		if pathAndQuery[0] == '?' || pathAndQuery[0] == '#' {
			pathAndQuery = append([]byte("/"), pathAndQuery...)
		}
	} else {
		hostPort = afterScheme
		pathAndQuery = []byte("/")
	}

	if len(hostPort) == 0 {
		return raw
	}

	var host, portStr []byte
	if hostPort[0] == '[' {
		bracketEnd := bytes.IndexByte(hostPort, ']')
		if bracketEnd < 0 {
			return raw
		}
		host = hostPort[1:bracketEnd]
		rest := hostPort[bracketEnd+1:]
		if len(rest) > 0 && rest[0] == ':' {
			portStr = rest[1:]
		}
	} else {
		colonIdx := bytes.LastIndexByte(hostPort, ':')
		if colonIdx >= 0 {
			host = hostPort[:colonIdx]
			portStr = hostPort[colonIdx+1:]
		} else {
			host = hostPort
		}
	}

	if len(host) == 0 {
		return raw
	}

	scheme := "http"
	if schemeLen == 8 {
		scheme = "https"
	}
	port := 80
	if scheme == "https" {
		port = 443
	}
	if len(portStr) > 0 {
		if p, err := strconv.Atoi(string(portStr)); err == nil && p > 0 && p <= 65535 {
			port = p
		}
	}

	var b strings.Builder
	b.Grow(len(baseURL) + 1 + len(scheme) + 1 + len(host) + 1 + 5 + len(pathAndQuery))
	b.WriteString(baseURL)
	b.WriteByte('/')
	b.WriteString(scheme)
	b.WriteByte('/')
	b.Write(host)
	b.WriteByte('/')
	b.WriteString(strconv.Itoa(port))
	b.Write(pathAndQuery)
	return []byte(b.String())
}

func isURLTerminator(c byte) bool {
	switch c {
	case '"', '\'', '<', '>', ' ', '\t', '\n', '\r', '`', '(', ')', '{', '}', '[', ']', '\\', '|', '^':
		return true
	}
	return false
}

func rewriteSingleURL(rawURL, baseURL string) string {
	var schemeLen int
	if strings.HasPrefix(rawURL, "https://") {
		schemeLen = 8
	} else if strings.HasPrefix(rawURL, "http://") {
		schemeLen = 7
	} else {
		return rawURL
	}
	return string(rewriteURLFast([]byte(rawURL), schemeLen, baseURL))
}
