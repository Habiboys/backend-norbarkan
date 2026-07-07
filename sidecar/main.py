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
COOKIES_FILE = os.environ.get("COOKIES_FILE", "")

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
    # Try without format selection first — if fails due to format issues, retry with ignore
    cmd = [sys.executable, "-m", "yt_dlp", "--dump-json", "--no-warnings",
           "--extractor-args", "youtube:player_client=web"]
    if COOKIES_FILE:
        cmd += ["--cookies", COOKIES_FILE]
    cmd += args
    result = subprocess.run(
        cmd,
        capture_output=True,
        text=True,
        timeout=30,
    )
    if result.returncode == 0 and result.stdout.strip():
        lines = result.stdout.strip().splitlines()
        return json.loads(lines[-1])

    # Retry with flat info (no format extraction) for videos with restricted formats
    cmd2 = [sys.executable, "-m", "yt_dlp", "--dump-json", "--no-warnings",
            "--ignore-no-formats-error", "--flat", "--extractor-args", "youtube:player_client=web"]
    if COOKIES_FILE:
        cmd2 += ["--cookies", COOKIES_FILE]
    cmd2 += args
    result2 = subprocess.run(
        cmd2,
        capture_output=True,
        text=True,
        timeout=30,
    )
    if result2.returncode == 0 and result2.stdout.strip():
        lines2 = result2.stdout.strip().splitlines()
        info = json.loads(lines2[-1])
        # Flat mode may miss some fields — fill defaults
        if "title" not in info or not info.get("title"):
            info["title"] = info.get("id", "Unknown")
        return info

    # Last resort: try --print title (just gets title from webpage, no format extraction)
    cmd3 = [sys.executable, "-m", "yt_dlp", "--print", "title", "--print", "duration", "--print", "thumbnail",
            "--extractor-args", "youtube:player_client=web"]
    if COOKIES_FILE:
        cmd3 += ["--cookies", COOKIES_FILE]
    cmd3 += args
    result3 = subprocess.run(
        cmd3,
        capture_output=True,
        text=True,
        timeout=30,
    )
    if result3.returncode == 0 and result3.stdout.strip():
        lines3 = result3.stdout.strip().splitlines()
        title = lines3[0] if len(lines3) > 0 else "Unknown"
        dur_str = lines3[1] if len(lines3) > 1 else "0"
        thumb = lines3[2] if len(lines3) > 2 else ""
        try:
            dur = float(dur_str)
        except ValueError:
            dur = 0
        return {"title": title, "duration": dur, "thumbnail": thumb}

    raise RuntimeError(f"yt-dlp extract failed: {result.stderr.strip()}")


def run_ytdlp_download(args: list[str]) -> subprocess.CompletedProcess:
    """Run yt-dlp download, return CompletedProcess."""
    cmd = [sys.executable, "-m", "yt_dlp", "--no-warnings"]
    if COOKIES_FILE:
        cmd += ["--cookies", COOKIES_FILE]
    cmd += args
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
        # Try with format selector first, then fall back
        direct_url = None

        # Attempt 1: with format
        cmd1 = [sys.executable, "-m", "yt_dlp", "-g", "-f", "best[ext=mp4]/bestvideo[ext=mp4]+bestaudio[ext=m4a]/best",
                "--no-warnings", "--extractor-args", "youtube:player_client=web"]
        if COOKIES_FILE:
            cmd1 += ["--cookies", COOKIES_FILE]
        cmd1 += [req.url]
        result1 = subprocess.run(cmd1, capture_output=True, text=True, timeout=30)
        if result1.returncode == 0:
            urls = result1.stdout.strip().splitlines()
            if urls:
                direct_url = urls[-1]

        # Attempt 2: no format restriction (yt-dlp picks best)
        if not direct_url:
            cmd2 = [sys.executable, "-m", "yt_dlp", "-g", "--no-warnings",
                    "--extractor-args", "youtube:player_client=web"]
            if COOKIES_FILE:
                cmd2 += ["--cookies", COOKIES_FILE]
            cmd2 += [req.url]
            result2 = subprocess.run(cmd2, capture_output=True, text=True, timeout=30)
            if result2.returncode == 0:
                urls = result2.stdout.strip().splitlines()
                if urls:
                    direct_url = urls[-1]

        # Attempt 3: --ignore-no-formats-error
        if not direct_url:
            cmd3 = [sys.executable, "-m", "yt_dlp", "-g", "--ignore-no-formats-error", "--no-warnings",
                    "--extractor-args", "youtube:player_client=web"]
            if COOKIES_FILE:
                cmd3 += ["--cookies", COOKIES_FILE]
            cmd3 += [req.url]
            result3 = subprocess.run(cmd3, capture_output=True, text=True, timeout=30)
            if result3.returncode == 0:
                urls = result3.stdout.strip().splitlines()
                if urls:
                    direct_url = urls[-1]

        if not direct_url:
            # Best effort: return last error
            err = result3.stderr.strip() if 'result3' in dir() else (result2.stderr.strip() if 'result2' in dir() else result1.stderr.strip())
            raise RuntimeError(f"yt-dlp stream failed: {err}")

        # Get metadata for response
        try:
            meta = run_ytdlp_json([req.url])
        except Exception:
            meta = {"title": "Unknown", "duration": None, "thumbnail": None, "extractor": None}

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
