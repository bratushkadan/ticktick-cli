"""Checks for habit schedule diagnostics and the standalone evidence format."""

from contextlib import redirect_stdout
from datetime import date
from io import StringIO
import json
import sys
import unittest
from unittest.mock import patch
from zoneinfo import ZoneInfo

from collect import TickTick, collect_habits, enrich_habits, load_habits, main
from habit_stats import compact_habits


ZONE = ZoneInfo("Europe/Moscow")
START = date(2024, 2, 1)
END = date(2024, 2, 29)
TODAY = date(2024, 3, 1)
CREATED = "2024-01-01T03:00:00+03:00"


def habit(identifier, name=None, **fields):
    return {
        "id": identifier, "name": name or identifier, "createdTime": CREATED,
        "repeatRule": "RRULE:FREQ=DAILY", "checkins": [], **fields,
    }


def checkin(stamp, **fields):
    return {"stamp": stamp, "status": 0, "value": 1, "goal": 1, **fields}


class HabitClient(TickTick):
    def __init__(self, habits, checkins):
        super().__init__(sys.executable, None, None)
        self.habits = habits
        self.checkins = checkins
        self.calls = []

    def check_version(self):
        return "1.1.0"

    def read(self, *args):
        self.calls.append(args)
        result = self.habits if args == ("habit", "list") else self.checkins
        if isinstance(result, Exception):
            raise result
        return result


