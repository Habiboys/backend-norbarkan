package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SidecarClient communicates with the Python yt-dlp sidecar service.
type SidecarClient struct {
	baseURL string
	client  *http.Client
}

type ExtractResult struct {
	Title         string  `json:"title"`
	Duration      float64 `json:"duration,omitempty"`
	Thumbnail     string  `json:"thumbnail,omitempty"`
	Extractor     string  `json:"extractor,omitempty"`
	ExtractorKey  string  `json:"extractor_key,omitempty"`
	WebpageURL    string  `json:"webpage_url,omitempty"`
}

type DownloadResult struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

type StatusResult struct {
	TaskID   string  `json:"task_id"`
	Status   string  `json:"status"`
	Progress float64 `json:"progress"`
	Error    string  `json:"error"`
}

type ExtractorsResult struct {
	Count      int      `json:"count"`
	Extractors []string `json:"extractors"`
}

func NewSidecarClient(baseURL string) *SidecarClient {
	return &SidecarClient{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (s *SidecarClient) Extract(url string) (*ExtractResult, error) {
	body := map[string]string{"url": url}
	payload, _ := json.Marshal(body)

	resp, err := s.client.Post(s.baseURL+"/extract", "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("sidecar extract request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sidecar extract returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result ExtractResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("sidecar extract decode failed: %w", err)
	}
	return &result, nil
}

func (s *SidecarClient) StartDownload(movieID, url string) (*DownloadResult, error) {
	body := map[string]string{"movie_id": movieID, "url": url}
	payload, _ := json.Marshal(body)

	resp, err := s.client.Post(s.baseURL+"/download", "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("sidecar download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sidecar download returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result DownloadResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("sidecar download decode failed: %w", err)
	}
	return &result, nil
}

func (s *SidecarClient) GetStatus(taskID string) (*StatusResult, error) {
	resp, err := s.client.Get(s.baseURL + "/status/" + taskID)
	if err != nil {
		return nil, fmt.Errorf("sidecar status request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sidecar status returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result StatusResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("sidecar status decode failed: %w", err)
	}
	return &result, nil
}

func (s *SidecarClient) ListExtractors() (*ExtractorsResult, error) {
	resp, err := s.client.Get(s.baseURL + "/extractors")
	if err != nil {
		return nil, fmt.Errorf("sidecar extractors request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sidecar extractors returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result ExtractorsResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("sidecar extractors decode failed: %w", err)
	}
	return &result, nil
}

type StreamURLResult struct {
	URL       string  `json:"url"`
	Title     string  `json:"title"`
	Duration  float64 `json:"duration,omitempty"`
	Thumbnail string  `json:"thumbnail,omitempty"`
	Extractor string  `json:"extractor,omitempty"`
}

func (s *SidecarClient) StreamURL(videoURL string) (*StreamURLResult, error) {
	body := map[string]string{"url": videoURL}
	payload, _ := json.Marshal(body)

	resp, err := s.client.Post(s.baseURL+"/stream", "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("sidecar stream request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sidecar stream returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result StreamURLResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("sidecar stream decode failed: %w", err)
	}
	return &result, nil
}
