"""Bound the evidence shown in a digest and format focus consistently."""

import math
import re
from urllib.parse import urlsplit, urlunsplit


IMAGE = re.compile(r"!\[(?:\[[^\]]*\]|[^\]])*\]\([^\n)]*\)|!\[\[[^\n]*?\]\]")
URL = re.compile(r"https?://[^\s<>\])]+")
GROUPS = ("completed", "open", "abandoned", "focus_only")
CANCELLATION = re.compile(
    r"отмен|отказал|неактуаль|потерял[аои]? актуальность|дубликат|"
    r"cancel|won.t do|no longer needed|duplicate", re.I,
)
CONTEXT = re.compile(
    r"остал|в планах|застрял|блокер|причин|следующ|blocked|remaining|next step|reason", re.I,
)


def duration(seconds):
    minutes = math.floor(seconds / 60 + 0.5)
    hours, remaining = divmod(minutes, 60)
    if 0 < seconds < 60:
        human = "<1 мин"
    elif hours:
        human = f"{hours} ч {remaining} мин" if remaining else f"{hours} ч"
    else:
        human = f"{minutes} мин"
    return {"seconds": seconds, "minutes": minutes, "human": human}


def excerpt(body, limit, links, anchor=None):
    def shorten_link(match):
        original = match[0].rstrip(".,;!?")
        try:
            parsed = urlsplit(original)
        except ValueError:
            return "[ссылка]"
        link = (urlunsplit(parsed._replace(query=""))
                if len(parsed.query) > 80 else original)
        if len(link) > 240:
            return "[ссылка]"
        links.setdefault(link, None)
        return "[ссылка]" + match[0][len(original):]

    body = URL.sub(shorten_link, IMAGE.sub("[вложение]", body))
    if anchor and (match := anchor.search(body)) and match.end() > limit:
        body = "…" + body[max(0, match.start() - limit // 4):].lstrip()
    if len(body) > limit:
        return body[:limit - 1].rstrip() + "…"
    return body


def evidence_pattern(body):
    if CANCELLATION.search(body):
        return CANCELLATION
    if CONTEXT.search(body):
        return CONTEXT
    return None


def compact_comments(comments, limit, links, prioritize):
    def body_of(comment):
        return comment["text"] if isinstance(comment, dict) else comment[1]

    patterns = [evidence_pattern(body_of(comment)) if prioritize else None for comment in comments]

    def weight(index):
        pattern = patterns[index]
        return (2 if pattern is CANCELLATION else int(pattern is not None), index)

    selected = sorted(sorted(range(len(comments)), key=weight, reverse=True)[:limit])
    result = []
    for index in selected:
        comment = comments[index]
        pattern = patterns[index]
        if isinstance(comment, dict):
            result.append({**comment, "text": excerpt(comment["text"], 200, links, pattern)})
        else:
            when, body = comment
            result.append([when, excerpt(body, 200, links, pattern)])
    return result


def compact_report(report):
    report.setdefault("counts", {group: len(report[group]) for group in GROUPS})
    report["focus"].update(duration(report["focus"]["seconds"]))
    active_projects = set()
    for group in GROUPS:
        for task in report[group]:
            active_projects.add(task["project_id"])
            if "focus_seconds" not in task:
                continue
            task["focus"] = duration(task.pop("focus_seconds"))
            links = {}
            if "description" in task:
                description = task["description"]
                task["description"] = excerpt(description, 300, links, evidence_pattern(description))
            omitted = 0
            for field, limit in (("comments", 5), ("earlier_comments", 3)):
                if field not in task:
                    continue
                evidence = task[field]
                task[field] = compact_comments(evidence, limit, links, group != "open")
                omitted += len(evidence) - len(task[field])
            if omitted:
                task["omitted_comments"] = omitted
            if links:
                task["links"] = list(links)[:8]
    report["projects"] = [
        {"id": row["id"], "name": row["name"], "focus": (
            duration(row["focus_seconds"]) if "focus_seconds" in row else row["focus"]
        )}
        for row in report["projects"]
        if row.get("focus_seconds", row.get("focus", {}).get("seconds")) or row["id"] in active_projects
    ]
    return report
