import { useCallback, useEffect, useState } from "react";
import { useT } from "../i18n/shared";
import { useDataSurface } from "../data-surface";
import { DataSurfaceSkeleton } from "../components/data-surface";
import { navigateHash } from "../hash-routing";
import { fabricTasksResourceKey } from "../nav-board-resources";
import { ToastNotice, type NoticeTone } from "../ui";
import {
  closeFabricTask,
  createFabricTask,
  fetchFabricEnabled,
  fetchFabricTaskDetail,
  fetchFabricTasks,
  removeFabricTask,
  startFabricTask,
  type FabricTaskDetail,
  type FabricTaskSummary,
  type FabricTimelineEntry,
} from "./tasks-api";

type TaskNotice = { text: string; tone: NoticeTone };

const EMPTY_TASKS: FabricTaskSummary[] = [];

function useTaskNotice() {
  const [notice, setNotice] = useState<TaskNotice | null>(null);
  const [revision, setRevision] = useState(0);
  const show = useCallback((text: string, ok: boolean) => {
    setNotice({ text, tone: ok ? "ok" : "err" });
    setRevision(current => current + 1);
  }, []);
  const clear = useCallback(() => setNotice(null), []);
  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(clear, notice.tone === "ok" ? 4500 : 15000);
    return () => window.clearTimeout(timer);
  }, [notice, revision, clear]);
  return { notice, show, clear };
}

function TasksOffBoard() {
  const t = useT();
  return (
    <div className="tasks-page">
      <div className="page-head">
        <h2>{t("tasks.offTitle")}</h2>
      </div>
      <div className="tasks-off">
        <div className="control-row">
          <div className="control-row-copy">
            <strong>{t("tasks.offTitle")}</strong>
            <span className="muted">{t("tasks.offHint")}</span>
          </div>
          <button
            type="button"
            className="providers-link providers-link--plain"
            onClick={() => navigateHash("startup")}
          >
            {t("tasks.offAction")}
          </button>
        </div>
      </div>
    </div>
  );
}

function TasksList({
  tasks,
  selectedId,
  onSelect,
}: {
  tasks: FabricTaskSummary[];
  selectedId: string | null;
  onSelect: (taskId: string) => void;
}) {
  const t = useT();
  if (tasks.length === 0) {
    return <div className="control-row"><span className="muted">{t("tasks.empty")}</span></div>;
  }
  return (
    <div className="tasks-list" aria-label={t("tasks.title")}>
      {tasks.map(task => {
        const active = task.taskId === selectedId;
        return (
          <button
            key={task.taskId}
            type="button"
            className={`tasks-row${active ? " is-active" : ""}`}
            onClick={() => onSelect(task.taskId)}
            aria-pressed={active}
          >
            <span>{task.title}</span>
            <span className="muted"><code>{task.taskState}</code></span>
          </button>
        );
      })}
    </div>
  );
}

function TasksTimeline({ entries }: { entries: FabricTimelineEntry[] }) {
  const t = useT();
  if (entries.length === 0) return <span className="muted">—</span>;
  return (
    <div className="tasks-timeline" aria-label={t("tasks.timeline")}>
      {entries.map(entry => (
        <div key={entry.sequence} className="control-row">
          <div className="control-row-copy">
            <strong><code>{entry.eventType}</code></strong>
            <span className="muted">{entry.actorId} · {entry.category}</span>
          </div>
          <span className="muted">{entry.sequence}</span>
        </div>
      ))}
    </div>
  );
}

function taskActionOkKey(action: "start" | "close" | "remove") {
  if (action === "start") return "tasks.startOk" as const;
  if (action === "close") return "tasks.closeOk" as const;
  return "tasks.removeOk" as const;
}

async function runFabricTaskAction(apiBase: string, taskId: string, action: "start" | "close" | "remove") {
  if (action === "start") {
    await startFabricTask(apiBase, taskId);
    return;
  }
  if (action === "close") {
    await closeFabricTask(apiBase, taskId);
    return;
  }
  await removeFabricTask(apiBase, taskId);
}

