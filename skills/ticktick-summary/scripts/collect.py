#!/usr/bin/env python3
"""Collect TickTick evidence for a summary or a habit review."""

import argparse
from collections import Counter
from concurrent.futures import ThreadPoolExecutor
from datetime import date, datetime, time, timedelta, timezone
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
from time import sleep
from zoneinfo import ZoneInfo, ZoneInfoNotFoundError

import focus


UTC = timezone.utc
DEFAULT_PROJECT = "69f71ec926b4e22bb727ca29"
DEFAULT_HABIT_DAYS = 14
HISTORY_PAGE_SIZE = 2000
MINIMUM_CLI_VERSION = (1, 1, 0)


def period(mode, anchor, zone):
    if mode == "week":
        anchor -= timedelta(days=anchor.weekday())
    end = anchor + timedelta(days=7 if mode == "week" else 1)
    return tuple(datetime.combine(day, time.min, zone).astimezone(UTC)
                 for day in (anchor, end))


def stamp(value):
    if value in (None, "", 0):
        return None
    if isinstance(value, (float, int)):
        return datetime.fromtimestamp(value / 1000, UTC)
    result = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if result.tzinfo is None:
        raise ValueError(f"Timestamp has no timezone: {value}")
    return result


def skill_checkout():
    for directory in Path(__file__).resolve().parents:
        if (directory / "cmd/ticktick/main.go").is_file() and (directory / "go.mod").is_file():
            return directory
    return None


class TickTick:
    def __init__(self, cli, env_file, checkout):
        self.cli = cli
        self.env_file = env_file
        self.open_env_file = env_file
        if env_file is None and checkout is not None and (checkout / ".env").is_file():
            self.open_env_file = checkout / ".env"

    def check_version(self):
        process = subprocess.run([self.cli, "version", "--json"], capture_output=True, text=True)
        found = None
        if process.returncode == 0:
            try:
                payload = json.loads(process.stdout)
            except ValueError:
                payload = None
            if isinstance(payload, dict) and payload.get("name") == "ticktick-cli":
                output = payload.get("version")
                if isinstance(output, str):
                    found = re.fullmatch(r"(\d+)\.(\d+)\.(\d+)", output)
        if found and tuple(map(int, found.groups())) >= MINIMUM_CLI_VERSION:
            return
        minimum = ".".join(map(str, MINIMUM_CLI_VERSION))
        detected = found[0] if found else "unknown"
        raise RuntimeError(
            f"{self.cli}: ticktick-cli {minimum} or newer required (found {detected}); "
            "run make build in ticktick-cli or select an updated executable with --cli or TICKTICK_CLI."
        )

    def run(self, *args):
        env_file = self.env_file if args[0] in ("v2", "v3") else self.open_env_file
        env_flags = ["--env-file", str(env_file)] if env_file is not None else []
        return subprocess.run(
            [self.cli, *args, *env_flags, "--json"], capture_output=True, text=True,
        )

    def read(self, *args):
        process = self.run(*args)
        if process.returncode:
            error = process.stderr.strip()
            raise RuntimeError(error or f"TickTick exited with code {process.returncode}")
        return json.loads(process.stdout)

    def check_summary_cli(self):
        for command in (("v2", "completed"), ("v3", "active")):
            help_page = self.run(*command, "--help")
            if help_page.returncode or "--cache-ids" not in help_page.stdout:
                raise RuntimeError(
                    f"{self.cli}: summary requires {' '.join(command)} --cache-ids; "
                    "run make build in ticktick-cli or select an updated executable with --cli."
                )

    def task_snapshot(self):
        # History is ordered by completedTime. Old tasks can have new comments.
        ids = {}
        warnings = []
        cursor = None
        try:
            while True:
                bounds = ("--to", cursor) if cursor else ()
                page = self.read("v2", "completed", "--cache-ids", "--limit",
                                 str(HISTORY_PAGE_SIZE), *bounds)
                rows = page["tasks"]
                ids.update((task["id"], None) for task in rows)
                if len(rows) < HISTORY_PAGE_SIZE:
                    break
                next_cursor = page.get("nextTo")
                if not next_cursor or (cursor is not None and next_cursor > cursor):
                    warnings.append("TickTick не вернул следующую страницу истории закрытий.")
                    break
                if next_cursor == cursor:
                    warnings.append(f"История закрытий могла быть усечена на границе {cursor} UTC.")
                    next_cursor = (datetime.fromisoformat(cursor) - timedelta(seconds=1)).isoformat(sep=" ")
                cursor = next_cursor
        except (RuntimeError, OSError, ValueError, KeyError) as error:
            warnings.append(f"Не удалось загрузить всю историю закрытий: {error}")
        try:
            active = self.read("v3", "active", "--cache-ids")
            ids.update((task["id"], None) for task in active)
        except (RuntimeError, OSError, ValueError, KeyError) as error:
            warnings.append(f"Не удалось загрузить активные задачи: {error}")
        if not ids:
            return [], warnings
        path = Path.home() / ".config/ticktick/cache/cache.json"
        with path.open(encoding="utf-8") as file:
            entries = json.load(file)["tasks"]
        tasks = [entries[task_id]["payload"] for task_id in ids if task_id in entries]
        if len(tasks) != len(ids):
            warnings.append(f"В кэше отсутствует {len(ids) - len(tasks)} задач из ответов TickTick.")
        return tasks, warnings

    def comments(self, projects, workers):
        comments = {}
        failures = 0
        refs = [f"{project}:{task_id}" for task_id, project in projects.items()]
        for offset in range(0, len(refs), 100):
            pending = refs[offset:offset + 100]
            for attempt in range(2):
                try:
                    process = self.run(
                        "task", "comments-batch", "--tasks", ",".join(pending),
                        "--workers", str(workers), "--use-cache",
                    )
                    result = json.loads(process.stdout or "{}")
                except (RuntimeError, OSError, ValueError):
                    continue
                comments.update(result)
                pending = [ref for ref in pending if ref.split(":", 1)[1] not in comments]
                if not pending:
                    break
                if attempt == 0 and "HTTP 429" in process.stderr:
                    print("TickTick rate limit reached; retrying missing comments in 60 seconds.", file=sys.stderr)
                    sleep(60)
            failures += len(pending)
        return comments, failures


