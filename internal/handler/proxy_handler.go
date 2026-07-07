package handler

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	externalCacheMaxChunkBytes = int64(4 * 1024 * 1024) // 4MB for Cloud Run
)

type ProxyHandler struct {
	CacheDir string
}

type externalCacheProgress struct {
	Downloaded int64 `json:"downloaded"`
	Total      int64 `json:"total"`
	UpdatedAt  int64 `json:"updated_at"`
}

var externalCacheProgresses sync.Map // map[movieID]externalCacheProgress

func NewProxyHandler(cacheDir string) *ProxyHandler {
	return &ProxyHandler{CacheDir: cacheDir}
}

// ---------------------------------------------------------------------------
// Cache path helpers
// ---------------------------------------------------------------------------

func (h *ProxyHandler) externalCachePath(movieID string) string {
	if h.CacheDir == "" {
		return ""
	}
	return filepath.Join(h.CacheDir, "external", movieID, "video.mp4")
}

func cacheFileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func updateExternalCacheProgress(movieID string, downloaded, total int64) {
	externalCacheProgresses.Store(movieID, externalCacheProgress{
		Downloaded: downloaded,
		Total:      total,
		UpdatedAt:  time.Now().Unix(),
	})
}

// ---------------------------------------------------------------------------
// MediaSourceProvider — interface for fetching a movie's original path
// ---------------------------------------------------------------------------

type MediaSourceProvider interface {
	GetOriginalPath(movieID string) (string, error)
}

// ---------------------------------------------------------------------------
// External Proxy handler (proxy to stream URL from yt-dlp)
// ---------------------------------------------------------------------------

func (h *ProxyHandler) ExternalProxy(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Expose-Headers", "Accept-Ranges, Content-Range, Content-Length, Content-Type")
	c.Header("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Range, Content-Type")

	if c.Request.Method == "OPTIONS" {
		c.Status(http.StatusNoContent)
		return
	}

	movieID := c.Param("movieId")
	if movieID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "movieId required"})
		return
	}

	// Try serving from local cache first
	cachePath := h.externalCachePath(movieID)
	if cacheFileExists(cachePath) {
		if h.tryServeBounded(c, cachePath) {
			return
		}
	}

	// No cache — serve from stream URL
	h.serveFromStream(c, movieID)
}

func (h *ProxyHandler) serveFromStream(c *gin.Context, movieID string) {
	// Get the movie's stored OriginalPath (stream URL)
	provider, ok := c.Get("mediaSourceProvider")
	if !ok {
		// No provider set — fall back to downloading status
		c.JSON(http.StatusAccepted, gin.H{
			"error": gin.H{
				"code":       "external_caching",
				"title":      "Film sedang diproses",
				"message":    "Stream URL belum tersedia. Coba lagi nanti.",
				"suggestion": "Tunggu beberapa saat lalu refresh halaman.",
			},
			"status": "pending",
		})
		return
	}

	msProvider := provider.(MediaSourceProvider)
	streamURL, err := msProvider.GetOriginalPath(movieID)
	if err != nil || streamURL == "" {
		c.JSON(http.StatusAccepted, gin.H{
			"error": gin.H{
				"code":       "external_caching",
				"title":      "Film sedang diproses",
				"message":    "Stream URL belum tersedia. Coba lagi nanti.",
				"suggestion": "Tunggu beberapa saat lalu refresh halaman.",
			},
			"status": "pending",
		})
		return
	}

	// Proxy the request to the actual video URL
	h.proxyStreamURL(c, streamURL)
}

func (h *ProxyHandler) proxyStreamURL(c *gin.Context, targetURL string) {
	// Build range header from request
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, targetURL, nil)
	if err != nil {
		log.Printf("[stream] create request error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "proxy error"})
		return
	}

	// Forward headers
	if rangeHeader := c.GetHeader("Range"); rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	if ua := c.GetHeader("User-Agent"); ua != "" {
		req.Header.Set("User-Agent", ua)
	} else {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	}
	req.Header.Set("Referer", "https://www.youtube.com/")
	req.Header.Set("Origin", "https://www.youtube.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[stream] upstream request error: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "upstream fetch failed"})
		return
	}
	defer resp.Body.Close()

	// Handle non-success — fail fast
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusNotModified {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[stream] upstream returned %d: %s", resp.StatusCode, string(body[:min(len(body), 500)]))
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"code":    "stream_error",
				"title":   "Gagal memuat video",
				"message": fmt.Sprintf("Server video mengembalikan error (HTTP %d)", resp.StatusCode),
			},
		})
		return
	}

	// Copy response headers — only pass through relevant ones
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "video/mp4"
	}
	contentLen := resp.Header.Get("Content-Length")
	contentRange := resp.Header.Get("Content-Range")
	acceptRanges := resp.Header.Get("Accept-Ranges")

	c.Header("Content-Type", contentType)
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Expose-Headers", "Accept-Ranges, Content-Range, Content-Length, Content-Type")
	c.Header("Accept-Ranges", acceptRanges)
	if contentLen != "" {
		c.Header("Content-Length", contentLen)
	}
	if contentRange != "" {
		c.Header("Content-Range", contentRange)
	}

	c.Status(resp.StatusCode)

	if c.Request.Method != http.MethodHead {
		_, _ = io.Copy(c.Writer, resp.Body)
	}
}

