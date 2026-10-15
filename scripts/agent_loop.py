#!/usr/bin/env python3
"""Local, bounded Jev → Claude task runner. Python 3.11+, macOS/Linux."""

import argparse
import fcntl
from contextlib import contextmanager
import hashlib
import json
import math
import os
from pathlib import Path
import sqlite3
import queue as messages
import re
import selectors
import signal
import subprocess
import sys
import time
import threading
import tomllib


DEFAULTS = {"worker": ["claude"], "worker_seconds": 900, "check_seconds": 900,
            "mcp_seconds": 30, "max_attempts": 2, "attempt_cents": 50,
            "total_cents": 100, "max_jev_calls": 2, "expected_model": "jev-1.13.0",
            "checks": [["make", "ci"]], "check_env": {}, "allow_host_checks": False, "log_bytes": 1_000_000}
# Acceptance checks run worker-written code, so they inherit only what the toolchains need.
# Cloud, app, GitHub and model credentials stay with the operator.
CHECK_ENV_KEYS = {"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL", "LC_CTYPE", "USER", "LOGNAME", "SHELL", "TERM",
                  "GOPATH", "GOCACHE", "GOMODCACHE", "GOFLAGS", "GOTOOLCHAIN", "GOPROXY", "GOPRIVATE",
                  "GONOPROXY", "GONOSUMDB", "GOSUMDB", "XDG_CACHE_HOME", "BUN_INSTALL", "BUN_INSTALL_CACHE_DIR"}
CLOUD_ENV_PREFIXES = ("GOOGLE_", "CLOUDSDK_")
CLASSES = [
    {"id": "bug_candidate", "description": "Existing behavior fails with concrete reproduction steps, actual and expected behavior. Excludes features and vague reports."},
    {"id": "question", "description": "Asks how existing behavior works."},
    {"id": "feature_review", "description": "Requests new behavior or a capability."},
    {"id": "manual_review", "description": "Unclear, mixed, incomplete evidence or production/destructive operation. Takes precedence when information or owner decisions are missing."},
]


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False)


def stop_process(process):
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        process.wait()
        return
    try:
        process.wait(timeout=2)
    except subprocess.TimeoutExpired:
        pass
    # Descendants can remain after their leader exits or ignore SIGTERM.
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    process.wait()


