import { readJsonIfOk, readJsonOrThrow } from "../fetch-json";

export type FabricTaskSummary = {
  taskId: string;
  title: string;
  taskState: string;
  runState: string;
  currentOwner: string;
  latestActivity: number;
  eventCount: number;
  terminal: boolean;
};

export type FabricTimelineEntry = {
  sequence: number;
  eventType: string;
  occurredAt: number;
  actorId: string;
  category: string;
};

export type FabricTaskDetail = {
  summary: FabricTaskSummary;
  goal: string;
  timeline: FabricTimelineEntry[];
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function asString(value: unknown): string | null {
  return typeof value === "string" ? value : null;
}

function asFiniteNumber(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function asBoolean(value: unknown): boolean | null {
  return typeof value === "boolean" ? value : null;
}

export function parseFabricTaskSummary(value: unknown): FabricTaskSummary | null {
  if (!isRecord(value)) return null;
  const taskId = asString(value.taskId);
  const title = asString(value.title);
  const taskState = asString(value.taskState);
  const runState = asString(value.runState);
  const currentOwner = asString(value.currentOwner);
  const latestActivity = asFiniteNumber(value.latestActivity);
  const eventCount = asFiniteNumber(value.eventCount);
  const terminal = asBoolean(value.terminal);
  if (
    taskId === null
    || title === null
    || taskState === null
    || runState === null
    || currentOwner === null
    || latestActivity === null
    || eventCount === null
    || terminal === null
  ) {
    return null;
  }
  return { taskId, title, taskState, runState, currentOwner, latestActivity, eventCount, terminal };
}

export function parseFabricTimelineEntry(value: unknown): FabricTimelineEntry | null {
  if (!isRecord(value)) return null;
  const sequence = asFiniteNumber(value.sequence);
  const eventType = asString(value.eventType);
  const occurredAt = asFiniteNumber(value.occurredAt);
  const actorId = asString(value.actorId);
  const category = asString(value.category);
  if (
    sequence === null
    || eventType === null
    || occurredAt === null
    || actorId === null
    || category === null
  ) {
    return null;
  }
  return { sequence, eventType, occurredAt, actorId, category };
}

export function parseFabricTaskList(value: unknown): FabricTaskSummary[] | null {
  if (!isRecord(value) || !Array.isArray(value.tasks)) return null;
  const tasks: FabricTaskSummary[] = [];
  for (const entry of value.tasks) {
    const parsed = parseFabricTaskSummary(entry);
    if (!parsed) return null;
    tasks.push(parsed);
  }
  return tasks;
}

export function parseFabricTaskDetail(value: unknown): FabricTaskDetail | null {
  if (!isRecord(value)) return null;
  const summary = parseFabricTaskSummary(value.summary);
  if (!summary) return null;
  const projection = isRecord(value.projection) ? value.projection : null;
  const goal = projection ? asString(projection.goal) ?? "" : "";
  if (!Array.isArray(value.timeline)) return null;
  const timeline: FabricTimelineEntry[] = [];
  for (const entry of value.timeline) {
    const parsed = parseFabricTimelineEntry(entry);
    if (!parsed) return null;
    timeline.push(parsed);
  }
  return { summary, goal, timeline };
}

export async function fetchFabricEnabled(apiBase: string, signal?: AbortSignal): Promise<boolean> {
  const res = await fetch(`${apiBase}/api/fabric/status`, { signal });
  const data = await readJsonIfOk<{ enabled?: unknown }>(res);
  return data?.enabled === true;
}

export async function fetchFabricTasks(apiBase: string, signal?: AbortSignal): Promise<FabricTaskSummary[]> {
  const res = await fetch(`${apiBase}/api/fabric/tasks`, { signal });
  const data = await readJsonOrThrow<unknown>(res);
  const parsed = parseFabricTaskList(data);
  if (!parsed) throw new Error("fabric-tasks-unavailable");
  return parsed;
}

export async function fetchFabricTaskDetail(
  apiBase: string,
  taskId: string,
  signal?: AbortSignal,
): Promise<FabricTaskDetail> {
  const res = await fetch(`${apiBase}/api/fabric/tasks/${encodeURIComponent(taskId)}`, { signal });
  const data = await readJsonOrThrow<unknown>(res);
  const parsed = parseFabricTaskDetail(data);
  if (!parsed) throw new Error("fabric-task-unavailable");
  return parsed;
}

export async function createFabricTask(apiBase: string, title: string, goal: string): Promise<string> {
  const res = await fetch(`${apiBase}/api/fabric/tasks`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ title, goal }),
  });
  const data = await readJsonOrThrow<{ id?: unknown }>(res);
  const id = typeof data?.id === "string" ? data.id : "";
  if (!id) throw new Error("fabric-task-create-unavailable");
  return id;
}

export async function startFabricTask(apiBase: string, taskId: string): Promise<void> {
  const res = await fetch(`${apiBase}/api/fabric/tasks/${encodeURIComponent(taskId)}/start`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ owner: "operator" }),
  });
  await readJsonOrThrow(res);
}

export async function closeFabricTask(apiBase: string, taskId: string): Promise<void> {
  const res = await fetch(`${apiBase}/api/fabric/tasks/${encodeURIComponent(taskId)}/close`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ owner: "operator" }),
  });
  await readJsonOrThrow(res);
}

export async function removeFabricTask(apiBase: string, taskId: string): Promise<void> {
  const res = await fetch(`${apiBase}/api/fabric/tasks/${encodeURIComponent(taskId)}`, {
    method: "DELETE",
  });
  await readJsonOrThrow(res);
}
