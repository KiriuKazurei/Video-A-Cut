package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// sameOriginWebHandler serves the built UI without relaxing the API's local
// Host/Origin gate. It refuses directory listings and symlinks outside webRoot.
func sameOriginWebHandler(webRoot string, apiHandler http.Handler) (http.Handler, error) {
	root, err := filepath.EvalSymlinks(webRoot)
	if err != nil {
		return nil, err
	}
	index := filepath.Join(root, "index.html")
	info, err := os.Stat(index)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("web_root needs a built index.html")
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", apiHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rel := strings.TrimPrefix(r.URL.Path, "/")
		if rel == "" {
			rel = "index.html"
		}
		if !filepath.IsLocal(rel) {
			http.NotFound(w, r)
			return
		}
		candidate, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		within, err := filepath.Rel(root, candidate)
		if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) || filepath.IsAbs(within) {
			http.NotFound(w, r)
			return
		}
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if rel == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeFile(w, r, candidate)
	})
	return mux, nil
}
