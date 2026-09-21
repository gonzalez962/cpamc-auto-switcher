import { spawn } from "node:child_process";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

const CLI = "cpamc-auto-switcher";
const TIMEOUT_MS = 15_000;
const TERMINAL_STATUSES = new Set([
	"completed",
	"failed",
	"cancelled",
	"interrupted",
]);

const RATE_LIMIT_PATTERN = /\b429\b|rate.?limit|too many requests|quota|resource.?exhausted/i;

function isRateLimitOrQuotaError(text: unknown): boolean {
	if (typeof text !== "string") return false;
	return RATE_LIMIT_PATTERN.test(text);
}

type TaskMeta = {
	id?: string;
	agent?: string;
	status?: string;
	mode?: string;
	effective_mode?: string;
	error?: string;
	result?: string;
};

type ToolDetails = {
	results?: unknown[];
	waited_task_ids?: unknown[];
};

type ExecResult = {
	stdout: string;
	stderr: string;
	code: number | null;
};

// Run cpamc-auto-switcher without popping transient console windows on Windows (windowsHide: true)
function runAutoSwitcher(signal: AbortSignal | undefined, force = false): Promise<ExecResult> {
	if (signal?.aborted) {
		return Promise.reject(new Error("aborted"));
	}

	const args = force ? ["--force"] : [];

	return new Promise((resolve, reject) => {
		const child = spawn(CLI, args, {
			stdio: ["ignore", "pipe", "pipe"],
			windowsHide: process.platform === "win32",
		});

		let stdout = "";
		let stderr = "";
		let settled = false;
		let timeoutHandle: ReturnType<typeof setTimeout> | null = null;
		let removeAbortListener: (() => void) | null = null;

		const cleanup = (): void => {
			if (timeoutHandle !== null) {
				clearTimeout(timeoutHandle);
				timeoutHandle = null;
			}
			if (removeAbortListener !== null) {
				removeAbortListener();
				removeAbortListener = null;
			}
		};

		const settle = (finalize: () => void, waitForClose: boolean): void => {
			if (settled) return;
			settled = true;
			cleanup();
			if (!waitForClose) {
				finalize();
				return;
			}
			try {
				child.kill();
			} catch {
				// child might have exited already
			}
			const closeGuard = setTimeout(() => finalize(), 1000);
			child.once("close", () => {
				clearTimeout(closeGuard);
				finalize();
			});
		};

		const onAbort = (): void => settle(() => reject(new Error("aborted")), true);

		timeoutHandle = setTimeout(() => {
			settle(() => reject(new Error("timeout")), true);
		}, TIMEOUT_MS);

		if (signal) {
			signal.addEventListener("abort", onAbort, { once: true });
			removeAbortListener = (): void => {
				signal.removeEventListener("abort", onAbort);
			};
		}

		child.stdout?.setEncoding("utf8");
		child.stderr?.setEncoding("utf8");
		child.stdout?.on("data", (chunk: string) => {
			stdout += chunk;
		});
		child.stderr?.on("data", (chunk: string) => {
			stderr += chunk;
		});

		child.once("error", (err) => settle(() => reject(err), false));
		child.once("close", (code) => settle(() => resolve({ stdout, stderr, code }), false));
	});
}

