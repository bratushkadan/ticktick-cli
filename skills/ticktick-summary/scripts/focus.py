"""Read and allocate TickTick Pomodoro and timer records."""

from collections import Counter, defaultdict
from datetime import timedelta
import json


def query_windows(start, end):
    # Keep a day of overlap for sessions crossing a boundary and stay within the API's 30-day limit.
    while start < end:
        through = min(start + timedelta(days=29), end)
        yield start - timedelta(days=1), through
        start = through


def read_sessions(client, start, end, stamp, warnings):
    sessions = []
    seen = set()
    complete = True
    invalid = 0
    for kind in (0, 1):
        records = []
        for since, through in query_windows(start, end):
            try:
                records.extend(client.read(
                    "focus", "list", "--type", str(kind),
                    "--from", since.isoformat(), "--to", through.isoformat(),
                ) or [])
            except (RuntimeError, OSError, ValueError) as error:
                warnings.append(f"Не удалось загрузить фокус типа {kind} за {since.date()}–{through.date()}: {error}")
                complete = False
        for record in records:
            identifier = record.get("id") or json.dumps(record, sort_keys=True)
            key = (kind, identifier)
            if key in seen:
                continue
            seen.add(key)
            try:
                begins, ends = stamp(record.get("startTime")), stamp(record.get("endTime"))
                if begins is None or ends is None or ends <= begins:
                    raise ValueError("missing focus interval")
                if ends <= start or begins >= end:
                    continue
                elapsed = (ends - begins).total_seconds()
                expected = max(0, elapsed - (record.get("pauseDuration") or 0))
                raw = record.get("duration")
                # Live responses can use milliseconds, while the CLI examples use seconds.
                duration = expected if raw is None else min(
                    (raw, raw / 1000), key=lambda value: abs(value - (expected or elapsed)),
                )
                if duration < 0:
                    raise ValueError("negative focus duration")
                segments = record.get("tasks") or []
                if not segments:
                    direct = {key: record[key] for key in ("taskId", "projectId", "projectName", "habitId", "title")
                              if record.get(key)}
                    if direct:
                        segments = [direct]
                links = []
                for segment in segments:
                    left, right = stamp(segment.get("startTime")), stamp(segment.get("endTime"))
                    if left is None or right is None:
                        if len(segments) == 1:
                            left, right = left or begins, right or ends
                        else:
                            continue
                    links.append((segment, max(begins, left), min(ends, right)))
            except (ValueError, TypeError, OverflowError):
                invalid += 1
                continue
            sessions.append((begins, ends, duration / elapsed, links))
    if invalid:
        warnings.append(f"Пропущено сессий фокуса с некорректными временем или длительностью: {invalid}.")
    return sessions, complete and not invalid


def overlap_seconds(begins, ends, start, end):
    return max(0, (min(ends, end) - max(begins, start)).total_seconds())


def allocations(sessions, start, end):
    for begins, ends, rate, links in sessions:
        start_of_session, end_of_session = max(begins, start), min(ends, end)
        if end_of_session <= start_of_session or not rate:
            continue
        clipped = [(link, max(left, start_of_session), min(right, end_of_session))
                   for link, left, right in links if overlap_seconds(left, right, start, end)]
        boundaries = sorted({start_of_session, end_of_session}
                            | {point for _, left, right in clipped for point in (left, right)})
        for left, right in zip(boundaries, boundaries[1:]):
            active = [link for link, starts, stops in clipped if starts <= left and right <= stops]
            active = active or [{}]
            seconds = (right - left).total_seconds() * rate / len(active)
            for link in active:
                yield link, seconds


def summarize(sessions, start, end, names, candidates, aliases, scope):
    by_name = defaultdict(list)
    for project, name in names.items():
        by_name[name].append(project)
    projects = Counter()
    tasks = Counter()
    briefs = {}
    unassigned = 0
    for link, seconds in allocations(sessions, start, end):
        task_id = link.get("taskId")
        project = aliases.get(link.get("projectId"), link.get("projectId"))
        candidate = candidates.get(task_id)
        if project not in names and candidate:
            project = candidate[0]
        if project not in names:
            matches = by_name.get(link.get("projectName"), [])
            project = matches[0] if len(matches) == 1 else None
        if project is None:
            unassigned += seconds
        else:
            projects[project] += seconds
        if task_id and (scope is None or project in scope):
            tasks[task_id] += seconds
            if task_id not in briefs or project is not None:
                briefs[task_id] = (project, link.get("title"))
    return projects, tasks, briefs, unassigned
