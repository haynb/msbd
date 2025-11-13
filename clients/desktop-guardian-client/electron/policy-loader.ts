import fs from "node:fs";
import path from "node:path";
import { z } from "zod";

const rectSchema = z.object({
  x: z.number().nonnegative().default(0),
  y: z.number().nonnegative().default(0),
  width: z.number().nonnegative().default(0),
  height: z.number().nonnegative().default(0)
});

export const guardianPolicySchema = z.object({
  version: z.string().default("local"),
  processWatchlist: z.array(z.string()).default([]),
  vmIndicators: z.array(z.string()).default([]),
  pollIntervalMs: z.number().int().min(250).max(10000).default(2000),
  cooldownMs: z.number().int().min(500).max(60000).default(5000),
  focusSpike: z.object({
    threshold: z.number().int().min(2).max(25).default(6),
    intervalMs: z.number().int().min(2000).max(60000).default(15000)
  }).default({ threshold: 6, intervalMs: 15000 }),
  mitigations: z.object({
    hideWindow: z.boolean().default(true),
    pauseAudio: z.boolean().default(true),
    pauseSession: z.boolean().default(true),
    forceKill: z.array(z.string()).default([])
  }).default({ hideWindow: true, pauseAudio: true, pauseSession: true, forceKill: [] }),
  screenshot: z.object({
    enabled: z.boolean().default(false),
    hotkey: z.string().optional(),
    intervalSeconds: z.number().int().min(30).max(900).optional(),
    redactions: z.array(rectSchema).default([]),
    encryptionKey: z.string().optional(),
    keyId: z.string().default("guardian-local"),
    retentionMinutes: z.number().int().min(5).max(120).default(30)
  }).default({
    enabled: false,
    redactions: [],
    keyId: "guardian-local",
    retentionMinutes: 30
  }),
  events: z.object({
    uploadMaxRetries: z.number().int().min(1).max(10).default(5),
    backoffMs: z.number().int().min(500).max(10000).default(2000)
  }).default({ uploadMaxRetries: 5, backoffMs: 2000 }),
  storage: z.object({
    retentionMinutes: z.number().int().min(5).max(120).default(30)
  }).default({ retentionMinutes: 30 })
});

export type GuardianPolicy = z.infer<typeof guardianPolicySchema>;

export function loadGuardianPolicy(policesPath?: string): GuardianPolicy {
  if (!policesPath) {
    return guardianPolicySchema.parse({});
  }

  const resolved = resolvePolicyPath(policesPath);
  const contents = fs.readFileSync(resolved, "utf-8");
  const parsed = JSON.parse(contents);
  return guardianPolicySchema.parse(parsed);
}

function resolvePolicyPath(customPath: string): string {
  const absolute = path.isAbsolute(customPath)
    ? customPath
    : path.resolve(process.cwd(), customPath);
  if (fs.existsSync(absolute)) {
    return absolute;
  }
  const sibling = path.resolve(__dirname, "..", "configs", path.basename(customPath));
  if (fs.existsSync(sibling)) {
    return sibling;
  }
  throw new Error(`Policy file not found: ${customPath}`);
}