func (h *ProxyHandler) ExternalCacheStatus(c *gin.Context) {
	movieID := c.Param("movieId")
	if movieID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "movieId required"})
		return
	}

	cachePath := h.externalCachePath(movieID)
	if cacheFileExists(cachePath) {
		if p, ok := externalCacheProgresses.Load(movieID); ok {
			prog := p.(externalCacheProgress)
			c.JSON(http.StatusOK, gin.H{
				"status":   "ready",
				"cached":   true,
				"progress": prog,
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready", "cached": true})
		return
	}

	// Check if movie has a stream URL
	provider, ok := c.Get("mediaSourceProvider")
	if ok {
		msProvider := provider.(MediaSourceProvider)
		streamURL, err := msProvider.GetOriginalPath(movieID)
		if err == nil && streamURL != "" {
			c.JSON(http.StatusOK, gin.H{"status": "ready", "cached": false, "stream_url": streamURL})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "preparing", "cached": false})
}

func (h *ProxyHandler) ExternalProgress(c *gin.Context) {
	movieID := c.Param("movieId")
	if movieID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "movieId required"})
		return
	}

	if p, ok := externalCacheProgresses.Load(movieID); ok {
		prog := p.(externalCacheProgress)
		c.JSON(http.StatusOK, prog)
		return
	}

	c.JSON(http.StatusOK, externalCacheProgress{})
}

// ---------------------------------------------------------------------------
// Bounded cache serving
// ---------------------------------------------------------------------------

func (h *ProxyHandler) tryServeBounded(c *gin.Context, cachePath string) bool {
	file, err := os.Open(cachePath)
	if err != nil {
		log.Printf("[cache] open error path=%s err=%v", cachePath, err)
		return false
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		log.Printf("[cache] stat error path=%s err=%v", cachePath, err)
		return false
	}

	size := stat.Size()

	movieID := c.Param("movieId")
	if movieID != "" {
		updateExternalCacheProgress(movieID, size, size)
	}

	start, end, ok := parseBoundedRange(c.GetHeader("Range"), size)
	if !ok {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Expose-Headers", "Accept-Ranges, Content-Range, Content-Length, Content-Type")
		c.Header("Accept-Ranges", "bytes")
		c.Header("Content-Type", "video/mp4")
		c.Status(http.StatusRequestedRangeNotSatisfiable)
		return true
	}

	length := end - start + 1
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		log.Printf("[cache] seek error path=%s start=%d err=%v", cachePath, start, err)
		return false
	}

	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Expose-Headers", "Accept-Ranges, Content-Range, Content-Length, Content-Type")
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Type", "video/mp4")
	c.Header("Content-Length", fmt.Sprintf("%d", length))
	c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	c.Status(http.StatusPartialContent)
	if c.Request.Method != http.MethodHead {
		_, _ = io.CopyN(c.Writer, file, length)
	}
	return true
}

func parseBoundedRange(header string, size int64) (int64, int64, bool) {
	if size <= 0 {
		return 0, 0, false
	}

	start := int64(0)
	end := min(size-1, externalCacheMaxChunkBytes-1)
	if strings.TrimSpace(header) == "" {
		return start, end, true
	}

	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false
	}
	value := strings.TrimPrefix(header, "bytes=")
	parts := strings.SplitN(value, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}

	if parts[0] == "" {
		suffix, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || suffix <= 0 {
			return 0, 0, false
		}
		if suffix > size {
			suffix = size
		}
		start = size - suffix
		end = size - 1
	} else {
		parsedStart, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || parsedStart < 0 || parsedStart >= size {
			return 0, 0, false
		}
		start = parsedStart
		if parts[1] != "" {
			parsedEnd, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil || parsedEnd < start {
				return 0, 0, false
			}
			end = min(parsedEnd, size-1)
		} else {
			end = size - 1
		}
	}

	maxEnd := min(size-1, start+externalCacheMaxChunkBytes-1)
	if end > maxEnd {
		end = maxEnd
	}
	return start, end, true
}
