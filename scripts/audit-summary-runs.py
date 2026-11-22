#!/usr/bin/env python3
"""Count rollout activity by turn without emitting messages or tool input."""

import argparse
from collections import Counter
from dataclasses import dataclass, field
import json
from pathlib import Path
import re


TOOL_NAMES = {"function_call", "custom_tool_call"}
NESTED_TOOL = re.compile(r"\btools\s*\.\s*(\w+)\s*\(")
SESSION_ID = re.compile(r"[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}")


@dataclass
class Turn:
    responses: set = field(default_factory=set)
    calls: Counter = field(default_factory=Counter)
    poll_requests: int = 0
    poll_tool_calls: int = 0
    poll_only_tool_calls: int = 0
    shell_requests: int = 0
    complete: bool = False
    duration_ms: int | None = None

    def add_tool(self, payload):
        name = payload["name"].rsplit(".", 1)[-1]
        self.calls[name] += 1
        if name == "exec":
            code = payload.get("input") or payload.get("arguments", "")
            if isinstance(code, str):
                nested = NESTED_TOOL.findall(code)
            else:
                nested = []
        elif name in ("exec_command", "write_stdin"):
            nested = [name]
        else:
            nested = []
        polls = nested.count("write_stdin")
        self.poll_requests += polls
        self.poll_tool_calls += int(polls > 0)
        self.poll_only_tool_calls += int(polls > 0 and polls == len(nested))
        self.shell_requests += nested.count("exec_command")


def rollout_path(value, sessions):
    if not SESSION_ID.fullmatch(value):
        return Path(value)
    matches = list(sessions.glob(f"*/*/*/rollout-*-{value}.jsonl"))
    if len(matches) != 1:
        raise ValueError(f"expected 1 rollout for {value}, got {len(matches)}")
    return matches[0]


def read_turns(path):
    turns = {}
    active = None
    with path.open(encoding="utf-8") as stream:
        for line in stream:
            row = json.loads(line)
            kind = row["type"]
            payload = row.get("payload", {})
            item = payload.get("type")
            if kind == "event_msg" and item == "task_started":
                active = payload["turn_id"]
                turns[active] = Turn()
            elif kind == "event_msg" and item == "task_complete":
                turn_id = payload["turn_id"]
                turn = turns[turn_id]
                turn.complete = True
                turn.duration_ms = payload.get("duration_ms")
                if active == turn_id:
                    active = None
            elif kind == "token_usage_record":
                turn = turns.get(payload.get("turn_id"))
                response_id = payload.get("response_id")
                if turn is not None and response_id:
                    turn.responses.add(response_id)
            elif active is not None and kind == "response_item" and item in TOOL_NAMES:
                turns[active].add_tool(payload)
    return turns.values()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("rollout", nargs="+", metavar="LABEL=ID_OR_PATH")
    parser.add_argument("--sessions", type=Path, default=Path.home() / ".codex" / "sessions")
    parser.add_argument("--expect-turns", type=int)
    args = parser.parse_args()

    print("\t".join(("case", "turn", "responses", "tools", "send_message",
                    "poll_requests", "poll_tool_calls", "poll_only_tool_calls", "shell_requests",
                    "duration_ms", "task_complete")))
    valid = True
    for spec in args.rollout:
        label, separator, value = spec.partition("=")
        if not separator or not label or not value:
            parser.error("specify each rollout as LABEL=ID_OR_PATH")
        turns = list(read_turns(rollout_path(value, args.sessions)))
        valid &= bool(turns) and all(turn.complete for turn in turns)
        if args.expect_turns is not None:
            valid &= len(turns) == args.expect_turns
        for index, turn in enumerate(turns, 1):
            fields = (label, index, len(turn.responses), turn.calls.total(),
                      turn.calls["send_message"], turn.poll_requests, turn.poll_tool_calls,
                      turn.poll_only_tool_calls, turn.shell_requests,
                      turn.duration_ms if turn.duration_ms is not None else "",
                      str(turn.complete).lower())
            print("\t".join(str(field) for field in fields))
    return 0 if valid else 1


if __name__ == "__main__":
    raise SystemExit(main())
