package handler

import (
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	driveSessionTTL         = 20 * time.Minute
	driveProxyMaxAttempts   = 4
	driveCacheMaxChunkBytes = int64(4 * 1024 * 1024)
)

type ProxyHandler struct {
	CacheDir string
}

type driveSession struct {
	Jar      http.CookieJar
	FinalURL string
	Expires  time.Time
}

type driveProxyError struct {
	Code       string `json:"code"`
	Title      string `json:"title"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion"`
}

type driveCacheProgress struct {
	Downloaded int64 `json:"downloaded"`
	Total      int64 `json:"total"`
	UpdatedAt  int64 `json:"updated_at"`
}

type driveServeResult int

const (
	driveServeFailed driveServeResult = iota
	driveServeHandled
	driveServeRetryableQuota
	driveServeNotPublic
	driveServeConfirmFailed
)

var driveSessions = struct {
	sync.Mutex
	items map[string]*driveSession
}{items: make(map[string]*driveSession)}

// driveCacheLockers prevents concurrent full-file downloads per fileID.
var driveCacheLockers sync.Map    // map[fileID]*sync.Mutex
var driveCacheFailures sync.Map   // map[fileID]string
var driveCacheProgresses sync.Map // map[fileID]driveCacheProgress

func NewProxyHandler(cacheDir string) *ProxyHandler {
	return &ProxyHandler{CacheDir: cacheDir}
}

// ---------------------------------------------------------------------------
// Cache helpers
// ---------------------------------------------------------------------------

func (h *ProxyHandler) cachePath(fileID string) string {
	if h.CacheDir == "" {
		return ""
	}
	return filepath.Join(h.CacheDir, fileID+".mp4")
}

func cacheFileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func updateDriveCacheProgress(fileID string, downloaded int64, total int64) {
	driveCacheProgresses.Store(fileID, driveCacheProgress{Downloaded: downloaded, Total: total, UpdatedAt: time.Now().Unix()})
}

type progressWriter struct {
	fileID     string
	total      int64
	downloaded int64
	lastUpdate time.Time
}

func (w *progressWriter) Write(p []byte) (int, error) {
	n := len(p)
	w.downloaded += int64(n)
	if time.Since(w.lastUpdate) > time.Second || (w.total > 0 && w.downloaded >= w.total) {
		updateDriveCacheProgress(w.fileID, w.downloaded, w.total)
		w.lastUpdate = time.Now()
	}
	return n, nil
}

func copyWithDriveProgress(fileID string, dest io.Writer, src io.Reader, total int64) error {
	updateDriveCacheProgress(fileID, 0, total)
	writer := &progressWriter{fileID: fileID, total: total, lastUpdate: time.Now()}
	_, err := io.Copy(dest, io.TeeReader(src, writer))
	updateDriveCacheProgress(fileID, writer.downloaded, total)
	return err
}

func getOrCreateDriveCacheLocker(fileID string) *sync.Mutex {
	locker := &sync.Mutex{}
	actual, _ := driveCacheLockers.LoadOrStore(fileID, locker)
	return actual.(*sync.Mutex)
}

func driveDownloadErrorCode(err error) string {
	switch {
	case errors.Is(err, errDownloadRetryableQuota):
		return "drive_quota_exceeded"
	case errors.Is(err, errDownloadNotPublic):
		return "drive_file_not_public"
	case errors.Is(err, errDownloadConfirmFailed):
		return "drive_confirm_failed"
	default:
		return "drive_proxy_failed"
	}
}

