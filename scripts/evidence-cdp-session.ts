import { CdpRequestRegistry, type CdpResponseMessage } from "./cdp-request-registry.ts";

export type CdpWebSocketLike = {
  send(data: string): void;
  addEventListener(type: "message" | "close" | "error", listener: (event: unknown) => void): void;
  removeEventListener?(type: "message" | "close" | "error", listener: (event: unknown) => void): void;
};

type MessageEventLike = {
  data?: unknown;
};

function messageData(event: unknown): unknown {
  if (!event || typeof event !== "object" || !("data" in event)) return undefined;
  return (event as MessageEventLike).data;
}

export class EvidenceCdpSession {
  readonly #socket: CdpWebSocketLike;
  readonly #requestTimeoutMs: number;
  readonly #registry = new CdpRequestRegistry();
  #closed = false;

  readonly #onMessage = (event: unknown): void => {
    if (this.#closed) return;
    const data = messageData(event);
    if (typeof data !== "string") return;

    let message: unknown;
    try {
      message = JSON.parse(data);
    } catch {
      this.#fail(new Error("CDP socket sent invalid JSON"));
      return;
    }
    if (!message || typeof message !== "object") return;
    this.#registry.resolveMessage(message as CdpResponseMessage);
  };

  readonly #onClose = (): void => {
    this.#fail(new Error("CDP socket closed"));
  };

  readonly #onError = (): void => {
    this.#fail(new Error("CDP socket error"));
  };

  constructor(socket: CdpWebSocketLike, requestTimeoutMs: number) {
    if (!Number.isFinite(requestTimeoutMs) || requestTimeoutMs <= 0) {
      throw new Error("CDP request timeout must be greater than zero");
    }
    this.#socket = socket;
    this.#requestTimeoutMs = requestTimeoutMs;
    socket.addEventListener("message", this.#onMessage);
    socket.addEventListener("close", this.#onClose);
    socket.addEventListener("error", this.#onError);
  }

  get pendingCount(): number {
    return this.#registry.size;
  }

  request<T>(method: string, params: Record<string, unknown> = {}): Promise<T> {
    if (this.#closed) return Promise.reject(new Error("CDP socket is closed"));
    return this.#registry.request<T>(
      method,
      params,
      (message) => this.#socket.send(JSON.stringify(message)),
      this.#requestTimeoutMs,
    );
  }

  dispose(reason: unknown = new Error("CDP session disposed")): void {
    this.#fail(reason instanceof Error ? reason : new Error(String(reason)));
  }

  #fail(error: Error): void {
    if (this.#closed) return;
    this.#closed = true;
    this.#registry.rejectAll(error);
    this.#socket.removeEventListener?.("message", this.#onMessage);
    this.#socket.removeEventListener?.("close", this.#onClose);
    this.#socket.removeEventListener?.("error", this.#onError);
  }
}
