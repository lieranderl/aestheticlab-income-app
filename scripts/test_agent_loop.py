"""Public CLI tests; external processes are replaced only at their boundaries."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import sqlite3
import unittest


RUNNER = Path(__file__).with_name("agent_loop.py")


class AgentLoopTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / "repo"
        self.repo.mkdir()
        for args in (["init", "-b", "main"], ["config", "user.email", "test@example.test"],
                     ["config", "user.name", "Test"]):
            subprocess.run(["git", "-C", str(self.repo), *args], check=True,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        (self.repo / "README.md").write_text("Fictional test repository\n")
        (self.repo / "AGENTS.md").write_text("Keep changes local.\n")
        subprocess.run(["git", "-C", str(self.repo), "add", "."], check=True)
        subprocess.run(["git", "-C", str(self.repo), "commit", "-qm", "initial"], check=True)
        self.state = self.root / "state"
        self.log = self.root / "calls.jsonl"
        self.config = self.root / "config.json"
        fixture = str(RUNNER.parent / "testdata/agent_process.py")
        self.settings = {"mcp": {"command": sys.executable, "args": [fixture, "mcp"],
                                 "env": {"TEST_CALL_LOG": str(self.log)}},
                         "worker": [sys.executable, fixture, "worker"],
                         "worker_env": {"TEST_CALL_LOG": str(self.log)},
                         "checks": [[sys.executable, "-c", "from pathlib import Path; assert Path('docs/result.md').exists()"]],
                         "worker_seconds": 5, "check_seconds": 5, "allow_host_checks": True}
        self.config.write_text(json.dumps(self.settings))

    def cli(self, *args, success=True, env=None):
        result = subprocess.run([sys.executable, str(RUNNER), "--repo", str(self.repo),
                                 "--state", str(self.state), "--config", str(self.config), *args],
                                capture_output=True, text=True, env=env)
        if success:
            self.assertEqual(result.returncode, 0, result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0)
        return json.loads(result.stdout) if result.stdout else result.stderr

    def task(self, **updates):
        task = {"id": "demo", "source_revision": "v1",
                "jev_text": "Fictional color picker stays red after selecting blue and applying; expected blue.",
                "brief": "Investigate this fictional color picker locally.",
                "acceptance": ["The preview reflects the selected color."],
                "allowed_paths": ["docs/"], "authorized": False}
        task.update(updates)
        path = self.root / "task.json"
        path.write_text(json.dumps(task))
        return path

    def calls(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()] if self.log.exists() else []

    def test_changed_tasks_are_triaged_once_and_require_explicit_current_approval(self):
        path = self.task()
        self.cli("enqueue", str(path))
        self.assertEqual(self.cli("cycle")["processed"], 1)
        self.assertEqual(self.cli("list")[0]["status"], "awaiting_approval")
        self.assertEqual(len(self.calls()), 1)
        sent = self.calls()[0]["arguments"]
        self.assertEqual(sent["items"][0]["text"], json.loads(path.read_text())["jev_text"])
        self.assertNotIn("brief", sent)
        self.assertEqual(self.cli("cycle")["processed"], 0)
        self.assertEqual(len(self.calls()), 1)
        self.cli("approve", "demo", "--reason", "Owner authorizes this fictional local change")
        self.cli("enqueue", str(self.task(source_revision="v2")))
        self.assertEqual(self.cli("list")[0]["status"], "pending")
        self.cli("cycle")
        self.assertEqual(self.cli("list")[0]["status"], "awaiting_approval")

    def test_unchanged_task_is_enqueued_once_and_empty_cycles_make_no_calls(self):
        path = self.task()
        first = self.cli("enqueue", str(path))
        second = self.cli("enqueue", str(path))
        self.assertEqual(first["id"], second["id"])
        self.assertFalse(second["changed"])
        self.assertEqual(len(self.cli("list")), 1)
        empty_state = self.root / "empty"
        self.state = empty_state
        self.assertEqual(self.cli("cycle"), {"processed": 0})

    def approved_task(self, task_id="demo"):
        self.cli("enqueue", str(self.task(id=task_id)))
        self.cli("cycle")
        self.cli("approve", task_id, "--reason", "Owner authorizes this fictional local change")

    def test_dispatch_is_isolated_bounded_and_requires_review_after_checks(self):
        self.approved_task()
        self.assertEqual(self.cli("cycle")["processed"], 1)
        row = self.cli("list")[0]
        self.assertEqual(row["status"], "awaiting_review")
        self.assertEqual(row["attempts"], 1)
        self.assertEqual(row["reserved_cents"], 50)
        self.assertFalse((self.repo / "docs/result.md").exists())
        detail = row["detail"]
        self.assertTrue((Path(detail["worktree"]) / "docs/result.md").exists())
        self.assertEqual(detail["session_id"], "fake-session")
        worker = self.calls()[-1]
        self.assertIn("--restricted", worker["worker_args"])
        self.assertIn("--permission-mode", worker["worker_args"])
        self.assertIn("dontAsk", worker["worker_args"])
        self.assertIn("--max-budget-usd", worker["worker_args"])
        self.assertIn("0.50", worker["worker_args"])
        self.assertNotIn("--dangerously-skip-permissions", worker["worker_args"])
        self.cli("complete", "demo", "--review", "Independent review found no actionable issues")
        self.assertEqual(self.cli("list")[0]["status"], "completed")
        self.cli("cycle")
        self.assertEqual(len(self.calls()), 2)

    def test_worker_reads_are_fenced_to_its_worktree(self):
        self.approved_task()
        self.cli("cycle")
        args = self.calls()[-1]["worker_args"]
        permissions = json.loads(args[args.index("--settings") + 1])["permissions"]
        self.assertIs(permissions["blockReadsOutsideWorkingDirectories"], True)
        self.assertFalse({"Read", "Glob", "Grep"} & set(permissions["allow"]),
                         "a bare read allow rule pre-approves reads anywhere on the host")
        self.assertEqual(permissions["allow"], ["Edit(./docs/**)", "Write(./docs/**)"])

    def test_acceptance_checks_cannot_inherit_cloud_or_app_credentials(self):
        probe = Path(self.temp.name) / "check_env.json"
        self.settings["checks"] = [[sys.executable, "-c",
                                    "import json, os, sys; json.dump(dict(os.environ), open(sys.argv[1], 'w'))",
                                    str(probe)]]
        self.settings["check_env"] = {"FIRESTORE_EMULATOR_HOST": "localhost:8086"}
        self.config.write_text(json.dumps(self.settings))
        operator = os.environ | {"GOOGLE_APPLICATION_CREDENTIALS": "/operator/adc.json",
                                 "CLOUDSDK_CONFIG": "/operator/gcloud", "GOOGLE_CLOUD_PROJECT": "example-project",
                                 "INCOME_FIRESTORE_DATABASE": "aestheticlab-income", "INCOME_SESSION_KEY": "secret",
                                 "GH_TOKEN": "secret", "ANTHROPIC_API_KEY": "secret", "GOCACHE": "/operator/go-build"}
        self.approved_task()
        self.cli("cycle", env=operator)
        self.assertEqual(self.cli("list")[0]["status"], "awaiting_review")
        seen = json.loads(probe.read_text())
        for name in ("GOOGLE_CLOUD_PROJECT", "INCOME_FIRESTORE_DATABASE", "INCOME_SESSION_KEY",
                     "GH_TOKEN", "ANTHROPIC_API_KEY", "TEST_CALL_LOG"):
            self.assertNotIn(name, seen)
        self.assertNotEqual(seen["GOOGLE_APPLICATION_CREDENTIALS"], "/operator/adc.json")
        self.assertFalse(Path(seen["GOOGLE_APPLICATION_CREDENTIALS"]).exists(),
                         "Google client libraries must fail closed instead of finding credentials")
        self.assertNotEqual(seen["CLOUDSDK_CONFIG"], "/operator/gcloud")
        self.assertEqual(list(Path(seen["CLOUDSDK_CONFIG"]).iterdir()), [])
        self.assertEqual(seen["FIRESTORE_EMULATOR_HOST"], "localhost:8086")
        self.assertEqual(seen["GOCACHE"], "/operator/go-build")
        self.assertEqual(seen["PATH"], operator["PATH"])

    def test_check_environment_cannot_reintroduce_cloud_credentials_and_is_part_of_policy(self):
        for name in ("GOOGLE_APPLICATION_CREDENTIALS", "CLOUDSDK_CONFIG", "CLOUDSDK_CORE_PROJECT"):
            with self.subTest(name=name):
                self.settings["check_env"] = {name: "/operator/value"}
                self.config.write_text(json.dumps(self.settings))
                self.cli("list", success=False)
        self.settings["check_env"] = {}
        self.config.write_text(json.dumps(self.settings))
        self.approved_task()
        self.cli("cycle")
        self.settings["check_env"] = {"FIRESTORE_EMULATOR_HOST": "localhost:8086"}
        self.config.write_text(json.dumps(self.settings))
        self.cli("complete", "demo", "--review", "Looks fine", success=False)

    def test_failed_runs_need_explicit_retry_and_reservations_never_reset(self):
        self.settings["worker"][-1] = "fail"
        self.config.write_text(json.dumps(self.settings))
        self.approved_task()
        self.cli("cycle")
        self.assertEqual(self.cli("list")[0]["status"], "budget_exhausted")
        self.assertEqual(self.cli("cycle")["processed"], 0)
        self.cli("retry", "demo", "--reason", "Owner supplies new evidence for a second attempt")
        self.cli("cycle")
        row = self.cli("list")[0]
        self.assertEqual(row["attempts"], 2)
        self.assertEqual(row["reserved_cents"], 100)
        self.cli("retry", "demo", "--reason", "One more attempt", success=False)
        self.assertEqual(len(self.calls()), 3)

    def test_timeout_stops_worker_and_scope_violations_do_not_complete(self):
        self.settings["worker"][-1] = "slow"
        self.settings["worker_seconds"] = 0.2
        self.config.write_text(json.dumps(self.settings))
        self.approved_task()
        self.cli("cycle")
        row = self.cli("list")[0]
        self.assertEqual(row["status"], "budget_exhausted")
        self.assertTrue(row["detail"]["worker"]["timed_out"])
        self.settings["worker"][-1] = "out_of_scope"
        self.settings["worker_seconds"] = 5
        self.settings["checks"] = [[sys.executable, "-c", "pass"]]
        self.config.write_text(json.dumps(self.settings))
        self.cli("retry", "demo", "--reason", "Changed fictional worker behavior")
        self.cli("cycle")
        row = self.cli("list")[0]
        self.assertEqual(row["status"], "blocked")
        self.assertEqual(row["detail"]["scope_violations"], ["README.md"])
        self.cli("complete", "demo", "--review", "Looks fine", success=False)

    def test_concurrent_cycles_launch_one_worker(self):
        self.approved_task()
        command = [sys.executable, str(RUNNER), "--repo", str(self.repo), "--state", str(self.state),
                   "--config", str(self.config), "cycle"]
        processes = [subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE) for _ in range(2)]
        results = [process.communicate(timeout=10) for process in processes]
        self.assertTrue(all(process.returncode == 0 for process in processes), results)
        self.assertEqual(sum("worker_args" in call for call in self.calls()), 1)

    def test_malformed_tasks_and_changed_evidence_cannot_be_accepted(self):
        self.cli("enqueue", str(self.task(allowed_paths=["../"])), success=False)
        self.cli("enqueue", str(self.task(jev_text="x" * 2001)), success=False)
        self.approved_task()
        self.cli("cycle")
        row = self.cli("list")[0]
        (Path(row["detail"]["worktree"]) / "docs/result.md").write_text("Changed after checks\n")
        self.cli("complete", "demo", "--review", "Looks fine", success=False)

    def test_malformed_mcp_blocks_and_historical_replay_keeps_consumed_caps(self):
        self.settings["mcp"]["env"]["TEST_BAD_INIT"] = "1"
        self.config.write_text(json.dumps(self.settings))
        self.cli("enqueue", str(self.task()))
        self.cli("cycle")
        self.assertEqual(self.cli("list")[0]["status"], "blocked")
        self.assertEqual(self.cli("list")[0]["jev_calls"], 1)
        self.cli("enqueue", str(self.task(source_revision="v2")))
        self.cli("enqueue", str(self.task(source_revision="v1")))
        self.assertEqual(self.cli("list")[0]["jev_calls"], 1)

    def test_untracked_symlink_change_invalidates_completion(self):
        self.settings["worker"][-1] = "symlink"
        self.config.write_text(json.dumps(self.settings))
        self.approved_task()
        self.cli("cycle")
        row = self.cli("list")[0]
        link = Path(row["detail"]["worktree"]) / "docs/link"
        link.unlink()
        link.symlink_to("b")
        self.cli("complete", "demo", "--review", "Looks fine", success=False)

    def test_server_backpressure_cannot_escape_deadline_and_unexpected_model_blocks(self):
        self.settings["mcp"]["env"]["TEST_HUGE_REQUEST"] = "1"
        self.settings["mcp_seconds"] = 0.2
        self.config.write_text(json.dumps(self.settings))
        self.cli("enqueue", str(self.task()))
        start = time.monotonic()
        self.cli("cycle")
        self.assertLess(time.monotonic() - start, 3)
        self.assertEqual(self.cli("list")[0]["status"], "blocked")
        del self.settings["mcp"]["env"]["TEST_HUGE_REQUEST"]
        self.settings["mcp"]["env"]["TEST_MODEL"] = "unexpected-model"
        self.settings["mcp_seconds"] = 5
        self.config.write_text(json.dumps(self.settings))
        self.cli("retry", "demo", "--reason", "Server now responds")
        self.cli("cycle")
        self.assertEqual(self.cli("list")[0]["status"], "blocked")

    def test_descendants_are_stopped_when_process_leader_exits(self):
        self.settings["worker"][-1] = "descendant"
        self.config.write_text(json.dumps(self.settings))
        self.approved_task()
        self.cli("cycle")
        heartbeat = Path(str(self.log) + ".heartbeat")
        observed = heartbeat.read_text()
        time.sleep(0.15)
        self.assertEqual(heartbeat.read_text(), observed, "descendant is still executing")

    def test_host_checks_require_explicit_opt_in_and_recovery_never_retries(self):
        self.settings["allow_host_checks"] = False
        self.config.write_text(json.dumps(self.settings))
        self.approved_task()
        self.cli("cycle")
        self.assertEqual(self.cli("list")[0]["status"], "awaiting_checks")
        self.cli("verify", "demo", "--allow-host-checks")
        self.assertEqual(self.cli("list")[0]["status"], "awaiting_review")
        with sqlite3.connect(self.state / "queue.sqlite") as db:
            db.execute("UPDATE tasks SET status='running', owner='interrupted'")
        self.cli("recover", "demo", "--confirm-no-worker", "--reason", "Observed stopped worker")
        self.assertEqual(self.cli("list")[0]["status"], "blocked")
        self.assertEqual(self.cli("cycle")["processed"], 0)

    def test_failed_worker_cannot_be_completed_via_verification(self):
        self.settings["worker"][-1] = "partial_failure"
        self.config.write_text(json.dumps(self.settings))
        self.approved_task()
        self.cli("cycle")
        self.assertEqual(self.cli("list")[0]["status"], "blocked")
        self.cli("verify", "demo", "--allow-host-checks", success=False)

    def test_committed_scope_and_changed_check_policy_invalidate_acceptance(self):
        self.approved_task()
        self.cli("cycle")
        self.settings["checks"] = [[sys.executable, "-c", "raise SystemExit(1)"]]
        self.config.write_text(json.dumps(self.settings))
        self.cli("complete", "demo", "--review", "Looks fine", success=False)
        self.settings["checks"] = [[sys.executable, "-c", "pass"]]
        self.config.write_text(json.dumps(self.settings))
        worktree = Path(self.cli("list")[0]["detail"]["worktree"])
        (worktree / "README.md").write_text("Out-of-scope committed change\n")
        subprocess.run(["git", "-C", str(worktree), "add", "."], check=True)
        subprocess.run(["git", "-C", str(worktree), "commit", "-qm", "out of scope"], check=True)
        self.cli("verify", "demo", "--allow-host-checks")
        self.assertEqual(self.cli("list")[0]["status"], "blocked")

    def test_worker_stdout_and_stderr_are_capped_during_execution(self):
        for mode, filename in [("noisy_stdout", "worker.json"), ("noisy_stderr", "worker.stderr")]:
            with self.subTest(mode=mode):
                self.state = self.root / mode
                self.settings["worker"][-1] = mode
                self.settings["log_bytes"] = 32768
                self.config.write_text(json.dumps(self.settings))
                self.approved_task(mode)
                start = time.monotonic()
                self.cli("cycle")
                row = self.cli("list")[0]
                self.assertEqual(row["status"], "budget_exhausted")
                self.assertTrue(row["detail"]["worker"]["log_limit_exceeded"])
                self.assertLessEqual((Path(row["detail"]["run_dir"]) / filename).stat().st_size, 32768)
                self.assertLess(time.monotonic() - start, 3)

    def test_acceptance_stdout_and_stderr_caps_block_completion_even_after_exit_zero(self):
        for stream in ("stdout", "stderr"):
            with self.subTest(stream=stream):
                self.state = self.root / f"check-{stream}"
                self.settings["log_bytes"] = 32768
                self.settings["checks"] = [[sys.executable, "-c",
                                            f"import sys; sys.{stream}.buffer.write(b'x' * 131072)"]]
                self.config.write_text(json.dumps(self.settings))
                self.approved_task(f"check-{stream}")
                self.cli("cycle")
                row = self.cli("list")[0]
                self.assertEqual(row["status"], "budget_exhausted")
                self.assertTrue(row["detail"]["checks"][0]["log_limit_exceeded"])
                captured = Path(row["detail"]["verification_dir"]) / f"check-0.{stream}"
                self.assertLessEqual(captured.stat().st_size, 32768)
                self.cli("complete", f"check-{stream}", "--review", "Looks fine", success=False)


if __name__ == "__main__":
    unittest.main()
