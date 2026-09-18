type CdpRequestMessage = {
  id: number;
  method: string;
  params: Record<string, unknown>;
};

export type CdpResponseMessage = {
  id?: number;
  result?: unknown;
  error?: { message?: string; code?: number };
};

type PendingRequest = {
  method: string;
  resolve: (value: unknown) => void;
  reject: (reason: Error) => void;
  timer: NodeJS.Timeout;
};

function asError(reason: unknown): Error {
  if (reason instanceof Error) return reason;
  return new Error(String(reason));
}

export class CdpRequestRegistry {
  #nextId = 1;
  #pending = new Map<number, PendingRequest>();

  get size(): number {
    return this.#pending.size;
  }

  request<T>(
    method: string,
    params: Record<string, unknown>,
    send: (message: CdpRequestMessage) => void,
    timeoutMs: number,
  ): Promise<T> {
    if (!Number.isFinite(timeoutMs) || timeoutMs <= 0) {
      return Promise.reject(new Error("CDP request timeout must be greater than zero"));
    }

    const id = this.#nextId++;
    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => {
        const pending = this.#pending.get(id);
        if (!pending) return;
        this.#pending.delete(id);
        pending.reject(new Error(`CDP ${method} request timed out after ${timeoutMs}ms`));
      }, timeoutMs);

      this.#pending.set(id, {
        method,
        resolve: resolve as (value: unknown) => void,
        reject,
        timer,
      });

      try {
        send({ id, method, params });
      } catch (error) {
        clearTimeout(timer);
        this.#pending.delete(id);
        reject(asError(error));
      }
    });
  }

  resolveMessage(message: CdpResponseMessage): boolean {
    if (typeof message.id !== "number") return false;
    const pending = this.#pending.get(message.id);
    if (!pending) return false;

    clearTimeout(pending.timer);
    this.#pending.delete(message.id);

    if (message.error) {
      const code = message.error.code === undefined ? "" : ` (${message.error.code})`;
      pending.reject(new Error(`CDP ${pending.method} failed${code}: ${message.error.message ?? "unknown error"}`));
      return true;
    }

    pending.resolve(message.result);
    return true;
  }

  rejectAll(reason: unknown): void {
    const error = asError(reason);
    for (const [id, pending] of this.#pending) {
      clearTimeout(pending.timer);
      this.#pending.delete(id);
      pending.reject(error);
    }
  }
}
