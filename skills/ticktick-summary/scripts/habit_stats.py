"""Calculate habit statistics for the current, explicitly supported schedule."""

from collections import Counter, defaultdict
from datetime import date, timedelta
from math import isfinite
import re


WEEKDAYS = ("MO", "TU", "WE", "TH", "FR", "SA", "SU")
DATE_ERRORS = (AttributeError, TypeError, ValueError, OverflowError, OSError)


class ScheduleError(ValueError):
    """A recurrence cannot be recovered from the current habit metadata."""


def calendar_date(value):
    value = str(value)
    if re.fullmatch(r"[0-9]{8}", value):
        value = f"{value[:4]}-{value[4:6]}-{value[6:]}"
    return date.fromisoformat(value)


def scheduled_days(habit, start, through, stamp, zone):
    rule = habit.get("repeatRule")
    if rule is None or rule == "":
        raise ScheduleError("missing_repeat_rule")
    try:
        parts = rule.removeprefix("RRULE:").split(";")
        fields = dict(part.split("=", 1) for part in parts)
        if len(fields) != len(parts):
            raise ValueError
        freq = fields.pop("FREQ")
        interval = int(fields.pop("INTERVAL", "1"))
        byday = fields.pop("BYDAY", None)
        weekdays = {WEEKDAYS.index(day) for day in byday.split(",")} if byday is not None else None
    except (AttributeError, KeyError, TypeError, ValueError) as error:
        raise ScheduleError("unsupported_repeat_rule") from error
    if freq not in ("DAILY", "WEEKLY") or interval < 1 or fields:
        raise ScheduleError("unsupported_repeat_rule")
    try:
        created = stamp(habit.get("createdTime"))
        anchor = created.astimezone(zone).date() if created else None
    except DATE_ERRORS as error:
        raise ScheduleError("invalid_creation_date") from error
    if anchor is None:
        raise ScheduleError("missing_creation_date")
    # The web calendar applies its (inclusive) archive bound only to archived
    # habits. Active records may retain a timestamp long before creation.
    status = habit.get("status")
    if status is not None and (type(status) is not int or status not in (0, 1)):
        raise ScheduleError("unsupported_habit_status")
    archived = None
    if status != 0:
        try:
            archived_at = stamp(habit.get("archivedTime"))
        except DATE_ERRORS as error:
            raise ScheduleError("invalid_archive_date") from error
        if status == 1 and archived_at is None:
            raise ScheduleError("missing_archive_date")
        if archived_at and archived_at < created:
            raise ScheduleError("contradictory_archival")
        archived = archived_at.astimezone(zone).date() if archived_at else None
    if habit.get("targetStartDate"):
        # TickTick can backdate a goal. An earlier recorded check-in in the
        # available history also establishes that the habit was in use.
        try:
            anchor = calendar_date(habit["targetStartDate"])
            for entry in habit.get("checkins", []):
                value = entry.get("value")
                if entry.get("status") == 1 or (type(value) in (int, float) and value > 0):
                    anchor = min(anchor, calendar_date(entry["stamp"]))
        except (TypeError, ValueError, KeyError) as error:
            raise ScheduleError("invalid_start_date") from error
    try:
        excluded = {calendar_date(value) for value in habit.get("exDates") or []}
    except (TypeError, ValueError) as error:
        raise ScheduleError("invalid_excluded_dates") from error
    if freq == "WEEKLY" and weekdays is None:
        weekdays = {anchor.weekday()}
    result = set()
    for offset in range((through - start).days + 1):
        day = start + timedelta(days=offset)
        if day < anchor or (archived and day > archived) or day in excluded:
            continue
        distance = (day - anchor).days
        if freq == "WEEKLY":
            distance = (distance + anchor.weekday()) // 7
        if distance % interval == 0 and (weekdays is None or day.weekday() in weekdays):
            result.add(day)
    return result


def checkin_kind(entry):
    # The schemas do not define an enum. Only explicitly recognised status/value
    # combinations are used; unverified statuses remain unclassified.
    status = entry.get("status")
    if type(status) is not int or status not in (0, 2):
        return "unknown"
    value, goal = entry.get("value"), entry.get("goal")
    if type(value) not in (int, float) or type(goal) not in (int, float):
        return "unknown"
    if goal <= 0 or any(isinstance(number, float) and not isfinite(number) for number in (value, goal)):
        return "unknown"
    if value >= goal:
        return "achieved"
    if status == 0 and value > 0:
        return "partial"
    return "unknown"