function TasksDetailPanel({
  apiBase,
  taskId,
  onNotice,
  onChanged,
}: {
  apiBase: string;
  taskId: string;
  onNotice: (text: string, ok: boolean) => void;
  onChanged: () => void;
}) {
  const t = useT();
  const [detail, setDetail] = useState<FabricTaskDetail | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      try {
        const next = await fetchFabricTaskDetail(apiBase, taskId, controller.signal);
        setDetail(next);
      } catch {
        if (controller.signal.aborted) return;
        setDetail(null);
        onNotice(t("tasks.denied"), false);
      }
    })();
    return () => controller.abort();
  }, [apiBase, taskId, onNotice, t]);

  const runAction = async (action: "start" | "close" | "remove") => {
    if (busy) return;
    setBusy(true);
    try {
      await runFabricTaskAction(apiBase, taskId, action);
      onNotice(t(taskActionOkKey(action)), true);
      if (action === "remove") {
        onChanged();
        return;
      }
      setDetail(await fetchFabricTaskDetail(apiBase, taskId));
      onChanged();
    } catch {
      onNotice(t("tasks.denied"), false);
    } finally {
      setBusy(false);
    }
  };

  if (!detail) {
    return <DataSurfaceSkeleton label={t("tasks.title")} rows={4} />;
  }

  const stateLabel = detail.summary.runState
    ? `${detail.summary.taskState} / ${detail.summary.runState}`
    : detail.summary.taskState;

  return (
    <div className="tasks-detail">
      <div className="control-row">
        <div className="control-row-copy">
          <span className="muted">{t("tasks.titleField")}</span>
          <strong>{detail.summary.title}</strong>
        </div>
      </div>
      <div className="control-row">
        <div className="control-row-copy">
          <span className="muted">{t("tasks.goal")}</span>
          <strong>{detail.goal || "—"}</strong>
        </div>
      </div>
      <div className="control-row">
        <div className="control-row-copy">
          <span className="muted">{t("tasks.state")}</span>
          <strong><code>{stateLabel}</code></strong>
        </div>
      </div>
      <div className="control-row">
        <div className="control-row-copy">
          <span className="muted">{t("tasks.timeline")}</span>
          <TasksTimeline entries={detail.timeline} />
        </div>
      </div>
      <div className="tasks-detail-actions">
        <button type="button" className="providers-link providers-link--plain" disabled={busy} onClick={() => { void runAction("start"); }}>
          {t("tasks.start")}
        </button>
        <button type="button" className="providers-link providers-link--plain" disabled={busy} onClick={() => { void runAction("close"); }}>
          {t("tasks.close")}
        </button>
        <button type="button" className="providers-link providers-link--plain" disabled={busy} onClick={() => { void runAction("remove"); }}>
          {t("tasks.remove")}
        </button>
      </div>
    </div>
  );
}

function TasksEnabledBoard({
  apiBase,
  onNotice,
}: {
  apiBase: string;
  onNotice: (text: string, ok: boolean) => void;
}) {
  const t = useT();
  const [title, setTitle] = useState("");
  const [goal, setGoal] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const loadList = useCallback((signal: AbortSignal) => fetchFabricTasks(apiBase, signal), [apiBase]);
  const list = useDataSurface(fabricTasksResourceKey(apiBase), [apiBase], loadList, {
    isEmpty: tasks => tasks.length === 0,
  });
  const tasks = list.data ?? EMPTY_TASKS;
  const activeId = selectedId && tasks.some(task => task.taskId === selectedId)
    ? selectedId
    : (tasks[0]?.taskId ?? null);

  const onCreate = async () => {
    if (creating) return;
    setCreating(true);
    try {
      const id = await createFabricTask(apiBase, title.trim(), goal.trim());
      setTitle("");
      setGoal("");
      setSelectedId(id);
      onNotice(t("tasks.created"), true);
      list.refresh();
    } catch {
      onNotice(t("tasks.denied"), false);
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="tasks-page">
      <div className="page-head">
        <h2>{t("tasks.title")}</h2>
        <div className="page-head-actions">
          <label className="sr-only" htmlFor="tasks-title-field">{t("tasks.titleField")}</label>
          <input
            id="tasks-title-field"
            className="input"
            value={title}
            onChange={event => setTitle(event.target.value)}
            placeholder={t("tasks.titleField")}
          />
          <label className="sr-only" htmlFor="tasks-goal-field">{t("tasks.goal")}</label>
          <input
            id="tasks-goal-field"
            className="input"
            value={goal}
            onChange={event => setGoal(event.target.value)}
            placeholder={t("tasks.goal")}
          />
          <button type="button" className="providers-link providers-link--plain" disabled={creating} onClick={() => { void onCreate(); }}>
            {t("tasks.create")}
          </button>
        </div>
      </div>
      {list.state.showSkeleton ? <DataSurfaceSkeleton label={t("tasks.title")} rows={5} /> : (
        <div className="tasks-split">
          <TasksList tasks={tasks} selectedId={activeId} onSelect={setSelectedId} />
          {activeId ? (
            <TasksDetailPanel
              apiBase={apiBase}
              taskId={activeId}
              onNotice={onNotice}
              onChanged={() => list.refresh()}
            />
          ) : (
            <div className="tasks-detail">
              <div className="control-row"><span className="muted">{t("tasks.empty")}</span></div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function TasksBody({
  fabricOn,
  apiBase,
  onNotice,
}: {
  fabricOn: boolean | null;
  apiBase: string;
  onNotice: (text: string, ok: boolean) => void;
}) {
  const t = useT();
  if (fabricOn === null) return <DataSurfaceSkeleton label={t("tasks.title")} rows={4} />;
  if (fabricOn) return <TasksEnabledBoard apiBase={apiBase} onNotice={onNotice} />;
  return <TasksOffBoard />;
}

export default function Tasks({ apiBase }: { apiBase: string }) {
  const t = useT();
  const [fabricOn, setFabricOn] = useState<boolean | null>(null);
  const notice = useTaskNotice();

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      try {
        setFabricOn(await fetchFabricEnabled(apiBase, controller.signal));
      } catch {
        if (!controller.signal.aborted) setFabricOn(false);
      }
    })();
    return () => controller.abort();
  }, [apiBase]);

  return (
    <>
      <TasksBody fabricOn={fabricOn} apiBase={apiBase} onNotice={notice.show} />
      {notice.notice ? (
        <ToastNotice tone={notice.notice.tone} dismissLabel={t("common.close")} onDismiss={notice.clear}>
          {notice.notice.text}
        </ToastNotice>
      ) : null}
    </>
  );
}