def select_projects(client, selection):
    names = {row["id"]: row["name"] for row in client.read("project", "list") or []}
    names.setdefault("inbox", "Inbox")
    if not selection:
        return names, list(names)
    projects = []
    for value in selection:
        matches = [key for key, name in names.items() if name.casefold() == value.casefold()]
        if value in names:
            projects.append(value)
        elif len(matches) == 1:
            projects.append(matches[0])
        else:
            raise ValueError(f"Не удалось однозначно выбрать проект «{value}». Укажи его ID.")
    return names, list(dict.fromkeys(projects))


def collect(client, selection, start, end, zone, workers, focus_future):
    names, projects = select_projects(client, selection)
    allowed = set(projects) if selection else None
    tasks, warnings = client.task_snapshot()
    candidates = {}
    comment_projects = {}
    aliases = {}
    missing_completion = 0
    for task in tasks:
        code = task["status"]
        if task.get("deleted") or code not in (-1, 0, 1, 2):
            continue
        source_project = task.get("projectId") or "inbox"
        project = "inbox" if source_project.startswith("inbox") else source_project
        if allowed is not None and project not in allowed:
            continue
        aliases[source_project] = project
        completed = stamp(task.get("completedTime")) if code != 0 else None
        closed_in_period = completed is not None and start <= completed < end
        if code in (1, 2) and completed is None:
            missing_completion += 1
        status = "abandoned" if code == -1 else "open"
        if code in (1, 2) and closed_in_period:
            status = "completed"
        created = stamp(task.get("createdTime"))
        if not closed_in_period and created is not None and created >= end:
            continue
        modified = stamp(task.get("modifiedTime"))
        # A later edit may supersede a change within the reporting period.
        needs_comments = task.get("commentCount") != 0 and (
            closed_in_period or modified is None or modified >= start
        )
        if project not in names:
            names[project] = "Проект без названия"
            projects.append(project)
        task_id = task["id"]
        candidates[task_id] = (project, status, task)
        if needs_comments:
            comment_projects[task_id] = source_project
    if missing_completion:
        warnings.append(f"Дата выполнения отсутствует у {missing_completion} задач.")
    comments, failures = client.comments(comment_projects, workers)
    if failures:
        warnings.append(f"Не удалось загрузить комментарии к {failures} задачам.")

    def local(value):
        return value.astimezone(zone).isoformat(timespec="minutes")

    sessions, focus_complete, focus_warnings = focus_future.result()
    warnings.extend(focus_warnings)
    by_project, by_task, focused_tasks, unassigned = focus.summarize(
        sessions, start, end, names, candidates, aliases, allowed,
    )
    groups = {"completed": [], "open": [], "abandoned": [], "focus_only": []}
    included = set()
    for task_id, (project, status, task) in candidates.items():
        event = stamp(task.get("completedTime")) if task["status"] != 0 else None
        event_in_period = event is not None and start <= event < end
        include_earlier = status == "completed" or (status == "abandoned" and event_in_period)
        evidence = []
        for comment in comments.get(task_id, []):
            created = stamp(comment.get("createdTime"))
            modified = stamp(comment.get("modifiedTime"))
            body = (comment.get("title") or "").strip()
            if not body or created is None or created >= end:
                continue
            if created >= start:
                evidence.append((created, body))
            elif modified is not None and start <= modified < end:
                evidence.append((modified, body))
            elif include_earlier:
                evidence.append((created, body))
        if status != "completed" and not evidence:
            continue
        record = {"project": names[project], "title": task["title"],
                  "focus_seconds": by_task[task_id]}
        if event is not None:
            record["abandoned_at" if status == "abandoned" else "completed_at"] = local(event)
        if status == "completed":
            description = dict.fromkeys(
                body for field in ("content", "desc")
                if (body := (task.get(field) or "").strip())
            )
            if description:
                record["description"] = "\n\n".join(description)
        if evidence:
            record["comments"] = [[local(created), body] for created, body in sorted(evidence)]
        groups[status].append(record)
        included.add(task_id)
    for task_id, (project, title) in focused_tasks.items():
        if task_id in included:
            continue
        if task_id in candidates:
            title = candidates[task_id][2]["title"]
        groups["focus_only"].append({
            "id": task_id, "project": names.get(project), "title": title, "focus_seconds": by_task[task_id],
        })
    groups["completed"].sort(key=lambda task: task["completed_at"])
    groups["open"].sort(key=lambda task: task["comments"][0][0])
    groups["abandoned"].sort(key=lambda task: task["comments"][0][0])
    groups["focus_only"].sort(key=lambda task: -task["focus_seconds"])
    return {
        "from": start.astimezone(zone).date().isoformat(),
        "through": (end.astimezone(zone).date() - timedelta(days=1)).isoformat(),
        "timezone": zone.key,
        "scope": "work" if selection else "all",
        "projects": [{"name": names[project], "focus_seconds": by_project[project]} for project in projects],
        "focus": {
            "complete": focus_complete,
            "seconds": sum(by_project[project] for project in projects) + (0 if selection else unassigned),
            **({} if selection else {"unassigned_to_project_seconds": unassigned}),
        },
        **groups,
        "warnings": list(dict.fromkeys(warnings)),
    }


