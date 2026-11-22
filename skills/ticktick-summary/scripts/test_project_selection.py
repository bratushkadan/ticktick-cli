from argparse import Namespace
from datetime import datetime
import unittest
from unittest.mock import patch

from collect import DEFAULT_PROJECT, digest, period, select_projects, selected_work, work_selection_incomplete
from test_collect import Snapshot, ZONE


class ProjectSelectionTest(unittest.TestCase):
    def test_exact_name_id_and_unique_emoji_free_name(self):
        names = {"p": "👩🏽‍💻 Work", "q": "⚙️ Digital Infrastructure", "r": "Work"}
        self.assertEqual(select_projects(names, ["Work", "digital Infrastructure", "p"]), ["r", "q", "p"])
        del names["r"]
        self.assertEqual(select_projects(names, ["Work", "Digital Infrastructure"]), ["p", "q"])
        names["r"] = "🚀 Work"
        with self.assertRaises(ValueError) as error:
            select_projects(names, ["Work"])
        for identifier in ["👩🏽‍💻 Work (p)", "🚀 Work (r)"]:
            self.assertIn(identifier, str(error.exception))

    def test_require_configuration_and_default(self):
        args = Namespace(require_work_config=True, projects=None)
        with patch.dict("os.environ", {"TICKTICK_SUMMARY_PROJECTS": "  "}):
            self.assertEqual(selected_work(args), (None, "missing"))
            args.require_work_config = False
            self.assertEqual(selected_work(args), (DEFAULT_PROJECT, "default"))
        args.require_work_config = True
        with patch.dict("os.environ", {"TICKTICK_SUMMARY_PROJECTS": "a,b"}):
            self.assertEqual(selected_work(args), ("a,b", "TICKTICK_SUMMARY_PROJECTS"))

    def test_unavailable_selection_preserves_general_day(self):
        now = datetime(2026, 11, 22, 13, tzinfo=ZONE)
        for selection, source in [(None, "missing"), (["Personal"], "--projects")]:
            with self.subTest(source=source), patch("collect.load_focus", return_value=([], True, [])):
                client = Snapshot()
                ranges = {"day": (*period("day", now.date(), ZONE), []),
                          "week": (*period("week", now.date(), ZONE), selection)}
                output = digest(client, ranges, now, ZONE, 8, None, False, "1.1.0", source)
                self.assertTrue(work_selection_incomplete(output))
                self.assertEqual(output["week"]["effective_projects"], [])
                self.assertNotIn("focus", output["week"])
                self.assertEqual(output["day"]["scope"], "all")
                self.assertEqual(client.task_reads, 1)

    def test_failed_project_list_preserves_focus_and_labels_scope_unavailable(self):
        now = datetime(2026, 11, 22, 13, tzinfo=ZONE)
        start = now.replace(hour=10)
        end = now.replace(hour=11)
        session = (start, end, 1, [({"projectId": "external"}, start, end)])
        client = Snapshot()
        client.sources = {}
        ranges = {"day": (*period("day", now.date(), ZONE), []),
                  "week": (*period("week", now.date(), ZONE), ["Work"])}
        with patch.object(client, "read", side_effect=RuntimeError("offline")), \
                patch("collect.load_focus", return_value=([session], True, [])):
            result = digest(client, ranges, now, ZONE, 8, None, True, "1.1.0", "--projects")
        self.assertFalse(client.sources["projects"]["complete"])
        self.assertEqual(result["week"]["work_selection_state"], "unavailable")
        self.assertEqual(result["day"]["focus"]["seconds"], 3600)
        self.assertEqual(result["day"]["focus"]["unassigned_to_project_seconds"], 0)
        self.assertEqual(result["day"]["counts"]["completed"], 1)