class HabitReportTest(unittest.TestCase):
    def test_archive_applies_only_to_archived_habits_and_includes_last_day(self):
        active = habit("active", status=0, archivedTime="2001-01-01T00:00:00.000+0000",
                       targetStartDate=20240203, checkins=[checkin(20240202)])
        archived = habit("archived", status=1, archivedTime="2024-02-02T21:30:00Z")
        missing = habit("missing", status=1)
        bad = habit("bad", status=1, archivedTime="2001-01-01T00:00:00Z")
        report = {"from": START.isoformat(), "through": END.isoformat(),
                  "today_included": False, "habits": [active, archived, missing, bad]}
        enrich_habits(report, ([], True, []), ZONE)
        self.assertEqual(active["stats"]["scheduled"], 28)
        self.assertEqual(active["stats"]["achieved"], 1)
        self.assertEqual(archived["stats"]["scheduled"], 3)
        self.assertEqual(missing["stats"]["schedule_reason"], "missing_archive_date")
        self.assertEqual(bad["stats"]["schedule_reason"], "contradictory_archival")

    def test_schedule_reason_codes_preserve_achievements(self):
        entries = [checkin(20240201), checkin(20240209), checkin(20240210)]
        expected = {
            "unsupported_repeat_rule": habit("month", repeatRule="RRULE:FREQ=MONTHLY"),
            "missing_creation_date": habit("no creation", createdTime=None),
            "contradictory_archival": habit("archive", archivedTime="2024-01-01T02:00:00+03:00"),
            "invalid_archive_date": habit("invalid archive", archivedTime="invalid"),
            "invalid_creation_date": habit("invalid creation", createdTime={}),
            "invalid_excluded_dates": habit("bad exclusion", exDates=["20240230"]),
            "missing_repeat_rule": habit("no recurrence", repeatRule=None),
        }
        for row in expected.values():
            row["checkins"] = entries
        report = {"from": START.isoformat(), "through": END.isoformat(),
                  "today_included": False, "habits": list(expected.values())}
        enrich_habits(report, ([], True, []), ZONE)

        self.assertEqual(report["schedule_reason_counts"], dict.fromkeys(expected, 1))
        for reason, row in expected.items():
            with self.subTest(reason=reason):
                stats = row["stats"]
                self.assertEqual(stats["schedule_state"], "unknown")
                self.assertEqual(stats["schedule_reason"], reason)
                for unknown in ("scheduled", "missed", "rate", "off_schedule_achievements"):
                    self.assertIsNone(stats[unknown])
                self.assertEqual(stats["weeks_with_known_schedule"], 0)
                self.assertEqual(stats["confirmed_achievements"], 3)
                self.assertEqual(stats["split_by_week"][1]["confirmed_achievements_delta"], 1)
        self.assertIsNone(report["split_by_week"][0]["scheduled"])
        self.assertEqual(report["split_by_week"][1]["confirmed_achievements_delta"], len(expected))
        self.assertEqual(report["totals"]["confirmed_achievements"], 3 * len(expected))

    def test_unknown_checkins_are_not_missed_and_off_schedule_achievements_are_visible(self):
        row = habit("monday", repeatRule="RRULE:FREQ=WEEKLY;BYDAY=MO", checkins=[
            checkin(20240201), checkin(20240203, status=3),
            checkin(20240205, opTime="2024-02-05T08:00:00Z"),
            checkin(20240205, status=5, opTime="2024-02-05T09:00:00Z"),
            checkin(20240212), checkin(20240219, status=2, value=0),
        ])
        unscheduled = habit("unknown", createdTime=None)
        report = {"from": START.isoformat(), "through": END.isoformat(),
                  "today_included": False, "habits": [row, unscheduled]}
        enrich_habits(report, ([], True, []), ZONE, week_start="monday")

        stats = row["stats"]
        self.assertEqual(stats["schedule_state"], "known")
        self.assertEqual(stats["weeks_with_known_schedule"], 5)
        self.assertEqual((stats["scheduled"], stats["achieved"], stats["missed"], stats["unknown"]),
                         (4, 1, 1, 2))
        self.assertEqual((stats["confirmed_achievements"], stats["off_schedule_achievements"]), (2, 1))
        self.assertEqual(stats["unknown_checkin_statuses"], {"2": 1, "3": 1, "5": 1})
        self.assertEqual(report["unknown_checkin_statuses"], stats["unknown_checkin_statuses"])
        first, second, *_ = report["split_by_week"]
        self.assertEqual((first["from"], first["through"], second["from"]),
                         ("2024-02-01", "2024-02-04", "2024-02-05"))
        self.assertEqual(report["week2_from"], second["from"])
        self.assertEqual(first["off_schedule_achievements"], 1)
        self.assertEqual(second["confirmed_achievements_delta"], -1)
        self.assertEqual(first["habits_with_known_schedule"], 1)
        self.assertEqual(report["totals"]["off_schedule_achievements"], 1)
        self.assertIsNone(report["totals"]["scheduled"])

    def test_selectors_reject_ambiguous_names_and_prefer_ids(self):
        client = HabitClient([habit("a", "same"), habit("b", "same"), habit("c", "a")], [])
        with self.assertRaisesRegex(ValueError, "ambiguous.*a, b"):
            collect_habits(client, START, END, TODAY, ZONE, ["same"])
        self.assertEqual(client.calls, [("habit", "list")])
        with self.assertRaisesRegex(ValueError, "habit not found"):
            collect_habits(client, START, END, TODAY, ZONE, ["unknown"])
        result = collect_habits(client, START, END, TODAY, ZONE, ["a", "a", "b"])
        self.assertEqual([row["id"] for row in result["habits"]], ["a", "b"])
        self.assertEqual(client.calls[-1], ("habit", "checkins", "--ids", "a,b",
                                            "--from", "2024-02-01", "--to", "2024-02-29"))

    def test_cli_calendar_month_filters_and_retains_full_checkins_on_request(self):
        entry = checkin(20240229, id="checkin", time="2024-02-29T12:00:00Z")
        client = HabitClient([habit("a", "Walk, Stretch"), habit("b")], [
            {"habitId": "a", "checkins": [entry, checkin(20240301)]},
            {"habitId": "b", "checkins": [checkin(20240201)]},
        ])
        argv = ["collect.py", "habits", "--cli", sys.executable, "--month", "2024-02",
                "--habit", "Walk, Stretch"]

        def run(*flags):
            stdout = StringIO()
            with (patch.object(sys, "argv", [*argv, *flags]),
                  patch("collect.TickTick", return_value=client),
                  patch("collect.load_focus", return_value=([], True, [])),
                  redirect_stdout(stdout)):
                self.assertEqual(main(), 0)
            return json.loads(stdout.getvalue())

        full = run("--format", "full")
        compact = run()
        self.assertEqual((full["from"], full["through"]), ("2024-02-01", "2024-02-29"))
        self.assertEqual(full["days"], 29)
        self.assertEqual(full["habits"][0]["checkins"], [entry])
        self.assertEqual(full["habits"][0]["stats"]["confirmed_achievements"], 1)
        self.assertEqual([row["id"] for row in compact["habits"]], ["a"])
        self.assertNotIn("checkins", compact["habits"][0])
        self.assertEqual(compact["habits"][0]["weekly"]["confirmed_achievements"], [0, 0, 0, 0, 1])
        self.assertEqual(compact_habits(full)["habits"], compact["habits"])
        self.assertEqual(client.calls[-1], ("habit", "checkins", "--ids", "a",
                                            "--from", "2024-02-01", "--to", "2024-02-29"))

    def test_checkin_failure_does_not_report_zero_or_missed_achievements(self):
        client = HabitClient([habit("a")], RuntimeError("check-ins offline"))
        report = load_habits(client, START, END, TODAY, ZONE)
        enrich_habits(report, ([], True, []), ZONE)
        self.assertFalse(report["checkins_complete"])
        self.assertEqual(client.sources["habit_checkins"]["state"], "unavailable")
        self.assertTrue(report["warnings"])
        stats = report["habits"][0]["stats"]
        self.assertEqual(stats["scheduled"], 29)
        for key in ("achieved", "confirmed_achievements", "missed", "unknown", "rate"):
            self.assertIsNone(stats[key])
        self.assertIsNone(report["split_by_week"][1]["confirmed_achievements_delta"])

    def test_habit_list_failure_is_distinct_from_an_empty_list(self):
        unavailable = HabitClient(RuntimeError("habit list offline"), [])
        empty = HabitClient([], [])
        for client, state, achieved in ((unavailable, "unavailable", None), (empty, "complete", 0)):
            with self.subTest(state=state):
                report = load_habits(client, START, END, TODAY, ZONE)
                enrich_habits(report, ([], True, []), ZONE)
                self.assertEqual(report["habits"], [])
                self.assertEqual(client.sources["habit_list"]["state"], state)
                self.assertEqual(report["totals"]["confirmed_achievements"], achieved)


if __name__ == "__main__":
    unittest.main()