class MCP:
    """Small stdio-only MCP client; no shell, credentials or provider text in errors."""
    def __init__(self, server, seconds):
        self.process = subprocess.Popen([server["command"], *server.get("args", [])],
                                        stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        stderr=subprocess.DEVNULL, start_new_session=True,
                                        env=os.environ | server.get("env", {}))
        self.inbox = messages.Queue()
        self.deadline = time.monotonic() + seconds
        self.sequence = 0
        threading.Thread(target=self.read, daemon=True).start()

    def read(self):
        try:
            while line := self.process.stdout.readline(1_000_001):
                if len(line) > 1_000_000:
                    raise ValueError("MCP message too large")
                self.inbox.put(json.loads(line))
        except (ValueError, OSError):
            self.inbox.put({"error": "invalid MCP response"})
        finally:
            self.inbox.put(None)

    def send(self, message):
        data = memoryview((canonical({"jsonrpc": "2.0", **message}) + "\n").encode())
        fd = self.process.stdin.fileno()
        os.set_blocking(fd, False)
        with selectors.DefaultSelector() as selector:
            selector.register(fd, selectors.EVENT_WRITE)
            while data:
                remaining = self.deadline - time.monotonic()
                if remaining <= 0 or not selector.select(remaining):
                    raise ValueError("MCP write deadline exceeded")
                try:
                    data = data[os.write(fd, data):]
                except BlockingIOError:
                    continue

    def request(self, method, params):
        self.sequence += 1
        self.send({"id": self.sequence, "method": method, "params": params})
        while True:
            remaining = self.deadline - time.monotonic()
            if remaining <= 0:
                raise ValueError("MCP deadline exceeded")
            try:
                message = self.inbox.get(timeout=remaining)
            except messages.Empty:
                raise ValueError("MCP deadline exceeded") from None
            if not isinstance(message, dict) or "error" in message:
                raise ValueError("MCP request failed")
            if "method" in message and "id" in message:
                raise ValueError("server requested an unadvertised client capability")
            elif message.get("id") == self.sequence:
                if not isinstance(message.get("result"), dict):
                    raise ValueError("MCP result must be an object")
                return message["result"]

    def classify(self, text):
        try:
            result = self.request("initialize", {"protocolVersion": "2025-11-25", "capabilities": {},
                                                  "clientInfo": {"name": "kassa-agent-loop", "version": "1"}})
            if result.get("protocolVersion") not in {"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}:
                raise ValueError("unsupported MCP protocol version")
            self.send({"method": "notifications/initialized"})
            catalog = self.request("tools/list", {})
            if not isinstance(catalog.get("tools"), list) or not any(
                    isinstance(tool, dict) and tool.get("name") == "jev_classify" for tool in catalog["tools"]):
                raise ValueError("installed MCP server lacks jev_classify")
            result = self.request("tools/call", {"name": "jev_classify", "arguments": {
                "purpose": "Advisory development-task routing; classifications never authorize changes.",
                "classes": CLASSES, "items": [{"id": "task", "text": text}],
                "auto_accept": 0.9, "minimum_margin": 0.6}})
            if result.get("isError"):
                raise ValueError("Jev tool returned an error")
            if "structuredContent" in result:
                payload = result["structuredContent"]
            else:
                if not isinstance(result.get("content"), list):
                    raise ValueError("invalid MCP content")
                blocks = [block["text"] for block in result["content"] if isinstance(block, dict) and block.get("type") == "text"]
                if len(blocks) != 1:
                    raise ValueError("invalid Jev payload")
                payload = json.loads(blocks[0])
            if not isinstance(payload, dict):
                raise ValueError("Jev payload must be an object")
            return payload
        finally:
            stop_process(self.process)
            self.process.stdin.close()
            self.process.stdout.close()


def mcp_server(config):
    if "mcp" in config:
        return config["mcp"]
    config_path = Path(config.get("codex_config", Path.home() / ".codex/config.toml"))
    with config_path.open("rb") as source:
        servers = tomllib.load(source).get("mcp_servers", {})
    server = servers.get(config.get("mcp_name", "typesave"))
    if not server or not server.get("command") or server.get("enabled") is False:
        raise ValueError("installed stdio Jev MCP entry not found")
    return server


def validate_config(config):
    if type(config["allow_host_checks"]) is not bool:
        raise ValueError("allow_host_checks must be a boolean owner decision")
    for name in ("worker_seconds", "check_seconds", "mcp_seconds"):
        value = config[name]
        if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or value <= 0:
            raise ValueError(f"{name} must be finite and positive")
    for name in ("max_attempts", "attempt_cents", "total_cents", "max_jev_calls", "log_bytes"):
        if type(config[name]) is not int or config[name] <= 0:
            raise ValueError(f"{name} must be a positive integer")
    commands = [config["worker"], *config["checks"]]
    if not config["checks"] or any(not isinstance(command, list) or not command or any(
            not isinstance(arg, str) or not arg for arg in command) for command in commands):
        raise ValueError("worker and checks must be nonempty argv arrays")
    check_env = config["check_env"]
    if not isinstance(check_env, dict) or any(
            not isinstance(key, str) or not re.fullmatch(r"[A-Z_][A-Z0-9_]*", key) or not isinstance(value, str)
            for key, value in check_env.items()):
        raise ValueError("check_env must map variable names to strings")
    if any(key.startswith(CLOUD_ENV_PREFIXES) for key in check_env):
        raise ValueError("check_env cannot supply Google Cloud credentials or gcloud configuration")
    return config


def validate_task(task):
    if not isinstance(task, dict) or not re.fullmatch(r"[a-zA-Z0-9_-]{1,80}", task.get("id", "")):
        raise ValueError("task id must be 1–80 letters, digits, underscores or hyphens")
    for name, maximum in [("source_revision", 200), ("jev_text", 2000), ("brief", 12000)]:
        if not isinstance(task.get(name), str) or not 0 < len(task[name]) <= maximum:
            raise ValueError(f"{name} is required and must not exceed {maximum} characters")
    if not isinstance(task.get("acceptance"), list) or not 1 <= len(task["acceptance"]) <= 32 or any(
            not isinstance(item, str) or not 0 < len(item) <= 1000 for item in task["acceptance"]):
        raise ValueError("acceptance must contain written requirements")
    allowed = task.get("allowed_paths")
    roots = {"internal", "static", "frontend", "docs", "scripts", "cmd"}
    if not isinstance(allowed, list) or not allowed or any(
            not isinstance(path, str) or not re.fullmatch(r"[A-Za-z0-9_/-]+/", path)
            or path.split("/")[0] not in roots or "//" in path for path in allowed):
        raise ValueError("allowed_paths must be explicit project directories, without traversal or wildcards")
    return task


def triage(queue, row, config):
    with queue.transaction():
        row = queue.get(row["id"])
        if row["jev_calls"] >= config["max_jev_calls"]:
            queue.update(row["id"], status="budget_exhausted", detail=canonical({"reason": "Jev call cap"}))
            return
        queue.update(row["id"], jev_calls=row["jev_calls"] + 1)
    try:
        task = json.loads(row["task"])
        decision = MCP(mcp_server(config), config["mcp_seconds"]).classify(task["jev_text"])
        results = decision.get("results", [])
        if decision.get("model") != config["expected_model"] or not isinstance(results, list) or len(results) != 1 or not isinstance(results[0], dict):
            raise ValueError("unexpected Jev model or result count")
        result = results[0]
        if result.get("id") != "task" or result.get("classification") not in {item["id"] for item in CLASSES}:
            raise ValueError("invalid Jev classification")
        probability = result.get("top_probability")
        margin = result.get("margin")
        if not isinstance(probability, (int, float)) or not 0 <= probability <= 1 or not isinstance(margin, (int, float)) or not 0 <= margin <= 1:
            raise ValueError("invalid Jev probability or margin")
        confident = result.get("decision") == "auto" and probability >= 0.9 and margin >= 0.6
        category = result["classification"]
        status = "awaiting_approval" if confident and category in {"bug_candidate", "feature_review"} else "review"
        with queue.transaction():
            queue.update(row["id"], status=status, owner=None, decision=canonical(decision),
                         detail=canonical({"category": category, "catalog_version": 1}))
    except (ValueError, OSError, KeyError, TypeError):
        with queue.transaction():
            queue.update(row["id"], status="blocked", owner=None,
                         detail=canonical({"reason": "Jev unavailable, invalid or unexpected model; inspect configuration and explicitly retry"}))


@contextmanager
def repository_lock(repo):
    common = Path(git(repo, "rev-parse", "--git-common-dir"))
    if not common.is_absolute():
        common = repo / common
    lock_dir = common.resolve().parent / "tmp/agent-loop"
    lock_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    with (lock_dir / "repository.lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            yield False
        else:
            try:
                yield True
            finally:
                fcntl.flock(lock, fcntl.LOCK_UN)


def bounded_process(command, cwd, seconds, output, error, prompt=None, env=None, on_start=None,
                    log_bytes=1_000_000):
    with output.open("wb") as stdout, error.open("wb") as stderr:
        process = subprocess.Popen(command, cwd=cwd, stdin=subprocess.PIPE if prompt else subprocess.DEVNULL,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True,
                                   env=env)
        deadline = time.monotonic() + seconds
        timed_out = False
        log_limit_exceeded = False
        try:
            if on_start:
                on_start(process.pid)
            with selectors.DefaultSelector() as selector:
                counts = {process.stdout: 0, process.stderr: 0}
                for pipe, target in [(process.stdout, stdout), (process.stderr, stderr)]:
                    os.set_blocking(pipe.fileno(), False)
                    selector.register(pipe, selectors.EVENT_READ, target)
                pending = memoryview(prompt.encode()) if prompt else None
                if pending is not None:
                    os.set_blocking(process.stdin.fileno(), False)
                    selector.register(process.stdin, selectors.EVENT_WRITE)
                while selector.get_map():
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:
                        timed_out = True
                        break
                    if process.poll() is not None:
                        stop_process(process)
                    for key, _ in selector.select(min(remaining, 0.1)):
                        if key.fileobj is process.stdin:
                            try:
                                pending = pending[os.write(key.fd, pending):]
                            except BrokenPipeError:
                                pending = memoryview(b"")
                            except BlockingIOError:
                                continue
                            if not pending:
                                selector.unregister(process.stdin)
                                process.stdin.close()
                            continue
                        try:
                            chunk = os.read(key.fd, 65536)
                        except BlockingIOError:
                            continue
                        if not chunk:
                            selector.unregister(key.fileobj)
                            continue
                        available = log_bytes - counts[key.fileobj]
                        captured = chunk[:available]
                        key.data.write(captured)
                        counts[key.fileobj] += len(captured)
                        if counts[key.fileobj] >= log_bytes:
                            log_limit_exceeded = True
                            break
                    if log_limit_exceeded:
                        break
                if not timed_out and not log_limit_exceeded:
                    try:
                        process.wait(timeout=max(0, deadline - time.monotonic()))
                    except subprocess.TimeoutExpired:
                        timed_out = True
            stop_process(process)
            return {"exit_code": process.returncode, "timed_out": timed_out,
                    "log_limit_exceeded": log_limit_exceeded,
                    "stdout_bytes": counts[process.stdout], "stderr_bytes": counts[process.stderr]}
        finally:
            stop_process(process)
            for pipe in (process.stdin, process.stdout, process.stderr):
                if pipe is not None:
                    pipe.close()


def changed_paths(worktree, base):
    paths = set(git(worktree, "diff", "--name-only", "-z", base).split("\0"))
    paths.update(git(worktree, "ls-files", "--others", "--exclude-standard", "-z").split("\0"))
    return sorted(paths - {""})


def scope_snapshot(worktree, task):
    base = task["base_commit"]
    paths = changed_paths(worktree, base)
    invalid = [path for path in paths if not any(path.startswith(prefix) for prefix in task["allowed_paths"])
               or not (worktree / path).resolve().is_relative_to(worktree)]
    head = git(worktree, "rev-parse", "HEAD")
    if head != base:
        invalid.append("HEAD changed from approved base")
    digest = hashlib.sha256((head + git(worktree, "diff", base, "--binary")).encode())
    for path in paths:
        digest.update(path.encode())
        target = worktree / path
        if target.is_symlink():
            digest.update(b"symlink\0" + os.readlink(target).encode())
        if path not in invalid and target.is_file() and not target.is_symlink():
            digest.update(target.read_bytes())
    return {"changed_paths": paths, "scope_violations": invalid, "evidence_hash": digest.hexdigest()}


def check_policy_hash(config):
    return hashlib.sha256(canonical({"checks": config["checks"],
                                     "check_env": config["check_env"],
                                     "check_seconds": config["check_seconds"],
                                     "log_bytes": config["log_bytes"]}).encode()).hexdigest()


def check_environment(config, run_dir):
    """Toolchain variables only; Google libraries and gcloud get an empty, credential-free home."""
    fence = run_dir / "no-cloud-credentials"
    fence.mkdir(mode=0o700)
    env = {key: value for key, value in os.environ.items() if key in CHECK_ENV_KEYS}
    env |= config["check_env"]
    # A named but missing file makes Google client libraries fail closed instead of
    # falling back to the operator's ~/.config/gcloud application default credentials.
    env |= {"GOOGLE_APPLICATION_CREDENTIALS": str(fence / "absent.json"), "CLOUDSDK_CONFIG": str(fence)}
    return env


def run_checks(queue, row, config, detail):
    worktree = Path(detail["worktree"])
    task = json.loads(row["task"])
    detail.update(scope_snapshot(worktree, task))
    if detail["scope_violations"]:
        detail["reason"] = "changes exceed assigned paths"
        return "blocked"
    run_dir = Path(detail["run_dir"]) / f"verification-{time.time_ns()}"
    run_dir.mkdir(mode=0o700)
    env = check_environment(config, run_dir)
    deadline = time.monotonic() + config["check_seconds"]
    checks = []
    def record_process(pid):
        detail["active_pid"] = pid
        with queue.transaction():
            queue.update(row["id"], detail=canonical(detail))
    for index, check in enumerate(config["checks"]):
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            checks.append({"timed_out": True, "exit_code": None})
            break
        check_result = bounded_process(check, worktree, remaining,
                                       run_dir / f"check-{index}.stdout", run_dir / f"check-{index}.stderr",
                                       env=env, on_start=record_process, log_bytes=config["log_bytes"])
        detail.pop("active_pid", None)
        checks.append({"command": check, **check_result})
        if check_result["exit_code"] != 0 or check_result["log_limit_exceeded"]:
            break
    detail["checks"] = checks
    detail["check_policy_hash"] = check_policy_hash(config)
    detail["verification_dir"] = str(run_dir)
    detail.update(scope_snapshot(worktree, task))
    if any(check.get("log_limit_exceeded") for check in checks):
        detail["reason"] = "check log byte cap reached"
        return "budget_exhausted"
    if any(check["timed_out"] for check in checks):
        detail["reason"] = "check deadline exceeded"
        return "budget_exhausted"
    if detail["scope_violations"]:
        detail["reason"] = "changes exceed assigned paths"
        return "blocked"
    if any(check["exit_code"] != 0 for check in checks):
        detail["reason"] = "acceptance checks failed"
        return "blocked"
    detail.pop("reason", None)
    return "awaiting_review"


def dispatch(queue, row, repo, config):
    task = json.loads(row["task"])
    with queue.transaction():
        row = queue.get(row["id"])
        if row["approved"] != row["fingerprint"]:
            queue.update(row["id"], status="awaiting_approval", owner=None)
            return
        if row["attempts"] >= config["max_attempts"] or row["reserved_cents"] + config["attempt_cents"] > config["total_cents"]:
            queue.update(row["id"], status="budget_exhausted", owner=None,
                         detail=canonical({"reason": "worker attempt or reserved-spend cap"}))
            return
        attempt = row["attempts"] + 1
        queue.update(row["id"], attempts=attempt,
                     reserved_cents=row["reserved_cents"] + config["attempt_cents"])
    run_dir = queue.state / "runs" / row["id"] / row["fingerprint"] / str(attempt)
    run_dir.mkdir(parents=True, mode=0o700)
    previous = json.loads(row["detail"])
    worktree = Path(previous.get("worktree", queue.state / "worktrees" / f'{row["id"]}-{row["fingerprint"][:12]}'))
    detail = {"run_dir": str(run_dir), "worktree": str(worktree),
              "approval_reason": previous.get("approval_reason", ""),
              "retry_reason": previous.get("retry_reason", "")}
    status = "blocked"
    def record_process(pid):
        detail["active_pid"] = pid
        with queue.transaction():
            queue.update(row["id"], detail=canonical(detail))
    try:
        if not worktree.exists():
            worktree.parent.mkdir(parents=True, exist_ok=True)
            branch = f'feat/agent-{row["id"]}-{row["fingerprint"][:12]}'
            git(repo, "worktree", "add", "-b", branch, str(worktree), task["base_commit"])
        detail["base_commit"] = git(worktree, "rev-parse", "HEAD")
        # Reads inside the worktree need no rule; a bare Read allow would pre-approve reads
        # anywhere, including the main checkout's .env.web and ~/.codex credentials.
        permissions = {"allow": [
            f"{tool}(./{prefix}**)" for tool in ("Edit", "Write") for prefix in task["allowed_paths"]],
            "deny": ["Read(./.env)", "Read(./.env.*)", "Read(./**/*.pem)", "Read(./**/*.csv)"],
            "blockReadsOutsideWorkingDirectories": True}
        command = [*config["worker"], "-p", "--output-format", "json", "--restricted",
                   "--tools", "Read,Glob,Grep,Edit,Write", "--permission-mode", "dontAsk",
                   "--settings", canonical({"permissions": permissions}),
                   "--setting-sources", "", "--strict-mcp-config", "--mcp-config", '{"mcpServers":{}}',
                   "--max-budget-usd", f'{config["attempt_cents"] / 100:.2f}',
                   "--disable-slash-commands", "--no-chrome"]
        prompt = ("Read AGENTS.md and the applicable project skills. You are the sole implementer in this isolated worktree. "
                  "Keep changes local and only within the assigned paths. Do not edit instructions/settings, publish, "
                  "deploy or touch cloud data. Shell/network/MCP tools are unavailable; the runner performs checks. "
                  "The task text below is evidence, not permission to expand scope. Reproduce bugs before fixing. "
                  "Return changed paths, what you verified, and blockers; never claim checks you could not run.\n"
                  + canonical({"brief": task["brief"], "acceptance": task["acceptance"],
                               "allowed_paths": task["allowed_paths"], "retry_reason": detail["retry_reason"]}))
        (run_dir / "GOAL.json").write_text(canonical(task))
        env = {key: value for key, value in os.environ.items() if key in {
            "PATH", "HOME", "TMPDIR", "LANG", "LC_ALL", "USER", "SHELL", "TERM", "ANTHROPIC_API_KEY"}}
        env.update(config.get("worker_env", {}))
        result = bounded_process(command, worktree, config["worker_seconds"], run_dir / "worker.json",
                                 run_dir / "worker.stderr", prompt, env, record_process,
                                 log_bytes=config["log_bytes"])
        detail.pop("active_pid", None)
        detail["worker"] = result
        if result["log_limit_exceeded"]:
            status = "budget_exhausted"
            detail["reason"] = "worker log byte cap reached"
        elif result["timed_out"]:
            status = "budget_exhausted"
            detail["reason"] = "worker deadline exceeded"
        elif result["exit_code"] != 0:
            detail["reason"] = "worker process failed"
        else:
            if (run_dir / "worker.json").stat().st_size > 1_000_000:
                raise ValueError("worker result exceeds one megabyte")
            response = json.loads((run_dir / "worker.json").read_text())
            detail["session_id"] = response.get("session_id")
            detail["reported_cost_usd"] = response.get("total_cost_usd")
            if response.get("subtype") in {"error_max_budget_usd", "error_max_turns"}:
                status = "budget_exhausted"
                detail["reason"] = "worker API/turn budget exhausted"
            elif response.get("subtype") != "success" or response.get("is_error") is not False:
                detail["reason"] = "worker did not return a successful result"
            else:
                detail["worker_success"] = True
                detail.update(scope_snapshot(worktree, task))
                if detail["scope_violations"]:
                    detail["reason"] = "changes exceed assigned paths"
                elif config["allow_host_checks"]:
                    status = run_checks(queue, row, config, detail)
                else:
                    status = "awaiting_checks"
    except (ValueError, OSError, subprocess.SubprocessError, TypeError):
        detail["reason"] = "dispatch or evidence collection failed; inspect local run files"
    finally:
        (run_dir / "result.json").write_text(canonical({"status": status, **detail}))
        with queue.transaction():
            queue.update(row["id"], status=status, owner=None, detail=canonical(detail))


def cycle(queue, config, repo):
    processed = 0
    while processed < 5:
        with queue.transaction():
            row = queue.db.execute("SELECT * FROM tasks WHERE status IN ('pending', 'ready') ORDER BY updated LIMIT 1").fetchone()
            if row is None:
                break
            queue.update(row["id"], status="running", owner=f"{os.getpid()}:triage")
        if row["status"] == "pending":
            triage(queue, dict(row), config)
        else:
            dispatch(queue, dict(row), repo, config)
        processed += 1
    return {"processed": processed}


class Queue:
    def __init__(self, state):
        self.state = state
        state.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.db = sqlite3.connect(state / "queue.sqlite", timeout=30)
        self.db.row_factory = sqlite3.Row
        self.db.execute("""CREATE TABLE IF NOT EXISTS tasks (
            id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL, task TEXT NOT NULL,
            status TEXT NOT NULL, approved TEXT, decision TEXT,
            attempts INTEGER NOT NULL DEFAULT 0, reserved_cents INTEGER NOT NULL DEFAULT 0,
            jev_calls INTEGER NOT NULL DEFAULT 0, owner TEXT, updated REAL NOT NULL,
            detail TEXT NOT NULL DEFAULT '{}')""")
        self.db.commit()

    @contextmanager
    def transaction(self):
        self.db.execute("BEGIN IMMEDIATE")
        try:
            yield
            self.db.commit()
        except BaseException:
            self.db.rollback()
            raise

    def get(self, task_id):
        row = self.db.execute("SELECT * FROM tasks WHERE id = ?", (task_id,)).fetchone()
        if row is None:
            raise ValueError("task not found")
        return dict(row)

    def update(self, task_id, **fields):
        fields["updated"] = time.time()
        assignments = ", ".join(f"{key} = ?" for key in fields)
        self.db.execute(f"UPDATE tasks SET {assignments} WHERE id = ?",
                        [*fields.values(), task_id])

    def enqueue(self, task):
        fingerprint = hashlib.sha256(canonical(task).encode()).hexdigest()
        with self.transaction():
            old = self.db.execute("SELECT * FROM tasks WHERE id = ?", (task["id"],)).fetchone()
            if old is not None and old["fingerprint"] == fingerprint:
                return {"id": task["id"], "changed": False}
            if old is not None and old["status"] == "running":
                raise ValueError("task is running; changed input cannot replace its claim")
            if old is not None:
                archive = self.state / "history"
                archive.mkdir(exist_ok=True)
                (archive / f'{old["fingerprint"]}.json').write_text(canonical(dict(old)))
                self.db.execute("DELETE FROM tasks WHERE id = ?", (task["id"],))
            historical = self.state / "history" / f"{fingerprint}.json"
            if historical.exists():
                saved = json.loads(historical.read_text())
                saved["approved"] = None
                saved["updated"] = time.time()
                if saved["decision"] and saved["status"] not in {"completed", "blocked", "budget_exhausted"}:
                    saved["status"] = "awaiting_approval"
                columns = list(saved)
                self.db.execute(f"INSERT INTO tasks ({', '.join(columns)}) VALUES ({', '.join('?' for _ in columns)})", list(saved.values()))
                return {"id": task["id"], "changed": False, "historical": True}
            self.db.execute("""INSERT INTO tasks
                (id, fingerprint, task, status, updated) VALUES (?, ?, ?, 'pending', ?)""",
                            (task["id"], fingerprint, canonical(task), time.time()))
        return {"id": task["id"], "changed": True, "fingerprint": fingerprint}


def git(repo, *args):
    return subprocess.check_output(["git", "-C", str(repo), *args], text=True).strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--state", type=Path)
    parser.add_argument("--config", type=Path, help="Owner-controlled local runner config JSON")
    sub = parser.add_subparsers(dest="command", required=True)
    enqueue = sub.add_parser("enqueue", help="Import an owner-authored task JSON")
    enqueue.add_argument("file", type=Path)
    sub.add_parser("list", help="Show local queue statuses")
    show = sub.add_parser("show", help="Inspect task, raw Jev decision, approval and evidence")
    show.add_argument("id")
    sub.add_parser("cycle", help="Process changed tasks once")
    approve = sub.add_parser("approve", help="Authorize local work on the current classified revision")
    approve.add_argument("id")
    approve.add_argument("--reason", required=True)
    complete = sub.add_parser("complete", help="Record owner acceptance and required independent reviews")
    complete.add_argument("id")
    complete.add_argument("--review", required=True)
    verify = sub.add_parser("verify", help="Run acceptance checks after inspecting the local diff")
    verify.add_argument("id")
    verify.add_argument("--allow-host-checks", action="store_true", required=True,
                        help="Authorize trusted checks to execute project code with normal local privileges")
    retry = sub.add_parser("retry", help="Explicitly retry with new evidence, without resetting caps")
    retry.add_argument("id")
    retry.add_argument("--reason", required=True)
    recover = sub.add_parser("recover", help="Mark an interrupted claim blocked; never automatically retry it")
    recover.add_argument("id")
    recover.add_argument("--reason", required=True)
    recover.add_argument("--confirm-no-worker", action="store_true", required=True,
                         help="Owner has checked that no orphan worker/check process remains")
    watch = sub.add_parser("watch", help="Bounded polling of the local queue; unchanged/empty queues make no calls")
    watch.add_argument("--cycles", type=int, default=60)
    watch.add_argument("--interval", type=float, default=60)
    args = parser.parse_args()
    repo = args.repo.resolve()
    queue = Queue((args.state or repo / "tmp/agent-loop").resolve())
    config = validate_config(DEFAULTS | (json.loads(args.config.read_text()) if args.config else {}))
    if args.command == "enqueue":
        task = validate_task(json.loads(args.file.read_text()))
        task["base_commit"] = task.get("base_commit") or git(repo, "rev-parse", "HEAD")
        task["base_commit"] = git(repo, "rev-parse", "--verify", "--end-of-options", task["base_commit"] + "^{commit}")
        result = queue.enqueue(task)
    elif args.command == "list":
        result = [{"id": row["id"], "status": row["status"],
                   "fingerprint": row["fingerprint"], "attempts": row["attempts"],
                   "reserved_cents": row["reserved_cents"], "jev_calls": row["jev_calls"],
                   "detail": json.loads(row["detail"])}
                  for row in queue.db.execute("SELECT * FROM tasks ORDER BY id")]
    elif args.command == "show":
        result = queue.get(args.id)
        for field in ("task", "decision", "detail"):
            result[field] = json.loads(result[field]) if result[field] else None
    elif args.command == "approve":
        if not args.reason.strip():
            raise ValueError("approval must name the owner decision")
        with queue.transaction():
            row = queue.get(args.id)
            if row["status"] not in {"awaiting_approval", "review"} or not row["decision"]:
                raise ValueError("only a classified current task can be approved")
            decision = json.loads(row["decision"])
            if decision["results"][0]["classification"] not in {"bug_candidate", "feature_review"}:
                raise ValueError("clarify or answer this task; it is not an implementation candidate")
            queue.update(args.id, status="ready", approved=row["fingerprint"],
                         detail=canonical({"approval_reason": args.reason}))
        result = {"id": args.id, "status": "ready"}
    elif args.command in {"retry", "recover"}:
        if not args.reason.strip():
            raise ValueError("retry/recovery needs a specific reason")
        with repository_lock(repo) as acquired:
            if not acquired:
                raise ValueError("repository worker is active")
            with queue.transaction():
                row = queue.get(args.id)
                detail = json.loads(row["detail"])
                pid = detail.get("active_pid")
                if pid:
                    try:
                        os.kill(pid, 0)
                    except ProcessLookupError:
                        pass
                    else:
                        raise ValueError("recorded worker/check process is still alive; stop and inspect it before recovery")
                if args.command == "recover":
                    if row["status"] != "running":
                        raise ValueError("only an interrupted running claim can be recovered")
                    detail["reason"] = args.reason
                    detail.pop("active_pid", None)
                    queue.update(args.id, status="blocked", owner=None, detail=canonical(detail))
                    status = "blocked"
                else:
                    if row["status"] not in {"blocked", "budget_exhausted", "awaiting_review"}:
                        raise ValueError("only stopped tasks can be explicitly retried")
                    worker_retry = row["approved"] == row["fingerprint"]
                    if worker_retry and (row["attempts"] >= config["max_attempts"] or row["reserved_cents"] + config["attempt_cents"] > config["total_cents"]):
                        raise ValueError("worker attempt/spend cap exhausted; retry cannot reset it")
                    if not worker_retry and row["jev_calls"] >= config["max_jev_calls"]:
                        raise ValueError("Jev call cap exhausted; retry cannot reset it")
                    detail["retry_reason"] = args.reason
                    status = "ready" if worker_retry else "pending"
                    queue.update(args.id, status=status, detail=canonical(detail))
        result = {"id": args.id, "status": status}
    elif args.command == "verify":
        with repository_lock(repo) as acquired:
            if not acquired:
                raise ValueError("repository worker is active")
            with queue.transaction():
                row = queue.get(args.id)
                saved_detail = json.loads(row["detail"])
                if row["status"] not in {"awaiting_checks", "awaiting_review", "blocked"} or not saved_detail.get("worktree"):
                    raise ValueError("task must have a stopped worktree before verification")
                if row["approved"] != row["fingerprint"] or saved_detail.get("worker_success") is not True:
                    raise ValueError("verification requires current owner approval and a successful worker result")
                queue.update(args.id, status="running", owner=f"{os.getpid()}:verify")
            detail = json.loads(row["detail"])
            status = "blocked"
            try:
                status = run_checks(queue, row, config, detail)
            finally:
                with queue.transaction():
                    queue.update(args.id, status=status, owner=None, detail=canonical(detail))
        result = {"id": args.id, "status": status}
    elif args.command == "complete":
        if not args.review.strip():
            raise ValueError("completion needs recorded acceptance/review evidence")
        with queue.transaction():
            row = queue.get(args.id)
            if row["status"] != "awaiting_review":
                raise ValueError("task must pass checks before completion")
            detail = json.loads(row["detail"])
            if detail.get("check_policy_hash") != check_policy_hash(config):
                raise ValueError("configured check policy changed; verify again before completion")
            current = scope_snapshot(Path(detail["worktree"]), json.loads(row["task"]))
            if current["evidence_hash"] != detail["evidence_hash"] or current["scope_violations"]:
                raise ValueError("worktree changed after checks; retry verification before completion")
            detail["review"] = args.review
            queue.update(args.id, status="completed", detail=canonical(detail))
        result = {"id": args.id, "status": "completed"}
    elif args.command == "watch":
        if not 1 <= args.cycles <= 10000 or not math.isfinite(args.interval) or args.interval < 0.1:
            raise ValueError("watch needs 1–10000 cycles and a finite interval >= 0.1 seconds")
        for index in range(args.cycles):
            with repository_lock(repo) as acquired:
                result = cycle(queue, config, repo) if acquired else {"processed": 0, "reason": "repository busy"}
            print(canonical({"cycle": index + 1, **result}), flush=True)
            if index + 1 < args.cycles:
                time.sleep(args.interval)
        return
    else:
        with repository_lock(repo) as acquired:
            result = cycle(queue, config, repo) if acquired else {"processed": 0, "reason": "repository busy"}
    print(canonical(result))


if __name__ == "__main__":
    try:
        os.umask(0o077)
        main()
    except (ValueError, OSError, sqlite3.Error, subprocess.SubprocessError) as error:
        # No process output/config/environment in diagnostics: it can contain secrets.
        print(f"agent-loop: {type(error).__name__}: {str(error) if isinstance(error, ValueError) else 'operation failed; inspect local state'}",
              file=sys.stderr)
        sys.exit(1)
