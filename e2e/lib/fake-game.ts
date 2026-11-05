// Spawn tools/fake-game and follow its stdout protocol (READY, COPIED <n>, EXIT).
import { spawn, type ChildProcess } from "node:child_process";
import { existsSync } from "node:fs";
import { createInterface } from "node:readline";
import { FAKE_GAME_BIN } from "./env.ts";

export interface FakeGameOptions {
  item?: string;
  advancedItem?: string;
  corpusId?: string;
  /** Hard stop so a hung run never leaks a window (seconds). */
  exitAfter?: number;
}

export class FakeGame {
  copies = 0;
  readonly proc: ChildProcess;
  private stderr = "";
  private waiters: (() => void)[] = [];
  private ready: Promise<void>;

  constructor(opts: FakeGameOptions) {
    if (!existsSync(FAKE_GAME_BIN)) {
      throw new Error(`fake-game not found at ${FAKE_GAME_BIN}; run \`cargo build -p fake-game\``);
    }
    const args: string[] = ["--exit-after", String(opts.exitAfter ?? 600)];
    if (opts.item) args.push("--item", opts.item);
    if (opts.advancedItem) args.push("--advanced-item", opts.advancedItem);
    if (opts.corpusId) args.push("--corpus-id", opts.corpusId);
    this.proc = spawn(FAKE_GAME_BIN, args, { stdio: ["ignore", "pipe", "pipe"] });
    this.proc.stderr?.on("data", (d: Buffer) => (this.stderr += d.toString()));
    const lines = createInterface({ input: this.proc.stdout! });
    this.ready = new Promise((resolve, reject) => {
      this.proc.once("exit", (code) => reject(new Error(`fake-game exited (${code}) before READY: ${this.stderr}`)));
      lines.on("line", (line) => {
        if (line === "READY") resolve();
        const m = /^COPIED (\d+)$/.exec(line);
        if (m) {
          this.copies = Number(m[1]);
          this.waiters.splice(0).forEach((w) => w());
        }
      });
    });
  }

  async waitReady(timeoutMs = 15000): Promise<void> {
    await withTimeout(this.ready, timeoutMs, `fake-game READY (stderr: ${this.stderr})`);
  }

  /** Wait until the game has served at least `n` copies (i.e. the app sent Ctrl+C to it). */
  async waitCopied(n: number, timeoutMs = 10000): Promise<void> {
    const reached = new Promise<void>((resolve) => {
      const check = () => (this.copies >= n ? resolve() : this.waiters.push(check));
      check();
    });
    await withTimeout(reached, timeoutMs, `fake-game COPIED ${n} (got ${this.copies}); the app never sent the copy chord`);
  }

  stop(): void {
    if (this.proc.exitCode === null) this.proc.kill();
  }
}

export async function startFakeGame(opts: FakeGameOptions): Promise<FakeGame> {
  const g = new FakeGame(opts);
  await g.waitReady();
  return g;
}

function withTimeout<T>(p: Promise<T>, ms: number, what: string): Promise<T> {
  let t: NodeJS.Timeout;
  return Promise.race([
    p.finally(() => clearTimeout(t)),
    new Promise<T>((_, reject) => {
      t = setTimeout(() => reject(new Error(`timed out after ${ms}ms waiting for ${what}`)), ms);
    }),
  ]);
}
