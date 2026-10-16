"""Owner-checked, bounded retained-History pagination and Run navigation."""

from __future__ import annotations

import asyncio
from datetime import timedelta
from typing import Any

from temporalio.api.common.v1 import WorkflowExecution
from temporalio.api.enums.v1 import EventType
from temporalio.api.workflowservice.v1 import (
    GetWorkflowExecutionHistoryRequest, GetWorkflowExecutionHistoryReverseRequest,
)
from temporalio.service import RPCError, RPCStatusCode

from ..protocol import TaskInvalidRequest, TaskNotFound, TaskServiceError, TaskUnavailable, valid_run_id
from .cursors import CursorCodec
from .runs import OwnedRuns, RunMissing


class HistoryReader:
    def __init__(self, runs: OwnedRuns, cursors: CursorCodec, timeout_seconds: float):
        self._runs = runs
        self._cursors = cursors
        self.client = runs.client
        self._seconds = timeout_seconds
        self._timeout = timedelta(seconds=timeout_seconds)

    async def _navigation(self, owner: str, task_id: str, current: tuple[Any, dict[str, Any]]) -> dict[str, Any]:
        description, _ = current
        execution = WorkflowExecution(workflow_id=task_id, run_id=description.run_id)
        first = await self.client.workflow_service.get_workflow_execution_history(
            GetWorkflowExecutionHistoryRequest(namespace=self.client.namespace, execution=execution,
                                                maximum_page_size=1), retry=True, timeout=self._timeout)
        if (not first.history.events or first.history.events[0].event_id != 1
                or not first.history.events[0].HasField("workflow_execution_started_event_attributes")):
            raise TaskUnavailable()
        started = first.history.events[0].workflow_execution_started_event_attributes
        if (started.first_execution_run_id != self._runs.first_run_id(description)
                or started.workflow_type.name != description.workflow_type):
            raise TaskNotFound()
        last = await self.client.workflow_service.get_workflow_execution_history_reverse(
            GetWorkflowExecutionHistoryReverseRequest(namespace=self.client.namespace, execution=execution,
                                                       maximum_page_size=1), retry=True, timeout=self._timeout)
        next_run_id = None
        if last.history.events and last.history.events[0].HasField("workflow_execution_continued_as_new_event_attributes"):
            next_run_id = last.history.events[0].workflow_execution_continued_as_new_event_attributes.new_execution_run_id
        if getattr(description.status, "name", None) == "CONTINUED_AS_NEW" and not next_run_id:
            raise TaskUnavailable()
        navigation = {"first_execution_run_id": self._runs.first_run_id(description),
                      "previous_run_id": None, "next_run_id": None, "history_navigation_incomplete": False}
        for key, run_id in (("previous_run_id", started.continued_execution_run_id), ("next_run_id", next_run_id)):
            if not run_id:
                continue
            if not valid_run_id(run_id) or run_id == description.run_id:
                raise TaskNotFound()
            try:
                adjacent = await self._runs.get(owner, task_id, run_id=run_id)
                self._runs.require_same_chain(current, adjacent)
            except RunMissing:
                # An expired adjacent Run grants no navigation capability, but
                # this independently owned Run's retained History remains useful.
                navigation["history_navigation_incomplete"] = True
                continue
            navigation[key] = run_id
        return navigation

    async def events(self, owner: str, task_id: str, cursor: str | None = None, limit: int = 50,
                     *, run_id: str | None = None) -> dict[str, Any]:
        """Read native retained History, newest first; no token-stream guarantee.

        The signed exclusive event-ID boundary survives appended History and API
        restarts. Each page and event ID belongs to one Run; navigation never
        merges them into a task-global sequence. Old pages scan from the head
        under one deadline and a 64-page cap; there is no event projection.
        """
        if type(limit) is not int or not 1 <= limit <= 100:
            raise TaskInvalidRequest("limit")
        if run_id is not None and not valid_run_id(run_id):
            raise TaskInvalidRequest("run_id")
        try:
            async with asyncio.timeout(self._seconds):
                latest = await self._runs.latest(owner, task_id)
                parsed = self._cursors.decode(self.client.namespace, cursor, owner, task_id) if cursor is not None else None
                if parsed is not None and run_id is not None and parsed["run_id"] != run_id:
                    raise TaskInvalidRequest("run_id")
                selected_run_id = parsed["run_id"] if parsed is not None else run_id
                current = latest
                if selected_run_id is not None and selected_run_id != latest[0].run_id:
                    current = await self._runs.get(owner, task_id, run_id=selected_run_id)
                    self._runs.require_same_chain(latest, current)
                description, _ = current
                before = parsed["before_event_id"] if parsed is not None else None
                navigation = await self._navigation(owner, task_id, current)
                token, events = b"", []
                for _ in range(64):
                    page = await self.client.workflow_service.get_workflow_execution_history_reverse(
                        GetWorkflowExecutionHistoryReverseRequest(
                            namespace=self.client.namespace,
                            execution=WorkflowExecution(workflow_id=task_id, run_id=description.run_id),
                            maximum_page_size=100, next_page_token=token,
                        ), retry=True, timeout=self._timeout,
                    )
                    for event in page.history.events:
                        if before is not None and event.event_id >= before:
                            continue
                        events.append({"event_id": event.event_id,
                                       "type": EventType.Name(event.event_type).removeprefix("EVENT_TYPE_").lower(),
                                       "time": event.event_time.ToDatetime().isoformat() + "+00:00"})
                        if len(events) == limit:
                            next_cursor = self._cursors.encode(self.client.namespace, owner, task_id, description.run_id, event.event_id) if event.event_id > 1 else None
                            return {"task_id": task_id, "run_id": description.run_id, "order": "descending",
                                    "events": events, "next_cursor": next_cursor, **navigation}
                    token = page.next_page_token
                    if not token:
                        return {"task_id": task_id, "run_id": description.run_id, "order": "descending",
                                "events": events, "next_cursor": None, **navigation}
                raise TaskUnavailable({"operation": "history_scan_limit"})
        except TaskServiceError:
            raise
        except RPCError as error:
            if error.status == RPCStatusCode.NOT_FOUND:
                raise TaskNotFound() from None
            raise TaskUnavailable() from None
        except Exception:
            raise TaskUnavailable() from None
