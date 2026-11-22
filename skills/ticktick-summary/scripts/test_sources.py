"""Exercise partial data, private page snapshots, and cache fallbacks."""

from concurrent.futures import ThreadPoolExecutor
from datetime import date, datetime
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from threading import Barrier
import unittest
from unittest.mock import patch
from zoneinfo import ZoneInfo

from collect import TickTick, digest, period


FIXTURE = '''#!/usr/bin/env python3
import json
import os
from pathlib import Path
import sys

args = sys.argv[1:]
mode = args[:2]

def arg(name, default=None):
    return args[args.index(name) + 1] if name in args else default

if args[0] == "version":
    print(json.dumps({"name": "ticktick-cli", "version": "1.1.0"}))
    sys.exit(0)
if "--help" in args:
    print("--cache-ids --cache-dir --snapshot-file")
    sys.exit(0)
if str(Path.home()) != os.environ["FIXTURE_HOME"]:
    sys.exit("authentication home changed")

directory = Path(arg("--cache-dir", os.environ.get("TICKTICK_CACHE_DIR")
                     or str(Path.home() / ".config/ticktick/cache")))
fail = os.environ.get("FIXTURE_FAILURE", "")
if mode == ["cache", "check"]:
    try:
        directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    except OSError as error:
        sys.exit(f"Error: cache_unavailable: {error}")
    print(json.dumps({"path": str(directory / "cache.json")}))
elif mode == ["project", "list"]:
    print(json.dumps([{"id": "project", "name": "Fixture"}]))
elif mode == ["v2", "completed"]:
    if "--to" in args:
        sys.exit("Error: v2 API HTTP 500")
    number = int(arg("--limit")) if fail == "history" else 1
    tasks = [{"id": f"done-{i}", "title": f"Task {i}", "projectId": "project", "status": 2,
              "completedTime": "2025-12-16T11:00:00Z", "commentCount": int(i == 0)} for i in range(number)]
    Path(arg("--snapshot-file")).write_text(json.dumps(tasks))
    print(json.dumps({"tasks": [{"id": row["id"], "status_slug": "completed"} for row in tasks],
                      "nextTo": "2025-12-16 11:00:00"}))
elif mode == ["v3", "active"]:
    Path(arg("--snapshot-file")).write_text("[]")
    print("[]")
elif mode == ["task", "comments-batch"]:
    assert directory.is_dir()
    if fail == "comments":
        print("{}")
        sys.exit("Error: comment HTTP 503")
    print(json.dumps({"done-0": [{"id": "comment", "title": "Fixture comment",
                                   "createdTime": "2025-12-16T10:00:00Z"}]}))
elif mode == ["focus", "list"]:
    if arg("--type") == "1" and fail == "focus":
        sys.exit("Error: focus HTTP 503")
    print("[]")
elif mode == ["habit", "list"]:
    if fail == "habit_list":
        sys.exit("Error: habit list HTTP 503")
    print("[]")
else:
    sys.exit(f"unexpected command {mode}")
'''


class SourcesTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.cli = self.directory / "ticktick-fixture"
        self.cli.write_text(FIXTURE, encoding="utf-8")
        self.cli.chmod(0o700)
        self.home = self.directory / "home"
        default_cache = self.home / ".config/ticktick/cache"
        default_cache.parent.mkdir(parents=True)
        default_cache.write_text("unusable cache", encoding="utf-8")
        self.script = Path(__file__).with_name("collect.py")

    def collect(self, *args, fail="", env_dir=""):
        return subprocess.run(
            [sys.executable, str(self.script), "digest", "--day", "2025-12-16", "--cli", str(self.cli), *args],
            env={**os.environ, "HOME": str(self.home), "FIXTURE_HOME": str(self.home), "FIXTURE_FAILURE": fail,
                 "TICKTICK_CACHE_DIR": str(env_dir), "TMPDIR": str(self.directory)},
            capture_output=True, text=True, check=False,
        )

    def test_cache_fallback_and_override_return_same_tasks_and_comments(self):
        flagged = self.directory / "flagged"
        envi = self.directory / "env-cache"
        explicit = self.collect("--cache-dir", str(flagged), "--strict", env_dir=envi)
        self.assertEqual(explicit.returncode, 0, explicit.stderr)
        self.assertTrue(flagged.is_dir())
        self.assertFalse(envi.exists())
        environment = self.collect("--strict", env_dir=envi)
        self.assertEqual(environment.returncode, 0, environment.stderr)
        self.assertTrue(envi.is_dir())
        fallback = self.collect("--strict")
        self.assertEqual(fallback.returncode, 0, fallback.stderr)
        reports = list(map(lambda process: json.loads(process.stdout), (explicit, environment, fallback)))
        self.assertEqual(reports[0]["day"], reports[1]["day"])
        self.assertEqual(reports[0]["day"], reports[2]["day"])
        self.assertEqual(reports[2]["day"]["completed"][0]["comments"][0]["text"], "Fixture comment")
        warnings = reports[-1]["diagnostics"]["cache"]["warnings"]
        self.assertEqual([warning["code"] for warning in warnings], ["cache_unavailable", "cache_temporary_fallback"])
        self.assertFalse(list(self.directory.glob("ticktick-summary-*")))

    def test_second_history_page_failure_preserves_report_and_strict_exit(self):
        path = self.directory / "report.json"
        result = self.collect("--strict", "--output", str(path), fail="history")
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertEqual(result.stdout, "")
        report = json.loads(path.read_text(encoding="utf-8"))
        self.assertFalse(report["day"]["tasks_complete"])
        self.assertEqual(len(report["day"]["completed"]), 2000)
        sources = report["diagnostics"]["sources"]
        self.assertEqual(sources["history"]["state"], "partial")
        warning, = sources["history"]["warnings"]
        self.assertEqual((warning["code"], warning["page"]), ("history_fetch_failed", 2))
        self.assertEqual(sources["active"]["state"], "complete")
        self.assertEqual(sources["comments"]["state"], "complete")
        non_strict = self.collect(fail="history")
        self.assertEqual(non_strict.returncode, 0, non_strict.stderr)
        self.assertEqual(json.loads(non_strict.stdout)["day"], report["day"])

    def test_focus_comments_and_habit_list_have_independent_statuses(self):
        for failure, source, state, code in (
            ("comments", "comments", "unavailable", "comments_fetch_failed"),
            ("focus", "focus", "partial", "focus_fetch_failed"),
            ("habit_list", "habit_list", "unavailable", "habit_list_fetch_failed"),
        ):
            with self.subTest(failure=failure):
                result = self.collect("--habits", "1", "--strict", fail=failure)
                self.assertEqual(result.returncode, 2, result.stderr)
                report = json.loads(result.stdout)
                self.assertEqual(len(report["day"]["completed"]), 1)
                sources = report["diagnostics"]["sources"]
                self.assertEqual(sources[source]["state"], state)
                self.assertEqual(sources[source]["warnings"][0]["code"], code)

    def test_history_pages_use_distinct_files_for_concurrent_collectors(self):
        barrier = Barrier(2)

        class Client(TickTick):
            def __init__(self, title, directory):
                super().__init__("ticktick", None, None, directory)
                self.title = title

            def read(self, *args):
                if args[:2] == ("cache", "check"):
                    return {"path": str(self.cache_dir / "cache.json")}
                path = Path(args[args.index("--snapshot-file") + 1])
                if args[:2] == ("v3", "active"):
                    path.write_text("[]", encoding="utf-8")
                    return []
                task = {"id": "same-id", "title": self.title}
                path.write_text(json.dumps([task]), encoding="utf-8")
                barrier.wait(timeout=10)
                return {"tasks": [{"id": task["id"], "status_slug": "completed"}]}

        with ThreadPoolExecutor(max_workers=2) as pool:
            futures = [pool.submit(Client(marker, self.directory).task_snapshot) for marker in ("one", "two")]
            self.assertEqual([job.result()[0][0]["title"] for job in futures], ["one", "two"])
        self.assertFalse(list(self.directory.glob(".summary-*")))

    def test_habit_postprocessing_error_keeps_day_and_marks_report_unavailable(self):
        client = TickTick(str(self.cli), None, None)
        zone = ZoneInfo("UTC")
        now = datetime(2025, 12, 16, 13, tzinfo=zone)
        day = now.date()
        bounds = period("day", day, zone)
        with (patch.dict(os.environ, {"HOME": str(self.home), "FIXTURE_HOME": str(self.home),
                                      "TICKTICK_CACHE_DIR": str(self.directory)}),
              patch("collect.enrich_habits", side_effect=ValueError("fixture invalid data"))):
            report = digest(client, {"day": (*bounds, [])}, now, zone, 1, (date(2025, 12, 15), day),
                            True, "1.1.0", None)
        self.assertEqual(len(report["day"]["completed"]), 1)
        self.assertNotIn("habits", report)
        self.assertEqual(client.sources["habit_report"]["state"], "unavailable")
        self.assertEqual(client.sources["habit_report"]["warnings"][0]["code"], "habit_report_failed")