// tryServeFromCache serves the locally cached video file with bounded Range
// responses. Cloud Run rejects very large responses, so even open-ended ranges
// like bytes=0- are capped to small chunks.
func (h *ProxyHandler) tryServeFromCache(c *gin.Context, fileID string) bool {
	cachePath := h.cachePath(fileID)
	if !cacheFileExists(cachePath) {
		return false
	}

	file, err := os.Open(cachePath)
	if err != nil {
		log.Printf("[cache] open error file=%s err=%v", fileID, err)
		return false
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		log.Printf("[cache] stat error file=%s err=%v", fileID, err)
		return false
	}

	size := stat.Size()
	start, end, ok := parseBoundedRange(c.GetHeader("Range"), size)
	if !ok {
		writeDriveProxyError(c, http.StatusRequestedRangeNotSatisfiable, "drive_serve_failed")
		return true
	}
	if size == 0 {
		writeDriveProxyError(c, http.StatusInternalServerError, "drive_serve_failed")
		return true
	}

	length := end - start + 1
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		log.Printf("[cache] seek error file=%s start=%d err=%v", fileID, start, err)
		writeDriveProxyError(c, http.StatusInternalServerError, "drive_serve_failed")
		return true
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
	log.Printf("[cache] served bounded chunk file=%s range=%d-%d size=%d", fileID, start, end, size)
	return true
}

func parseBoundedRange(header string, size int64) (int64, int64, bool) {
	if size <= 0 {
		return 0, 0, false
	}

	start := int64(0)
	end := min(size-1, driveCacheMaxChunkBytes-1)
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

	maxEnd := min(size-1, start+driveCacheMaxChunkBytes-1)
	if end > maxEnd {
		end = maxEnd
	}
	return start, end, true
}

// ---------------------------------------------------------------------------
// Background cache download
// ---------------------------------------------------------------------------

// triggerCacheDownload starts a background download of the full video file.
// Only one goroutine per fileID is started; subsequent calls are no-ops.
func (h *ProxyHandler) triggerCacheDownload(fileID string) {
	if h.CacheDir == "" {
		return
	}
	locker := getOrCreateDriveCacheLocker(fileID)
	if !locker.TryLock() {
		return // another goroutine is already downloading this file
	}

	driveCacheFailures.Delete(fileID)
	cachePath := h.cachePath(fileID)
	tmpPath := h.cacheTmpPath(fileID)

	go func() {
		defer locker.Unlock()

		log.Printf("[cache] background cache download start file=%s", fileID)
		if err := h.downloadDriveFile(fileID, tmpPath); err != nil {
			log.Printf("[cache] background cache download failed file=%s err=%v", fileID, err)
			driveCacheFailures.Store(fileID, driveDownloadErrorCode(err))
			driveCacheProgresses.Delete(fileID)
			os.Remove(tmpPath) // remove partial file
			return
		}

		if err := os.Rename(tmpPath, cachePath); err != nil {
			log.Printf("[cache] rename cache file failed file=%s err=%v", fileID, err)
			os.Remove(tmpPath)
			return
		}
		driveCacheFailures.Delete(fileID)
		driveCacheProgresses.Delete(fileID)
		log.Printf("[cache] background cache download complete file=%s", fileID)

		// Auto-delete cache 24 hours from download completion
		time.AfterFunc(24*time.Hour, func() {
			log.Printf("[cache] TTL expired (24h), clearing cache file=%s", fileID)
			if err := os.Remove(cachePath); err != nil && !os.IsNotExist(err) {
				log.Printf("[cache] TTL remove failed file=%s err=%v", fileID, err)
			}
			// Also clear the drive session and metadata
			clearDriveSession(fileID)
			driveCacheFailures.Delete(fileID)
		})
	}()
}