export default function autoSwitcherExtension(pi: ExtensionAPI): void {
	// Serialize switcher executions to prevent concurrent race conditions when mutating prefixes
	let queue: Promise<void> = Promise.resolve();
	let lastForcedRunTime = 0;
	const FORCED_DEBOUNCE_MS = 3_000;

	function notify(ctx: any, message: string, level: "info" | "warning" = "info"): void {
		try {
			ctx?.ui?.notify?.(message, level);
		} catch {
			// Notifications are best-effort
		}
	}

	function schedule(ctx: any, source: string, force = false): Promise<void> {
		if (force) {
			const now = Date.now();
			if (now - lastForcedRunTime < FORCED_DEBOUNCE_MS) {
				return queue;
			}
			lastForcedRunTime = now;
		}

		const check = async (): Promise<void> => {
			try {
				const result = await runAutoSwitcher(ctx?.signal, force);
				const output = (result.stdout || result.stderr).trim();

				// If CLI returned no output (e.g. within cooldown timeout), do not show any notification
				if (!output) {
					return;
				}

				if (result.code !== 0) {
					notify(ctx, `cpamc-auto-switcher: error after ${source}: ${output}`, "warning");
					return;
				}

				if (output.startsWith("[ROTATED]")) {
					notify(ctx, `🔄 ${output}`, "info");
				} else {
					const message = output.replace(/^\[OK\]\s*/, "").trim();
					if (message) {
						notify(ctx, message, "info");
					}
				}
			} catch (err: any) {
				notify(ctx, `cpamc-auto-switcher unavailable (${source}): ${err?.message || err}`, "warning");
			}
		};

		return queue = queue.then(check, check);
	}

	// 1. Immediate detection of HTTP 429 response from provider before stream consumption
	pi.on("after_provider_response", (event: any, ctx: any) => {
		if (event?.status === 429) {
			notify(ctx, "⚡ Rate limit (429) detectado en proveedor. Ejecutando cambio forzado de cuenta...", "warning");
			schedule(ctx, "provider status 429", true);
		}
	});

	// 2. Assistant message error with 429 or quota exhaustion (SDK error stopReason)
	pi.on("message_end", (event: any, ctx: any) => {
		const message = event?.message;
		if (message?.role === "assistant" && message?.stopReason === "error") {
			if (isRateLimitOrQuotaError(message.errorMessage)) {
				notify(ctx, `⚡ Error de cuota/429 detectado: ${message.errorMessage.split("\n")[0]}`, "warning");
				schedule(ctx, "assistant message 429/quota error", true);
			}
		}
	});

	// 3. After every main agent assistant turn finishes
	pi.on("turn_end", (event: any, ctx: any) => {
		const message = event?.message;
		if (message?.stopReason === "error" && isRateLimitOrQuotaError(message?.errorMessage)) {
			schedule(ctx, "turn 429 error", true);
		} else {
			schedule(ctx, "agent turn", false);
		}
	});

	// 4. Synchronous task-mode subagent completions
	pi.on("tool_result", (event: any, ctx: any) => {
		if (event?.toolName !== "subagent_run" && event?.toolName !== "subagent_continue") {
			return;
		}

		const details = event.details as ToolDetails | undefined;
		if (!Array.isArray(details?.results) || details.results.length === 0) return;

		const waitedIds = Array.isArray(details.waited_task_ids)
			? new Set(details.waited_task_ids.filter((id): id is string => typeof id === "string"))
			: undefined;

		for (const value of details.results) {
			const task = readTask(value);
			if (!task || !task.status || !TERMINAL_STATUSES.has(task.status)) continue;
			if ((task.effective_mode ?? task.mode) === "background") continue;
			if (waitedIds && (!task.id || !waitedIds.has(task.id))) continue;

			const hasRateLimit = task.status === "failed" && isRateLimitOrQuotaError(task.error || task.result);
			schedule(ctx, `subagent ${task.agent || task.id || "completed"}`, hasRateLimit);
		}
	});

	// 5. Background subagent completions
	pi.on("message_start", (event: any, ctx: any) => {
		const message = event?.message;
		if (message?.customType !== "subagent-completion") return;
		const task = readTask(message?.details?.task);
		const hasRateLimit = task?.status === "failed" && isRateLimitOrQuotaError(task?.error || task?.result);
		schedule(ctx, `bg-subagent ${task?.agent || task?.id || "completed"}`, hasRateLimit);
	});
}

function readTask(value: unknown): TaskMeta | undefined {
	if (!value || typeof value !== "object") return undefined;
	const source = value as Record<string, unknown>;
	const task: TaskMeta = {};

	for (const key of ["id", "agent", "status", "mode", "effective_mode", "error", "result"] as const) {
		if (typeof source[key] === "string") task[key] = source[key];
	}

	return Object.keys(task).length > 0 ? task : undefined;
}
