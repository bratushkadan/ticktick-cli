#!/usr/bin/env python3
"""Collect TickTick evidence for a summary or a habit review."""

import argparse
from calendar import monthrange
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
import tempfile
import unicodedata
from time import sleep
from zoneinfo import ZoneInfo, ZoneInfoNotFoundError

import focus
from presentation import GROUPS, compact_report, duration
from habit_stats import achievement_deltas, calendar_date, compact_habits, habit_stats, habit_totals


UTC = timezone.utc
DEFAULT_PROJECT = "69f71ec926b4e22bb727ca29"
DEFAULT_HABIT_DAYS = 14
HISTORY_PAGE_SIZE = 2000
MINIMUM_CLI_VERSION = (1, 1, 0)
SOURCE_NAMES = ("history", "active", "comments", "focus", "habit_list", "habit_checkins", "projects")
SOURCE_EXCEPTIONS = (RuntimeError, OSError, ValueError, TypeError, KeyError, AttributeError, OverflowError)


def source_warning(code, message, **details):
    return {"code": code, "message": message, **details}


def record_source(client, source, state, warnings=()):
    if hasattr(client, "sources"):
        client.sources[source] = {
            "state": state,
            "complete": None if state == "not_requested" else state == "complete",
            "warnings": list(warnings),
        }


def source_failure(error, source, **details):
    message = str(error)
    for code in ("cache_unavailable", "cache_write_failed", "snapshot_write_failed", "snapshot_read_failed"):
        if message.startswith((code + ":", "Error: " + code + ":")):
            return source_warning(code, message, **details)
    return source_warning(f"{source}_fetch_failed", message, **details)


