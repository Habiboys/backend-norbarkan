import json
import os
import subprocess
import sys
import threading
import time
import uuid
from datetime import datetime
from pathlib import Path

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, HttpUrl
import uvicorn

app = FastAPI(title="Nobarkan yt-dlp Sidecar")

SIDECAR_PORT = int(os.environ.get("SIDECAR_PORT", "5000"))
STORAGE_PATH = os.environ.get("STORAGE_PATH", "./storage/cache/external")
DOWNLOAD_TIMEOUT = int(os.environ.get("DOWNLOAD_TIMEOUT", "600"))  # 10 min default

os.makedirs(STORAGE_PATH, exist_ok=True)

# ---------------------------------------------------------------------------
# In-memory download tracking
# ---------------------------------------------------------------------------
download_tasks: dict[str, dict] = {}  # task_id -> task_info
tasks_lock = threading.Lock()


class TaskInfo:
    def __init__(self, movie_id: str, url: str, cache_path: str):
        self.movie_id = movie_id
        self.url = url
        self.cache_path = cache_path
        self.status = "pending"  # pending | downloading | done | failed
        self.progress = 0.0      # 0-100
        self.error = ""
        self.started_at = None
        self.completed_at = None
        self._process = None


# ---------------------------------------------------------------------------
# Models
# ---------------------------------------------------------------------------
class ExtractRequest(BaseModel):
    url: str


class ExtractResponse(BaseModel):
    title: str
    duration: float | None = None
    thumbnail: str | None = None
    extractor: str | None = None
    extractor_key: str | None = None
    webpage_url: str | None = None


class DownloadRequest(BaseModel):
    movie_id: str
    url: str


class DownloadResponse(BaseModel):
    task_id: str
    status: str


class StatusResponse(BaseModel):
    task_id: str
    status: str
    progress: float = 0.0
    error: str = ""


class StreamURLResponse(BaseModel):
    url: str
    title: str
    duration: float | None = None
    thumbnail: str | None = None
    extractor: str | None = None


# ---------------------------------------------------------------------------
# yt-dlp helpers
# ---------------------------------------------------------------------------

def run_ytdlp_json(args: list[str]) -> dict:
    """Run yt-dlp with --dump-json and return parsed dict."""
    cmd = [sys.executable, "-m", "yt_dlp", "--dump-json", "--no-warnings"] + args
    result = subprocess.run(
        cmd,
        capture_output=True,
        text=True,
        timeout=30,
    )
    if result.returncode != 0:
        raise RuntimeError(f"yt-dlp extract failed: {result.stderr.strip()}")
    if not result.stdout.strip():
        raise RuntimeError("yt-dlp returned empty output")
    lines = result.stdout.strip().splitlines()
    return json.loads(lines[-1])


def run_ytdlp_download(args: list[str]) -> subprocess.CompletedProcess:
    """Run yt-dlp download, return CompletedProcess."""
    cmd = [sys.executable, "-m", "yt_dlp", "--no-warnings"] + args
    return subprocess.run(cmd, capture_output=True, text=True, timeout=DOWNLOAD_TIMEOUT)


# ---------------------------------------------------------------------------
# Endpoints
# ---------------------------------------------------------------------------

@app.post("/extract", response_model=ExtractResponse)
def extract_metadata(req: ExtractRequest):
    """Extract video metadata from URL without downloading."""
    try:
        info = run_ytdlp_json([req.url])
        return ExtractResponse(
            title=info.get("title", "Unknown"),
            duration=info.get("duration"),
            thumbnail=info.get("thumbnail"),
            extractor=info.get("extractor"),
            extractor_key=info.get("extractor_key"),
            webpage_url=info.get("webpage_url"),
        )
    except RuntimeError as e:
        raise HTTPException(status_code=422, detail=str(e))
    except subprocess.TimeoutExpired:
        raise HTTPException(status_code=504, detail="yt-dlp timed out extracting metadata")
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Extraction failed: {str(e)}")


@app.post("/stream", response_model=StreamURLResponse)
def stream_url(req: ExtractRequest):
    """Get direct video URL for streaming. No download."""
    try:
        # yt-dlp -g gives direct media URL
        cmd = [sys.executable, "-m", "yt_dlp", "-g", "-f", "best", "--no-warnings", req.url]
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
        if result.returncode != 0:
            raise RuntimeError(f"yt-dlp stream failed: {result.stderr.strip()}")
        urls = result.stdout.strip().splitlines()
        if not urls:
            raise RuntimeError("yt-dlp returned no stream URL")
        direct_url = urls[-1]  # last line = video URL
        # Also get metadata for response
        meta = run_ytdlp_json([req.url])
        return StreamURLResponse(
            url=direct_url,
            title=meta.get("title", "Unknown"),
            duration=meta.get("duration"),
            thumbnail=meta.get("thumbnail"),
            extractor=meta.get("extractor"),
        )
    except RuntimeError as e:
        raise HTTPException(status_code=422, detail=str(e))
    except subprocess.TimeoutExpired:
        raise HTTPException(status_code=504, detail="yt-dlp timed out getting stream URL")
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Stream URL failed: {str(e)}")


