"""Regression tests for the digest's period, collection and evidence rules."""

from datetime import date, datetime, timezone
import unittest
from unittest.mock import patch
from zoneinfo import ZoneInfo

from collect import DEFAULT_PROJECT, digest, enrich_habits, period
from presentation import duration, excerpt


ZONE = ZoneInfo("Europe/Moscow")


class Snapshot:
    def __init__(self):
        self.project_reads = []
        self.task_reads = 0
        self.comment_reads = []

    def read(self, *args):
        self.project_reads.append(args)
        return [{"id": DEFAULT_PROJECT, "name": "Work"}, {"id": "personal", "name": "Personal"},
                {"id": "other", "name": "Personal"}]

    def task_snapshot(self):
        self.task_reads += 1
        return [
            {"id": "closed", "status": 2, "projectId": DEFAULT_PROJECT, "title": "Closed",
             "completedTime": "2026-11-22T10:00:00+03:00", "content": "A" * 400},
            {"id": "personal", "status": 0, "projectId": "personal", "title": "Personal progress"},
            {"id": "future", "status": 2, "projectId": DEFAULT_PROJECT, "title": "Future",
             "completedTime": "2026-11-22T14:00:00+03:00", "commentCount": 0},
        ], []

    def comments(self, refs, workers):
        self.comment_reads.append(refs)
        return {
            "closed": [
                {"createdTime": "2026-11-16T10:00:00+03:00", "title": "Осталось закончить"},
                *[{"createdTime": f"2026-11-21T1{hour}:00:00+03:00", "title": f"Comment {hour}"}
                  for hour in range(6)],
            ],
            "personal": [{"createdTime": "2026-11-22T10:00:00+03:00", "title": "Progress"}],
        }, 0


class DigestTest(unittest.TestCase):
    def test_shared_reads_partial_scope_and_evidence(self):
        now = datetime(2026, 11, 22, 13, tzinfo=ZONE)
        today = now.date()
        client = Snapshot()
        day = period("day", today, ZONE)
        week = period("week", today, ZONE)
        ranges = {"day": (*day, []), "week": (*week, [DEFAULT_PROJECT])}
        start = datetime(2026, 11, 22, 9, tzinfo=ZONE)
        end = datetime(2026, 11, 22, 10, tzinfo=ZONE)
        session = (start, end, 1.0, [({"taskId": "closed", "projectId": DEFAULT_PROJECT}, start, end)])
        with patch("collect.load_focus", return_value=([session], True, [])) as load:
            output = digest(client, ranges, now, ZONE, 8, None, True, "1.1.0", "default")
        load.assert_called_once_with(client, week[0], now)
        self.assertEqual(client.project_reads, [("project", "list")])
        self.assertEqual(client.task_reads, 1)
        self.assertEqual(client.comment_reads, [{"closed": DEFAULT_PROJECT, "personal": "personal"}])
        self.assertEqual([task["id"] for task in output["day"]["completed"]], ["closed"])
        self.assertEqual([task["id"] for task in output["day"]["open"]], ["personal"])
        self.assertEqual([row["id"] for row in output["day"]["projects"]], [DEFAULT_PROJECT, "personal"])
        self.assertEqual(output["week"]["effective_projects"], [{"id": DEFAULT_PROJECT, "name": "Work"}])
        self.assertTrue(output["day"]["partial_day"])
        self.assertTrue(output["week"]["partial_day"])
        self.assertNotIn("habits", output)
        self.assertEqual(output["week"]["focus"]["human"], "1 ч")
        task, = output["week"]["completed"]
        self.assertEqual(task["omitted_comments"], 2)
        self.assertEqual(len(task["comments"]), 5)
        self.assertEqual(task["comments"][0]["text"], "Осталось закончить")
        self.assertEqual(len(task["description"]), 300)

    def test_links_images_and_rounding(self):
        links = {}
        result = excerpt("![[image]](https://example.com/private) https://example.com/a?x=" + "a" * 100,
                         200, links)
        self.assertEqual(result, "[вложение] [ссылка]")
        self.assertEqual(list(links), ["https://example.com/a"])
        self.assertEqual(duration(15269)["human"], "4 ч 14 мин")
        self.assertEqual(duration(59)["human"], "<1 мин")


class HabitsTest(unittest.TestCase):
    def test_interval_exceptions_duplicates_and_partial_today(self):
        raw = {
            "id": "habit", "createdTime": "2026-11-02T00:00:00Z",
            "repeatRule": "RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR",
            "exDates": ["20261118"],
            "checkins": [
                {"stamp": 20261102, "status": 0, "value": 17.9, "goal": 10},
                {"stamp": 20261104, "status": 2, "value": 1, "goal": 1, "opTime": 1793800000000},
                {"stamp": 20261104, "status": 0, "value": 2, "goal": 4, "opTime": 1793800000001},
                {"stamp": 20261106, "status": 1, "value": 1, "goal": 1},
            ],
        }
        report = {"from": "2026-11-02", "through": "2026-11-20", "today_included": True, "habits": [raw]}
        start = datetime(2026, 11, 4, 12, tzinfo=timezone.utc)
        end = datetime(2026, 11, 4, 13, tzinfo=timezone.utc)
        session = (start, end, 1.0, [({"habitId": "habit"}, start, end)])
        enrich_habits(report, ([session], True, []), ZONE)
        stats = raw["stats"]
        self.assertEqual((stats["scheduled"], stats["achieved"], stats["partial"]), (5, 1, 1))
        self.assertEqual((stats["unknown"], stats["missed"], stats["skipped"]), (1, 1, None))
        self.assertEqual(stats["rate"], 0.2)
        self.assertEqual(stats["focus_minutes"], 60)
        self.assertEqual([week["scheduled"] for week in stats["split_by_week"]], [3, 0, 2])
        self.assertEqual(stats["split_by_week"][-1]["through"], "2026-11-20")

        raw["repeatRule"] = "RRULE:FREQ=MONTHLY"
        enrich_habits(report, ([], False, []), ZONE)
        self.assertIsNone(raw["stats"]["scheduled"])
        self.assertIsNone(raw["stats"]["rate"])
        self.assertIsNone(raw["stats"]["focus_minutes"])
        self.assertEqual(raw["stats"]["achieved"], 1)


if __name__ == "__main__":
    unittest.main()