def attach_sources(output, client):
    diagnostics = output.setdefault("diagnostics", {})
    diagnostics["sources"] = client.sources
    if client.cache_status is not None:
        diagnostics["cache"] = client.cache_status
    task_sources = [client.sources[name]["complete"] for name in ("history", "active", "comments")]
    for report in (output, output.get("day"), output.get("week")):
        if isinstance(report, dict) and "completed" in report:
            report["tasks_complete"] = all(state is True for state in task_sources)


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
    def __init__(self, cli, env_file, checkout, cache_dir=None):
        self.cli = cli
        self.env_file = env_file
        self.open_env_file = env_file
        self.cache_dir = cache_dir
        self.cache_status = None
        self.temporary_cache = None
        self.sources = {}
        for source in SOURCE_NAMES:
            record_source(self, source, "not_requested")
        if env_file is None and checkout is not None and (checkout / ".env").is_file():
            self.open_env_file = checkout / ".env"

    def close(self):
        if self.temporary_cache is not None:
            self.temporary_cache.cleanup()

    def cli_version(self):
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
        return found[0] if found else None

    def check_version(self):
        found = self.cli_version()
        if found and tuple(map(int, found.split("."))) >= MINIMUM_CLI_VERSION:
            return found
        minimum = ".".join(map(str, MINIMUM_CLI_VERSION))
        detected = found or "unknown"
        raise RuntimeError(
            f"{self.cli}: ticktick-cli {minimum} or newer required (found {detected}); "
            "run make build in ticktick-cli or select an updated executable with --cli or TICKTICK_CLI."
        )

    def run(self, *args):
        env_file = self.env_file if args[0] in ("v2", "v3") else self.open_env_file
        env_flags = ["--env-file", str(env_file)] if env_file is not None else []
        cache_flags = []
        if self.cache_dir and args[:2] in (("cache", "check"), ("v2", "completed"),
                                           ("v3", "active"), ("task", "comments-batch")):
            cache_flags = ["--cache-dir", str(self.cache_dir)]
        return subprocess.run(
            [self.cli, *args, *env_flags, *cache_flags, "--json"], capture_output=True, text=True,
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
            if help_page.returncode or any(flag not in help_page.stdout
                                          for flag in ("--cache-ids", "--cache-dir", "--snapshot-file")):
                raise RuntimeError(
                    f"{self.cli}: summary requires {' '.join(command)} with --cache-ids, --cache-dir and --snapshot-file; "
                    "run make build in ticktick-cli or select an updated executable with --cli."
                )

    def prepare_cache(self):
        warnings = []
        try:
            path = Path(self.read("cache", "check")["path"])
        except SOURCE_EXCEPTIONS as error:
            warnings.append(source_warning("cache_unavailable", str(error)))
            try:
                self.temporary_cache = tempfile.TemporaryDirectory(prefix="ticktick-summary-")
                self.cache_dir = self.temporary_cache.name
                path = Path(self.read("cache", "check")["path"])
            except SOURCE_EXCEPTIONS as fallback_error:
                warnings.append(source_warning("cache_fallback_failed", str(fallback_error)))
                self.cache_status = {"state": "unavailable", "warnings": warnings}
                return None
            warnings.append(source_warning("cache_temporary_fallback", "Использован временный кэш сводки."))
        self.cache_status = {
            "state": "temporary" if self.temporary_cache is not None else "configured",
            "warnings": warnings,
        }
        return path

    def task_snapshot(self):
        path = self.prepare_cache()
        try:
            if path is None:
                raise OSError("Не удалось открыть кэш задач.")
            with tempfile.TemporaryDirectory(prefix=".summary-", dir=path.parent) as directory:
                return self.fetch_tasks(Path(directory))
        except OSError as error:
            warning = source_warning("cache_unavailable", str(error))
        for source in ("history", "active"):
            record_source(self, source, "unavailable", [warning])
        return [], [warning["message"]]

    def fetch_tasks(self, directory):
        # History is ordered by completedTime. Old tasks can have new comments.
        tasks = {"history": {}, "active": {}}
        warnings = {"history": [], "active": []}
        pages = 0
        cursor = None
        try:
            while True:
                path = directory / f"history-{pages + 1}.json"
                bounds = ("--to", cursor) if cursor else ()
                page = self.read("v2", "completed", "--cache-ids", "--snapshot-file", str(path),
                                 "--limit", str(HISTORY_PAGE_SIZE), *bounds)
                rows = page["tasks"]
                tasks["history"].update(read_task_page(rows, path))
                pages += 1
                if len(rows) < HISTORY_PAGE_SIZE:
                    break
                next_cursor = page.get("nextTo")
                if not isinstance(next_cursor, str) or not next_cursor or (cursor is not None and next_cursor > cursor):
                    warnings["history"].append(source_warning(
                        "history_cursor_missing", "TickTick не вернул следующую страницу истории закрытий.",
                        page=pages, cursor=cursor))
                    break
                if next_cursor == cursor:
                    warnings["history"].append(source_warning(
                        "history_cursor_truncated", f"История закрытий могла быть усечена на границе {cursor} UTC.",
                        page=pages, cursor=cursor))
                    next_cursor = (datetime.fromisoformat(cursor) - timedelta(seconds=1)).isoformat(sep=" ")
                cursor = next_cursor
        except SOURCE_EXCEPTIONS as error:
            warning = source_failure(error, "history", page=pages + 1, cursor=cursor)
            warning["message"] = f"Не удалось загрузить всю историю закрытий: {error}"
            warnings["history"].append(warning)
        try:
            path = directory / "active.json"
            active = self.read("v3", "active", "--cache-ids", "--snapshot-file", str(path))
            tasks["active"].update(read_task_page(active, path))
        except SOURCE_EXCEPTIONS as error:
            warning = source_failure(error, "active")
            warning["message"] = f"Не удалось загрузить активные задачи: {error}"
            warnings["active"].append(warning)

        for source, found in tasks.items():
            state = "complete" if not warnings[source] else "partial" if found else "unavailable"
            record_source(self, source, state, warnings[source])
        snapshot = {**tasks["history"], **tasks["active"]}
        return list(snapshot.values()), [warning["message"] for group in warnings.values() for warning in group]

    def comments(self, projects, workers):
        comments = {}
        failures = 0
        warnings = []
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
                    if not isinstance(result, dict):
                        raise ValueError("comment result must be an object")
                except SOURCE_EXCEPTIONS:
                    continue
                if "comments_cache_failed:" in process.stderr and not warnings:
                    message = next(line for line in process.stderr.splitlines()
                                   if "comments_cache_failed:" in line)
                    warnings.append(source_warning("comments_cache_failed", message))
                comments.update((task_id, rows) for task_id, rows in result.items()
                                if isinstance(rows, list) and all(isinstance(row, dict) for row in rows))
                pending = [ref for ref in pending if ref.split(":", 1)[1] not in comments]
                if not pending:
                    break
                if attempt == 0 and "HTTP 429" in process.stderr:
                    print("TickTick rate limit reached; retrying missing comments in 60 seconds.", file=sys.stderr)
                    sleep(60)
            failures += len(pending)
        if failures:
            warnings.append(source_warning("comments_fetch_failed", f"Не удалось загрузить комментарии к {failures} задачам.",
                                           missing=failures))
        state = ("partial" if comments else "unavailable") if failures else "complete"
        record_source(self, "comments", state, warnings)
        return comments, failures


def task_ids(rows):
    if not isinstance(rows, list) or any(not isinstance(row, dict) or not isinstance(row.get("id"), str)
                                         or not row["id"] for row in rows):
        raise ValueError("invalid tasks returned by TickTick CLI")
    return {row["id"]: row.get("status_slug") for row in rows}


def read_task_page(rows, path):
    try:
        with path.open(encoding="utf-8") as file:
            payloads = json.load(file)
        slugs = task_ids(rows)
        if slugs.keys() != task_ids(payloads).keys():
            raise ValueError("task IDs in the response and snapshot file differ")
        return {task["id"]: {**task, "status_slug": slugs[task["id"]]} for task in payloads}
    except SOURCE_EXCEPTIONS as error:
        raise RuntimeError(f"snapshot_read_failed: {error}") from error
    finally:
        path.unlink(missing_ok=True)


def project_names(client):
    names = {row["id"]: row["name"] for row in client.read("project", "list") or []}
    names.setdefault("inbox", "Inbox")
    return names


def project_catalog(client):
    try:
        names = project_names(client)
    except (RuntimeError, OSError, ValueError, TypeError, KeyError) as error:
        warning = f"Не удалось загрузить список проектов: {error}"
        record_source(client, "projects", "unavailable", [source_warning("projects_fetch_failed", warning)])
        return {"inbox": "Inbox"}, warning
    record_source(client, "projects", "complete")
    return names, None


def select_projects(names, selection):
    if not selection:
        return list(names)
    projects = []
    def without_leading_emoji(name):
        # Some TickTick project names start with emoji, variation selectors or joined glyphs.
        while name and (name[0].isspace() or unicodedata.category(name[0]) in ("So", "Sk")
                        or name[0] in "\ufe0f\ufe0e\u200d"):
            name = name[1:]
        return name.casefold()

    for value in selection:
        if value in names:
            projects.append(value)
            continue
        matches = [key for key, name in names.items() if name == value]
        if not matches:
            matches = [key for key, name in names.items() if name.casefold() == value.casefold()]
        if not matches:
            normalized = without_leading_emoji(value)
            matches = [key for key, name in names.items()
                       if normalized and without_leading_emoji(name) == normalized]
        if len(matches) != 1:
            candidates = ", ".join(f"{names[key]} ({key})" for key in matches)
            raise ValueError(f"Укажи ID проекта «{value}». " +
                             (f"Подходят {candidates}." if matches else "Проект не найден."))
        projects.append(matches[0])
    return list(dict.fromkeys(projects))


def task_status(task):
    return task.get("status_slug") or {
        -1: "abandoned", 0: "open", 2: "completed",
    }.get(task.get("status"), "unknown")


def prepare_period(tasks, names, selection, start, end):
    names = names.copy()
    projects = select_projects(names, selection)
    allowed = set(projects) if selection else None
    warnings = []
    candidates = {}
    comment_projects = {}
    aliases = {}
    missing_completion = 0
    unknown_status = 0
    for task in tasks:
        slug = task_status(task)
        if task.get("deleted"):
            continue
        source_project = task.get("projectId") or "inbox"
        project = "inbox" if source_project.startswith("inbox") else source_project
        if allowed is not None and project not in allowed:
            continue
        aliases[source_project] = project
        if slug not in ("abandoned", "open", "completed"):
            unknown_status += 1
            continue
        completed = stamp(task.get("completedTime")) if slug != "open" else None
        closed_in_period = completed is not None and start <= completed < end
        if slug == "completed" and completed is None:
            missing_completion += 1
        status = "abandoned" if slug == "abandoned" else "open"
        if slug == "completed" and closed_in_period:
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
    if unknown_status:
        warnings.append(f"Неизвестный статус у {unknown_status} задач. Их закрытие не учтено.")
    if missing_completion:
        warnings.append(f"Дата выполнения отсутствует у {missing_completion} задач.")
    return names, projects, allowed, candidates, aliases, comment_projects, warnings


def render_period(prepared, comments, snapshot_warnings, start, end, zone, focus_data,
                  calendar_end=None):
    names, projects, allowed, candidates, aliases, comment_projects, warnings = prepared
    warnings = [*snapshot_warnings, *warnings]
    failures = comment_projects.keys() - comments.keys()
    if failures:
        warnings.append(f"Не удалось загрузить комментарии к {len(failures)} задачам.")

    def local(value):
        return value.astimezone(zone).isoformat()

    sessions, focus_complete, focus_warnings = focus_data
    warnings.extend(focus_warnings)
    by_project, by_task, focused_tasks, unassigned = focus.summarize(
        sessions, start, end, names, candidates, aliases, allowed,
    )
    if allowed is None:
        for project in by_project.keys() - names.keys():
            names[project] = "Проект без названия"
            projects.append(project)
    groups = {group: [] for group in GROUPS}
    included = set()
    for task_id, (project, status, task) in candidates.items():
        event = stamp(task.get("completedTime")) if task_status(task) != "open" else None
        event_in_period = event is not None and start <= event < end
        include_earlier = status in ("completed", "abandoned") and event_in_period
        evidence = []
        earlier = []
        for comment in comments.get(task_id, []):
            created = stamp(comment.get("createdTime"))
            modified = stamp(comment.get("modifiedTime"))
            body = (comment.get("title") or "").strip()
            if not body or created is None or created >= end:
                continue
            changed_in_period = modified is not None and start <= modified < end
            in_window = created >= start or changed_in_period
            if not in_window and not include_earlier:
                continue
            evidence_date = modified if modified is not None and created <= modified < end else created
            item = {"evidence_at": local(evidence_date), "created_at": local(created), "text": body}
            if comment.get("id"):
                item["id"] = comment["id"]
            if modified is not None:
                item["modified_at"] = local(modified)
                if modified >= end:
                    item["edited_after_period"] = True
            (evidence if in_window else earlier).append((evidence_date, item))
        if not event_in_period and not evidence:
            continue
        record = {"id": task_id, "project_id": project, "project": names[project], "title": task["title"],
                  "focus_seconds": by_task[task_id]}
        if event is not None:
            record["abandoned_at" if status == "abandoned" else "completed_at"] = local(event)
            record["event_in_period"] = event_in_period
        for field, source in (("created_at", "createdTime"), ("modified_at", "modifiedTime")):
            if (value := stamp(task.get(source))) is not None:
                record[field] = local(value)
                if field == "modified_at" and value >= end:
                    record["edited_after_period"] = True
        if status in ("completed", "abandoned"):
            description = dict.fromkeys(
                body for field in ("content", "desc")
                if (body := (task.get(field) or "").strip())
            )
            if description:
                record["description"] = "\n\n".join(description)
        if evidence:
            record["comments"] = [item for _, item in sorted(evidence, key=lambda pair: pair[0])]
        if earlier:
            record["earlier_comments"] = [item for _, item in sorted(earlier, key=lambda pair: pair[0])]
        groups[status].append(record)
        included.add(task_id)
    for task_id, (project, title) in focused_tasks.items():
        if task_id in included:
            continue
        if task_id in candidates:
            title = candidates[task_id][2]["title"]
        groups["focus_only"].append({
            "id": task_id, "project_id": project, "project": names.get(project), "title": title, "focus_seconds": by_task[task_id],
        })
    groups["completed"].sort(key=lambda task: stamp(task["completed_at"]))
    groups["open"].sort(key=lambda task: stamp(task["comments"][0]["evidence_at"]))
    groups["abandoned"].sort(key=lambda task: stamp(
        task["abandoned_at"] if task.get("event_in_period") else task["comments"][0]["evidence_at"]
    ))
    groups["focus_only"].sort(key=lambda task: -task["focus_seconds"])
    through = ((end - timedelta(microseconds=1)).astimezone(zone).date()
               if end > start else start.astimezone(zone).date())
    return {
        "from": start.astimezone(zone).date().isoformat(),
        "through": through.isoformat(),
        **({"partial_day": end < calendar_end} if calendar_end else {}),
        "timezone": zone.key,
        "scope": "work" if allowed is not None else "all",
        "projects": [{"id": project, "name": names[project], "focus_seconds": by_project[project]} for project in projects],
        "focus": {
            "complete": focus_complete,
            "seconds": sum(by_project[project] for project in projects) + (0 if allowed is not None else unassigned),
            **({} if allowed is not None else {"unassigned_to_project_seconds": unassigned}),
        },
        "counts": {group: len(tasks) for group, tasks in groups.items()},
        **groups,
        "warnings": list(dict.fromkeys(warnings)),
    }


def habit_start_date(through, days, since):
    start = since or through - timedelta(days=days - 1)
    if start > through:
        raise ValueError("habit review start date cannot follow its end date")
    return start


def month_start(value):
    if not re.fullmatch(r"[0-9]{4}-[0-9]{2}", value):
        raise argparse.ArgumentTypeError("expected YYYY-MM")
    try:
        return date.fromisoformat(f"{value}-01")
    except ValueError as error:
        raise argparse.ArgumentTypeError("expected a valid month (YYYY-MM)") from error


def select_habits(habits, selectors):
    if not selectors:
        return habits
    selected = {}
    for selector in selectors:
        if selector in habits:
            selected[selector] = habits[selector]
            continue
        matches = [habit for habit in habits.values() if habit.get("name") == selector]
        if not matches:
            raise ValueError(f"habit not found: {selector!r}")
        if len(matches) > 1:
            identifiers = ", ".join(habit["id"] for habit in matches)
            raise ValueError(f"habit name {selector!r} is ambiguous. Choose an ID: {identifiers}")
        habit, = matches
        selected[habit["id"]] = habit
    return selected


def collect_habits(client, start, through, today, zone, selectors=()):
    habit_fields = (
        "id", "name", "status", "type", "goal", "unit", "repeatRule", "exDates",
        "createdTime", "archivedTime", "targetStartDate", "targetDays",
    )
    habits = {}
    checkins_complete = True
    warnings = []
    try:
        habits = {
            habit["id"]: {
                **{key: habit[key] for key in habit_fields if key in habit},
                "checkins": [],
            }
            for habit in client.read("habit", "list") or []
        }
    except (RuntimeError, OSError, ValueError, TypeError, KeyError, AttributeError) as error:
        checkins_complete = False
        warning = f"Не удалось загрузить список привычек: {error}"
        warnings.append(warning)
        record_source(client, "habit_list", "unavailable", [
            source_warning("habit_list_fetch_failed", warning),
        ])
    else:
        habits = select_habits(habits, selectors)
    if habits:
        try:
            documents = client.read(
                "habit", "checkins", "--ids", ",".join(habits),
                "--from", start.isoformat(), "--to", through.isoformat(),
            ) or []
            entries = {identifier: [] for identifier in habits}
            for document in documents:
                identifier = document.get("habitId")
                if identifier not in habits:
                    continue
                for entry in document.get("checkins") or []:
                    day = calendar_date(entry["stamp"])
                    if start <= day <= through:
                        entries[identifier].append((day, entry))
        except (RuntimeError, OSError, ValueError, TypeError, KeyError, AttributeError) as error:
            checkins_complete = False
            warning = f"Не удалось загрузить отметки привычек: {error}"
            warnings.append(warning)
            record_source(client, "habit_checkins", "unavailable", [
                source_warning("habit_checkins_fetch_failed", warning),
            ])
        else:
            for identifier, habit in habits.items():
                habit["checkins"] = [entry for _, entry in sorted(entries[identifier], key=lambda pair: pair[0])]
    return {
        "from": start.isoformat(),
        "through": through.isoformat(),
        "days": (through - start).days + 1,
        "timezone": zone.key,
        "today_included": through == today,
        "checkins_complete": checkins_complete,
        "habits": list(habits.values()),
        "warnings": warnings,
    }


def load_focus(client, start, end):
    warnings = []
    if start >= end:
        record_source(client, "focus", "complete")
        return [], True, warnings
    sessions, state = focus.read_sessions(client, start, end, stamp, warnings)
    record_source(client, "focus", state, warnings)
    return sessions, state == "complete", [warning["message"] for warning in warnings]


def load_habits(client, *args):
    report = collect_habits(client, *args)
    sources = getattr(client, "sources", {})
    if sources.get("habit_list", {}).get("state", "not_requested") == "not_requested":
        record_source(client, "habit_list", "complete")
    if sources.get("habit_checkins", {}).get("state", "not_requested") == "not_requested":
        state = "complete" if report.get("checkins_complete", True) else "unavailable"
        record_source(client, "habit_checkins", state, [source_warning("habit_list_fetch_failed", message)
                                                        for message in report.get("warnings", [])])
    return report


def habit_report_failure(client, error):
    warning = f"Не удалось подготовить отчёт по привычкам: {error}"
    record_source(client, "habit_report", "unavailable", [source_warning("habit_report_failed", warning)])
    return warning


def collect_summary(client, projects, start, end, today, zone, workers,
                    habit_days=DEFAULT_HABIT_DAYS, habit_from=None, no_habits=False, as_of=None):
    _, today_end = period("day", today, zone)
    observed_end = min(end, as_of or today_end)
    focus_start = start
    habit_through = None
    if not projects and not no_habits:
        last_day = end.astimezone(zone).date() - timedelta(days=1)
        habit_through = min(last_day, today - timedelta(days=1))
        habit_since = habit_start_date(habit_through, habit_days, habit_from)
        habit_start, _ = period("day", habit_since, zone)
        _, habit_end = period("day", habit_through, zone)
        focus_start = min(start, habit_start)
    with ThreadPoolExecutor(max_workers=2) as pool:
        focus_future = pool.submit(load_focus, client, focus_start, observed_end)
        if habit_through is not None:
            habit_future = pool.submit(load_habits, client, habit_since, habit_through, today, zone)
        names, project_error = project_catalog(client)
        if projects and project_error:
            return unavailable_work(start, observed_end, zone, None, project_error)
        tasks, warnings = client.task_snapshot()
        if project_error:
            warnings = [*warnings, project_error]
        prepared = prepare_period(tasks, names, projects, start, observed_end)
        comments, _ = client.comments(prepared[-2], workers)
        output = render_period(prepared, comments, warnings, start, observed_end, zone,
                               focus_future.result(), calendar_end=end)
        if habit_through is not None:
            try:
                report = habit_future.result()
                enrich_habits(report, focus_future.result(), zone)
            except SOURCE_EXCEPTIONS as error:
                output["warnings"].append(habit_report_failure(client, error))
            else:
                output["habit_report"] = report
    return output



def enrich_habits(report, focus_data, zone, week_start="range"):
    start = date.fromisoformat(report["from"])
    through = date.fromisoformat(report["through"])
    windows = []
    left = start
    while left <= through:
        days_to_end = 6 - left.weekday() if week_start == "monday" else 6
        right = left + timedelta(days=min(days_to_end, (through - left).days))
        windows.append((left, right))
        if right == through:
            break
        left = right + timedelta(days=1)
    sessions, complete, _ = focus_data
    focus_by_window = {}
    for left, right in [(start, through), *windows]:
        begins = period("day", left, zone)[0]
        ends = period("day", right, zone)[1]
        totals = Counter()
        for link, seconds in focus.allocations(sessions, begins, ends):
            if link.get("habitId"):
                totals[link["habitId"]] += seconds
        focus_by_window[left, right] = totals
    today = through if report["today_included"] else through + timedelta(days=1)
    checkins_complete = report.get("checkins_complete", True)
    for habit in report["habits"]:
        identifier = habit["id"]

        def minutes(left, right):
            seconds = focus_by_window[left, right][identifier]
            return duration(seconds)["minutes"] if complete or seconds else None

        habit["focus_seconds"] = focus_by_window[start, through][identifier]
        habit["stats"] = habit_stats(habit, start, through, today, stamp, zone, windows, minutes,
                                     checkins_complete=checkins_complete)
    report["focus_complete"] = complete
    report["week_start"] = week_start
    if len(windows) > 1:
        report["week2_from"] = windows[1][0].isoformat()
    else:
        report.pop("week2_from", None)
    stats = [habit["stats"] for habit in report["habits"]]
    reasons = Counter(row["schedule_reason"] for row in stats if row["schedule_reason"])
    unknown_statuses = Counter()
    for row in stats:
        unknown_statuses.update(row["unknown_checkin_statuses"] or {})
    report["schedule_reason_counts"] = dict(reasons)
    report["unknown_checkin_statuses"] = dict(unknown_statuses) if checkins_complete else None

    def total(left, right, rows):
        seconds = sum(focus_by_window[left, right][habit["id"]] for habit in report["habits"])
        return {
            **habit_totals(rows, checkins_complete),
            "focus_minutes": duration(seconds)["minutes"] if complete or seconds else None,
        }

    report["totals"] = total(start, through, stats)
    report["split_by_week"] = [
        {"from": left.isoformat(), "through": right.isoformat(),
         **total(left, right, [row["split_by_week"][index] for row in stats])}
        for index, (left, right) in enumerate(windows)
    ]
    achievement_deltas(report["split_by_week"])


def selected_work(args):
    configured = os.environ.get("TICKTICK_SUMMARY_PROJECTS", "").strip()
    if args.projects is not None:
        return args.projects, "--projects"
    if configured:
        return configured, "TICKTICK_SUMMARY_PROJECTS"
    if args.require_work_config:
        return None, "missing"
    return DEFAULT_PROJECT, "default"


def project_rows(names, ids):
    return [{"id": project, "name": names[project]} for project in ids]


def unavailable_work(start, end, zone, source, reason):
    return {
        "from": start.astimezone(zone).date().isoformat(),
        "through": (end - timedelta(microseconds=1)).astimezone(zone).date().isoformat(),
        "scope": "work", "work_selection_source": source, "effective_projects": [],
        "work_selection_state": "unavailable", "warnings": [reason],
    }


def work_selection_incomplete(output):
    return any(section.get("work_selection_state") == "unavailable"
               for section in (output, output.get("day", {}), output.get("week", {})))


MISSING_WORK_CONFIG = "Рабочий набор не задан в TICKTICK_SUMMARY_PROJECTS."


def doctor(client, selection, source, zone):
    result = {"cli": client.cli, "timezone": zone.key, "warnings": []}
    warnings = result["warnings"]
    try:
        result["version"] = client.cli_version()
    except OSError:
        result["version"] = None
    if not result["version"] or tuple(map(int, result["version"].split("."))) < MINIMUM_CLI_VERSION:
        warnings.append("Требуется ticktick-cli 1.1.0+.")
    try:
        client.check_summary_cli()
        result["cache_ids"] = True
    except (RuntimeError, OSError):
        result["cache_ids"] = False
        warnings.append("Сборка CLI должна поддерживать --cache-ids, --cache-dir и --snapshot-file для v2 completed и v3 active.")
    try:
        flags = ["--web-env-file", str(client.env_file) if client.env_file is not None else ""]
        result["auth"] = client.read("auth-status", *flags)
    except (RuntimeError, OSError, ValueError):
        result["auth"] = {"token_configured": None, "cookie_configured": None}
        warnings.append("Статус конфигурации недоступен. Обнови сборку CLI для auth-status.")
    result["auth"]["project_list_accessible"] = False
    try:
        names = project_names(client)
    except (RuntimeError, OSError, ValueError, KeyError):
        warnings.append("Не удалось загрузить список проектов через Open API.")
    else:
        result["auth"]["project_list_accessible"] = True
        result["projects"] = project_rows(names, names)
        if source == "missing":
            result["work_selection_state"] = "unavailable"
            warnings.append(MISSING_WORK_CONFIG)
        else:
            try:
                result["work_projects"] = project_rows(names, select_projects(names, selection))
            except ValueError as error:
                result["work_selection_state"] = "unavailable"
                warnings.append(str(error))
    result["work_selection_source"] = source
    if source == "default":
        warnings.append("Используется встроенный ID Work. Задай TICKTICK_SUMMARY_PROJECTS для своего набора.")
    return result


def digest(client, ranges, now, zone, workers, habit_range, compact, version, source):
    result = {
        "generated_at": now.astimezone(zone).isoformat(timespec="seconds"),
        "diagnostics": {"cli_version": version, "timezone": zone.key, "warnings": []},
    }
    ranges = ranges.copy()
    names, project_error = (project_catalog(client) if any(selection is not None for _, _, selection in ranges.values())
                            else ({}, None))
    for key, (start, calendar_end, selection) in list(ranges.items()):
        if selection is None:
            reason = MISSING_WORK_CONFIG
        elif selection and project_error:
            reason = project_error
        else:
            try:
                if selection:
                    ranges[key] = start, calendar_end, select_projects(names, selection)
                continue
            except ValueError as error:
                reason = str(error)
        result[key] = unavailable_work(start, min(calendar_end, now), zone, source, reason)
        del ranges[key]
    if not ranges and not habit_range:
        return result
    tasks, warnings = client.task_snapshot() if ranges else ([], [])
    if project_error:
        warnings = [*warnings, project_error]
    prepared = {}
    refs = {}
    for key, (start, calendar_end, selection) in ranges.items():
        end = min(calendar_end, now)
        prepared[key] = prepare_period(tasks, names, selection, start, end)
        refs.update(prepared[key][-2])

    starts = [start for start, _, _ in ranges.values()]
    ends = [end for _, end, _ in ranges.values()]
    if habit_range:
        since, through = habit_range
        starts.append(period("day", since, zone)[0])
        ends.append(period("day", through, zone)[1])
    if source == "default":
        result["diagnostics"]["warnings"].append(
            "Используется встроенный ID Work. Задай TICKTICK_SUMMARY_PROJECTS для своего набора.")
    with ThreadPoolExecutor(max_workers=2) as pool:
        focus_future = pool.submit(load_focus, client, min(starts), min(max(ends), now))
        if habit_range:
            habit_future = pool.submit(load_habits, client, since, through, now.date(), zone)
        comments = client.comments(refs, workers)[0] if ranges else {}
        focus_data = focus_future.result()
        for key, (start, calendar_end, selection) in ranges.items():
            report = render_period(prepared[key], comments, warnings, start, min(calendar_end, now),
                                   zone, focus_data, calendar_end=calendar_end)
            if selection:
                report["work_selection_source"] = source
                report["effective_projects"] = project_rows(names, prepared[key][1])
            result[key] = compact_report(report) if compact else report
        if habit_range:
            try:
                report = habit_future.result()
                enrich_habits(report, focus_data, zone)
                result["habits"] = compact_habits(report) if compact else report
            except SOURCE_EXCEPTIONS as error:
                result["diagnostics"]["warnings"].append(habit_report_failure(client, error))
    return result


def load_snapshot(path, output_format, task_id):
    with path.open(encoding="utf-8") as file:
        snapshot = json.load(file)
    if not isinstance(snapshot, dict):
        raise ValueError("expected a summary JSON object")

    periods = {key: snapshot[key] for key in ("day", "week") if key in snapshot}
    if not periods and ("focus" in snapshot or "work_selection_state" in snapshot):
        periods = {"period": snapshot}
    habit_report = snapshot.get("habits", snapshot.get("habit_report"))
    standalone_habits = isinstance(habit_report, list)
    if standalone_habits:
        habit_report = snapshot
    if not periods and not isinstance(habit_report, dict):
        raise ValueError("expected a saved day, week, digest or habits report")

    reports = {}
    legacy_compact = False
    for key, report in periods.items():
        if not isinstance(report, dict):
            raise ValueError(f"invalid {key} report in snapshot")
        if report.get("work_selection_state") == "unavailable":
            continue
        if not isinstance(report.get("focus"), dict) or not all(
            isinstance(report.get(group), list) for group in GROUPS
        ):
            raise ValueError(f"missing focus or task groups in saved {key} report")
        reports[key] = report
        legacy_compact |= "human" in report["focus"]
    snapshot_format = snapshot.get("format") or ("compact" if legacy_compact else "full")
    if snapshot_format == "compact" and output_format == "full":
        raise ValueError("full evidence requires a snapshot saved with --format full")
    if output_format == "compact":
        for report in reports.values():
            compact_report(report)

    if task_id is not None:
        matches = [
            {"period": period_name, "from": report["from"], "through": report["through"],
             "group": group, "task": task}
            for period_name, report in reports.items()
            for group in GROUPS
            for task in report[group]
            if task["id"] == task_id
        ]
        if not matches:
            raise ValueError(f"task ID {task_id} is absent from the saved report")
        return {"task_id": task_id, "format": output_format, "matches": matches}
    if output_format == "compact" and habit_report is not None:
        report = compact_habits(habit_report)
        if standalone_habits:
            snapshot = report
        else:
            snapshot["habits" if "habits" in snapshot else "habit_report"] = report
    snapshot["format"] = output_format
    return snapshot


def write_output(output, path):
    body = json.dumps(output, ensure_ascii=False, separators=(",", ":")) + "\n"
    if path is None:
        sys.stdout.write(body)
        return
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=path.parent,
                                         prefix=".ticktick-summary-", delete=False) as file:
            temporary = Path(file.name)
            file.write(body)
            file.flush()
            os.fsync(file.fileno())
        os.replace(temporary, path)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("day", "week", "habits", "digest", "doctor", "render"))
    parser.add_argument("date", nargs="?", type=date.fromisoformat,
                        help="YYYY-MM-DD (day/week: anchor, default today; habits: inclusive end, default yesterday)")
    habit_range = parser.add_mutually_exclusive_group()
    habit_range.add_argument("--habits", "--habit-days", dest="habit_days", type=int,
                             help=f"number of habit days ({DEFAULT_HABIT_DAYS} by default outside digest)")
    habit_range.add_argument("--habit-from", type=date.fromisoformat,
                             help="inclusive habit report start date (YYYY-MM-DD)")
    habit_range.add_argument("--no-habits", action="store_true")
    habit_range.add_argument("--month", type=month_start, metavar="YYYY-MM",
                             help="calendar month for habits (the current month ends today)")
    parser.add_argument("--habit", action="append", metavar="ID_OR_NAME",
                        help="select a habit by ID or exact unique name (repeatable)")
    parser.add_argument("--week-start", choices=("range", "monday"),
                        help="habit weeks start at the range start (default) or align with Monday")
    parser.add_argument("--day", nargs="?", const="", metavar="YYYY-MM-DD",
                        help="include a day in the digest (default: today)")
    parser.add_argument("--week", action="store_true", help="include the anchor's week in the digest")
    parser.add_argument("--format", choices=("compact", "full"),
                        help="evidence format (digest and habits default to compact, day/week to full)")
    parser.add_argument("--input", type=Path, help="read a saved JSON snapshot in render mode")
    parser.add_argument("--task-id", help="show details of a task from a snapshot in render mode")
    parser.add_argument("--output", type=Path, help="atomically write JSON to a file instead of stdout")
    parser.add_argument("--timezone", default=os.environ.get("TICKTICK_SUMMARY_TZ", "Europe/Moscow"))
    parser.add_argument("--work", action="store_true", help="include only work projects")
    parser.add_argument("--projects", help="work project IDs or unique names, comma-separated")
    parser.add_argument("--require-work-config", action="store_true",
                        help="select the projects in TICKTICK_SUMMARY_PROJECTS; report an unavailable period if unset")
    parser.add_argument("--cli", default=os.environ.get("TICKTICK_CLI"),
                        help="TickTick CLI 1.1.0+ (default: checkout binary, then PATH)")
    parser.add_argument("--env-file", type=Path,
                        help="TickTick dotenv file for both APIs (default: Open API config or checkout .env; web API ~/.env)")
    parser.add_argument("--cache-dir", help="task and comment cache directory (default: TICKTICK_CACHE_DIR or CLI cache)")
    parser.add_argument("--strict", action="store_true", help="exit with code 2 after writing an incomplete report")
    parser.add_argument("--workers", type=int, choices=range(1, 9), default=8)
    args = parser.parse_args()
    if args.strict and args.mode in ("render", "doctor"):
        parser.error("--strict requires a collection mode: day, week, digest or habits")
    if args.mode == "render":
        if args.input is None or args.date is not None:
            parser.error("render requires --input and does not take a date")
        output_format = args.format or ("full" if args.task_id is not None else "compact")
        try:
            write_output(load_snapshot(args.input, output_format, args.task_id), args.output)
        except (OSError, ValueError, TypeError, KeyError, AttributeError, OverflowError) as error:
            print(f"TickTick summary: {error}", file=sys.stderr)
            return 1
        return 0
    if args.input is not None or args.task_id is not None:
        parser.error("--input and --task-id are only supported by render")
    output_format = args.format or ("compact" if args.mode in ("digest", "habits") else "full")
    if args.habit_days is not None and args.habit_days < 1:
        parser.error("--habit-days must be positive")
    if args.mode != "digest" and (args.day is not None or args.week):
        parser.error("--day and --week are only supported by digest")
    if args.mode == "doctor" and args.date is not None:
        parser.error("doctor does not take a date")
    if args.mode == "habits" and args.no_habits:
        parser.error("habits cannot be combined with --no-habits")
    if args.mode != "habits" and (args.month is not None or args.habit or args.week_start):
        parser.error("--month, --habit and --week-start are only supported by habits")
    if args.month is not None and args.date is not None:
        parser.error("--month cannot be combined with a positional date")
    if args.require_work_config and (args.projects is not None or args.mode == "habits"):
        parser.error("--require-work-config requires a work report or doctor and a selection from TICKTICK_SUMMARY_PROJECTS")
    uses_work = args.work or args.projects is not None or args.require_work_config
    selection, source = selected_work(args) if uses_work or args.mode == "doctor" else (None, None)
    projects = [part.strip() for part in (selection or "").split(",") if part.strip()]
    if selection is not None and not projects:
        parser.error("select at least one work project")
    if source == "missing":
        projects = None
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
        if args.mode == "doctor":
            try:
                write_output({
                    "cli": None,
                    "version": None,
                    "timezone": zone.key,
                    "work_selection_source": source,
                    "warnings": ["Бинарник ticktick не найден. Укажи --cli или TICKTICK_CLI."],
                }, args.output)
            except OSError as error:
                print(f"TickTick summary: {error}", file=sys.stderr)
            return 1
        parser.error("ticktick executable not found; use --cli or TICKTICK_CLI")
    now = datetime.now(zone)
    today = now.date()
    if args.mode == "habits" and args.date is not None and args.date > today:
        parser.error("habit review end date cannot be in the future")
    if args.month is not None and args.month > today:
        parser.error("habit review month cannot be in the future")
    client = TickTick(str(Path(cli).resolve()), args.env_file, checkout, args.cache_dir)
    try:
        if args.mode == "doctor":
            output = doctor(client, projects, source, zone)
        else:
            version = client.check_version()
            days = args.habit_days or DEFAULT_HABIT_DAYS
            if args.mode == "habits":
                if args.month is not None:
                    since = args.month
                    through = min(since.replace(day=monthrange(since.year, since.month)[1]), today)
                else:
                    through = args.date or today - timedelta(days=1)
                    since = habit_start_date(through, days, args.habit_from)
                output = load_habits(client, since, through, today, zone, args.habit)
                focus_data = load_focus(client, period("day", since, zone)[0],
                                        min(period("day", through, zone)[1], now))
                output["warnings"].extend(focus_data[2])
                try:
                    enrich_habits(output, focus_data, zone, week_start=args.week_start or "range")
                    if output_format == "compact":
                        output = compact_habits(output)
                except SOURCE_EXCEPTIONS as error:
                    output["warnings"].append(habit_report_failure(client, error))
                    if output_format == "compact":
                        output["habits"] = [{"id": habit["id"], "name": habit.get("name")} for habit in output["habits"]]
            elif args.mode == "digest":
                if args.day and args.date is not None:
                    parser.error("use either a positional date or --day YYYY-MM-DD")
                anchor = date.fromisoformat(args.day) if args.day else (args.date or today)
                if anchor > today:
                    parser.error("digest date cannot be in the future")
                client.check_summary_cli()
                include_day = args.day is not None or not args.week
                ranges = {}
                if include_day:
                    ranges["day"] = (*period("day", anchor, zone), [] if args.week else projects)
                if args.week:
                    ranges["week"] = (*period("week", anchor, zone), projects)
                habits = None
                if args.habit_days is not None or args.habit_from is not None:
                    latest_end = max(end for _, end, _ in ranges.values())
                    through = min(latest_end.astimezone(zone).date(), today) - timedelta(days=1)
                    habits = habit_start_date(through, days, args.habit_from), through
                output = digest(client, ranges, now, zone, args.workers, habits,
                                output_format == "compact", version, source)
            else:
                client.check_summary_cli()
                start, end = period(args.mode, args.date or today, zone)
                if projects is None:
                    output = unavailable_work(start, min(end, now), zone, source, MISSING_WORK_CONFIG)
                else:
                    output = collect_summary(client, projects, start, end, today, zone, args.workers,
                                             days, args.habit_from, args.no_habits, as_of=now)
                    if output_format == "compact" and not work_selection_incomplete(output):
                        compact_report(output)
                        if "habit_report" in output:
                            output["habit_report"] = compact_habits(output["habit_report"])
                output["partial_day"] = start.astimezone(zone) <= now < end.astimezone(zone)
                if projects:
                    output["work_selection_source"] = source
                    if source == "default":
                        output["warnings"].append("Используется встроенный ID Work. Задай TICKTICK_SUMMARY_PROJECTS для своего набора.")
        if args.mode != "doctor":
            output["format"] = output_format
            attach_sources(output, client)
        write_output(output, args.output)
    except (RuntimeError, OSError, ValueError, TypeError, KeyError, OverflowError) as error:
        print(f"TickTick summary: {error}", file=sys.stderr)
        return 1
    finally:
        client.close()
    if args.strict and (work_selection_incomplete(output) or any(
        source["complete"] is False for source in client.sources.values()
    )):
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