// downloadDriveFile downloads the full video from Google Drive to destPath.
// It negotiates Drive cookies, confirm forms, and retries on quota.
func (h *ProxyHandler) downloadDriveFile(fileID, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("mkdir cache dir: %w", err)
	}

	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer destFile.Close()

	// Create a fresh Drive client for the download
	jar, _ := cookiejar.New(nil)
	client := newDriveClient(jar)

	// Try cached final URL first
	if finalURL := getDriveCachedFinalURL(fileID); finalURL != "" {
		log.Printf("[cache] trying cached final url file=%s", fileID)
		_, _ = destFile.Seek(0, 0)
		_ = destFile.Truncate(0)
		err := tryDownloadURL(client, finalURL, "", fileID, destFile)
		if err == nil {
			return nil
		}
		log.Printf("[cache] cached url failed file=%s err=%v", fileID, err)
	}

	urls := []string{
		fmt.Sprintf("https://drive.usercontent.google.com/download?id=%s&export=download&authuser=0", fileID),
		fmt.Sprintf("https://drive.google.com/uc?export=download&id=%s", fileID),
	}

	var lastErr error
	for _, targetURL := range urls {
		for attempt := 1; attempt <= driveProxyMaxAttempts; attempt++ {
			_, _ = destFile.Seek(0, 0)
			_ = destFile.Truncate(0)
			err := tryDownloadURL(client, targetURL, "", fileID, destFile)
			if err == nil {
				return nil
			}
			lastErr = err
			if errors.Is(err, errDownloadRetryableQuota) {
				log.Printf("[cache] quota retry file=%s attempt=%d", fileID, attempt)
				waitBeforeDriveRetry(attempt)
				continue
			}
			// Non-retryable error — try next URL
			break
		}
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("all download attempts failed for file=%s", fileID)
}

var (
	errDownloadRetryableQuota = fmt.Errorf("retryable quota")
	errDownloadNotPublic      = fmt.Errorf("file not public")
	errDownloadConfirmFailed  = fmt.Errorf("confirm failed")
)

// tryDownloadURL attempts to download from a single Drive URL, negotiating
// HTML confirm pages. It writes the final video bytes to dest.
func tryDownloadURL(client *http.Client, targetURL, rangeHeader, fileID string, dest io.Writer) error {
	resp, err := requestDrive(client, targetURL, rangeHeader)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")

	if !isHTML(contentType) {
		if err := copyWithDriveProgress(fileID, dest, resp.Body, resp.ContentLength); err != nil {
			return fmt.Errorf("copy body: %w", err)
		}
		saveDriveFinalURL(fileID, targetURL)
		return nil
	}

	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if readErr != nil {
		return fmt.Errorf("read html body: %w", readErr)
	}

	body := string(bodyBytes)
	if isDriveQuotaExceeded(body) {
		log.Printf("[cache] quota exceeded file=%s", fileID)
		return errDownloadRetryableQuota
	}
	if isDriveAccessDenied(body) {
		log.Printf("[cache] file not public file=%s", fileID)
		return errDownloadNotPublic
	}

	finalURL := extractDriveDownloadURL(body, targetURL, fileID)
	if finalURL == "" {
		return errDownloadConfirmFailed
	}

	// Follow the confirm URL (no Range — full file)
	resp2, err2 := requestDrive(client, finalURL, rangeHeader)
	if err2 != nil {
		return fmt.Errorf("final request failed: %w", err2)
	}
	defer resp2.Body.Close()

	finalContentType := resp2.Header.Get("Content-Type")
	if isHTML(finalContentType) {
		finalBodyBytes, _ := io.ReadAll(io.LimitReader(resp2.Body, 1024*1024))
		finalBody := string(finalBodyBytes)
		if isDriveQuotaExceeded(finalBody) {
			return errDownloadRetryableQuota
		}
		if isDriveAccessDenied(finalBody) {
			return errDownloadNotPublic
		}

		retryURL := extractDriveDownloadURL(finalBody, finalURL, fileID)
		if retryURL == "" || retryURL == finalURL {
			return errDownloadConfirmFailed
		}

		log.Printf("[cache] retry with new confirm token file=%s", fileID)
		resp3, err3 := requestDrive(client, retryURL, rangeHeader)
		if err3 != nil {
			return fmt.Errorf("retry request failed: %w", err3)
		}
		defer resp3.Body.Close()

		retryContentType := resp3.Header.Get("Content-Type")
		if isHTML(retryContentType) {
			retryBody, _ := io.ReadAll(io.LimitReader(resp3.Body, 256*1024))
			retryBodyText := string(retryBody)
			if isDriveQuotaExceeded(retryBodyText) {
				return errDownloadRetryableQuota
			}
			if isDriveAccessDenied(retryBodyText) {
				return errDownloadNotPublic
			}
			return errDownloadConfirmFailed
		}

		if err3 = copyWithDriveProgress(fileID, dest, resp3.Body, resp3.ContentLength); err3 != nil {
			return fmt.Errorf("copy final retry body: %w", err3)
		}
		saveDriveFinalURL(fileID, retryURL)
		return nil
	}

	if err2 = copyWithDriveProgress(fileID, dest, resp2.Body, resp2.ContentLength); err2 != nil {
		return fmt.Errorf("copy final body: %w", err2)
	}
	saveDriveFinalURL(fileID, finalURL)
	return nil
}