def habit_stats(habit, start, through, today, stamp, zone, windows, minutes, checkins_complete=True):
    schedule_reason = None
    try:
        scheduled = scheduled_days(habit, start, through, stamp, zone)
    except ScheduleError as error:
        scheduled = None
        schedule_reason = str(error)
    schedule_state = "known" if scheduled is not None else "unknown"

    grouped = defaultdict(list)
    for entry in habit["checkins"]:
        grouped[calendar_date(entry["stamp"])].append(entry)
    kinds = {}
    unknown_statuses = Counter()
    for day, entries in grouped.items():
        if len(entries) > 1:
            try:
                times = [stamp(entry.get("opTime")) for entry in entries]
            except DATE_ERRORS:
                times = []
            if times and all(when is not None for when in times):
                latest = max(times)
                entries = [entry for entry, when in zip(entries, times) if when == latest]
        possible = {checkin_kind(entry) for entry in entries}
        kinds[day] = possible.pop() if len(possible) == 1 else "unknown"
        if kinds[day] == "unknown" and start <= day <= through:
            unknown_statuses.update(
                str(entry["status"]) if entry.get("status") is not None else "missing" for entry in entries
            )

    achievements = {day for day, kind in kinds.items() if kind == "achieved"}

    def count(left, right):
        days = {day for day in scheduled if left <= day <= right} if scheduled is not None else None
        observed = {day: kind for day, kind in kinds.items()
                    if left <= day <= right and (days is None or day in days)}
        confirmed = sum(left <= day <= right for day in achievements)
        achieved = sum(kind == "achieved" for kind in observed.values())
        return {
            "scheduled": len(days) if days is not None else None,
            "achieved": achieved if checkins_complete else None,
            "confirmed_achievements": confirmed if checkins_complete else None,
            "off_schedule_achievements": confirmed - achieved if days is not None and checkins_complete else None,
            "partial": sum(kind == "partial" for kind in observed.values()) if checkins_complete else None,
            "skipped": None,
            "missed": (sum(day < today and day not in observed for day in days)
                       if days is not None and checkins_complete else None),
            "unknown": sum(kind == "unknown" for kind in observed.values()) if checkins_complete else None,
            "rate": achieved / len(days) if days and checkins_complete else None,
            "focus_minutes": minutes(left, right),
        }

    weeks = [
        {"from": left.isoformat(), "through": right.isoformat(), **count(left, right)}
        for left, right in windows
    ]
    achievement_deltas(weeks)
    return {
        **count(start, through),
        "schedule_state": schedule_state,
        "schedule_reason": schedule_reason,
        "weeks_with_known_schedule": len(weeks) if scheduled is not None else 0,
        "unknown_checkin_statuses": dict(unknown_statuses) if checkins_complete else None,
        "split_by_week": weeks,
    }


def achievement_deltas(windows):
    previous = None
    for window in windows:
        current = window["confirmed_achievements"]
        window["confirmed_achievements_delta"] = (
            current - previous if current is not None and previous is not None else None
        )
        previous = current


def habit_totals(stats, checkins_complete):
    known_schedules = sum(row["scheduled"] is not None for row in stats)
    totals = {"habits_with_known_schedule": known_schedules if stats or checkins_complete else None}
    for key in ("scheduled", "achieved", "confirmed_achievements", "partial", "unknown", "missed"):
        values = [row[key] for row in stats]
        available = checkins_complete or (key == "scheduled" and stats)
        totals[key] = sum(values) if available and all(value is not None for value in values) else None
    totals["off_schedule_achievements"] = (
        sum(row["off_schedule_achievements"] or 0 for row in stats)
        if checkins_complete and (known_schedules or not stats) else None
    )
    totals["rate"] = (totals["achieved"] / totals["scheduled"]
                      if totals["scheduled"] and totals["achieved"] is not None else None)
    return totals


def compact_habits(report):
    habits = []
    for habit in report["habits"]:
        if "stats" not in habit:
            habits.append(habit)
            continue
        row = {"id": habit["id"], "name": habit.get("name"), **habit["stats"]}
        if report.get("split_by_week") and "split_by_week" in row:
            windows = row.pop("split_by_week")
            row["weekly"] = {
                key: [window[key] for window in windows]
                for key in ("confirmed_achievements", "confirmed_achievements_delta", "scheduled",
                            "achieved", "off_schedule_achievements", "partial", "missed", "unknown", "focus_minutes")
                if any(window.get(key) is not None for window in windows)
            }
        habits.append(row)
    return {**report, "habits": habits}