def habit_start_date(through, days, since):
    start = since or through - timedelta(days=days - 1)
    if start > through:
        raise ValueError("habit review start date cannot follow its end date")
    return start


def collect_habits(client, start, through, today, zone):
    start_stamp = int(start.strftime("%Y%m%d"))
    end_stamp = int(through.strftime("%Y%m%d"))
    habit_fields = (
        "id", "name", "status", "type", "goal", "unit", "repeatRule", "exDates",
        "createdTime", "archivedTime", "targetStartDate", "targetDays",
    )
    entry_fields = ("stamp", "status", "value", "goal", "opTime")
    habits = {
        habit["id"]: {
            **{key: habit[key] for key in habit_fields if key in habit},
            "checkins": [],
        }
        for habit in client.read("habit", "list") or []
    }
    if habits:
        documents = client.read(
            "habit", "checkins", "--ids", ",".join(habits),
            "--from", start.isoformat(), "--to", through.isoformat(),
        ) or []
        for document in documents:
            habit = habits.get(document.get("habitId"))
            if habit is None:
                continue
            for entry in document.get("checkins") or []:
                day = entry["stamp"]
                if start_stamp <= day <= end_stamp:
                    habit["checkins"].append({key: entry[key] for key in entry_fields if key in entry})
        for habit in habits.values():
            habit["checkins"].sort(key=lambda entry: entry["stamp"])
    return {
        "from": start.isoformat(),
        **({"week2_from": (start + timedelta(days=7)).isoformat()} if (through - start).days >= 7 else {}),
        "through": through.isoformat(),
        "days": (through - start).days + 1,
        "timezone": zone.key,
        "today_included": through == today,
        "habits": list(habits.values()),
    }


def load_focus(client, start, end):
    warnings = []
    if start >= end:
        return [], True, warnings
    sessions, complete = focus.read_sessions(client, start, end, stamp, warnings)
    return sessions, complete, warnings