// getDriveCachedFinalURL returns the saved final URL for a fileID, or "".
func getDriveCachedFinalURL(fileID string) string {
	driveSessions.Lock()
	defer driveSessions.Unlock()
	if session, ok := driveSessions.items[fileID]; ok && session.FinalURL != "" {
		return session.FinalURL
	}
	return ""
}

func (h *ProxyHandler) cacheTmpPath(fileID string) string {
	if h.CacheDir == "" {
		return ""
	}
	return filepath.Join(h.CacheDir, fileID+".mp4.tmp.download")
}

func (h *ProxyHandler) hasDriveCache(fileID string) bool {
	return cacheFileExists(h.cachePath(fileID))
}

func (h *ProxyHandler) hasDriveCacheOrCaching(fileID string) bool {
	return h.hasDriveCache(fileID) || cacheFileExists(h.cacheTmpPath(fileID))
}

// ClearDriveCache removes the cached video file and any partial download.
func (h *ProxyHandler) ClearDriveCache(fileID string) {
	paths := []string{h.cachePath(fileID), h.cacheTmpPath(fileID)}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if err := os.Remove(p); err == nil {
			log.Printf("[cache] cleared file=%s path=%s", fileID, filepath.Base(p))
		} else if !os.IsNotExist(err) {
			log.Printf("[cache] remove error file=%s err=%v", fileID, err)
		}
	}
	clearDriveSession(fileID)
	driveCacheFailures.Delete(fileID)
	driveCacheProgresses.Delete(fileID)
	driveCacheLockers.Delete(fileID)
}

// DriveCacheStatus returns the cache status for a file.
// Returns one of: "ready", "downloading", "none"
func (h *ProxyHandler) DriveCacheStatus(fileID string) string {
	if h.hasDriveCache(fileID) {
		return "ready"
	}
	if cacheFileExists(h.cacheTmpPath(fileID)) {
		return "downloading"
	}
	return "none"
}

// PrefetchDrive triggers a background cache download and returns the current status.
func (h *ProxyHandler) PrefetchDrive(c *gin.Context) {
	fileID := c.Param("fileId")
	if fileID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "fileId required"})
		return
	}

	if failure, ok := driveCacheFailures.Load(fileID); ok {
		code, _ := failure.(string)
		writeDriveProxyError(c, http.StatusFailedDependency, code)
		return
	}

	status := h.DriveCacheStatus(fileID)
	if status == "ready" {
		c.JSON(http.StatusOK, gin.H{"status": "ready", "cached": true})
		return
	}
	if status == "downloading" {
		payload := gin.H{"status": "downloading", "cached": false}
		if progress, ok := driveCacheProgresses.Load(fileID); ok {
			payload["progress"] = progress
		}
		c.JSON(http.StatusOK, payload)
		return
	}

	h.triggerCacheDownload(fileID)
	c.JSON(http.StatusAccepted, gin.H{"status": "downloading", "cached": false})
}

// ---------------------------------------------------------------------------
// Drive proxy handler
// ---------------------------------------------------------------------------

