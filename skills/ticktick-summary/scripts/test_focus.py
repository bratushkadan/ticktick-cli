import unittest
from collections import Counter
from datetime import datetime, timedelta, timezone
from unittest.mock import Mock

import focus
from collect import stamp


class FocusAllocationTest(unittest.TestCase):
    start = datetime(2026, 1, 1, 23, 30, tzinfo=timezone.utc)

    def totals(self, elapsed, duration, pause, segments, window=None):
        def at(seconds):
            return self.start + timedelta(seconds=seconds)

        record = {
            "startTime": at(0).isoformat(), "endTime": at(elapsed).isoformat(),
            "duration": duration, "pauseDuration": pause,
            "tasks": [{"taskId": task, "startTime": at(left).isoformat(), "endTime": at(right).isoformat()}
                      for task, left, right in segments],
        }
        client = Mock()
        client.read.side_effect = [[], [record]]
        start, end = map(at, window or (0, elapsed))
        sessions, state = focus.read_sessions(client, start, end, stamp, [])
        self.assertEqual(state, "complete")
        totals = Counter()
        for link, seconds in focus.allocations(sessions, start, end):
            totals[link.get("taskId")] += seconds
        return dict(totals)

    def test_trailing_pause_does_not_dilute_task(self):
        self.assertEqual(self.totals(3848, 1399000, 2449, [("task", 0, 1399)]), {"task": 1399})

    def test_paused_across_midnight(self):
        spans = [("A", 0, 1200), ("B", 2400, 3600)]
        self.assertEqual(self.totals(3600, 2400, 1200, spans, (0, 1800)), {"A": 1200})
        self.assertEqual(self.totals(3600, 2400, 1200, spans, (1800, 3600)), {"B": 1200})

    def test_overlap_and_unassigned_focus(self):
        spans = [("A", 0, 600), ("B", 300, 900)]
        self.assertEqual(self.totals(1500, 1200, 300, spans), {"A": 450, "B": 450, None: 300})

    def test_no_links_and_pause_inside_link(self):
        self.assertEqual(self.totals(600, 400, 200, []), {None: 400})
        self.assertEqual(self.totals(600, 400, 200, [("A", 0, 600)]), {"A": 400})
