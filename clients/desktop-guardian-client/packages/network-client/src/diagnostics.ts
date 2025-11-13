import { z } from "zod";

export const diagnosticsMetricsSchema = z.object({
  cpuPercent: z.number(),
  memoryTotal: z.number().nonnegative(),
  memoryUsed: z.number().nonnegative(),
  jitterMs: z.number().nonnegative(),
  droppedChunks: z.number().nonnegative(),
  guardianEvents: z.number().nonnegative(),
  audioRunning: z.boolean(),
  sampleRate: z.number().positive(),
  chunkMillis: z.number().positive()
});

export const diagnosticsPayloadSchema = z.object({
  trigger: z.string().default("manual"),
  collectedAt: z.string(),
  deviceId: z.string().optional(),
  sessionId: z.string().optional(),
  version: z.string(),
  platform: z.string(),
  metrics: diagnosticsMetricsSchema
});

export type DiagnosticsUploadPayload = z.infer<typeof diagnosticsPayloadSchema>;

export interface DiagnosticsClientOptions {
  baseUrl: string;
  token: string;
  payload: DiagnosticsUploadPayload;
  fetchImpl?: typeof fetch;
}

export async function uploadDiagnostics(options: DiagnosticsClientOptions): Promise<void> {
  const { baseUrl, token, payload, fetchImpl = fetch } = options;
  const body = diagnosticsPayloadSchema.parse(payload);
  const response = await fetchImpl(new URL("/api/diagnostics", baseUrl).toString(), {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json"
    },
    body: JSON.stringify(body)
  });

  if (!response.ok) {
    throw new Error(`diagnostics upload failed: ${response.status}`);
  }
}