def collect_summary(client, projects, start, end, today, zone, workers,
                    habit_days=DEFAULT_HABIT_DAYS, habit_from=None):
    _, today_end = period("day", today, zone)
    focus_start = start
    habit_through = None
    if not projects:
        last_day = end.astimezone(zone).date() - timedelta(days=1)
        habit_through = min(last_day, today - timedelta(days=1))
        habit_since = habit_start_date(habit_through, habit_days, habit_from)
        habit_start, _ = period("day", habit_since, zone)
        _, habit_end = period("day", habit_through, zone)
        focus_start = min(start, habit_start)
    with ThreadPoolExecutor(max_workers=2) as pool:
        focus_future = pool.submit(load_focus, client, focus_start, min(end, today_end))
        if habit_through is not None:
            habit_future = pool.submit(collect_habits, client, habit_since, habit_through, today, zone)
        output = collect(client, projects, start, end, zone, workers, focus_future)
        if habit_through is not None:
            try:
                report = habit_future.result()
            except (RuntimeError, OSError, ValueError, TypeError, KeyError) as error:
                output["warnings"].append(f"Не удалось загрузить привычки: {error}")
            else:
                sessions, complete, _ = focus_future.result()
                by_habit = Counter()
                for link, seconds in focus.allocations(sessions, habit_start, habit_end):
                    if link.get("habitId"):
                        by_habit[link["habitId"]] += seconds
                for habit in report["habits"]:
                    habit["focus_seconds"] = by_habit[habit["id"]]
                report["focus_complete"] = complete
                output["habit_report"] = report
    return output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("day", "week", "habits"))
    parser.add_argument("date", nargs="?", type=date.fromisoformat,
                        help="YYYY-MM-DD (day/week: anchor, default today; habits: inclusive end, default yesterday)")
    habit_range = parser.add_mutually_exclusive_group()
    habit_range.add_argument("--habit-days", type=int, default=DEFAULT_HABIT_DAYS,
                             help=f"number of calendar days in the habit report (default: {DEFAULT_HABIT_DAYS})")
    habit_range.add_argument("--habit-from", type=date.fromisoformat,
                             help="inclusive habit report start date (YYYY-MM-DD)")
    parser.add_argument("--timezone", default=os.environ.get("TICKTICK_SUMMARY_TZ", "Europe/Moscow"))
    parser.add_argument("--work", action="store_true", help="include only work projects")
    parser.add_argument("--projects", help="work project IDs or unique names, comma-separated")
    parser.add_argument("--cli", default=os.environ.get("TICKTICK_CLI"),
                        help="TickTick CLI 1.1.0+ (default: checkout binary, then PATH)")
    parser.add_argument("--env-file", type=Path,
                        help="TickTick dotenv file for both APIs (default: Open API config or checkout .env; web API ~/.env)")
    parser.add_argument("--workers", type=int, choices=range(1, 9), default=8)
    args = parser.parse_args()
    if args.habit_days < 1:
        parser.error("--habit-days must be positive")
    selection = args.projects
    if args.work and selection is None:
        selection = os.environ.get("TICKTICK_SUMMARY_PROJECTS") or DEFAULT_PROJECT
    projects = [part.strip() for part in (selection or "").split(",") if part.strip()]
    if selection is not None and not projects:
        parser.error("select at least one work project")
    try:
        zone = ZoneInfo(args.timezone)
    except ZoneInfoNotFoundError:
        parser.error(f"unknown timezone: {args.timezone}")
    checkout = skill_checkout()
    if args.cli:
        cli = shutil.which(args.cli)
    else:
        local = shutil.which(str(checkout / "ticktick")) if checkout is not None else None
        cli = local or shutil.which("ticktick")
    if cli is None:
        parser.error("ticktick executable not found; use --cli or TICKTICK_CLI")
    today = datetime.now(zone).date()
    if args.mode == "habits" and args.date is not None and args.date > today:
        parser.error("habit review end date cannot be in the future")
    try:
        client = TickTick(cli, args.env_file, checkout)
        client.check_version()
        if args.mode == "habits":
            through = args.date or today - timedelta(days=1)
            since = habit_start_date(through, args.habit_days, args.habit_from)
            output = collect_habits(client, since, through, today, zone)
        else:
            client.check_summary_cli()
            start, end = period(args.mode, args.date or today, zone)
            output = collect_summary(client, projects, start, end, today, zone, args.workers,
                                     args.habit_days, args.habit_from)
    except (RuntimeError, OSError, ValueError, TypeError, KeyError, OverflowError) as error:
        print(f"TickTick summary: {error}", file=sys.stderr)
        return 1
    print(json.dumps(output, ensure_ascii=False, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    sys.exit(main())
