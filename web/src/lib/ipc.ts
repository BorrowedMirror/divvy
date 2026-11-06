import type { IpcError_Serialize } from "@src/bindings";

/** Error thrown by `unwrap` so TanStack Query / callers get a typed IPC error. */
export class IpcFailure extends Error {
  readonly kind: string;
  readonly retryAfterMs: number | null;

  constructor(error: IpcError_Serialize) {
    super(error.message);
    this.name = "IpcFailure";
    this.kind = error.kind;
    this.retryAfterMs = error.retry_after_ms ?? null;
  }

  toIpcError(): IpcError_Serialize {
    return { kind: this.kind, message: this.message, retry_after_ms: this.retryAfterMs };
  }
}

type Result<T> = { status: "ok"; data: T } | { status: "error"; error: IpcError_Serialize };

/** Turn a tauri-specta `Result` into a value or a thrown `IpcFailure`. */
export function unwrap<T>(result: Result<T>): T {
  if (result.status === "ok") return result.data;
  throw new IpcFailure(result.error);
}

/** Normalise anything caught into an `IpcError` shape for display. */
export function toIpcError(err: unknown): IpcError_Serialize {
  if (err instanceof IpcFailure) return err.toIpcError();
  if (err instanceof Error) return { kind: "internal", message: err.message, retry_after_ms: null };
  if (isIpcErrorLike(err)) {
    return { kind: err.kind, message: err.message, retry_after_ms: err.retry_after_ms ?? null };
  }
  return { kind: "internal", message: String(err), retry_after_ms: null };
}

function isIpcErrorLike(v: unknown): v is { kind: string; message: string; retry_after_ms?: number | null } {
  return typeof v === "object" && v !== null && "kind" in v && "message" in v;
}
