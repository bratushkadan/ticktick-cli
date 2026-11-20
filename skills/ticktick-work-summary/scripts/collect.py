#!/usr/bin/env python3
"""Collect TickTick evidence for a daily or weekly work summary."""

import argparse
from concurrent.futures import ThreadPoolExecutor
from datetime import date, datetime, time, timedelta, timezone
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
from zoneinfo import ZoneInfo, ZoneInfoNotFoundError


UTC = timezone.utc
DEFAULT_PROJECT = "69f71ec926b4e22bb727ca29"
API_LIMIT = 200


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


def cli_environment(env_file):
    env = os.environ.copy()
    allowed = {"TICKTICK_TOKEN", "TICKTICK_BASE_URL"}
    if env_file.is_file():
        for line in env_file.read_text(encoding="utf-8").splitlines():
            match = re.fullmatch(r"\s*(?:export\s+)?(\w+)\s*=\s*(.*)", line)
            if not match or match[1] not in allowed or env.get(match[1]):
                continue
            try:
                parts = shlex.split(match[2], comments=True)
            except ValueError:
                raise ValueError(f"Invalid {match[1]} in {env_file}") from None
            if parts:
                env[match[1]] = parts[0]
    return env


class TickTick:
    def __init__(self, cli, env_file):
        self.cli = cli
        self.env = cli_environment(env_file)

    def read(self, *args, partial=False):
        process = subprocess.run(
            [self.cli, *args, "--json"], capture_output=True, text=True, env=self.env,
        )
        if process.returncode and not (partial and process.stdout.strip()):
            error = process.stderr.strip()
            token = self.env.get("TICKTICK_TOKEN")
            if token:
                error = error.replace(token, "[redacted]")
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

    def project_tasks(self, project, start, end, history_end):
        warnings = []
        data = self.read("project", "data", project)
        name = data["project"]["name"]
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
        return name, tasks, warnings

    def comments(self, projects, workers):
        comments = {}
        failures = 0
        refs = [f"{project}:{task_id}" for task_id, project in projects.items()]
        for offset in range(0, len(refs), 100):
            pending = refs[offset:offset + 100]
            for _ in range(2):
                result = self.read(
                    "task", "comments-batch", "--tasks", ",".join(pending),
                    "--workers", str(workers), partial=True,
                )
                comments.update(result)
                pending = [ref for ref in pending if ref.split(":", 1)[1] not in comments]
                if not pending:
                    break
            failures += len(pending)
        return comments, failures


def collect(client, projects, start, end, history_end, zone, workers):
    def read_project(project):
        return client.project_tasks(project, start, end, history_end)

    candidates = {}
    names = {}
    warnings = []
    with ThreadPoolExecutor(max_workers=min(4, len(projects))) as pool:
        for project, (name, tasks, issues) in zip(projects, pool.map(read_project, projects)):
            names[project] = name
            warnings.extend(issues)
            for status, rows in tasks.items():
                for task in rows:
                    created = stamp(task.get("createdTime"))
                    if status == "completed" or created is None or created < end:
                        candidates.setdefault(task["id"], (project, status, task))

    comments, failures = client.comments(
        {task_id: project for task_id, (project, _, _) in candidates.items()}, workers,
    )
    if failures:
        warnings.append(f"Не удалось загрузить комментарии к {failures} задачам.")

    def local(value):
        return value.astimezone(zone).isoformat(timespec="minutes")

    groups = {"completed": [], "open": [], "abandoned": []}
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
        record = {"project": names[project], "title": task["title"]}
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
    groups["completed"].sort(key=lambda task: task["completed_at"])
    groups["open"].sort(key=lambda task: task["comments"][0][0])
    groups["abandoned"].sort(key=lambda task: task["comments"][0][0])
    return {
        "from": start.astimezone(zone).date().isoformat(),
        "through": (end.astimezone(zone).date() - timedelta(days=1)).isoformat(),
        "timezone": zone.key,
        "projects": list(names.values()),
        **groups,
        "warnings": list(dict.fromkeys(warnings)),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("day", "week"))
    parser.add_argument("date", nargs="?", type=date.fromisoformat,
                        help="YYYY-MM-DD (for week, any day in that week; default: today)")
    parser.add_argument("--timezone", default=os.environ.get("TICKTICK_SUMMARY_TZ", "Europe/Moscow"))
    parser.add_argument("--projects", default=os.environ.get("TICKTICK_SUMMARY_PROJECTS", DEFAULT_PROJECT),
                        help="comma-separated TickTick project IDs (default: Work)")
    parser.add_argument("--cli", default=os.environ.get("TICKTICK_CLI"),
                        help="TickTick executable (default: repo binary, then PATH)")
    parser.add_argument("--env-file", type=Path, help="credentials file (default: .env next to CLI)")
    parser.add_argument("--workers", type=int, choices=range(1, 9), default=8)
    args = parser.parse_args()
    projects = list(dict.fromkeys(part.strip() for part in args.projects.split(",") if part.strip()))
    if not projects:
        parser.error("select at least one work project")
    try:
        zone = ZoneInfo(args.timezone)
    except ZoneInfoNotFoundError:
        parser.error(f"unknown timezone: {args.timezone}")
    repo_cli = Path(__file__).resolve().parents[3] / "ticktick"
    cli = shutil.which(args.cli or str(repo_cli)) or (None if args.cli else shutil.which("ticktick"))
    if cli is None:
        parser.error("ticktick executable not found; use --cli or TICKTICK_CLI")
    env_file = args.env_file or Path(cli).resolve().with_name(".env")
    today = datetime.now(zone).date()
    start, end = period(args.mode, args.date or today, zone)
    # Use a whole-second boundary for the CLI, which drops fractional seconds.
    _, today_end = period("day", today, zone)
    try:
        output = collect(TickTick(cli, env_file), projects, start, end, max(end, today_end), zone,
                         args.workers)
    except (RuntimeError, OSError, ValueError, TypeError, KeyError) as error:
        print(f"TickTick summary: {error}", file=sys.stderr)
        return 1
    print(json.dumps(output, ensure_ascii=False, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    sys.exit(main())
