"""Regression coverage for cancellation evidence and local snapshot rendering."""

from datetime import date
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from zoneinfo import ZoneInfo

from collect import period, prepare_period, render_period


ZONE = ZoneInfo("Europe/Moscow")
COLLECT = Path(__file__).with_name("collect.py")


def report_with_evidence():
    start, end = period("day", date(2026, 11, 16), ZONE)
    tasks = [
        {"id": "canceled", "status": -1, "title": "Canceled", "projectId": "p",
         "createdTime": "2026-11-01T08:00:00+03:00", "modifiedTime": "2026-11-18T09:00:00+03:00",
         "completedTime": "2026-11-16T16:45:10+03:00", "content": "Current description"},
        {"id": "silent", "status": -1, "title": "Silent", "projectId": "p",
         "completedTime": "2026-11-16T10:59:00+03:00", "commentCount": 0},
        {"id": "progress", "status": 2, "title": "Progress", "projectId": "p",
         "completedTime": "2026-11-18T12:00:00+03:00"},
    ]
    comments = {
        "canceled": [
            {"id": "reason", "createdTime": "2026-11-02T08:00:00+03:00",
             "title": "prefix " * 50 + "Отменили из-за дубликата"},
            *[{"createdTime": f"2026-11-1{index}T08:00:00+03:00", "title": "Остался следующий шаг"}
              for index in range(6)],
            {"id": "later-edit", "createdTime": "2026-11-16T14:00:00+03:00",
             "modifiedTime": "2026-11-17T08:00:00+03:00", "title": "Later text"},
            {"createdTime": "2026-11-17T14:00:00+03:00", "title": "Future"},
        ],
        "progress": [
            {"createdTime": "2026-11-15T14:00:00+03:00", "title": "Outside window"},
            {"id": "edited-in-period", "createdTime": "2026-11-15T18:00:00+03:00",
             "modifiedTime": "2026-11-16T08:03:00+03:00", "title": "Edited in period"},
        ],
    }
    prepared = prepare_period(tasks, {"p": "Project"}, [], start, end)
    return render_period(prepared, comments, [], start, end, ZONE, ([], True, []))


class EvidenceTest(unittest.TestCase):
    def setUp(self):
        self.full = {"format": "full", "day": report_with_evidence()}
        folder = tempfile.TemporaryDirectory()
        self.addCleanup(folder.cleanup)
        self.folder = Path(folder.name)
        self.snapshot = self.folder / "snapshot.json"
        self.snapshot.write_text(json.dumps(self.full), encoding="utf-8")

    def render(self, *arguments):
        # An invalid CLI and timezone demonstrate that render doesn't initialize collection.
        return subprocess.run(
            [sys.executable, str(COLLECT), "render", "--input", self.snapshot,
             "--cli", "/nonexistent/ticktick", "--timezone", "invalid", *arguments],
            capture_output=True, text=True, check=False,
        )

    def test_event_dates_progress_and_historical_edits(self):
        day = self.full["day"]
        self.assertEqual(day["counts"], {"completed": 0, "open": 1, "abandoned": 2, "focus_only": 0})
        silent, canceled = day["abandoned"]
        self.assertEqual(silent["id"], "silent")
        self.assertTrue(canceled["event_in_period"])
        self.assertEqual(canceled["abandoned_at"], "2026-11-16T16:45:10+03:00")
        self.assertEqual(canceled["modified_at"], "2026-11-18T09:00:00+03:00")
        self.assertTrue(canceled["edited_after_period"])
        edited, = canceled["comments"]
        self.assertEqual(edited["created_at"], "2026-11-16T14:00:00+03:00")
        self.assertEqual(edited["evidence_at"], edited["created_at"])
        self.assertEqual(edited["modified_at"], "2026-11-17T08:00:00+03:00")
        self.assertTrue(edited["edited_after_period"])
        progress, = day["open"]
        self.assertFalse(progress["event_in_period"])
        evidence, = progress["comments"]
        self.assertEqual(evidence["evidence_at"], evidence["modified_at"])
        self.assertEqual(evidence["id"], "edited-in-period")

    def test_local_compaction_and_full_details(self):
        target = self.folder / "compact.json"
        result = self.render("--output", target)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "")
        compact = json.loads(target.read_text())
        self.assertEqual(compact["format"], "compact")
        _, task = compact["day"]["abandoned"]
        self.assertEqual(task["omitted_comments"], 4)
        self.assertEqual(len(task["earlier_comments"]), 3)
        reason = task["earlier_comments"][0]
        self.assertEqual(reason["id"], "reason")
        self.assertIn("Отменили из-за дубликата", reason["text"])
        self.assertLessEqual(len(reason["text"]), 200)
        result = self.render("--task-id", "canceled")
        self.assertEqual(result.returncode, 0, result.stderr)
        match, = json.loads(result.stdout)["matches"]
        self.assertEqual(match["group"], "abandoned")
        self.assertEqual(match["task"], self.full["day"]["abandoned"][1])

    def test_failed_output_preserves_files(self):
        result = self.render("--task-id", "missing", "--output", self.snapshot)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(json.loads(self.snapshot.read_text()), self.full)
        result = self.render("--output", self.folder)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(list(self.folder.iterdir()), [self.snapshot])
        result = self.render("--output", self.snapshot)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(self.snapshot.read_text())["format"], "compact")
        result = self.render("--task-id", "canceled")
        self.assertEqual(result.returncode, 1)
        self.assertIn("requires a snapshot saved with --format full", result.stderr)

    def test_compacts_legacy_full_snapshot(self):
        legacy = self.full["day"]
        task, = legacy["open"]
        task["comments"] = [["2026-11-16T08:03+03:00", "Legacy body"]]
        self.snapshot.write_text(json.dumps(legacy), encoding="utf-8")
        result = self.render()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["open"][0]["comments"], task["comments"])


if __name__ == "__main__":
    unittest.main()
