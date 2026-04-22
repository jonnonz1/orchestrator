"""Orchestrator REST client.

Uses only the standard library (urllib) to avoid pulling in requests — keeps
the install footprint tiny for ad-hoc scripts.
"""

from __future__ import annotations

import json
import time
from typing import Any, Dict, Iterator, List, Optional
from urllib import error as urlerror
from urllib import request as urlrequest
from urllib.parse import urljoin


class OrchestratorError(Exception):
    """Raised for non-2xx responses or client-side validation failures."""

    def __init__(self, message: str, status: int = 0, body: str = ""):
        super().__init__(message)
        self.status = status
        self.body = body


class Client:
    """Synchronous client for the Orchestrator REST API."""

    def __init__(self, base_url: str, token: Optional[str] = None, timeout: float = 30.0):
        self.base_url = base_url.rstrip("/") + "/"
        self.token = token
        self.timeout = timeout

    # ---- VM management ----

    def list_vms(self) -> List[Dict[str, Any]]:
        return self._request("GET", "/api/v1/vms")

    def create_vm(self, name: str, ram_mb: int = 2048, vcpus: int = 2) -> Dict[str, Any]:
        return self._request("POST", "/api/v1/vms", body={"name": name, "ram_mb": ram_mb, "vcpus": vcpus})

    def destroy_vm(self, name: str) -> None:
        self._request("DELETE", f"/api/v1/vms/{name}")

    # ---- Tasks ----

    def run_task(
        self,
        prompt: str,
        *,
        runtime: str = "claude",
        ram_mb: int = 2048,
        vcpus: int = 2,
        timeout: int = 600,
        max_turns: Optional[int] = None,
    ) -> Dict[str, Any]:
        """Submit a task and return immediately. Use wait() to block on completion."""
        body: Dict[str, Any] = {
            "prompt": prompt,
            "runtime": runtime,
            "ram_mb": ram_mb,
            "vcpus": vcpus,
            "timeout": timeout,
        }
        if max_turns is not None:
            body["max_turns"] = max_turns
        return self._request("POST", "/api/v1/tasks", body=body)

    def get_task(self, task_id: str) -> Dict[str, Any]:
        return self._request("GET", f"/api/v1/tasks/{task_id}")

    def list_tasks(self) -> List[Dict[str, Any]]:
        return self._request("GET", "/api/v1/tasks")

    def cancel_task(self, task_id: str) -> None:
        self._request("DELETE", f"/api/v1/tasks/{task_id}")

    def wait(self, task_id: str, poll_interval: float = 1.0, timeout: float = 600.0) -> Dict[str, Any]:
        """Poll until the task reaches a terminal status. Returns the final task dict."""
        deadline = time.time() + timeout
        while time.time() < deadline:
            t = self.get_task(task_id)
            if t["status"] in ("completed", "failed", "cancelled"):
                return t
            time.sleep(poll_interval)
        raise OrchestratorError(f"task {task_id} did not finish within {timeout}s")

    # ---- Result files ----

    def list_files(self, task_id: str) -> List[Dict[str, Any]]:
        return self._request("GET", f"/api/v1/tasks/{task_id}/files")

    def get_file(self, task_id: str, filename: str) -> bytes:
        req = self._build_request("GET", f"/api/v1/tasks/{task_id}/files/{filename}")
        with urlrequest.urlopen(req, timeout=self.timeout) as resp:
            return resp.read()

    # ---- Streaming (polling-based line iterator) ----

    def stream(self, task_id: str, poll_interval: float = 0.5) -> Iterator[str]:
        """Yield new stdout/stderr lines from a running task.

        Uses polling on the task's accumulated output rather than WebSocket,
        which keeps the SDK dependency-free. For lower latency use the
        /api/v1/tasks/{id}/stream WebSocket endpoint directly.
        """
        seen = 0
        while True:
            t = self.get_task(task_id)
            out = t.get("output") or ""
            if len(out) > seen:
                yield out[seen:]
                seen = len(out)
            if t["status"] in ("completed", "failed", "cancelled"):
                return
            time.sleep(poll_interval)

    # ---- Internals ----

    def _request(self, method: str, path: str, *, body: Optional[Dict[str, Any]] = None) -> Any:
        req = self._build_request(method, path, body=body)
        try:
            with urlrequest.urlopen(req, timeout=self.timeout) as resp:
                raw = resp.read()
        except urlerror.HTTPError as e:
            raise OrchestratorError(f"{method} {path} → {e.code}", status=e.code, body=e.read().decode(errors="replace")) from e
        except urlerror.URLError as e:
            raise OrchestratorError(f"{method} {path} → {e.reason}") from e

        if not raw:
            return None
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            return raw.decode(errors="replace")

    def _build_request(self, method: str, path: str, *, body: Optional[Dict[str, Any]] = None) -> urlrequest.Request:
        data = None
        headers = {"Accept": "application/json"}
        if self.token:
            headers["Authorization"] = f"Bearer {self.token}"
        if body is not None:
            data = json.dumps(body).encode("utf-8")
            headers["Content-Type"] = "application/json"
        return urlrequest.Request(urljoin(self.base_url, path.lstrip("/")), method=method, data=data, headers=headers)