func (h *ProxyHandler) DriveProxy(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Expose-Headers", "Accept-Ranges, Content-Range, Content-Length, Content-Type")
	c.Header("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Range, Content-Type")

	if c.Request.Method == "OPTIONS" {
		c.Status(http.StatusNoContent)
		return
	}

	fileID := c.Param("fileId")
	if fileID == "" {
		writeDriveProxyError(c, http.StatusBadRequest, "drive_proxy_failed")
		return
	}

	// Serve only from local cache. If cache is not ready yet, start/keep
	// downloading in the background and tell the frontend to wait.
	if h.tryServeFromCache(c, fileID) {
		return
	}

	if failure, ok := driveCacheFailures.Load(fileID); ok {
		code, _ := failure.(string)
		writeDriveProxyError(c, http.StatusFailedDependency, code)
		return
	}

	status := h.DriveCacheStatus(fileID)
	switch status {
	case "ready":
		// Retry — file exists but tryServeFromCache failed (unlikely race)
		if h.tryServeFromCache(c, fileID) {
			return
		}
		log.Printf("[cache] file marked ready but cannot serve file=%s", fileID)
		writeDriveProxyError(c, http.StatusInternalServerError, "drive_serve_failed")
		return
	case "none":
		h.triggerCacheDownload(fileID)
		fallthrough
	case "downloading":
		c.JSON(http.StatusAccepted, gin.H{
			"error": gin.H{
				"code":       "drive_caching",
				"title":      "Film sedang didownload ke server",
				"message":    "Film belum siap ditonton karena server sedang mengambil file dari Google Drive.",
				"suggestion": "Tunggu sampai proses download selesai. Setelah siap, semua peserta akan streaming dari server.",
			},
			"status": "downloading",
		})
		return
	default:
		writeDriveProxyError(c, http.StatusInternalServerError, "drive_proxy_failed")
		return
	}
}

func writeDriveProxyError(c *gin.Context, status int, code string) {
	detail := getDriveProxyError(code)
	c.JSON(status, gin.H{"error": detail})
}

func getDriveProxyError(code string) driveProxyError {
	switch code {
	case "drive_quota_exceeded":
		return driveProxyError{
			Code:       code,
			Title:      "Google Drive membatasi file ini",
			Message:    "File Google Drive sedang terkena limit view/download atau terlalu sering diakses dari server.",
			Suggestion: "Upload atau copy file ke Drive kamu sendiri, pastikan akses Anyone with the link sebagai Viewer, lalu ganti link movie.",
		}
	case "drive_file_not_public":
		return driveProxyError{
			Code:       code,
			Title:      "File Drive tidak bisa diakses publik",
			Message:    "Google Drive tidak memberikan file video ke server. File kemungkinan private, butuh login, atau link sharing belum publik.",
			Suggestion: "Buka pengaturan share file dan ubah General access menjadi Anyone with the link sebagai Viewer.",
		}
	case "drive_confirm_failed":
		return driveProxyError{
			Code:       code,
			Title:      "Konfirmasi download Google Drive gagal",
			Message:    "Google Drive menampilkan halaman konfirmasi, tetapi server tidak berhasil mendapatkan link download final.",
			Suggestion: "Coba buka link Drive langsung, klik Tetap download, atau upload/copy file ke Drive kamu sendiri lalu gunakan link baru.",
		}
	default:
		return driveProxyError{
			Code:       "drive_proxy_failed",
			Title:      "Video Google Drive gagal dimuat",
			Message:    "Server gagal mengambil video dari Google Drive.",
			Suggestion: "Coba muat ulang. Jika tetap gagal, gunakan link Drive lain atau upload/copy file ke Drive kamu sendiri.",
		}
	}
}