@app.post("/download", response_model=DownloadResponse)
def start_download(req: DownloadRequest):
    """Start background download using yt-dlp."""
    movie_dir = os.path.join(STORAGE_PATH, req.movie_id)
    os.makedirs(movie_dir, exist_ok=True)
    cache_path = os.path.join(movie_dir, "video.mp4")

    task_id = str(uuid.uuid4())
    task = TaskInfo(
        movie_id=req.movie_id,
        url=req.url,
        cache_path=cache_path,
    )

    with tasks_lock:
        download_tasks[task_id] = task.__dict__

    thread = threading.Thread(
        target=_do_download,
        args=(task, task_id),
        daemon=True,
    )
    thread.start()

    return DownloadResponse(task_id=task_id, status="pending")


def _do_download(task: TaskInfo, task_id: str):
    """Background download worker."""
    def update(**kw):
        with tasks_lock:
            info = download_tasks.get(task_id)
            if info:
                info.update(kw)

    update(status="downloading", started_at=datetime.utcnow().isoformat())

    try:
        # Use output template to write to the target path
        ydl_opts = [
            "-f", "bestvideo+bestaudio/best",
            "--merge-output-format", "mp4",
            "-o", task.cache_path,
            "--no-part",        # don't use .part files
            "--no-mtime",
            task.url,
        ]
        proc = subprocess.Popen(
            [sys.executable, "-m", "yt_dlp", "--no-warnings"] + ydl_opts,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
        )
        task._process = proc

        # Read output line by line to extract progress
        progress_re = __import__("re").compile(r"\[download\]\s+([\d.]+)%")
        for line in proc.stdout or []:
            m = progress_re.search(line)
            if m:
                pct = float(m.group(1))
                update(progress=pct)

        proc.wait()

        if proc.returncode != 0:
            raise RuntimeError(f"yt-dlp exited with code {proc.returncode}")

        # Verify file exists and has size
        if not os.path.isfile(task.cache_path) or os.path.getsize(task.cache_path) == 0:
            raise RuntimeError("Download produced empty or missing file")

        update(status="done", progress=100.0, completed_at=datetime.utcnow().isoformat())

    except Exception as e:
        update(status="failed", error=str(e))
        # Clean up partial download
        if os.path.isfile(task.cache_path):
            try:
                os.remove(task.cache_path)
            except OSError:
                pass


@app.get("/status/{task_id}", response_model=StatusResponse)
def get_status(task_id: str):
    """Get download status for a task."""
    with tasks_lock:
        info = download_tasks.get(task_id)
    if not info:
        raise HTTPException(status_code=404, detail="Task not found")
    return StatusResponse(
        task_id=task_id,
        status=info.get("status", "unknown"),
        progress=info.get("progress", 0.0),
        error=info.get("error", ""),
    )


@app.get("/extractors")
def list_extractors():
    """List all yt-dlp supported extractors."""
    try:
        result = subprocess.run(
            [sys.executable, "-m", "yt_dlp", "--list-extractors"],
            capture_output=True, text=True, timeout=30,
        )
        if result.returncode != 0:
            raise RuntimeError(result.stderr.strip())
        lines = result.stdout.strip().splitlines()
        return {"count": len(lines), "extractors": lines}
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to list extractors: {str(e)}")


@app.get("/ping")
def ping():
    return {"status": "ok"}


# ---------------------------------------------------------------------------
# Health
# ---------------------------------------------------------------------------

@app.get("/health")
def health():
    """Check yt-dlp is available."""
    try:
        result = subprocess.run(
            [sys.executable, "-m", "yt_dlp", "--version"],
            capture_output=True, text=True, timeout=10,
        )
        yt_dlp_version = result.stdout.strip() if result.returncode == 0 else "unknown"
    except Exception as e:
        yt_dlp_version = f"error: {e}"

    return {
        "status": "ok",
        "yt_dlp_version": yt_dlp_version,
        "storage_path": STORAGE_PATH,
    }


if __name__ == "__main__":
    uvicorn.run("main:app", host="0.0.0.0", port=SIDECAR_PORT, log_level="info")
