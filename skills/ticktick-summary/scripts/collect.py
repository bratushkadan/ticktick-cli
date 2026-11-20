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
API_LIMIT = 200
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
    def __init__(self, cli, env_file):
        self.cli = cli
        self.env_flags = ["--env-file", str(env_file)] if env_file is not None else []

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
        return subprocess.run(
            [self.cli, *args, *self.env_flags, "--json"], capture_output=True, text=True,
        )

    def read(self, *args):
        process = self.run(*args)
        if process.returncode:
            error = process.stderr.strip()
            raise RuntimeError(error or f"TickTick exited with code {process.returncode}")
        return json.loads(process.stdout)

    def completed(self, project, start, end, warnings):
        rows = self.read(
            "task", "completed", "--projects", project,
            "--start", start.isoformat(), "--end", end.isoformat(),
        ) or []
        if len(rows) >= API_LIMIT:
            seconds = int((end - start).total_seconds())
            if seconds > 1:
                middle = start + timedelta(seconds=seconds // 2)
                return (self.completed(project, start, middle, warnings)
                        + self.completed(project, middle, end, warnings))
            warnings.append(f"Проект {project}: достигнут лимит выдачи выполненных задач.")
        result = []
        for task in rows:
            completed_at = stamp(task.get("completedTime"))
            if completed_at is None:
                warnings.append(f"Проект {project}: есть выполненные задачи без даты выполнения.")
            elif start <= completed_at < end:
                result.append(task)
        return result

    def project_tasks(self, project, name, start, end, history_end):
        warnings = []
        data = self.read("project", "data", project)
        completed = []
        open_tasks = [task for task in data.get("tasks") or [] if task.get("status", 0) == 0]
        for task in self.completed(project, start, history_end, warnings):
            if stamp(task["completedTime"]) < end:
                completed.append(task)
            else:
                open_tasks.append(task)
        abandoned = self.read("task", "filter", "--projects", project, "--status=-1") or []
        if len(abandoned) >= API_LIMIT:
            warnings.append(f"Проект {name}: выдача отменённых задач достигла лимита {API_LIMIT}.")
        tasks = {
            "completed": completed,
            "open": open_tasks,
            "abandoned": abandoned,
        }
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
                        "--workers", str(workers),
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


def collect(client, selection, start, end, history_end, zone, workers, focus_future):
    names, projects = select_projects(client, selection)

    def read_project(project):
        try:
            return client.project_tasks(project, names[project], start, end, history_end)
        except (RuntimeError, OSError, ValueError, KeyError) as error:
            return {}, [f"Не удалось загрузить задачи проекта «{names[project]}»: {error}"]

    candidates = {}
    aliases = {}
    warnings = []
    with ThreadPoolExecutor(max_workers=min(4, len(projects))) as pool:
        for project, (tasks, issues) in zip(projects, pool.map(read_project, projects)):
            warnings.extend(issues)
            for status, rows in tasks.items():
                for task in rows:
                    if task.get("projectId") and project == "inbox":
                        aliases[task["projectId"]] = project
                    created = stamp(task.get("createdTime"))
                    if status == "completed" or created is None or created < end:
                        candidates.setdefault(task["id"], (project, status, task))

    comments, failures = client.comments(
        {task_id: task.get("projectId") or project
         for task_id, (project, _, task) in candidates.items()}, workers,
    )
    if failures:
        warnings.append(f"Не удалось загрузить комментарии к {failures} задачам.")

    def local(value):
        return value.astimezone(zone).isoformat(timespec="minutes")

    sessions, focus_complete, focus_warnings = focus_future.result()
    warnings.extend(focus_warnings)
    by_project, by_task, focused_tasks, unassigned = focus.summarize(
        sessions, start, end, names, candidates, aliases, set(projects) if selection else None,
    )
    groups = {"completed": [], "open": [], "abandoned": [], "focus_only": []}
    included = set()
    for task_id, (project, status, task) in candidates.items():
        event = stamp(task.get("completedTime")) if status != "open" else None
        event_in_period = event is not None and start <= event < end
        include_earlier = status == "completed" or (status == "abandoned" and event_in_period)
        evidence = []
        for comment in comments.get(task_id, []):
            created = stamp(comment.get("createdTime"))
            body = (comment.get("title") or "").strip()
            if body and created is not None and created < end and (include_earlier or created >= start):
                evidence.append((created, body))
        if status != "completed" and not evidence:
            continue
        record = {"project": names[project], "title": task["title"],
                  "focus_seconds": by_task[task_id]}
        if event is not None:
            record["completed_at" if status == "completed" else "abandoned_at"] = local(event)
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
        # Fetch through today to find comments from the period on tasks completed later.
        output = collect(client, projects, start, end, max(end, today_end), zone, workers, focus_future)
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
                        help="TickTick dotenv file (default: checkout .env, then ~/.config/ticktick/.env)")
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
    env_file = args.env_file
    if env_file is None and checkout is not None and (checkout / ".env").is_file():
        env_file = checkout / ".env"
    today = datetime.now(zone).date()
    if args.mode == "habits" and args.date is not None and args.date > today:
        parser.error("habit review end date cannot be in the future")
    try:
        client = TickTick(cli, env_file)
        client.check_version()
        if args.mode == "habits":
            through = args.date or today - timedelta(days=1)
            since = habit_start_date(through, args.habit_days, args.habit_from)
            output = collect_habits(client, since, through, today, zone)
        else:
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