// streamFromDrive streams video from Google Drive to the client. On success
// it also triggers a background cache download via proxyHandler.
func streamFromDrive(c *gin.Context, fileID string, proxyHandler *ProxyHandler) {
	rangeHeader := c.GetHeader("Range")
	quotaSeen := false
	lastFailureCode := "drive_proxy_failed"
	session := getDriveSession(fileID)
	client := newDriveClient(session.Jar)

	if session.FinalURL != "" {
		for attempt := 1; attempt <= driveProxyMaxAttempts; attempt++ {
			log.Printf("[proxy] cache hit final url file=%s attempt=%d", fileID, attempt)
			result := tryServe(c, client, session.FinalURL, rangeHeader, fileID, true)
			if result == driveServeHandled {
				proxyHandler.triggerCacheDownload(fileID)
				return
			}
			if result != driveServeRetryableQuota {
				lastFailureCode = driveFailureCode(result)
				break
			}
			quotaSeen = true
			log.Printf("[proxy] cached quota retry file=%s attempt=%d", fileID, attempt)
			waitBeforeDriveRetry(attempt)
		}
		log.Printf("[proxy] cached final url failed, clearing cache file=%s", fileID)
		clearDriveSession(fileID)
		session = getDriveSession(fileID)
		client = newDriveClient(session.Jar)
	}

	urls := []string{
		fmt.Sprintf("https://drive.usercontent.google.com/download?id=%s&export=download&authuser=0", fileID),
		fmt.Sprintf("https://drive.google.com/uc?export=download&id=%s", fileID),
	}

	for _, targetURL := range urls {
		for attempt := 1; attempt <= driveProxyMaxAttempts; attempt++ {
			result := tryServe(c, client, targetURL, rangeHeader, fileID, false)
			if result == driveServeHandled {
				proxyHandler.triggerCacheDownload(fileID)
				return
			}
			if result != driveServeRetryableQuota {
				lastFailureCode = driveFailureCode(result)
				break
			}
			quotaSeen = true
			log.Printf("[proxy] quota retry file=%s attempt=%d", fileID, attempt)
			waitBeforeDriveRetry(attempt)
		}
	}

	if quotaSeen {
		log.Printf("[proxy] quota exceeded after retries file=%s", fileID)
		writeDriveProxyError(c, http.StatusTooManyRequests, "drive_quota_exceeded")
		return
	}

	log.Printf("[proxy] all attempts failed for file=%s code=%s", fileID, lastFailureCode)
	writeDriveProxyError(c, http.StatusFailedDependency, lastFailureCode)
}

func getDriveSession(fileID string) *driveSession {
	now := time.Now()
	driveSessions.Lock()
	defer driveSessions.Unlock()

	if session, ok := driveSessions.items[fileID]; ok {
		if now.Before(session.Expires) {
			session.Expires = now.Add(driveSessionTTL)
			return session
		}
		log.Printf("[proxy] cache expired file=%s", fileID)
		delete(driveSessions.items, fileID)
	}

	jar, _ := cookiejar.New(nil)
	session := &driveSession{Jar: jar, Expires: now.Add(driveSessionTTL)}
	driveSessions.items[fileID] = session
	return session
}

func saveDriveFinalURL(fileID, finalURL string) {
	driveSessions.Lock()
	defer driveSessions.Unlock()
	if session, ok := driveSessions.items[fileID]; ok {
		if session.FinalURL != finalURL {
			log.Printf("[proxy] save final url file=%s url=%s", fileID, truncate(finalURL, 90))
		}
		session.FinalURL = finalURL
		session.Expires = time.Now().Add(driveSessionTTL)
	}
}

func clearDriveSession(fileID string) {
	driveSessions.Lock()
	defer driveSessions.Unlock()
	delete(driveSessions.items, fileID)
}

func waitBeforeDriveRetry(attempt int) {
	time.Sleep(time.Duration(250*attempt) * time.Millisecond)
}

func driveFailureCode(result driveServeResult) string {
	switch result {
	case driveServeNotPublic:
		return "drive_file_not_public"
	case driveServeConfirmFailed:
		return "drive_confirm_failed"
	default:
		return "drive_proxy_failed"
	}
}

