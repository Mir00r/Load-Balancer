package handler

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mir00r/load-balancer/pkg/logger"
)

// StaticHandler handles static content delivery with optimizations
type StaticHandler struct {
	logger      *logger.Logger
	rootPath    string
	enableGzip  bool
	indexFiles  []string
	cacheMaxAge int
}

// NewStaticHandler creates a new static content handler
func NewStaticHandler(rootPath string, logger *logger.Logger) *StaticHandler {
	return &StaticHandler{
		logger:      logger,
		rootPath:    rootPath,
		enableGzip:  true,
		indexFiles:  []string{"index.html", "index.htm", "default.html"},
		cacheMaxAge: 3600, // 1 hour
	}
}

// ServeHTTP handles static content requests with optimizations
func (sh *StaticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Security: prevent directory traversal
	if strings.Contains(r.URL.Path, "..") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Clean the path
	path := filepath.Clean(r.URL.Path)
	if path == "/" {
		path = "/index.html"
	}

	// Check if file exists
	fileInfo, err := http.Dir(sh.rootPath).Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer fileInfo.Close()

	// Get file info
	stat, err := fileInfo.Stat()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// If directory, try to serve index file
	if stat.IsDir() {
		sh.serveDirectory(w, r, fileInfo, path)
		return
	}

	// Set content type
	contentType := sh.getContentType(path)
	w.Header().Set("Content-Type", contentType)

	// Set cache headers
	if sh.cacheMaxAge > 0 {
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", sh.cacheMaxAge))
		w.Header().Set("Expires", time.Now().Add(time.Duration(sh.cacheMaxAge)*time.Second).Format(http.TimeFormat))
	}

	// Set ETag
	etag := fmt.Sprintf(`"%x-%x"`, stat.ModTime().Unix(), stat.Size())
	w.Header().Set("ETag", etag)

	// Check If-None-Match
	if match := r.Header.Get("If-None-Match"); match != "" {
		if match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	// Set Last-Modified
	w.Header().Set("Last-Modified", stat.ModTime().Format(http.TimeFormat))

	// Check If-Modified-Since
	if modSince := r.Header.Get("If-Modified-Since"); modSince != "" {
		if t, err := time.Parse(http.TimeFormat, modSince); err == nil {
			if stat.ModTime().Before(t.Add(1 * time.Second)) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
	}

	// Handle range requests
	if sh.handleRangeRequest(w, r, fileInfo, stat) {
		return
	}

	// Read file content
	content, err := io.ReadAll(fileInfo)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Handle gzip compression
	if sh.enableGzip && sh.shouldCompress(contentType) && sh.acceptsGzip(r) {
		sh.serveCompressed(w, content)
		return
	}

	// Serve uncompressed
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	w.Write(content)
}

// serveDirectory handles directory requests
func (sh *StaticHandler) serveDirectory(w http.ResponseWriter, r *http.Request, dir http.File, path string) {
	// Try to find index file
	for _, indexFile := range sh.indexFiles {
		indexPath := filepath.Join(path, indexFile)
		if indexFileInfo, err := http.Dir(sh.rootPath).Open(indexPath); err == nil {
			indexFileInfo.Close()
			// Redirect to index file
			http.Redirect(w, r, indexPath, http.StatusFound)
			return
		}
	}

	// Auto-indexing - generate directory listing
	sh.generateDirectoryListing(w, r, dir, path)
}

// generateDirectoryListing creates an HTML directory listing
func (sh *StaticHandler) generateDirectoryListing(w http.ResponseWriter, r *http.Request, dir http.File, path string) {
	files, err := dir.Readdir(-1)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <title>Index of %s</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; }
        h1 { color: #333; }
        table { border-collapse: collapse; width: 100%%; }
        th, td { text-align: left; padding: 8px; border-bottom: 1px solid #ddd; }
        th { background-color: #f2f2f2; }
        a { text-decoration: none; color: #0066cc; }
        a:hover { text-decoration: underline; }
        .size { text-align: right; }
        .date { text-align: center; }
    </style>
</head>
<body>
    <h1>Index of %s</h1>
    <table>
        <tr>
            <th>Name</th>
            <th class="size">Size</th>
            <th class="date">Last Modified</th>
        </tr>`, path, path)

	// Add parent directory link if not root
	if path != "/" {
		html += `<tr><td><a href="../">../</a></td><td class="size">-</td><td class="date">-</td></tr>`
	}

	// Add files and directories
	for _, file := range files {
		name := file.Name()
		if file.IsDir() {
			name += "/"
		}

		size := "-"
		if !file.IsDir() {
			size = sh.formatFileSize(file.Size())
		}

		html += fmt.Sprintf(`<tr>
			<td><a href="%s">%s</a></td>
			<td class="size">%s</td>
			<td class="date">%s</td>
		</tr>`, name, name, size, file.ModTime().Format("2006-01-02 15:04:05"))
	}

	html += `</table></body></html>`
	w.Write([]byte(html))
}

// handleRangeRequest handles HTTP range requests for partial content
func (sh *StaticHandler) handleRangeRequest(w http.ResponseWriter, r *http.Request, file http.File, stat os.FileInfo) bool {
	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		return false
	}

	// Simple range handling (bytes=start-end)
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return false
	}

	// Parse range
	rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
	parts := strings.Split(rangeSpec, "-")
	if len(parts) != 2 {
		return false
	}

	// Get file size
	fileSize := stat.Size()

	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}

	end := fileSize - 1
	if parts[1] != "" {
		if e, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			end = e
		}
	}

	// Validate range
	if start < 0 || end >= fileSize || start > end {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return true
	}

	// Set partial content headers
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)

	// Serve partial content
	file.Seek(start, io.SeekStart)
	io.CopyN(w, file, end-start+1)
	return true
}

// serveCompressed serves gzip-compressed content
func (sh *StaticHandler) serveCompressed(w http.ResponseWriter, content []byte) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write(content)
	gz.Close()

	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.Write(buf.Bytes())
}

// getContentType determines content type based on file extension
func (sh *StaticHandler) getContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))

	contentTypes := map[string]string{
		".html": "text/html; charset=utf-8",
		".htm":  "text/html; charset=utf-8",
		".css":  "text/css",
		".js":   "application/javascript",
		".json": "application/json",
		".xml":  "application/xml",
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".gif":  "image/gif",
		".svg":  "image/svg+xml",
		".ico":  "image/x-icon",
		".pdf":  "application/pdf",
		".zip":  "application/zip",
		".tar":  "application/x-tar",
		".gz":   "application/gzip",
		".txt":  "text/plain; charset=utf-8",
		".md":   "text/markdown; charset=utf-8",
	}

	if contentType, exists := contentTypes[ext]; exists {
		return contentType
	}
	return "application/octet-stream"
}

// shouldCompress determines if content should be compressed
func (sh *StaticHandler) shouldCompress(contentType string) bool {
	compressible := []string{
		"text/",
		"application/javascript",
		"application/json",
		"application/xml",
		"image/svg+xml",
	}

	for _, prefix := range compressible {
		if strings.HasPrefix(contentType, prefix) {
			return true
		}
	}
	return false
}

// acceptsGzip checks if client accepts gzip encoding
func (sh *StaticHandler) acceptsGzip(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
}

// formatFileSize formats file size in human-readable format
func (sh *StaticHandler) formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}

	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	units := []string{"KB", "MB", "GB", "TB"}
	return fmt.Sprintf("%.1f %s", float64(size)/float64(div), units[exp])
}
