"""Fake external MCP/Claude boundary for runner integration tests only."""
import json
import os
from pathlib import Path
import sys
import subprocess
import signal
import time

mode = sys.argv[1]
log = Path(os.environ["TEST_CALL_LOG"])
if mode == "mcp":
    for line in sys.stdin:
        request = json.loads(line)
        if "id" not in request:
            continue
        if request["method"] == "initialize":
            if os.environ.get("TEST_HUGE_REQUEST"):
                print(json.dumps({"jsonrpc": "2.0", "id": "x" * 100000, "method": "unsupported/request"}), flush=True)
                time.sleep(20)
            result = {"protocolVersion": request["params"]["protocolVersion"],
                      "capabilities": {"tools": {}}, "serverInfo": {"name": "fake", "version": "1"}}
            if os.environ.get("TEST_BAD_INIT"):
                result = []
        elif request["method"] == "tools/call":
            with log.open("a") as output:
                output.write(json.dumps(request["params"]) + "\n")
            category = os.environ.get("TEST_CATEGORY", "bug_candidate")
            payload = {"tool": "jev_classify", "model": os.environ.get("TEST_MODEL", "jev-1.13.0"),
                       "results": [{"id": "task", "classification": category,
                                    "top_probability": 0.99, "margin": 0.98,
                                    "confidence": 0.98, "decision": "auto",
                                    "probabilities": {category: 0.99}}],
                       "usage": {"input_tokens": 100, "output_tokens": 20}}
            result = {"content": [{"type": "text", "text": json.dumps(payload)}]}
        else:
            result = {"tools": [{"name": "jev_classify", "inputSchema": {"type": "object"}}]}
        print(json.dumps({"jsonrpc": "2.0", "id": request["id"], "result": result}), flush=True)
else:
    with log.open("a") as output:
        output.write(json.dumps({"worker_args": sys.argv[2:], "cwd": os.getcwd()}) + "\n")
    sys.stdin.read()
    if mode == "slow":
        time.sleep(20)
    elif mode in {"noisy_stdout", "noisy_stderr"}:
        stream = sys.stdout.buffer if mode == "noisy_stdout" else sys.stderr.buffer
        for _ in range(256):
            stream.write(b"x" * 8192)
            stream.flush()
        time.sleep(20)
    elif mode == "fail":
        print(json.dumps({"type": "result", "subtype": "error_max_budget_usd",
                          "is_error": True, "session_id": "fake-budget-session"}))
    elif mode == "partial_failure":
        Path("docs").mkdir(exist_ok=True)
        Path("docs/result.md").write_text("Partial result\n")
        sys.exit(1)
    else:
        target = Path("docs/result.md") if mode != "out_of_scope" else Path("README.md")
        target.parent.mkdir(exist_ok=True)
        target.write_text("Test result\n")
        if mode == "symlink":
            Path("docs/a").write_text("a")
            Path("docs/b").write_text("b")
            Path("docs/link").symlink_to("a")
        if mode == "descendant":
            heartbeat = os.environ["TEST_CALL_LOG"] + ".heartbeat"
            child = subprocess.Popen([sys.executable, "-c",
                                      "import signal,time; from pathlib import Path; signal.signal(signal.SIGTERM, signal.SIG_IGN); "
                                      f"p=Path({heartbeat!r});\nwhile True: p.write_text(str(time.time_ns())); time.sleep(0.01)"],
                                     stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            Path(os.environ["TEST_CALL_LOG"] + ".pid").write_text(str(child.pid))
            deadline = time.monotonic() + 2
            while not Path(heartbeat).exists() and time.monotonic() < deadline:
                time.sleep(0.01)
        print(json.dumps({"type": "result", "subtype": "success", "is_error": False,
                          "session_id": "fake-session", "total_cost_usd": 0.01,
                          "result": "Fictional change ready for review."}))