func newDriveClient(jar http.CookieJar) *http.Client {
	return &http.Client{
		Jar:     jar,
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

func tryServe(c *gin.Context, client *http.Client, targetURL, rangeHeader, fileID string, fromCache bool) driveServeResult {
	resp, err := requestDrive(client, targetURL, rangeHeader)
	if err != nil {
		log.Printf("[proxy] request failed url=%s err=%v", truncate(targetURL, 70), err)
		return driveServeFailed
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	log.Printf("[proxy] url=%s status=%d ct=%s", truncate(targetURL, 70), resp.StatusCode, contentType)

	if !isHTML(contentType) {
		saveDriveFinalURL(fileID, targetURL)
		pipeDriveResponse(c, resp)
		return driveServeHandled
	}

	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if readErr != nil {
		return driveServeFailed
	}

	body := string(bodyBytes)
	if isDriveQuotaExceeded(body) {
		log.Printf("[proxy] quota exceeded for file=%s", fileID)
		return driveServeRetryableQuota
	}
	if isDriveAccessDenied(body) {
		log.Printf("[proxy] file is not public or requires login file=%s", fileID)
		return driveServeNotPublic
	}

	finalURL := extractDriveDownloadURL(body, targetURL, fileID)
	if finalURL == "" {
		log.Printf("[proxy] no final download URL found, trying next URL")
		return driveServeConfirmFailed
	}

	resp2, err2 := requestDrive(client, finalURL, rangeHeader)
	if err2 != nil {
		log.Printf("[proxy] final request failed url=%s err=%v", truncate(finalURL, 90), err2)
		return driveServeFailed
	}
	defer resp2.Body.Close()

	finalContentType := resp2.Header.Get("Content-Type")
	log.Printf("[proxy] final url=%s status=%d ct=%s", truncate(finalURL, 90), resp2.StatusCode, finalContentType)

	if isHTML(finalContentType) {
		finalBodyBytes, _ := io.ReadAll(io.LimitReader(resp2.Body, 1024*1024))
		finalBody := string(finalBodyBytes)
		if isDriveQuotaExceeded(finalBody) {
			log.Printf("[proxy] quota exceeded for file=%s", fileID)
			return driveServeRetryableQuota
		}
		if isDriveAccessDenied(finalBody) {
			log.Printf("[proxy] file is not public or requires login file=%s", fileID)
			return driveServeNotPublic
		}

		retryURL := extractDriveDownloadURL(finalBody, finalURL, fileID)
		if retryURL == "" || retryURL == finalURL {
			log.Printf("[proxy] final response is still HTML file=%s from_cache=%t", fileID, fromCache)
			return driveServeConfirmFailed
		}

		log.Printf("[proxy] retry with new confirm token file=%s", fileID)
		resp3, err3 := requestDrive(client, retryURL, rangeHeader)
		if err3 != nil {
			log.Printf("[proxy] retry request failed url=%s err=%v", truncate(retryURL, 90), err3)
			return driveServeFailed
		}
		defer resp3.Body.Close()

		retryContentType := resp3.Header.Get("Content-Type")
		log.Printf("[proxy] retry final url=%s status=%d ct=%s", truncate(retryURL, 90), resp3.StatusCode, retryContentType)
		if isHTML(retryContentType) {
			retryBody, _ := io.ReadAll(io.LimitReader(resp3.Body, 256*1024))
			retryBodyText := string(retryBody)
			if isDriveQuotaExceeded(retryBodyText) {
				log.Printf("[proxy] quota exceeded for file=%s", fileID)
				return driveServeRetryableQuota
			}
			if isDriveAccessDenied(retryBodyText) {
				log.Printf("[proxy] file is not public or requires login file=%s", fileID)
				return driveServeNotPublic
			}
			return driveServeConfirmFailed
		}

		saveDriveFinalURL(fileID, retryURL)
		pipeDriveResponse(c, resp3)
		return driveServeHandled
	}

	saveDriveFinalURL(fileID, finalURL)
	pipeDriveResponse(c, resp2)
	return driveServeHandled
}

func requestDrive(client *http.Client, targetURL, rangeHeader string) (*http.Response, error) {
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "video/mp4,video/*,*/*;q=0.8")
	req.Header.Set("Referer", "https://drive.google.com/")
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}

	return client.Do(req)
}

func pipeDriveResponse(c *gin.Context, resp *http.Response) bool {
	forwardHeaders(c, resp)
	c.Status(resp.StatusCode)
	_, _ = io.Copy(c.Writer, resp.Body)
	return true
}

func isHTML(contentType string) bool {
	return strings.Contains(contentType, "text/html") || strings.Contains(contentType, "text/plain")
}

func isDriveQuotaExceeded(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "quota exceeded") ||
		strings.Contains(lower, "too many users have viewed or downloaded") ||
		strings.Contains(lower, "can't view or download this file at this time") ||
		strings.Contains(lower, "cannot view or download this file at this time")
}

