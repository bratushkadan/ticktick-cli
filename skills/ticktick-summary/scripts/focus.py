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
    received = 0
    invalid = 0
    for kind in (0, 1):
        records = []
        for since, through in query_windows(start, end):
            try:
                rows = client.read(
                    "focus", "list", "--type", str(kind),
                    "--from", since.isoformat(), "--to", through.isoformat(),
                )
                if not isinstance(rows, list):
                    raise ValueError("focus response must be an array")
                records.extend(rows)
                received += 1
            except (RuntimeError, OSError, ValueError, TypeError, KeyError) as error:
                warnings.append({
                    "code": "focus_fetch_failed", "type": kind,
                    "from": since.isoformat(), "to": through.isoformat(),
                    "message": f"Не удалось загрузить фокус типа {kind} за {since.date()}–{through.date()}: {error}",
                })
        for record in records:
            if not isinstance(record, dict):
                invalid += 1
                continue
            identifier = record.get("id")
            if not isinstance(identifier, str) or not identifier:
                identifier = json.dumps(record, sort_keys=True)
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
                    left, right = max(begins, left), min(ends, right)
                    if left < right:
                        links.append((segment, left, right))
            except (ValueError, TypeError, OverflowError, AttributeError):
                invalid += 1
                continue
            sessions.append((begins, ends, duration, links))
    if invalid:
        warnings.append({
            "code": "focus_invalid_records", "missing": invalid,
            "message": f"Пропущено сессий фокуса с некорректными данными: {invalid}.",
        })
    return sessions, "complete" if not warnings else "partial" if received else "unavailable"


def overlap_seconds(begins, ends, start, end):
    return max(0, (min(ends, end) - max(begins, start)).total_seconds())


def allocations(sessions, start, end):
    for begins, ends, duration, links in sessions:
        if not duration or ends <= start or begins >= end:
            continue

        # Measure coverage across the whole session before clipping to a report day.
        boundaries = sorted({begins, ends} | {t for _, left, right in links for t in (left, right)})
        spans = []
        covered = 0
        for left, right in zip(boundaries, boundaries[1:]):
            active = [link for link, first, last in links if first <= left and right <= last]
            spans.append((left, right, active))
            if active:
                covered += (right - left).total_seconds()

        elapsed = (ends - begins).total_seconds()
        gaps = elapsed - covered
        # Prefer explicit active intervals. Only focus left over can belong to gaps.
        # An upward duration adjustment beyond wall time scales the entire session.
        if duration > elapsed:
            linked_rate = gap_rate = duration / elapsed
        else:
            linked_rate = min(duration, covered) / covered if covered else 0
            gap_rate = max(0, duration - covered) / gaps if gaps else 0
        for left, right, active in spans:
            seconds = overlap_seconds(left, right, start, end) * (linked_rate if active else gap_rate)
            if seconds:
                for link in active or [{}]:
                    yield link, seconds / max(1, len(active))


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
            if len(matches) == 1:
                project = matches[0]
        if project is None:
            unassigned += seconds
        else:
            projects[project] += seconds
        if task_id and (scope is None or project in scope):
            tasks[task_id] += seconds
            if task_id not in briefs or project is not None:
                briefs[task_id] = (project, link.get("title"))
    return projects, tasks, briefs, unassigned