func isDriveAccessDenied(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "you need access") ||
		strings.Contains(lower, "access denied") ||
		strings.Contains(lower, "permission") ||
		strings.Contains(lower, "sign in") ||
		strings.Contains(lower, "login") ||
		strings.Contains(lower, "request access")
}

var (
	reDownloadHref = regexp.MustCompile(`(?i)href=["']([^"']*drive\.usercontent\.google\.com/download[^"']*)["']`)
	reFormAction   = regexp.MustCompile(`(?is)<form[^>]+action=["']([^"']+)["'][^>]*>(.*?)</form>`)
	reInput        = regexp.MustCompile(`(?is)<input\b[^>]*>`)
	reAttr         = regexp.MustCompile(`(?is)\s([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*["']([^"']*)["']`)
	reConfirmURL   = regexp.MustCompile(`(?i)(?:[?&]|&amp;)confirm=([^&"']+)`)
)

func extractDriveDownloadURL(body, baseURL, fileID string) string {
	if m := reDownloadHref.FindStringSubmatch(body); len(m) >= 2 {
		return normalizeDriveURL(m[1], baseURL)
	}

	if m := reFormAction.FindStringSubmatch(body); len(m) >= 3 {
		actionURL := normalizeDriveURL(m[1], baseURL)
		if actionURL != "" {
			parsed, err := url.Parse(actionURL)
			if err == nil {
				query := parsed.Query()
				for _, input := range reInput.FindAllString(m[2], -1) {
					attrs := parseHTMLAttrs(input)
					name := attrs["name"]
					if name != "" {
						query.Set(name, attrs["value"])
					}
				}
				if query.Get("id") == "" {
					query.Set("id", fileID)
				}
				if query.Get("export") == "" {
					query.Set("export", "download")
				}
				parsed.RawQuery = query.Encode()
				return parsed.String()
			}
		}
	}

	if confirm := extractConfirmToken(body); confirm != "" {
		return fmt.Sprintf("https://drive.usercontent.google.com/download?id=%s&export=download&authuser=0&confirm=%s", fileID, url.QueryEscape(confirm))
	}

	return ""
}

func normalizeDriveURL(rawURL, baseURL string) string {
	rawURL = html.UnescapeString(strings.TrimSpace(rawURL))
	if rawURL == "" {
		return ""
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	if parsed.IsAbs() {
		return parsed.String()
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	return base.ResolveReference(parsed).String()
}

func extractConfirmToken(body string) string {
	for _, input := range reInput.FindAllString(body, -1) {
		attrs := parseHTMLAttrs(input)
		if attrs["name"] == "confirm" && attrs["value"] != "" {
			return attrs["value"]
		}
	}
	if m := reConfirmURL.FindStringSubmatch(body); len(m) >= 2 && m[1] != "" {
		return html.UnescapeString(m[1])
	}
	return ""
}

func parseHTMLAttrs(tag string) map[string]string {
	attrs := make(map[string]string)
	for _, match := range reAttr.FindAllStringSubmatch(tag, -1) {
		if len(match) >= 3 {
			attrs[strings.ToLower(html.UnescapeString(match[1]))] = html.UnescapeString(match[2])
		}
	}
	return attrs
}

func forwardHeaders(c *gin.Context, resp *http.Response) {
	for _, header := range []string{"Content-Type", "Content-Length", "Accept-Ranges", "Content-Range"} {
		if v := resp.Header.Get(header); v != "" {
			c.Header(header, v)
		}
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" || (!strings.Contains(ct, "video") && !strings.Contains(ct, "octet-stream") && !strings.Contains(ct, "binary") && !strings.Contains(ct, "media")) {
		c.Header("Content-Type", "video/mp4")
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
